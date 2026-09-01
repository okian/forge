package people_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/okian/forge/examples/people"
)

// A value assembled a field at a time is the value the fields describe.
//
// The whole of what a builder buys is at the call site: every field is named
// where it is given, so a reader checks the value against the names rather than
// against the order — and reordering two fields of one type changes nothing,
// which is the mistake a positional literal makes silently.
func TestWhatABuilderBuilds(t *testing.T) {
	held, err := people.NewPersonBuilder().
		Name("Ada").
		Email("ada@example.com").
		Age(36).
		Aliases([]string{"A. Lovelace"}).
		Build()
	if err != nil {
		t.Fatalf("building somebody every field was given: %v", err)
	}

	if held.Name != "Ada" || held.Email != "ada@example.com" || held.Age != 36 {
		t.Errorf("built %+v", held)
	}
	if len(held.Aliases) != 1 || held.Aliases[0] != "A. Lovelace" {
		t.Errorf("built aliases %v", held.Aliases)
	}
}

// A field the author said a value has to carry, and whose setter was never
// called, is refused — and the value that comes back is the zero one, so a
// caller who ignores the error cannot mistake half a person for a whole one.
func TestWhatABuilderRefuses(t *testing.T) {
	held, err := people.NewPersonBuilder().Name("Ada").Build()
	if err == nil {
		t.Fatal("a person with no address was built")
	}

	if held.Name != "" {
		t.Errorf("a refused build handed back %+v", held)
	}
	if !strings.Contains(err.Error(), "Email") {
		t.Errorf("the failure does not name the field: %v", err)
	}
	if strings.Contains(err.Error(), "Name") {
		t.Errorf("the failure names a field that was given: %v", err)
	}
}

// Every missing field is reported rather than the first, because a caller
// filling in a form wants the whole list.
func TestEveryMissingFieldIsReported(t *testing.T) {
	_, err := people.NewPersonBuilder().Age(36).Build()
	if err == nil {
		t.Fatal("a person with neither a name nor an address was built")
	}

	for _, want := range []string{"Name", "Email"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("%s is not reported: %v", want, err)
		}
	}
}

// What the builder reports is what the check reports, so a caller who handles
// one handles the other.
//
// It is the reason the two layers share a vocabulary. A field that was never
// given and a field that was given and found empty are the same thing to
// whoever is showing somebody a form.
func TestABuilderAndACheckFailAlike(t *testing.T) {
	_, err := people.NewPersonBuilder().Name("Ada").Build()

	var failed people.ValidationErrors
	if !errors.As(err, &failed) {
		t.Fatalf("the builder reported %T, which is not what a check reports", err)
	}
	if len(failed) != 1 || failed[0].Path != "Email" || failed[0].Rule != "required" {
		t.Errorf("the builder reported %+v", failed)
	}
}

// The builder does not check what it was given, which is the other layer's
// question and is asked where a rule added to the tag reaches it.
//
// A caller who gave an address that is not one has given it; that it is not an
// address is what the check says, and saying it twice in two places is how the
// two come to disagree.
func TestABuilderDoesNotCheckWhatItWasGiven(t *testing.T) {
	held, err := people.NewPersonBuilder().Name("Ada").Email("not an address").Build()
	if err != nil {
		t.Fatalf("the builder refused a value it was given: %v", err)
	}

	if err := held.Validate(); err == nil {
		t.Error("the check accepted an address that is not one")
	}
}

// The zero value is a builder, so a variable of the type is ready without being
// made.
func TestTheZeroBuilderIsOne(t *testing.T) {
	var held people.PersonBuilder

	built, err := held.Name("Ada").Email("ada@example.com").Build()
	if err != nil {
		t.Fatalf("building from a zero builder: %v", err)
	}
	if built.Name != "Ada" {
		t.Errorf("built %+v", built)
	}
}
