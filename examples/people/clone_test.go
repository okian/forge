package people_test

import (
	"slices"
	"testing"

	"github.com/okian/forge/examples/people"
)

// A copy shares nothing with what it was copied from.
//
// This is the whole claim, and it is the one an assignment quietly fails: `b :=
// a` copies the slice header and not the array behind it, so writing through
// b.Aliases writes through a.Aliases too. The test is what says the generated
// copy does not.
func TestACopySharesNothing(t *testing.T) {
	held := people.Person{
		ID: 1, Name: "Ada", Email: "ada@example.com", Age: 36,
		Aliases: []string{"A. Lovelace", "Ada Byron"},
	}

	copied := held.Clone()
	copied.Aliases[0] = "somebody else"

	if held.Aliases[0] != "A. Lovelace" {
		t.Errorf("writing to the copy's aliases changed the original: %v", held.Aliases)
	}

	// The field an assignment does copy is copied too, which is easy and still
	// worth saying: a copy that shared nothing and held nothing would pass the
	// line above.
	if copied.Name != "Ada" {
		t.Errorf("the copy is called %s", copied.Name)
	}
}

// And an assignment does share, which is what makes the copy worth generating.
//
// Stated here rather than left implicit: without it, a copy that happened to be
// an assignment would pass the test above for a subject whose fields nobody
// had written to yet.
func TestAnAssignmentSharesWhatACopyDoesNot(t *testing.T) {
	held := people.Person{Aliases: []string{"A. Lovelace"}}

	assigned := held
	assigned.Aliases[0] = "somebody else"

	if held.Aliases[0] == "A. Lovelace" {
		t.Error("an assignment did not share the slice, so this example proves nothing")
	}
}

// A copy holds the same values as what it was copied from, which is the other
// half of the claim: sharing nothing is easy if the copy is empty.
func TestACopyHoldsTheSameValues(t *testing.T) {
	held := people.Person{
		ID: 1, Name: "Ada", Email: "ada@example.com", Age: 36,
		Aliases: []string{"A. Lovelace", "Ada Byron"},
	}

	copied := held.Clone()

	if !same(held, copied) {
		t.Errorf("the copy is %+v, want %+v", copied, held)
	}
}

// A nil slice stays nil and an empty one stays empty, because the two are
// different values and a copy that turned one into the other would have changed
// something.
//
// It is the one place a copy and a round trip through JSON differ, and they
// differ because JSON cannot carry the distinction and a copy can.
func TestACopyKeepsNilAndEmptyApart(t *testing.T) {
	absent := people.Person{Name: "Ada"}.Clone()
	if absent.Aliases != nil {
		t.Errorf("a nil slice became %v", absent.Aliases)
	}

	empty := people.Person{Name: "Ada", Aliases: []string{}}.Clone()
	if empty.Aliases == nil {
		t.Error("an empty slice became nil")
	}
	if len(empty.Aliases) != 0 {
		t.Errorf("an empty slice became %v", empty.Aliases)
	}
}

// Copying nothing allocates nothing, so a subject that happens to hold no
// references costs what an assignment costs.
func TestCopyingWithoutReferencesAllocatesNothing(t *testing.T) {
	held := people.Person{ID: 1, Name: "Ada", Email: "ada@example.com", Age: 36}

	got := testing.AllocsPerRun(100, func() {
		if held.Clone().Name != "Ada" {
			sink(1)
		}
	})
	if got != 0 {
		t.Errorf("copying a person with no references allocated %.0f times, want none", got)
	}
}

// Every person a container holds can be copied, because the copy is written on
// the element rather than on the container.
func TestEveryPersonInAContainerCanBeCopied(t *testing.T) {
	held := people.NewRecent()
	held.Push(people.Person{ID: 1, Name: "Ada", Aliases: []string{"A. Lovelace"}})
	held.Push(people.Person{ID: 2, Name: "Grace"})

	copies := make([]people.Person, 0, held.Len())
	for one := range held.All() {
		copies = append(copies, one.Clone())
	}

	copies[0].Aliases[0] = "somebody else"

	original := slices.Collect(held.All())
	if original[0].Aliases[0] != "A. Lovelace" {
		t.Errorf("writing to a copy changed what the container holds: %v", original[0].Aliases)
	}
}

// The copy over a person holding a slice, which is what a copy costs when there
// is something to copy: one allocation for the slice and nothing else.
func BenchmarkClone(b *testing.B) {
	held := people.Person{
		ID: 1, Name: "Ada", Email: "ada@example.com", Age: 36,
		Aliases: []string{"A. Lovelace", "Ada Byron"},
	}

	b.ReportAllocs()

	for b.Loop() {
		if held.Clone().ID != 1 {
			b.Fatal("the copy is not the original")
		}
	}
}

// The same copy written by hand, which is what the generated one has to cost no
// more than.
func BenchmarkCloneByHand(b *testing.B) {
	held := people.Person{
		ID: 1, Name: "Ada", Email: "ada@example.com", Age: 36,
		Aliases: []string{"A. Lovelace", "Ada Byron"},
	}

	b.ReportAllocs()

	for b.Loop() {
		if cloneByHand(held).ID != 1 {
			b.Fatal("the copy is not the original")
		}
	}
}

// cloneByHand is the copy somebody would write, which is the same one.
func cloneByHand(p people.Person) people.Person {
	out := p
	out.Aliases = slices.Clone(p.Aliases)
	return out
}
