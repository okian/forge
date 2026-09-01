package people_test

import (
	"encoding/json/v2"
	"testing"

	"github.com/okian/forge/examples/people"
)

// A patch writes what it was asked to and leaves the rest, which is the whole
// of what a partial update means.
func TestWhatAPatchChangesAndWhatItLeaves(t *testing.T) {
	held := somebody()

	name := "Grace"
	people.PersonPatch{Name: &name}.Apply(&held)

	if held.Name != "Grace" {
		t.Errorf("the patch did not change the name: %+v", held)
	}
	if held.Email != "ada@example.com" || held.Age != 36 {
		t.Errorf("the patch changed a field it said nothing about: %+v", held)
	}
	if len(held.Aliases) != 2 {
		t.Errorf("the patch changed the aliases: %v", held.Aliases)
	}
}

// A field set to the zero value is a field the patch was asked to clear, and a
// field the patch says nothing about is left alone.
//
// This is the distinction the whole shape exists for. A handler given a whole
// Person cannot tell the two apart, because both arrive as the empty string.
func TestClearingIsNotTheSameAsSayingNothing(t *testing.T) {
	cleared := somebody()
	empty := ""
	people.PersonPatch{Name: &empty}.Apply(&cleared)

	silent := somebody()
	people.PersonPatch{}.Apply(&silent)

	if cleared.Name != "" {
		t.Errorf("a patch asked to clear the name left %q", cleared.Name)
	}
	if silent.Name != "Ada" {
		t.Errorf("a patch that said nothing about the name changed it to %q", silent.Name)
	}
}

// A patch that sets nothing says so, which is also what a codec asks before it
// decides whether to write one at all.
func TestWhenAPatchAsksForNothing(t *testing.T) {
	if !(people.PersonPatch{}).IsZero() {
		t.Error("a patch holding nothing does not say it asks for nothing")
	}

	name := "Grace"
	if (people.PersonPatch{Name: &name}).IsZero() {
		t.Error("a patch asking for a name says it asks for nothing")
	}

	// Including one that asks for a zero value, which is a request rather than
	// an absence.
	empty := ""
	if (people.PersonPatch{Name: &empty}).IsZero() {
		t.Error("a patch asking to clear a field says it asks for nothing")
	}
}

// A patch replaces rather than merges, which is worth pinning because the other
// reading is the one somebody will assume.
func TestAPatchReplacesRatherThanMerges(t *testing.T) {
	held := somebody()

	aliases := []string{"only this one"}
	people.PersonPatch{Aliases: &aliases}.Apply(&held)

	if len(held.Aliases) != 1 || held.Aliases[0] != "only this one" {
		t.Errorf("the patch merged rather than replaced: %v", held.Aliases)
	}
}

// A document describing a person is a document describing a change to one,
// because the patch's members are named as the person's are.
//
// It is the difference between a partial update and a no-op nobody notices. A
// patch whose fields carried none of the subject's tags would be read under the
// field's own names, so a request written with the names a reply came back
// under would name nothing the patch recognised — and would decode without
// complaint into a patch that sets nothing.
func TestAPatchIsReadUnderTheNamesTheSubjectIsWrittenUnder(t *testing.T) {
	document, err := json.Marshal(somebody())
	if err != nil {
		t.Fatalf("writing a person: %v", err)
	}

	var changes people.PersonPatch
	if err := json.Unmarshal(document, &changes); err != nil {
		t.Fatalf("reading a person back as a patch: %v", err)
	}

	if changes.IsZero() {
		t.Fatalf("a person's own document named nothing a patch recognised: %s", document)
	}
	if changes.Name == nil || *changes.Name != "Ada" {
		t.Errorf("the patch read %v from %s", changes.Name, document)
	}

	// And what it read describes the person it was read from.
	var held people.Person
	changes.Apply(&held)

	if held.Name != "Ada" || held.Email != "ada@example.com" {
		t.Errorf("the round trip gave %+v", held)
	}
}

// A patch hands over what it holds rather than a copy of it, which is what
// assignment does and what the generated comment says.
//
// Worth pinning because the other reading is the one somebody will assume: the
// copy layer exists precisely because assignment shares, and a patch is an
// assignment per field.
func TestAPatchSharesWhatItHolds(t *testing.T) {
	aliases := []string{"A. Lovelace"}
	changes := people.PersonPatch{Aliases: &aliases}

	var first, second people.Person
	changes.Apply(&first)
	changes.Apply(&second)

	first.Aliases[0] = "somebody else"

	if second.Aliases[0] != "somebody else" {
		t.Error("the patch copied what it held, which the comment on it says it does not")
	}
}

// What a patch produces is checked by the rules of the whole value, once the
// whole value exists.
//
// The patch itself checks nothing, and could not: a rule about a Person is
// about a Person, and a patch is only ever part of one.
func TestAPatchIsCheckedAfterItIsApplied(t *testing.T) {
	held := somebody()

	empty := ""
	people.PersonPatch{Email: &empty}.Apply(&held)

	if err := held.Validate(); err == nil {
		t.Error("a person whose address was cleared passed the check")
	}
}
