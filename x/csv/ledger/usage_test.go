package ledger_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/okian/forge/x/csv/ledger"
)

// What the rest of the stack buys, read as usage.
//
// A transport is one layer of three here, and the reason the example composes
// rather than declaring a bare table is that composing is the point: the
// document is what goes over the wire, and everything above it in this file is
// what the same type does while it is still in the process.
//
// None of it is this layer's code. It is here because an example whose tests
// only exercise the layer that wrote them is an example of one layer, and
// because a stack that stopped composing would fail here rather than in
// somebody's repository.

// The query surface a collection puts on the declared type.
//
// One projection per field, named after the field rather than taking a function
// at the call site, which is the whole of what a collection is for.
func TestTheQuerySurface(t *testing.T) {
	held := ledger.NewEntries(entries()...)

	if got, want := held.Len(), len(entries()); got != want {
		t.Errorf("the ledger holds %d entries, want %d", got, want)
	}

	if got := held.IDs(); !slices.Equal(got, []int{1, 2, 3}) {
		t.Errorf("the ids are %v, want 1, 2 and 3", got)
	}
	if got := held.Amounts(); !slices.Equal(got, []ledger.Cents{-4250, -1899, 120000}) {
		t.Errorf("the amounts are %v", got)
	}
	if got := held.Currencies(); !slices.Equal(got, []ledger.Currency{"CAD", "CAD", "USD"}) {
		t.Errorf("the currencies are %v", got)
	}
	if got := held.Revisions(); !slices.Equal(got, []uint8{0, 2, 255}) {
		t.Errorf("the revisions are %v", got)
	}
	if got := held.Settleds(); !slices.Equal(got, []bool{true, false, true}) {
		t.Errorf("the settlements are %v", got)
	}
	if got := held.Rates(); len(got) != 3 || got[0] != 1 {
		t.Errorf("the rates are %v", got)
	}
	if got := held.Payees(); len(got) != 3 || got[0] != "Hydro" {
		t.Errorf("the payees are %v", got)
	}
	if got := held.Posteds(); len(got) != 3 || !got[0].Equal(posted) {
		t.Errorf("the dates are %v", got)
	}

	// A field no document carries is still a field the container can project.
	// Which column a codec writes and which field a query reads are two
	// questions, and the tag answers only the first.
	if got := held.Notes(); len(got) != 3 || got[1] == "" {
		t.Errorf("the notes are %v, and the second entry has one", got)
	}
}

// The sorted view and the lookup the declaration asked for by name.
func TestTheSortedViewAndTheIndex(t *testing.T) {
	held := ledger.NewEntries(entries()...)

	sorted := held.SortedByPayee()
	if len(sorted) != 3 {
		t.Fatalf("the sorted view holds %d entries, want 3", len(sorted))
	}
	if !slices.IsSortedFunc(sorted, func(a, b ledger.Entry) int {
		return strings.Compare(a.Payee, b.Payee)
	}) {
		t.Errorf("the view is not sorted by payee: %v", held.Payees())
	}

	// Sorting a view leaves the container alone, which is what makes it a view.
	if got := held.IDs(); !slices.Equal(got, []int{1, 2, 3}) {
		t.Errorf("the container was reordered by being read: %v", got)
	}

	byID := held.ByID()
	if len(byID) != 3 {
		t.Fatalf("the index holds %d entries, want 3", len(byID))
	}
	if got, ok := byID[2]; !ok || got.Payee != "Café, Bakery" {
		t.Errorf("the index found %+v under 2", got)
	}
}

// The lazy view, which is the combinator surface the package shares.
//
// Every operation returns another view and walks nothing; the walk happens once,
// when a terminal asks for it. So a chain over the ledger holds one entry at a
// time whatever the ledger holds.
func TestTheLazyView(t *testing.T) {
	held := ledger.NewEntries(entries()...)

	unsettled := held.Seq().
		Filter(func(one ledger.Entry) bool { return !one.Settled }).
		Map(func(one ledger.Entry) string { return one.Payee }).
		Collect()

	if !slices.Equal(unsettled, []string{"Café, Bakery"}) {
		t.Errorf("the unsettled payees are %q", unsettled)
	}

	total := held.Seq().Reduce(ledger.Cents(0), func(sum ledger.Cents, one ledger.Entry) ledger.Cents {
		return sum + one.Amount
	})
	if want := ledger.Cents(-4250 - 1899 + 120000); total != want {
		t.Errorf("the total is %d, want %d", total, want)
	}

	if first, ok := held.Seq().Skip(1).Take(1).First(); !ok || first.ID != 2 {
		t.Errorf("the second entry is %+v", first)
	}

	// Into is Collect for a caller who owns the memory.
	into := held.Seq().Into(make([]ledger.Entry, 0, 3))
	if len(into) != 3 {
		t.Errorf("the view collected %d entries into a slice of its own", len(into))
	}

	// Runs of equal values collapse to their first, adjacent ones only.
	currencies := held.Seq().
		Map(func(one ledger.Entry) ledger.Currency { return one.Currency }).
		Dedup(func(a, b ledger.Currency) bool { return a == b }).
		Collect()
	if !slices.Equal(currencies, []ledger.Currency{"CAD", "USD"}) {
		t.Errorf("the currencies dedup to %v", currencies)
	}

	// Chunks are slices of their own rather than one buffer handed out again.
	var batches int
	for batch := range held.Seq().Chunk(2) {
		batches++
		if len(batch) == 0 {
			t.Error("a chunk arrived empty")
		}
	}
	if batches != 2 {
		t.Errorf("three entries came out as %d chunks of two", batches)
	}
}

// The walks, forwards and backwards, over each of the three containers.
//
// Backward is what an ordered container adds over one that is merely walkable,
// and it is worth reading here because the ring's is the interesting one: it
// walks from the newest element to the oldest, which is the order somebody
// looking at a tail wants.
func TestTheWalks(t *testing.T) {
	held := ledger.NewEntries(entries()...)

	var forwards, backwards []int

	for one := range held.All() {
		forwards = append(forwards, one.ID)
	}
	for one := range held.Backward() {
		backwards = append(backwards, one.ID)
	}

	slices.Reverse(backwards)
	if !slices.Equal(forwards, backwards) {
		t.Errorf("the walks disagree: %v forwards and %v backwards", forwards, backwards)
	}

	// The plain storage beneath Bare walks the same way, with none of the
	// surface above it.
	bare := ledger.NewBare(entries()...)
	if got := bare.Len(); got != 3 {
		t.Errorf("the bare ledger holds %d entries, want 3", got)
	}

	var reversed []int
	for one := range bare.Backward() {
		reversed = append(reversed, one.ID)
	}
	if !slices.Equal(reversed, []int{3, 2, 1}) {
		t.Errorf("the bare ledger walks backwards as %v", reversed)
	}

	// And a ring filled one element at a time walks newest first.
	room := ledger.NewRecent()
	for _, one := range entries() {
		room.Push(one)
	}

	var newest []int
	for one := range room.Backward() {
		newest = append(newest, one.ID)
	}
	if !slices.Equal(newest, []int{3, 2, 1}) {
		t.Errorf("the ring walks newest first as %v", newest)
	}

	room.Reset()
	if got := room.Len(); got != 0 {
		t.Errorf("a reset ring holds %d entries", got)
	}
}

// The container satisfies the sorting interface the standard library asks for,
// which is what a declared sort key earns.
func TestTheContainerSorts(t *testing.T) {
	held := ledger.NewEntries(entries()...)

	if held.Less(0, 1) == held.Less(1, 0) {
		t.Error("the comparison says the same thing both ways round")
	}

	held.Swap(0, 2)
	if got := held.IDs(); !slices.Equal(got, []int{3, 2, 1}) {
		t.Errorf("swapping the ends left %v", got)
	}
}

// A container filled from a sequence and emptied again reuses what it took.
func TestFillingAndEmptying(t *testing.T) {
	var held ledger.Entries

	held.AppendSeq(ledger.NewEntries(entries()...).All())
	if got := held.Len(); got != 3 {
		t.Fatalf("the container holds %d entries after being filled, want 3", got)
	}

	held.Reset()
	if got := held.Len(); got != 0 {
		t.Errorf("a reset container holds %d entries", got)
	}

	var bare ledger.Bare

	bare.AppendSeq(ledger.NewBare(entries()...).All())
	if got := bare.Len(); got != 3 {
		t.Errorf("the bare container holds %d entries after being filled, want 3", got)
	}

	bare.Reset()
	if got := bare.Len(); got != 0 {
		t.Errorf("a reset bare container holds %d entries", got)
	}
}
