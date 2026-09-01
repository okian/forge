package validate_test

import (
	"strings"
	"testing"

	"github.com/okian/forge/internal/diag"
	"github.com/okian/forge/internal/layer"
	"github.com/okian/forge/internal/layers/validate"
	"github.com/okian/forge/internal/model"
	"github.com/okian/forge/internal/shape"
)

// refusedPkg is the fixture package holding the tags that cannot become a
// check.
const refusedPkg = "validatefixture/refused"

// A tag that cannot become a check is refused, by a code that says which kind
// of thing was wrong and a message that names the field and the rule.
//
// The refusal is the feature. A rule quietly ignored leaves an author believing
// a value is checked, and nothing anywhere ever says otherwise — not a test,
// which passes, and not a failure, which never comes. So every rule this cannot
// write is a rule nobody may write.
func TestWhatCannotBecomeACheck(t *testing.T) {
	cases := map[string]struct {
		code  string
		says  string
		hints string
	}{
		"Present":    {"FRG2014", "required needs", "nonzero"},
		"Compared":   {"FRG2014", "nonzero needs", "required"},
		"Matched":    {"FRG2014", "regexp needs a string", "documented"},
		"Measured":   {"FRG2014", "len needs", "min"},
		"Listed":     {"FRG2014", "oneof needs", "documented"},
		"Invented":   {"FRG2012", "whatever is not a rule", "documented"},
		"Overfed":    {"FRG2012", "required takes no value", "documented"},
		"Starved":    {"FRG2012", "min needs a number", "documented"},
		"Worded":     {"FRG2012", "some is not a number", "documented"},
		"Fractional": {"FRG2013", "not a whole number", "whole number"},
		"Negative":   {"FRG2013", "nothing is shorter than nothing", "documented"},
		"Partial":    {"FRG2013", "a length is a whole number", "documented"},
		"Mistyped":   {"FRG2013", "not a whole number", "documented"},
		"Broken":     {"FRG2012", "does not compile", "documented"},
		"Empty":      {"FRG2012", "nothing after the sign", "documented"},
		"Trailing":   {"FRG2012", "swallowed min", "documented"},
		"Doubled":    {"FRG2012", "empty rule", "documented"},
	}

	for name, want := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := refusing(t, name)
			if err == nil {
				t.Fatalf("a check was written for %s", name)
			}

			// Read as a diagnostic rather than as its own text. A hint is not
			// part of what an error prints — it is rendered beneath the
			// message, where an author reads it — so checking the string would
			// pass for a refusal that carried no hint at all.
			reported, ok := diag.From(err)
			if !ok {
				t.Fatalf("%v is not a diagnostic", err)
			}

			if got := reported.Code.String(); got != want.code {
				t.Errorf("%s is reported as %s, want %s: %s", name, got, want.code, reported.Message)
			}
			if !strings.Contains(reported.Message, want.says) {
				t.Errorf("the complaint about %s does not mention %q:\n%s", name, want.says, reported.Message)
			}
			if !strings.Contains(reported.Hint, want.hints) {
				t.Errorf("the hint for %s does not say %q:\n%s", name, want.hints, reported.Hint)
			}
		})
	}
}

// A refusal points at the field rather than at the declaration, because the
// field is the line an author would edit.
func TestWhereARefusalPoints(t *testing.T) {
	_, err := refusing(t, "Present")
	if err == nil {
		t.Fatal("a check was written for a rule that cannot be asked")
	}

	reported, ok := diag.From(err)
	if !ok {
		t.Fatalf("%v is not a diagnostic", err)
	}
	if !strings.HasSuffix(reported.Pos.Filename, "refused.go") {
		t.Errorf("the refusal points at %s, want the file the field is in", reported.Pos.Filename)
	}
	if reported.Pos.Line == 0 {
		t.Error("the refusal points at no line")
	}
}

// The two rules that look like synonyms teach the difference when the wrong one
// is written.
//
// It is the whole reason they are two rules. An author who writes required on
// an int has asked a question an int has no answer to, and the useful thing to
// tell them is not that they were wrong but which question they meant.
func TestTheTwoRulesThatLookAlikeTeachTheDifference(t *testing.T) {
	for name, want := range map[string]string{"Present": "nonzero", "Compared": "required"} {
		t.Run(name, func(t *testing.T) {
			_, err := refusing(t, name)
			if err == nil {
				t.Fatalf("a check was written for %s", name)
			}

			reported, _ := diag.From(err)
			if !strings.Contains(reported.Hint, "write "+want) {
				t.Errorf("the hint does not name %s:\n%s", want, reported.Hint)
			}
		})
	}
}

// A subject no method can be attached to is refused, and the refusal says which
// subject it was.
//
// The same distinction the copy layer draws, for the same reason. A check is
// declared on the subject, so a struct with no name in the package being
// generated into has nothing for the check to be declared on. This is not one
// of the tag refusals above: nothing is wrong with what the author wrote, and
// the message says so by naming the declaration rather than a rule.
func TestACheckIsRefusedForASubjectItCannotName(t *testing.T) {
	_, err := validate.New().Generate(&layer.Context{
		Model: &model.Model{Name: "Elsewhere", Subject: &model.Struct{}},
	}, shape.Shape{})

	if err == nil {
		t.Fatal("generating for a subject with no name of its own: want an error, got none")
	}
	if !strings.Contains(err.Error(), "Elsewhere") {
		t.Errorf("the refusal does not name the declaration: %v", err)
	}
	if !strings.Contains(err.Error(), "cannot be named") {
		t.Errorf("the refusal does not say what was wrong: %v", err)
	}
}
