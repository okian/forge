package tmpl

import (
	"maps"
	"slices"
	"testing"
)

// The helpers are unexported, because they are how the generated methods do
// their work and not something an author is offered — so these tests are inside
// the package rather than beside it.

// person is what the generated methods of a real declaration hand these bodies
// one field of.
type person struct {
	Name string
	Age  int
}

// people is the collection every test below asks a question of.
func people() Collection[person] {
	return Collection[person]{
		{Name: "Ida", Age: 61},
		{Name: "Ada", Age: 36},
		{Name: "Grace", Age: 45},
		{Name: "Ada", Age: 27},
	}
}

// A projection is the field of every element, in the order the elements come
// in — including the repeats, since it is a projection and not a set.
func TestProjectingOneField(t *testing.T) {
	names := people().project(func(p person) string { return p.Name })

	if want := []string{"Ida", "Ada", "Grace", "Ada"}; !slices.Equal(names, want) {
		t.Errorf("projected %v, want %v", names, want)
	}

	// A projection to something the subject does not hold is the same
	// operation: what is projected is whatever the function makes.
	lengths := people().project(func(p person) int { return len(p.Name) })
	if want := []int{3, 3, 5, 3}; !slices.Equal(lengths, want) {
		t.Errorf("projected %v, want %v", lengths, want)
	}

	// Nothing to project is nothing, rather than a slice of length zero that a
	// caller has to tell apart from one.
	if got := (Collection[person]{}).project(func(p person) int { return p.Age }); got != nil {
		t.Errorf("projecting nothing gave %v", got)
	}
}

// Two elements sharing a key leave the later one, which is what writing into a
// map does and is the only answer that needs no policy.
func TestKeyingByOneField(t *testing.T) {
	byName := people().keyed(func(p person) string { return p.Name })

	if got, want := len(byName), 3; got != want {
		t.Fatalf("keyed %d elements, want %d", got, want)
	}
	if got := byName["Ada"].Age; got != 27 {
		t.Errorf("the Ada that survived is %d, want the later one at 27", got)
	}

	held := slices.Sorted(maps.Keys(byName))
	if want := []string{"Ada", "Grace", "Ida"}; !slices.Equal(held, want) {
		t.Errorf("keyed by %v, want %v", held, want)
	}
}

// Sorting leaves the collection as it was and equal elements in the order they
// came in, so asking a question does not change the answer to the next one.
func TestSortingByOneField(t *testing.T) {
	held := people()

	byAge := held.ordered(func(p person) int { return p.Age })
	if want := []int{27, 36, 45, 61}; !slices.Equal(ages(byAge), want) {
		t.Errorf("sorted to %v, want %v", ages(byAge), want)
	}

	if want := []int{61, 36, 45, 27}; !slices.Equal(ages(held), want) {
		t.Errorf("the collection was reordered to %v", ages(held))
	}

	// Stable: the two Adas keep the order they were written in.
	byName := held.ordered(func(p person) string { return p.Name })
	if got, want := ages(byName)[:2], []int{36, 27}; !slices.Equal(got, want) {
		t.Errorf("equal keys came out as %v, want %v", got, want)
	}
}

// ages is what the tests compare, since a person is not worth printing whole.
func ages(held []person) []int {
	out := make([]int, len(held))
	for i, p := range held {
		out[i] = p.Age
	}
	return out
}

// Every body walks through All rather than over the collection directly, so
// what the collection is underneath stays the storage layer's business. A
// storage that is not a slice is what this has to keep working over.
func TestEverythingWalksThroughAll(t *testing.T) {
	walked := 0
	held := Collection[person]{{Name: "Ada", Age: 36}}

	// The template's All is a stand-in for the storage layer's, so what is
	// checked here is that each body reaches it exactly once per question.
	for range held.All() {
		walked++
	}
	if walked != 1 {
		t.Fatalf("the stand-in walked %d elements", walked)
	}

	if got := len(held.project(func(p person) int { return p.Age })); got != 1 {
		t.Errorf("projecting walked %d elements", got)
	}
	if got := len(held.keyed(func(p person) string { return p.Name })); got != 1 {
		t.Errorf("keying walked %d elements", got)
	}
	if got := len(held.ordered(func(p person) int { return p.Age })); got != 1 {
		t.Errorf("sorting walked %d elements", got)
	}
}
