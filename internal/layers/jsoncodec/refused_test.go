package jsoncodec_test

import (
	"go/token"
	"go/types"
	"strings"
	"testing"

	"github.com/okian/forge/internal/layers/jsoncodec"
	"github.com/okian/forge/internal/subject"
	"github.com/okian/forge/plugin"
)

// refusedPkg is the fixture package holding what cannot be generated for.
const refusedPkg = "codecfixture/refused"

// A field a static codec cannot be written for is refused, by a code that says
// which kind of thing was wrong and a message that names the field.
//
// The refusal is the feature. A codec that skipped what it could not see through
// would produce an object with a member missing, and nothing that round-trips
// through the same codec would catch it: both halves would agree the field does
// not exist. What an author can act on is the field's name and the way out, so
// both are checked rather than only the code.
func TestWhatACodecRefusesToWrite(t *testing.T) {
	cases := map[string]struct {
		code  string
		says  string
		hints string
	}{
		"Opaque":       {"FRG2007", "Anything", "fallback=stdlib"},
		"Interfaced":   {"FRG2007", "Reader", "fallback=stdlib"},
		"Foreign":      {"FRG2007", "time.Time", "fallback=stdlib"},
		"Channelled":   {"FRG2007", "Updates", "fallback=stdlib"},
		"Keyed":        {"FRG2007", "map[int]string", "fallback=stdlib"},
		"Formatted":    {"FRG2008", "format:RFC3339", "withdrawn"},
		"Insensitive":  {"FRG2008", "case:ignore", "matches a name exactly"},
		"Colliding":    {"FRG2009", "same", "different name"},
		"Misspelled":   {"FRG2008", "fallbck", "fallback=stdlib"},
		"Misvalued":    {"FRG2008", "reflect", "stdlib"},
		"EmbedsScalar": {"FRG2007", "Celsius", "fallback=stdlib"},
		"EmbedsCodec":  {"FRG2007", "Bag", "fallback=stdlib"},
		"CannotOmit":   {"FRG2010", "Held", "IsZero"},
		"Quoted":       {"FRG2008", `",string"`, "reflectively"},
		"Holds":        {"FRG2011", "MarshalJSONTo", "redeclare"},
		"Named":        {"FRG2007", "member name", "fallback=stdlib"},
		"Held":         {"FRG2010", "One", "IsZero"},
		"Looped":       {"FRG2007", "contains itself", "fallback=stdlib"},
		"Labelled":     {"FRG2007", "contains itself", "fallback=stdlib"},
	}

	for name, want := range cases {
		t.Run(name, func(t *testing.T) {
			err := generating(t, refusedPkg, name)
			if err == nil {
				t.Fatalf("a codec was written for %s", name)
			}

			// Read as a diagnostic rather than as its own text. A hint is not
			// part of what an error prints — it is rendered beneath the
			// message, where an author reads it — so checking the string would
			// pass for a refusal that carried no hint at all.
			reported, ok := plugin.From(err)
			if !ok {
				t.Fatalf("%v is not a diagnostic", err)
			}

			if got := reported.Code.String(); got != want.code {
				t.Errorf("%s is reported as %s, want %s: %s", name, got, want.code, reported.Message)
			}
			if !strings.Contains(reported.Message, want.says) {
				t.Errorf("the complaint about %s does not mention %s:\n%s", name, want.says, reported.Message)
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
	err := generating(t, refusedPkg, "Opaque")
	if err == nil {
		t.Fatal("a codec was written for a field nothing can see through")
	}

	reported, ok := plugin.From(err)
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

// A field marked as the reflective boundary is written rather than refused, and
// the mark is what makes the difference — the same field without it is a
// refusal, which is what makes the option worth writing.
func TestTheBoundaryIsWhatMakesTheDifference(t *testing.T) {
	if err := generating(t, modelPkg, "Reflective"); err != nil {
		t.Fatalf("a marked field was refused anyway: %v", err)
	}
	if err := generating(t, refusedPkg, "Opaque"); err == nil {
		t.Fatal("an unmarked field of the same kind was written")
	}
}

// generating asks the layer for one fixture subject's codec and returns what it
// said, if anything.
func generating(t *testing.T, pkgPath, name string) error {
	t.Helper()

	loaded := loadFixture(t)
	builder := subject.New(subject.Config{
		Fset:  loaded.Fset,
		Owned: loaded.Owned(),
		Docs:  loaded.FieldDocs(),
	})

	pkg, ok := loaded.Package(pkgPath)
	if !ok {
		t.Fatalf("the fixture has no package %s", pkgPath)
	}

	obj := pkg.Types.Scope().Lookup(name)
	if obj == nil {
		t.Fatalf("%s declares no %s", pkgPath, name)
	}
	held, ok := types.Unalias(obj.Type()).(*types.Named)
	if !ok {
		t.Fatalf("%s is a %T, want a named type", name, obj.Type())
	}

	built, problems := builder.Build(held, subject.Site{})
	if !problems.Empty() {
		t.Fatalf("modelling %s: %s", name, problems.Render())
	}

	_, err := jsoncodec.New().Generate(&plugin.Context{
		Model: &plugin.Model{
			Name: name, Form: plugin.FormInline, Subject: built,
			Pkg: pkg, Pos: token.Position{Filename: "refused.go"},
		},
	}, plugin.Shape{})

	return err
}
