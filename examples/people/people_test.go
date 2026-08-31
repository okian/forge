package people_test

import (
	"maps"
	"slices"
	"strconv"
	"testing"

	"github.com/okian/forge/examples/people"
)

// directory is the fixture every test below reads, deliberately out of order in
// both sort keys so that a sorted view that returned its input would fail.
func directory() people.Persons {
	return people.NewPersons(
		people.Person{ID: 3, Name: "Ada", Email: "ada@example.com", Age: 36},
		people.Person{ID: 1, Name: "Grace", Email: "grace@example.com", Age: 45},
		people.Person{ID: 2, Name: "Alan", Email: "alan@example.com", Age: 41},
	)
}

// The container is built from elements and holds them in the order it was
// given, which is what every view below is relative to.
func TestWhatTheContainerHolds(t *testing.T) {
	held := directory()

	if got, want := held.Len(), 3; got != want {
		t.Errorf("Len() = %d, want %d", got, want)
	}

	if got, want := held.Names(), []string{"Ada", "Grace", "Alan"}; !slices.Equal(got, want) {
		t.Errorf("Names() = %v, want %v", got, want)
	}
}

// The elements are copied in, so the caller's slice is not the container's.
//
// The constructor takes a variadic, which means a caller who already has a
// slice passes it with ... and would otherwise be handing over its backing
// array — and would then find the container changing underneath them.
func TestTheContainerDoesNotFollowTheSliceItWasBuiltFrom(t *testing.T) {
	elems := []people.Person{{ID: 1, Name: "Ada"}}
	held := people.NewPersons(elems...)

	elems[0].Name = "somebody else"

	if got, want := held.Names(), []string{"Ada"}; !slices.Equal(got, want) {
		t.Errorf("Names() = %v, want %v", got, want)
	}
}

// One projection method per field, each spelled with the field's own name and
// returning that field's own type.
//
// The names are the point. A helper package would offer one Project taking a
// func(Person) string, and every call site would spell the field itself.
func TestEveryFieldIsProjected(t *testing.T) {
	held := directory()

	if got, want := held.IDs(), []int{3, 1, 2}; !slices.Equal(got, want) {
		t.Errorf("IDs() = %v, want %v", got, want)
	}
	if got, want := held.Ages(), []int{36, 45, 41}; !slices.Equal(got, want) {
		t.Errorf("Ages() = %v, want %v", got, want)
	}
	if got, want := held.Emails(), []string{"ada@example.com", "grace@example.com", "alan@example.com"}; !slices.Equal(got, want) {
		t.Errorf("Emails() = %v, want %v", got, want)
	}
}

// A sorted view per declared sort key, over the key's own type: Name orders as
// a string and Age as a number.
func TestTheSortedViewsAreOrderedByTheirOwnKey(t *testing.T) {
	held := directory()

	byName := people.Persons(held.SortedByName())
	if got, want := byName.Names(), []string{"Ada", "Alan", "Grace"}; !slices.Equal(got, want) {
		t.Errorf("SortedByName() gave %v, want %v", got, want)
	}

	byAge := people.Persons(held.SortedByAge())
	if got, want := byAge.Ages(), []int{36, 41, 45}; !slices.Equal(got, want) {
		t.Errorf("SortedByAge() gave %v, want %v", got, want)
	}
}

// Elements that tie on the sort key come back in the order they went in.
//
// The generated method says so — "leaving equal ones as they were" — and a
// stable sort is what makes a sorted view composable: sorting by one key and
// then by another leaves the first key breaking ties in the second, which an
// unstable sort would scramble. Nothing else here would notice the difference,
// because a fixture whose keys are all distinct sorts the same either way.
func TestElementsThatTieKeepTheOrderTheyCameIn(t *testing.T) {
	// Enough of them that the sort does not fall back to the insertion sort it
	// uses for a handful, which is stable whether it was asked to be or not and
	// would let this pass over an unstable sort.
	var held people.Persons
	for i := range 64 {
		held = append(held, people.Person{ID: i, Name: strconv.Itoa(i), Age: i % 2})
	}

	sorted := people.Persons(held.SortedByAge())

	var want []string
	for _, key := range []int{0, 1} {
		for i := range 64 {
			if i%2 == key {
				want = append(want, strconv.Itoa(i))
			}
		}
	}

	if got := sorted.Names(); !slices.Equal(got, want) {
		t.Errorf("SortedByAge() gave %v, want %v", got, want)
	}
}

// Sorting answers a question rather than changing the container, so asking
// twice gives the same answer and asking once does not change what anything
// else sees.
func TestSortingDoesNotDisturbTheContainer(t *testing.T) {
	held := directory()
	before := held.Names()

	_ = held.SortedByName()

	if got := held.Names(); !slices.Equal(got, before) {
		t.Errorf("the container reads %v after being sorted, and read %v before", got, before)
	}
}

// A lookup map per declared index key, keyed by the field's own type.
func TestTheIndexIsKeyedByItsField(t *testing.T) {
	held := directory()
	byID := held.ByID()

	if got, want := len(byID), 3; got != want {
		t.Fatalf("ByID() holds %d entries, want %d", got, want)
	}
	if got, want := byID[1].Name, "Grace"; got != want {
		t.Errorf("ByID()[1].Name = %q, want %q", got, want)
	}

	keys := slices.Sorted(maps.Keys(byID))
	if want := []int{1, 2, 3}; !slices.Equal(keys, want) {
		t.Errorf("ByID() is keyed by %v, want %v", keys, want)
	}
}

// Two elements sharing an index key leave the later one in the map, which is
// what writing into a map does and what the method documents.
func TestTheLastElementWinsAKeyTwoShare(t *testing.T) {
	held := people.NewPersons(
		people.Person{ID: 1, Name: "first"},
		people.Person{ID: 1, Name: "second"},
	)

	if got, want := held.ByID()[1].Name, "second"; got != want {
		t.Errorf("ByID()[1].Name = %q, want %q", got, want)
	}
}

// The lazy view chains, changes type mid-chain, and ends in a terminal.
//
// Map returning a view of the new type is what lets the chain keep going: the
// combinators after it are these same ones rather than a second set written for
// strings.
func TestTheLazyChainRunsEndToEnd(t *testing.T) {
	held := directory()

	got := held.Seq().
		Filter(func(p people.Person) bool { return p.Age < 42 }).
		Map(func(p people.Person) string { return p.Name }).
		Collect()

	if want := []string{"Ada", "Alan"}; !slices.Equal(got, want) {
		t.Errorf("the chain gave %v, want %v", got, want)
	}
}

// Take stops the walk beneath it rather than reading on and discarding, so
// taking two from the chain reads two.
func TestTakeStopsTheWalkBeneathIt(t *testing.T) {
	held := directory()

	read := 0
	got := held.Seq().
		Filter(func(people.Person) bool { read++; return true }).
		Take(2).
		Collect()

	if len(got) != 2 {
		t.Errorf("Take(2) gave %d elements, want 2", len(got))
	}
	if read != 2 {
		t.Errorf("Take(2) read %d elements, want 2", read)
	}
}

// Reduce folds the sequence into a value of its own type.
func TestReduceFoldsIntoItsOwnType(t *testing.T) {
	held := directory()

	total := held.Seq().Reduce(0, func(sum int, p people.Person) int { return sum + p.Age })

	if want := 36 + 45 + 41; total != want {
		t.Errorf("Reduce gave %d, want %d", total, want)
	}
}

// The container gathers a sequence rather than replacing itself with one, so
// several sequences can be collected into one container.
func TestAppendingGathersRatherThanReplaces(t *testing.T) {
	held := directory()
	more := people.NewPersons(people.Person{ID: 4, Name: "Edsger", Age: 72})

	held.AppendSeq(more.All())

	if got, want := held.Names(), []string{"Ada", "Grace", "Alan", "Edsger"}; !slices.Equal(got, want) {
		t.Errorf("Names() after appending = %v, want %v", got, want)
	}
}

// Walking backward reads the same elements in the other order.
func TestWalkingBackward(t *testing.T) {
	held := directory()

	var names []string
	for p := range held.Backward() {
		names = append(names, p.Name)
	}

	if want := []string{"Alan", "Grace", "Ada"}; !slices.Equal(names, want) {
		t.Errorf("Backward() gave %v, want %v", names, want)
	}
}

// An empty container answers every view without special-casing at the call
// site: empty projections, empty sorted views, an empty map, and a chain that
// yields nothing.
func TestAnEmptyContainerAnswersEverything(t *testing.T) {
	var held people.Persons

	if got := held.Len(); got != 0 {
		t.Errorf("Len() = %d, want 0", got)
	}
	if got := held.Names(); len(got) != 0 {
		t.Errorf("Names() = %v, want nothing", got)
	}
	if got := held.SortedByAge(); len(got) != 0 {
		t.Errorf("SortedByAge() = %v, want nothing", got)
	}
	if got := held.ByID(); len(got) != 0 {
		t.Errorf("ByID() = %v, want nothing", got)
	}
	if got := held.Seq().Take(3).Collect(); len(got) != 0 {
		t.Errorf("the chain gave %v, want nothing", got)
	}
}

// The remaining combinators of the shared view, which the container reaches
// through Seq and which nothing above happens to walk through.
func TestTheRestOfTheSharedView(t *testing.T) {
	held := directory()

	if got, ok := held.Seq().First(); !ok || got.Name != "Ada" {
		t.Errorf("First() = %v, %v, want Ada, true", got.Name, ok)
	}

	skipped := held.Seq().Skip(2).Collect()
	if len(skipped) != 1 || skipped[0].Name != "Alan" {
		t.Errorf("Skip(2) gave %v, want Alan alone", people.NewPersons(skipped...).Names())
	}

	sameAge := people.NewPersons(
		people.Person{Name: "a", Age: 1},
		people.Person{Name: "b", Age: 1},
		people.Person{Name: "c", Age: 2},
	)
	deduped := people.NewPersons(sameAge.Seq().
		Dedup(func(a, b people.Person) bool { return a.Age == b.Age }).
		Collect()...)
	if got, want := deduped.Names(), []string{"a", "c"}; !slices.Equal(got, want) {
		t.Errorf("Dedup gave %v, want %v", got, want)
	}

	var chunks [][]string
	for chunk := range held.Seq().Map(func(p people.Person) string { return p.Name }).Chunk(2) {
		chunks = append(chunks, slices.Clone(chunk))
	}
	if len(chunks) != 2 || !slices.Equal(chunks[0], []string{"Ada", "Grace"}) || !slices.Equal(chunks[1], []string{"Alan"}) {
		t.Errorf("Chunk(2) gave %v, want [[Ada Grace] [Alan]]", chunks)
	}

	into := held.Seq().Into(make([]people.Person, 0, 3))
	if got, want := people.NewPersons(into...).Names(), []string{"Ada", "Grace", "Alan"}; !slices.Equal(got, want) {
		t.Errorf("Into gave %v, want %v", got, want)
	}

	var none people.Persons
	if _, ok := none.Seq().First(); ok {
		t.Error("First() on nothing reported an element")
	}
}

// A consumer that stops reading stops the whole chain, at every link.
//
// A lazy view is only lazy if breaking out of the range reaches all the way
// down: the yield returns false, and each combinator has to hand that answer to
// the one beneath it rather than reading on. The count is what says so — a link
// that ignored the answer would still produce the right first element and would
// walk the whole sequence to do it.
func TestBreakingOutOfTheChainStopsEveryLinkBeneathIt(t *testing.T) {
	long := make([]people.Person, 0, 100)
	for i := range 100 {
		long = append(long, people.Person{ID: i, Name: "x", Age: i})
	}
	held := people.NewPersons(long...)

	// One read is the answer everywhere but Skip, which discards an element
	// before it can offer one, and Chunk, which fills a batch before it can.
	links := []struct {
		name  string
		most  int
		chain func(read *int) func(func(people.Person) bool)
	}{
		{"Filter", 1, func(read *int) func(func(people.Person) bool) {
			return held.Seq().Filter(counting(read)).All()
		}},
		{"Take", 1, func(read *int) func(func(people.Person) bool) {
			return held.Seq().Filter(counting(read)).Take(50).All()
		}},
		{"Skip", 2, func(read *int) func(func(people.Person) bool) {
			return held.Seq().Filter(counting(read)).Skip(1).All()
		}},
		{"Dedup", 1, func(read *int) func(func(people.Person) bool) {
			return held.Seq().Filter(counting(read)).
				Dedup(func(a, b people.Person) bool { return a.Age == b.Age }).All()
		}},
		{"Map", 1, func(read *int) func(func(people.Person) bool) {
			return held.Seq().Filter(counting(read)).
				Map(func(p people.Person) people.Person { return p }).All()
		}},
		{"Backward", 1, func(read *int) func(func(people.Person) bool) {
			return func(yield func(people.Person) bool) {
				for p := range held.Backward() {
					*read++
					if !yield(p) {
						return
					}
				}
			}
		}},
	}

	for _, link := range links {
		t.Run(link.name, func(t *testing.T) {
			read := 0
			for range link.chain(&read) {
				break
			}

			if read > link.most {
				t.Errorf("breaking after the first element read %d of 100, want at most %d", read, link.most)
			}
		})
	}

	t.Run("Chunk", func(t *testing.T) {
		read := 0
		for range held.Seq().Filter(counting(&read)).Chunk(10) {
			break
		}
		if read > 10 {
			t.Errorf("breaking after the first chunk of ten read %d of 100", read)
		}
	})
}

// counting keeps every element and records that it was read.
func counting(read *int) func(people.Person) bool {
	return func(people.Person) bool {
		*read++
		return true
	}
}

// Take of nothing and Chunk of an impossible size, which are the two edges the
// shared view answers rather than walks.
func TestTheEdgesOfTheLazyView(t *testing.T) {
	held := directory()

	if got := held.Seq().Take(0).Collect(); len(got) != 0 {
		t.Errorf("Take(0) gave %d elements, want none", len(got))
	}

	defer func() {
		if recovered := recover(); recovered == nil {
			t.Error("Chunk(0) returned rather than panicking")
		}
	}()
	held.Seq().Chunk(0)
}

// A view that was never given a sequence yields nothing rather than panicking.
//
// The view is a function underneath, so the zero value is a nil function and
// ranging over it directly would panic. Every method goes through All, which is
// where that is made safe — a struct holding a zero view therefore behaves like
// one holding an empty sequence, which is what a caller who never assigned it
// would expect.
func TestTheZeroViewYieldsNothing(t *testing.T) {
	var zero people.PersonsSeq

	if got := zero.Collect(); len(got) != 0 {
		t.Errorf("the zero view collected %v, want nothing", got)
	}
	if got := zero.Filter(func(people.Person) bool { return true }).Collect(); len(got) != 0 {
		t.Errorf("filtering the zero view gave %v, want nothing", got)
	}
	for range zero.All() {
		t.Error("the zero view yielded an element")
	}
}
