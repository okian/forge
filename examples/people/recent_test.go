package people_test

import (
	"slices"
	"testing"

	"github.com/okian/forge/examples/people"
)

// recentHeld is the three people the projections and the walks are read off.
//
// Three rather than one, because the questions worth asking of a projection are
// about order and about arity, and neither is visible in a container holding a
// single element.
func recentHeld() []people.Person {
	return []people.Person{
		{ID: 1, Name: "Ada", Email: "ada@example.com", Age: 36, Aliases: []string{"AAL"}},
		{ID: 2, Name: "Grace", Email: "grace@example.com", Age: 45, Aliases: nil},
		{ID: 3, Name: "Alan", Email: "alan@example.com", Age: 41, Aliases: []string{"AMT", "Prof"}},
	}
}

// recentFilled returns a container holding them, in the order they were pushed.
func recentFilled(t *testing.T) *people.Recent {
	t.Helper()

	r := people.NewRecent()
	for _, one := range recentHeld() {
		r.Push(one)
	}
	return r
}

// Every field the subject declares is projected, in the order the elements are
// held in.
//
// One test over all of them rather than one each, because a projection is
// generated from the same template per field: what could differ between them is
// the field it reads and the slice type it builds, and both are visible here.
// The order is the assertion that matters — a projection that returned the
// right values in the wrong order would satisfy a length check and be useless.
func TestEveryFieldIsProjectedInOrder(t *testing.T) {
	r := recentFilled(t)

	if got, want := r.IDs(), []int{1, 2, 3}; !slices.Equal(got, want) {
		t.Errorf("IDs() = %v, want %v", got, want)
	}
	if got, want := r.Names(), []string{"Ada", "Grace", "Alan"}; !slices.Equal(got, want) {
		t.Errorf("Names() = %v, want %v", got, want)
	}
	if got, want := r.Emails(), []string{"ada@example.com", "grace@example.com", "alan@example.com"}; !slices.Equal(got, want) {
		t.Errorf("Emails() = %v, want %v", got, want)
	}
	if got, want := r.Ages(), []int{36, 45, 41}; !slices.Equal(got, want) {
		t.Errorf("Ages() = %v, want %v", got, want)
	}

	aliases := r.Aliaseses()
	if len(aliases) != 3 {
		t.Fatalf("Aliaseses() returned %d rows, want 3", len(aliases))
	}
	if !slices.Equal(aliases[2], []string{"AMT", "Prof"}) {
		t.Errorf("Aliaseses()[2] = %v, want the two aliases the element holds", aliases[2])
	}
	if len(aliases[1]) != 0 {
		t.Errorf("Aliaseses()[1] = %v, want nothing for an element with no aliases", aliases[1])
	}
}

// The container walks backward from the newest element to the oldest, and stops
// when the caller stops.
//
// Backward is not All reversed by the caller: it reads the ring from the other
// end, so the index arithmetic is its own and can be wrong on its own. Stopping
// early is tested with it because a walk that ignored a false from yield would
// pass every test that only ever consumed the whole sequence.
func TestTheContainerWalksBackwardAndStopsWhenAsked(t *testing.T) {
	r := recentFilled(t)

	var names []string
	for one := range r.Backward() {
		names = append(names, one.Name)
	}
	if want := []string{"Alan", "Grace", "Ada"}; !slices.Equal(names, want) {
		t.Errorf("Backward() = %v, want %v", names, want)
	}

	var first []string
	for one := range r.Backward() {
		first = append(first, one.Name)
		break
	}
	if want := []string{"Alan"}; !slices.Equal(first, want) {
		t.Errorf("stopping after one element yielded %v, want %v", first, want)
	}
}

// The lazy view is over the same elements, in the same order.
//
// Seq is the one projection that hands back a view rather than a slice, and the
// point of it is that nothing is built until something reads it. What is
// checked here is that it is a view over this container: a combinator chain is
// the shared view's own business and is tested where that view is.
func TestTheLazyViewIsOverTheSameElements(t *testing.T) {
	r := recentFilled(t)

	var names []string
	for one := range r.Seq().Seq {
		names = append(names, one.Name)
	}

	if want := []string{"Ada", "Grace", "Alan"}; !slices.Equal(names, want) {
		t.Errorf("Seq() yielded %v, want %v", names, want)
	}
}
