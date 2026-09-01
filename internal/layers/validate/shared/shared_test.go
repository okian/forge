package shared

import (
	"errors"
	"strings"
	"testing"
)

// This package is emitted into somebody else's module, where nothing this
// repository runs will ever exercise it again. So it is tested here, as the
// ordinary Go it is — which is the whole reason it is a file rather than a
// string.

// A failure reads as a sentence naming the field, whichever of the three shapes
// it has.
func TestWhatAFailureReads(t *testing.T) {
	cases := map[string]struct {
		held ValidationError
		want string
	}{
		"a rule with something to say": {
			held: ValidationError{Path: "Name", Rule: "min=2", Want: "at least 2 characters"},
			want: "Name: min=2 wants at least 2 characters",
		},
		"a rule with nothing to add": {
			held: ValidationError{Path: "Name", Rule: "required"},
			want: "Name: required",
		},
		"a check the author wrote": {
			held: ValidationError{Path: "Token", Cause: errors.New("a token starts with t")},
			want: "Token: a token starts with t",
		},
	}

	for name, want := range cases {
		t.Run(name, func(t *testing.T) {
			if got := want.held.Error(); got != want.want {
				t.Errorf("reads %q, want %q", got, want.want)
			}
		})
	}
}

// What a check the author wrote returned is reachable through the failure that
// carries it, so that a caller can ask what it was.
func TestTheCauseIsReachable(t *testing.T) {
	cause := errors.New("a token starts with t")
	held := ValidationError{Path: "Token", Cause: cause}

	if !errors.Is(held, cause) {
		t.Error("the cause cannot be reached through the failure")
	}
	if errors.Is(ValidationError{Path: "Name", Rule: "required"}, cause) {
		t.Error("a failure with no cause reports one anyway")
	}
}

// A list of failures reads as a list when there is more than one, and as the
// failure itself when there is one.
func TestWhatAListReads(t *testing.T) {
	one := ValidationError{Path: "Name", Rule: "required", Want: "a value"}
	two := ValidationError{Path: "Age", Rule: "min=0", Want: "at least 0"}

	if got, want := (ValidationErrors{one}).Error(), one.Error(); got != want {
		t.Errorf("one failure reads %q, want %q", got, want)
	}

	held := ValidationErrors{one, two}.Error()
	if !strings.HasPrefix(held, "2 failures:") {
		t.Errorf("a list does not say how many:\n%s", held)
	}
	if strings.Count(held, "\n\t") != 2 {
		t.Errorf("a list does not put each failure on its own line:\n%s", held)
	}

	// A state a caller does not reach through a generated check, which returns
	// no error at all rather than an empty list — and which still has to render
	// rather than index past the end of itself.
	if got := (ValidationErrors{}).Error(); got != "nothing failed" {
		t.Errorf("an empty list reads %q", got)
	}
}

// Each failure in a list is reachable on its own, which is what a caller
// picking out the one about a particular field does.
func TestEachFailureIsReachable(t *testing.T) {
	held := ValidationErrors{
		{Path: "Name", Rule: "required"},
		{Path: "Age", Rule: "min=0"},
	}

	var one ValidationError
	if !errors.As(error(held), &one) {
		t.Fatal("no single failure could be reached")
	}
	if one.Path != "Name" {
		t.Errorf("the failure reached is about %s, want the first", one.Path)
	}
}

// What a field's own check reported is folded in under the path that reaches
// the field.
//
// It is what makes a nested check worth having: a City that is too short is
// reported by Address as "City", and the value holding it has to say
// "Address.City" or a caller cannot tell which of two addresses it was.
func TestANestedFailureIsRepathed(t *testing.T) {
	inner := ValidationErrors{{Path: "City", Rule: "required", Want: "a value"}}

	held := nestedValidation(nil, "Address", error(inner))
	if len(held) != 1 {
		t.Fatalf("folding one failure produced %d", len(held))
	}
	if got, want := held[0].Path, "Address.City"; got != want {
		t.Errorf("the path is %s, want %s", got, want)
	}

	// And the failure it came from is left alone, since the caller may still
	// be holding it.
	if got, want := inner[0].Path, "City"; got != want {
		t.Errorf("folding rewrote the failure it was given: %s, want %s", got, want)
	}
}

// An error that is not a list of failures is carried whole, under the field's
// own path.
//
// That is what a check the author wrote returns and what a type from somewhere
// else returns, and neither is this package's to take apart.
func TestAnErrorFromElsewhereIsCarriedWhole(t *testing.T) {
	cause := errors.New("the address is not deliverable")

	held := nestedValidation(nil, "Home", cause)
	if len(held) != 1 {
		t.Fatalf("folding one error produced %d failures", len(held))
	}
	if got, want := held[0].Path, "Home"; got != want {
		t.Errorf("the path is %s, want %s", got, want)
	}
	if !errors.Is(held[0], cause) {
		t.Error("the error it was given cannot be reached")
	}
}

// Failures wrapped by a check of somebody's own still arrive as failures with
// paths, because they are looked for through the chain rather than in the hand.
func TestWrappedFailuresAreStillFound(t *testing.T) {
	inner := ValidationErrors{{Path: "City", Rule: "required"}}
	wrapped := errors.Join(errors.New("reading the address"), error(inner))

	held := nestedValidation(nil, "Address", wrapped)
	if len(held) != 1 {
		t.Fatalf("folding produced %d failures", len(held))
	}
	if got, want := held[0].Path, "Address.City"; got != want {
		t.Errorf("the path is %s, want %s", got, want)
	}
}

// Folding adds to what it was given rather than replacing it, since a value
// with several bad fields folds one after another.
func TestFoldingAddsToWhatItWasGiven(t *testing.T) {
	already := ValidationErrors{{Path: "Name", Rule: "required"}}
	inner := ValidationErrors{{Path: "City", Rule: "required"}}

	held := nestedValidation(already, "Address", error(inner))
	if len(held) != 2 {
		t.Fatalf("folding into one failure produced %d", len(held))
	}
	if held[0].Path != "Name" {
		t.Errorf("the failure that was already there is now %s", held[0].Path)
	}
}
