package options_test

import (
	"go/token"
	"go/types"
	"strings"
	"testing"

	"github.com/okian/forge/internal/discover"
	"github.com/okian/forge/internal/layers"
	"github.com/okian/forge/internal/model"
	"github.com/okian/forge/internal/options"
	"github.com/okian/forge/internal/tags"
)

// line is the line every directive in these tests is written on, so that a
// column can be read as the offset within it.
const line = 12

// written builds a directive the way discovery hands one over, with the
// positions it would carry.
//
// The offsets are computed from the text rather than given, because what these
// tests are about is where a diagnostic points, and a fixture that stated the
// answer would agree with a broken implementation.
func written(text string) discover.Directive {
	const prefix = "//forge:"

	rest := strings.TrimPrefix(text, prefix)
	name, args, _ := strings.Cut(rest, " ")

	offset := len(prefix) + len(name)
	if args != "" {
		offset++
	}

	return discover.Directive{
		Layer:      name,
		Args:       args,
		Text:       text,
		ArgsOffset: offset,
		Pos:        token.Position{Filename: "model/spec.go", Line: line, Column: 1, Offset: 100},
	}
}

// naming builds a stack from marker names, outermost first.
func naming(markers ...string) []model.LayerRef {
	out := make([]model.LayerRef, len(markers))
	for i, name := range markers {
		out[i] = model.LayerRef{Origin: model.TypeRef{Pkg: model.MarkerPkg, Name: name}}
	}
	return out
}

// person is the subject an option naming a field is resolved against.
//
// Named, because a message about a field opens with the type that does not have
// it — and a struct assembled without one would leave every such message
// starting with a space nobody would notice was a missing word.
var person = &model.Struct{
	Named: types.NewNamed(
		types.NewTypeName(token.NoPos, types.NewPackage("example.com/model", "model"), "Person", nil),
		types.NewStruct(nil, nil), nil),
	Fields: []model.Field{
		{Name: "ID", Exported: true},
		{Name: "Age", Exported: true, Tags: []tags.Tag{{Key: "json", Name: "age"}}},
		{Name: "LastName", Exported: true},
	},
}

// on validates directives written on a declaration over the given stack.
func on(stack []model.LayerRef, texts ...string) ([]model.Options, string) {
	directives := make([]discover.Directive, len(texts))
	for i, text := range texts {
		directives[i] = written(text)
	}

	set, diags := options.Read(options.Declaration{
		Directives: directives,
		Stack:      stack,
		Subject:    person,
	}, layers.Builtins())

	return set, diags.Render()
}

// Options an author wrote are carried through in the order they were written,
// with the layer they were written for.
func TestWhatWasWritten(t *testing.T) {
	set, reported := on(naming("Collection", "Ring", "Json"),
		"//forge:collection sort=Age index=ID",
		"//forge:ring cap=1024 overflow=overwrite",
		"//forge:json names=snake omitzero=true")

	if reported != "" {
		t.Fatalf("options that are all correct were reported:\n%s", reported)
	}
	if len(set) != 3 {
		t.Fatalf("read %d directives, want 3", len(set))
	}

	if set[0].Layer != "collection" {
		t.Errorf("the first directive is for %s", set[0].Layer)
	}
	if got := set[0].Entries; len(got) != 2 || got[0].Key != "sort" || got[1].Key != "index" {
		t.Errorf("the first directive's options are %v", got)
	}
	if entry, has := set[1].Lookup("cap"); !has || entry.Value != "1024" {
		t.Errorf("ring's cap is %q", entry.Value)
	}
}

// Every failure points at the text that has to change. A directive is short, a
// caret is only worth drawing if it lands, and the option is what the author
// edits rather than the line it sits on.
func TestWhereAFailurePoints(t *testing.T) {
	cases := map[string]struct {
		stack     []model.LayerRef
		directive string
		code      string
		at        string
	}{
		"a directive naming no layer": {
			stack: naming("Collection"), directive: "//forge:",
			code: "FRG3003", at: "//forge:",
		},
		"a layer the declaration does not use": {
			stack: naming("Collection"), directive: "//forge:ring cap=8",
			code: "FRG3004", at: "//forge:",
		},
		"an option the layer does not have": {
			stack: naming("Collection"), directive: "//forge:collection nonesuch=1",
			code: "FRG3006", at: "nonesuch",
		},
		"an option written twice": {
			stack: naming("Collection"), directive: "//forge:collection sort=Age sort=ID",
			code: "FRG3007", at: "sort=ID",
		},
		"an option that belongs on a field": {
			stack: naming("Json"), directive: "//forge:json fallback=stdlib",
			code: "FRG3008", at: "fallback",
		},
		"a number that is not one": {
			stack: naming("Ring"), directive: "//forge:ring cap=lots",
			code: "FRG3009", at: "cap=lots",
		},
		"a choice nobody offers": {
			stack: naming("Ring"), directive: "//forge:ring overflow=explode",
			code: "FRG3009", at: "overflow",
		},
		"a truth value that is not one": {
			stack: naming("Json"), directive: "//forge:json omitzero=perhaps",
			code: "FRG3009", at: "omitzero",
		},
		"a field the subject does not have": {
			stack: naming("Collection"), directive: "//forge:collection sort=Nickname",
			code: "FRG3010", at: "sort",
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, reported := on(tc.stack, tc.directive)

			if !strings.Contains(reported, tc.code) {
				t.Errorf("the failure is not %s:\n%s", tc.code, reported)
			}

			// The column the diagnostic reports, read back as an offset into
			// the directive, has to land on the text the message is about.
			want := line
			if got := columnOf(t, reported); !strings.HasPrefix(tc.directive[got-1:], tc.at) {
				t.Errorf("the failure points at %q, want it at %q:\n%s",
					tc.directive[got-1:], tc.at, reported)
			}
			if got := lineOf(t, reported); got != want {
				t.Errorf("the failure is reported on line %d, want %d", got, want)
			}
		})
	}
}

// lineOf reads the line a rendered diagnostic reports.
func lineOf(t *testing.T, rendered string) int {
	t.Helper()
	return int(number(t, rendered, 1))
}

// columnOf reads the column a rendered diagnostic reports.
func columnOf(t *testing.T, rendered string) int {
	t.Helper()
	return int(number(t, rendered, 2))
}

// number reads the nth colon-separated field of a rendered position.
func number(t *testing.T, rendered string, nth int) int64 {
	t.Helper()

	head, _, _ := strings.Cut(rendered, ": ")
	parts := strings.Split(head, ":")
	if len(parts) < nth+1 {
		t.Fatalf("no position in %q", rendered)
	}

	var out int64
	for _, r := range parts[nth] {
		if r < '0' || r > '9' {
			t.Fatalf("position field %q is not a number, in %q", parts[nth], rendered)
		}
		out = out*10 + int64(r-'0')
	}
	return out
}

// A layer appears at most once per stack, so a second directive for one is two
// answers to a question with one asker.
func TestALayerConfiguredTwice(t *testing.T) {
	_, reported := on(naming("Collection"),
		"//forge:collection sort=Age",
		"//forge:collection index=ID")

	if !strings.Contains(reported, "FRG3005") {
		t.Errorf("a layer configured twice was accepted:\n%s", reported)
	}
	if !strings.Contains(reported, "one directive per layer") {
		t.Errorf("the failure does not say what to do:\n%s", reported)
	}
}

// The first of the two stays, because it is what the author most likely meant
// and what a run that carries on acts on.
func TestTheFirstOfTwoDirectivesIsKept(t *testing.T) {
	set, _ := on(naming("Collection"),
		"//forge:collection sort=Age",
		"//forge:collection sort=ID")

	if len(set) != 1 {
		t.Fatalf("read %d directives, want 1", len(set))
	}
	if entry, _ := set[0].Lookup("sort"); entry.Value != "Age" {
		t.Errorf("the kept directive sorts by %q, want Age", entry.Value)
	}
}

// A failure names what the layer does take, since the reason for the mistake is
// nearly always not knowing.
func TestAFailureSaysWhatIsAccepted(t *testing.T) {
	_, reported := on(naming("Ring"), "//forge:ring size=8")

	for _, want := range []string{"cap", "overflow"} {
		if !strings.Contains(reported, want) {
			t.Errorf("the failure does not offer %s:\n%s", want, reported)
		}
	}
}

// And a layer that takes none says so, rather than offering an empty list.
func TestALayerThatTakesNoOptions(t *testing.T) {
	_, reported := on(naming("Validate"), "//forge:validate strict=true")

	if !strings.Contains(reported, "takes no options") {
		t.Errorf("the failure does not say the layer takes none:\n%s", reported)
	}
}

// A layer this release does not ship has a schema nobody has finished writing,
// so a key it does not list is not yet wrong. The answer its author needs is
// that the layer is not here at all, which generation gives them.
func TestOptionsForALayerThisReleaseDoesNotShip(t *testing.T) {
	_, reported := on(naming("Sorted"), "//forge:sorted by=Age direction=descending")

	if reported != "" {
		t.Errorf("an option for a staged layer was held to a provisional schema:\n%s", reported)
	}
}

// An option naming several fields resolves each of them, since one wrong name
// among four is the case a single answer would hide.
func TestAnOptionNamingSeveralFields(t *testing.T) {
	_, reported := on(naming("Collection"), "//forge:collection sort=Age,Nickname,LastName")

	if !strings.Contains(reported, "Nickname") {
		t.Errorf("the wrong name among the right ones was not reported:\n%s", reported)
	}
	for _, right := range []string{"Age", "LastName"} {
		if strings.Contains(reported, "has no field "+right) {
			t.Errorf("%s was reported as missing:\n%s", right, reported)
		}
	}
}

// The subject is what a field name is resolved against, so a declaration whose
// subject was refused has nothing to resolve against — and a second complaint
// about fields it does not have would bury the first.
func TestFieldsWithNoSubjectToResolveAgainst(t *testing.T) {
	_, diags := options.Read(options.Declaration{
		Directives: []discover.Directive{written("//forge:collection sort=Nickname")},
		Stack:      naming("Collection"),
	}, layers.Builtins())

	if !diags.Empty() {
		t.Errorf("a field was resolved against a subject nobody has:\n%s", diags.Render())
	}
}

// A marker no layer claims is reported by the stage that resolves the stack,
// and judging its options against a schema nobody wrote would be a second
// complaint about one mistake.
func TestOptionsForAMarkerNoLayerClaims(t *testing.T) {
	_, reported := on(naming("Nonesuch"), "//forge:nonesuch whatever=1")

	if reported != "" {
		t.Errorf("an unclaimed marker's options were judged:\n%s", reported)
	}
}

// A walk with no catalog holds nothing to a schema, rather than reporting every
// option as unknown.
func TestReadingWithNoCatalogAtAll(t *testing.T) {
	_, diags := options.Read(options.Declaration{
		Directives: []discover.Directive{written("//forge:collection sort=Age")},
		Stack:      naming("Collection"),
	}, nil)

	if !diags.Empty() {
		t.Errorf("options were judged against a catalog nobody supplied:\n%s", diags.Render())
	}
}

// A declaration with no directives has nothing to validate and nothing to say.
func TestADeclarationWithNoDirectives(t *testing.T) {
	set, reported := on(naming("Collection", "Ring", "Json"))

	if len(set) != 0 {
		t.Errorf("read %d directives from a declaration with none", len(set))
	}
	if reported != "" {
		t.Errorf("a declaration with no directives was reported:\n%s", reported)
	}
}

// A directive naming a layer the declaration does not use says which layers it
// does, since the mistake is nearly always a stack that changed.
func TestAFailureSaysWhichLayersThereAre(t *testing.T) {
	_, reported := on(naming("Collection", "Json"), "//forge:ring cap=8")

	for _, want := range []string{"collection", "json"} {
		if !strings.Contains(reported, want) {
			t.Errorf("the failure does not name %s:\n%s", want, reported)
		}
	}
}
