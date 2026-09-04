package tmpl

import (
	"errors"
	"slices"
	"testing"
)

// person stands in for a subject: one key field, one field a secondary lookup
// would be built over.
type person struct {
	id   int
	name string
}

// wired is a container with its maps kept up the way a generated placeChecked
// keeps them: the walk order, a unique primary and one secondary of keys.
//
// The template's own place writes into nothing but the order — its real body
// is built per declaration, because only a declaration knows the keys — so the
// tests wire the maps here, exactly as the built statements do.
type wired struct {
	held   *Index[person]
	byID   map[int]*entryOf[person]
	byName map[string][]int
}

// add files one element the way a generated placeChecked does, refusing a key
// that is already held.
func (w *wired) add(v person) error {
	if _, held := w.byID[v.id]; held {
		return errDup
	}

	e := &entryOf[person]{elem: v, at: len(w.held.order)}
	w.held.order = append(w.held.order, e)
	w.byID = w.held.noted(w.byID, v.id, e)
	w.byName = w.held.listed(w.byName, v.name, v.id)

	return nil
}

// remove takes one element out the way a generated Remove does.
func (w *wired) remove(k int) bool {
	e, held := w.byID[k]
	if !held {
		return false
	}

	delete(w.byID, k)
	w.byName = w.held.delisted(w.byName, e.elem.name, k)
	w.held.cut(e.at)

	return true
}

func wire(elems ...person) *wired {
	w := &wired{held: &Index[person]{}}
	for _, v := range elems {
		if err := w.add(v); err != nil {
			panic(err)
		}
	}
	return w
}

// The zero value takes elements without being constructed: every map is made
// on first use, and the order grows from nothing.
func TestTheZeroValueIsReadyToUse(t *testing.T) {
	var w wired
	w.held = &Index[person]{}

	if err := w.add(person{id: 1, name: "ada"}); err != nil {
		t.Fatalf("the first add was refused: %v", err)
	}
	if w.held.Len() != 1 {
		t.Errorf("one element was added and Len says %d", w.held.Len())
	}
}

// A walk yields the elements in the order they were added.
func TestTheWalkIsTheOrderOfAddition(t *testing.T) {
	w := wire(person{1, "ada"}, person{2, "bob"}, person{3, "cid"})

	var ids []int
	for v := range w.held.All() {
		ids = append(ids, v.id)
	}

	if want := []int{1, 2, 3}; !slices.Equal(ids, want) {
		t.Errorf("the walk yielded %v rather than %v", ids, want)
	}
}

// Which slots a walk covers is fixed when it starts: adding during one does
// not extend it.
func TestAddingDuringAWalkDoesNotExtendIt(t *testing.T) {
	w := wire(person{1, "ada"}, person{2, "bob"})

	var seen int
	for range w.held.All() {
		seen++
		if err := w.add(person{id: 10 + seen, name: "late"}); err != nil {
			t.Fatalf("adding during the walk was refused: %v", err)
		}
	}

	if seen != 2 {
		t.Errorf("a walk over two elements yielded %d", seen)
	}
}

// A lookup answers with a pointer to the held element, and the pointer stays
// good while other elements come and go.
func TestALookupSurvivesItsNeighboursLeaving(t *testing.T) {
	w := wire(person{1, "ada"}, person{2, "bob"}, person{3, "cid"})

	held, ok := w.held.pick(w.byID, 2)
	if !ok || held.name != "bob" {
		t.Fatalf("the lookup answered %v, %t", held, ok)
	}

	w.remove(1)
	w.remove(3)

	if held.name != "bob" {
		t.Errorf("removing neighbours changed what the pointer names: %s", held.name)
	}
}

// A key nothing holds answers nothing, rather than a pointer to a zero value.
func TestAMissingKeyAnswersNothing(t *testing.T) {
	w := wire(person{1, "ada"})

	if held, ok := w.held.pick(w.byID, 9); ok || held != nil {
		t.Errorf("a missing key answered %v, %t", held, ok)
	}
}

// Removal swaps the last element into the hole: the walk stays whole, every
// remaining element is still reachable, and nothing was searched for.
func TestRemovalKeepsTheRestReachable(t *testing.T) {
	w := wire(person{1, "ada"}, person{2, "bob"}, person{3, "cid"}, person{4, "dee"})

	if !w.remove(2) {
		t.Fatal("removing a held key reported nothing held")
	}
	if w.remove(2) {
		t.Error("removing it again reported something held")
	}

	var ids []int
	for v := range w.held.All() {
		ids = append(ids, v.id)
	}
	slices.Sort(ids)

	if want := []int{1, 3, 4}; !slices.Equal(ids, want) {
		t.Errorf("after removal the walk yields %v rather than %v", ids, want)
	}

	// The element the swap moved is still where its slot says it is, which is
	// what a second removal relies on.
	for _, id := range []int{4, 3, 1} {
		if !w.remove(id) {
			t.Errorf("removing %d after the swap reported nothing held", id)
		}
	}
	if w.held.Len() != 0 {
		t.Errorf("everything was removed and Len says %d", w.held.Len())
	}
}

// Removing the newest element is the swap's own edge: the element moved into
// the hole is the one being removed.
func TestRemovingTheNewestElement(t *testing.T) {
	w := wire(person{1, "ada"}, person{2, "bob"})

	if !w.remove(2) {
		t.Fatal("removing the newest element reported nothing held")
	}
	if got := w.held.Len(); got != 1 {
		t.Errorf("one of two elements was removed and Len says %d", got)
	}
}

// A secondary bucket resolves through the primary map, so the elements a value
// reaches are exactly the held ones that carry it.
func TestASecondaryLookupWalksItsValue(t *testing.T) {
	w := wire(person{1, "ada"}, person{2, "bob"}, person{3, "ada"})

	var ids []int
	for v := range w.held.found(w.byName["ada"], w.byID) {
		ids = append(ids, v.id)
	}

	if want := []int{1, 3}; !slices.Equal(ids, want) {
		t.Errorf("the value reaches %v rather than %v", ids, want)
	}

	w.remove(1)

	ids = ids[:0]
	for v := range w.held.found(w.byName["ada"], w.byID) {
		ids = append(ids, v.id)
	}
	if want := []int{3}; !slices.Equal(ids, want) {
		t.Errorf("after removal the value reaches %v rather than %v", ids, want)
	}
}

// Taking the last key out of a secondary bucket takes the bucket out of the
// map, so values nothing carries any more do not accumulate.
func TestAnEmptiedBucketLeavesTheMap(t *testing.T) {
	w := wire(person{1, "ada"}, person{2, "ada"})

	w.remove(1)
	if _, held := w.byName["ada"]; !held {
		t.Fatal("a bucket with a key left was dropped")
	}

	w.remove(2)
	if _, held := w.byName["ada"]; held {
		t.Error("an emptied bucket is still in the map")
	}
}

// A multi-valued primary files every element sharing a key in one bucket, and
// spread walks the bucket oldest first.
func TestAMultiValuedPrimaryHoldsThemAll(t *testing.T) {
	held := &Index[person]{}

	var byID map[int][]*entryOf[person]
	for _, v := range []person{{7, "ada"}, {7, "bob"}, {8, "cid"}} {
		e := &entryOf[person]{elem: v, at: len(held.order)}
		held.order = append(held.order, e)
		byID = held.grouped(byID, v.id, e)
	}

	var names []string
	for v := range held.spread(byID[7]) {
		names = append(names, v.name)
	}

	if want := []string{"ada", "bob"}; !slices.Equal(names, want) {
		t.Errorf("the bucket walks %v rather than %v", names, want)
	}
	if got := len(byID[8]); got != 1 {
		t.Errorf("the other key holds %d elements rather than one", got)
	}
}

// A refused add reports the sentinel, so a caller can match it with errors.Is
// and act on the answer.
//
// The refusal itself lives in the built placeChecked rather than in the
// template's placeholder, so it is exercised through the same statements a
// generated file compiles: the wiring above is those statements.
func TestARefusedAddReportsTheSentinel(t *testing.T) {
	w := wire(person{1, "ada"})

	err := w.add(person{1, "late"})
	if err == nil {
		t.Fatal("a duplicate key was added without a word")
	}
	if !errors.Is(err, errDup) {
		t.Errorf("the refusal is %v rather than the sentinel", err)
	}

	if got := w.held.Len(); got != 1 {
		t.Errorf("a refused add changed what is held: Len says %d", got)
	}
	if held, ok := w.held.pick(w.byID, 1); !ok || held.name != "ada" {
		t.Errorf("a refused add displaced the element that was there: %v, %t", held, ok)
	}
}

// The appends forward every element a sequence yields to the placing method,
// which is where a declaration's own upkeep happens.
func TestTheAppendsForwardEveryElement(t *testing.T) {
	plain := &Index[person]{}
	plain.AppendSeq(slices.Values([]person{{1, "ada"}, {2, "bob"}}))
	if got := plain.Len(); got != 2 {
		t.Errorf("two elements were appended and %d are held", got)
	}

	checked := &Index[person]{}
	if err := checked.AppendSeqChecked(slices.Values([]person{{1, "ada"}})); err != nil {
		t.Fatalf("an append the placeholder cannot refuse was refused: %v", err)
	}
	if got := checked.Len(); got != 1 {
		t.Errorf("one element was appended and %d are held", got)
	}
}

// Reset empties the order and lets go of what it held, so the elements can be
// collected while the memory stays.
func TestResetLetsTheElementsGo(t *testing.T) {
	held := New(person{1, "ada"}, person{2, "bob"})
	before := cap(held.order)

	held.Reset()

	if held.Len() != 0 {
		t.Errorf("a reset container holds %d", held.Len())
	}
	if cap(held.order) != before {
		t.Errorf("reset gave the memory back: cap %d became %d", before, cap(held.order))
	}
}

// Both constructors hold what they were given, in order.
//
// The checked one's panic on a shared key lives in the built placeChecked and
// cannot fire through the template's placeholder; the layer's own tests read
// it in the output, and the worked example runs it.
func TestTheConstructors(t *testing.T) {
	if got := New(person{1, "ada"}, person{2, "bob"}).Len(); got != 2 {
		t.Errorf("two elements were given and %d are held", got)
	}

	if got := NewChecked(person{1, "ada"}).Len(); got != 1 {
		t.Errorf("one element was given and %d are held", got)
	}
}
