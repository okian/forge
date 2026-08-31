package options_test

import (
	"go/token"
	"strings"
	"testing"

	"github.com/okian/forge/internal/discover"
	"github.com/okian/forge/internal/layer"
	"github.com/okian/forge/internal/model"
	"github.com/okian/forge/internal/options"
	"github.com/okian/forge/internal/shape"
)

// declared is where the declaration these tests are about was written, which is
// what a report about the declaration as a whole points at.
var declared = token.Position{Filename: "model/spec.go", Line: 14, Column: 6, Offset: 200}

// read validates directives over a stack, with the declaration's own position
// supplied the way the pipeline supplies it.
func read(t *testing.T, registry *layer.Registry, stack []model.LayerRef, texts ...string) ([]model.Options, string) {
	t.Helper()

	directives := make([]discover.Directive, len(texts))
	for i, text := range texts {
		directives[i] = written(text)
	}

	set, diags := options.Read(options.Declaration{
		Pos:        declared,
		Directives: directives,
		Stack:      stack,
		Subject:    person,
	}, registry)

	return set, diags.Render()
}

// A layer is handed these to act on, so what it is handed has to be worth
// acting on. An option whose value is not a number would reach it as a zero
// nobody wrote, and one written about a field would be applied to every field
// at once — which is the opposite of what saying so was for.
func TestOnlyWhatSurvivedIsHandedOver(t *testing.T) {
	cases := map[string]struct {
		directive string
		kept      []string
	}{
		"a number that is not one": {
			directive: "//forge:ring cap=lots overflow=error",
			kept:      []string{"overflow"},
		},
		"a choice nobody offers": {
			directive: "//forge:ring cap=8 overflow=explode",
			kept:      []string{"cap"},
		},
		"an option that belongs on a field": {
			directive: "//forge:json names=snake fallback=stdlib",
			kept:      []string{"names"},
		},
		"an option nobody declared": {
			directive: "//forge:ring cap=8 nonesuch=1",
			kept:      []string{"cap"},
		},
		"a field the subject does not have": {
			directive: "//forge:collection sort=Age index=Nickname",
			kept:      []string{"sort"},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			stack := naming("Collection", "Ring", "Json")
			set, reported := read(t, layer.Builtins(), stack, tc.directive)

			if reported == "" {
				t.Fatal("an option that should have been refused was accepted")
			}
			if len(set) != 1 {
				t.Fatalf("read %d directives, want 1", len(set))
			}

			var kept []string
			for _, entry := range set[0].Entries {
				kept = append(kept, entry.Key)
			}
			if strings.Join(kept, ",") != strings.Join(tc.kept, ",") {
				t.Errorf("handed over %v, want %v", kept, tc.kept)
			}
		})
	}
}

// A set carries where its directive was written, since a layer reporting about
// one of its own options has nowhere else to point.
func TestASetKnowsWhereItWasWritten(t *testing.T) {
	set, _ := read(t, layer.Builtins(), naming("Collection"), "//forge:collection sort=Age")

	if len(set) != 1 {
		t.Fatalf("read %d directives, want 1", len(set))
	}
	if set[0].Pos.Line != line {
		t.Errorf("the set was written at line %d, want %d", set[0].Pos.Line, line)
	}
}

// An option a layer cannot generate without, with no directive to add it to,
// is reported against the declaration — which is the only thing there is to
// point at, and is not nowhere.
func TestWhereAMissingOptionIsReported(t *testing.T) {
	registry := catalog(t, required)

	_, diags := options.Read(options.Declaration{
		Pos:     declared,
		Stack:   naming("Ring"),
		Subject: person,
	}, registry)

	rendered := diags.Render()
	if !strings.Contains(rendered, "model/spec.go:14:6") {
		t.Errorf("the report does not point at the declaration:\n%s", rendered)
	}

	// And with a directive to add it to, at the directive: the author is being
	// told to add a key to something they have already written.
	_, withOne := read(t, registry, naming("Ring"), "//forge:ring strict")
	if !strings.Contains(withOne, "model/spec.go:12:") {
		t.Errorf("the report does not point at the directive:\n%s", withOne)
	}
}

// A stack naming one layer twice is a composition failure reported where
// composition is decided. Saying the same thing twice about it here would bury
// that under noise.
func TestOneComplaintPerLayerHoweverOftenItAppears(t *testing.T) {
	_, reported := read(t, catalog(t, required), naming("Ring", "Ring"), "//forge:ring strict")

	if got := strings.Count(reported, "FRG3011"); got != 1 {
		t.Errorf("reported %d times, want 1:\n%s", got, reported)
	}
}

// And one field named twice is one mistake.
func TestOneComplaintPerFieldHoweverOftenItIsNamed(t *testing.T) {
	_, reported := read(t, layer.Builtins(), naming("Collection"),
		"//forge:collection sort=Nickname,Nickname,Nickname")

	if got := strings.Count(reported, "FRG3010"); got != 1 {
		t.Errorf("reported %d times, want 1:\n%s", got, reported)
	}
}

// A directive names its layer in lower case, which is the one way it can be
// written and the one way that is easy to get wrong: the marker is spelled
// Collection and the directive is spelled collection.
func TestALayerNamedInTheWrongCase(t *testing.T) {
	_, reported := read(t, catalog(t, required), naming("Ring"), "//forge:Ring cap=8")

	if !strings.Contains(reported, "lower case") {
		t.Errorf("the failure does not say what the spelling should be:\n%s", reported)
	}
	if !strings.Contains(reported, "//forge:ring") {
		t.Errorf("the failure does not offer the spelling that works:\n%s", reported)
	}
	// One mistyped name is one mistake. The layer the author plainly meant is
	// not also reported as unconfigured.
	if strings.Contains(reported, "FRG3011") {
		t.Errorf("a mistyped name was reported twice over:\n%s", reported)
	}
}

// A comment written after a directive's options is still on the line, and
// reading its words as options gives an author complaints about prose they
// wrote for a reader.
func TestAnExplanationWrittenAfterTheOptions(t *testing.T) {
	set, reported := read(t, layer.Builtins(), naming("Collection"),
		"//forge:collection sort=Age // sorted for the report")

	if reported != "" {
		t.Errorf("an explanation was read as options:\n%s", reported)
	}
	if len(set) != 1 || len(set[0].Entries) != 1 {
		t.Fatalf("read %v", set)
	}
	if set[0].Entries[0].Key != "sort" {
		t.Errorf("the option is %s", set[0].Entries[0].Key)
	}
}

// A value may hold an equals sign, since a pattern or a format string is a
// plausible thing to configure and neither should have to be escaped.
func TestAValueHoldingAnEqualsSign(t *testing.T) {
	one := demanding{name: "Slice", options: []layer.OptionDef{
		{Key: "match", Value: layer.ValueString, Doc: "a pattern to match against"},
	}}

	set, reported := read(t, catalog(t, one), naming("Slice"), "//forge:slice match=a=b=c")

	if reported != "" {
		t.Errorf("a value holding an equals sign was reported:\n%s", reported)
	}
	if got := set[0].Entries[0].Value; got != "a=b=c" {
		t.Errorf("the value is %q, want %q", got, "a=b=c")
	}
}

// Arguments are separated by space, whichever space was written.
func TestOptionsSeparatedByATab(t *testing.T) {
	set, reported := read(t, layer.Builtins(), naming("Ring"), "//forge:ring cap=8\toverflow=error")

	if reported != "" {
		t.Errorf("options separated by a tab were reported:\n%s", reported)
	}
	if len(set[0].Entries) != 2 {
		t.Errorf("read %d options, want 2", len(set[0].Entries))
	}
}

// An option carries its offset as well as its line and column, because a set of
// diagnostics is ordered by position and the offset is what separates two
// written on one line.
func TestAnOptionKnowsItsOffset(t *testing.T) {
	set, _ := read(t, layer.Builtins(), naming("Ring"), "//forge:ring cap=8 overflow=error")

	first, second := set[0].Entries[0], set[0].Entries[1]
	if second.Pos.Offset <= first.Pos.Offset {
		t.Errorf("the second option is at offset %d and the first at %d",
			second.Pos.Offset, first.Pos.Offset)
	}
	if want := second.Pos.Column - first.Pos.Column; second.Pos.Offset-first.Pos.Offset != want {
		t.Errorf("the offsets are %d apart and the columns %d",
			second.Pos.Offset-first.Pos.Offset, want)
	}
}

// An option written with no name at all names nothing, and reporting it as an
// option the layer does not have would print a sentence with a gap in it.
func TestAnOptionWithNoName(t *testing.T) {
	_, reported := read(t, layer.Builtins(), naming("Collection"), "//forge:collection =Age")

	if !strings.Contains(reported, "FRG3012") {
		t.Errorf("an option with no name was reported as something else:\n%s", reported)
	}
	if !strings.Contains(reported, "names nothing") {
		t.Errorf("the failure does not say what is wrong:\n%s", reported)
	}
}

// staged is a layer forge has not committed to, with an option it would need.
type staged struct{ demanding }

func (staged) Stage() layer.Stage { return layer.StageStaged }
func (staged) Doc() string        { return "a layer forge has not committed to" }

// A layer this release does not ship cannot be missing an option, because the
// schema that would say so is provisional.
func TestARequiredOptionOfALayerNobodyHasFinished(t *testing.T) {
	l := staged{demanding{name: "Sorted", options: []layer.OptionDef{
		{Key: "by", Value: layer.ValueField, Required: true, Doc: "the field to sort on"},
	}}}

	_, reported := read(t, catalog(t, l), naming("Sorted"))

	if reported != "" {
		t.Errorf("a staged layer was held to a schema nobody has finished:\n%s", reported)
	}
}

// An option a layer needs and that belongs on a field cannot be demanded on the
// declaration: it would be asked for in the one place it is refused.
func TestARequiredOptionThatBelongsOnAField(t *testing.T) {
	l := demanding{name: "Json", options: []layer.OptionDef{
		{
			Key: "fallback", Scope: layer.ScopeField, Value: layer.ValueString, Required: true,
			Doc: "how to encode a field forge cannot see through",
		},
	}}

	_, reported := read(t, catalog(t, l), naming("Json"))

	if reported != "" {
		t.Errorf("an option that belongs on a field was demanded on the declaration:\n%s", reported)
	}
}

// An option naming one field names one, so a comma in its value is part of the
// name rather than a separator — and the name is then one the subject does not
// have, which is the honest answer.
func TestAnOptionNamingOneFieldTakesOne(t *testing.T) {
	l := demanding{name: "Slice", options: []layer.OptionDef{
		{Key: "key", Value: layer.ValueField, Doc: "the field to key on"},
	}}

	_, reported := read(t, catalog(t, l), naming("Slice"), "//forge:slice key=Age,ID")

	if !strings.Contains(reported, "Age,ID") {
		t.Errorf("a single field name was split on its comma:\n%s", reported)
	}
}

// A field-scoped option is offered to nobody writing on a declaration, since
// taking the advice would earn the complaint that says it belongs elsewhere.
func TestWhatALayerOffersOnADeclaration(t *testing.T) {
	_, reported := read(t, layer.Builtins(), naming("Json"), "//forge:json nonesuch=1")

	if strings.Contains(reported, "fallback") {
		t.Errorf("an option that belongs on a field was offered for a declaration:\n%s", reported)
	}
	if !strings.Contains(reported, "names") {
		t.Errorf("the failure does not offer what the layer does take:\n%s", reported)
	}
}

// And a layer whose every option is about a field takes none where the author
// is writing, which is a different thing from taking none at all.
func TestALayerWhoseOptionsAreAllAboutFields(t *testing.T) {
	l := demanding{name: "Json", options: []layer.OptionDef{
		{Key: "fallback", Scope: layer.ScopeField, Value: layer.ValueString, Doc: "how to encode what cannot be seen through"},
	}}

	_, reported := read(t, catalog(t, l), naming("Json"), "//forge:json nonesuch=1")

	if !strings.Contains(reported, "no options on a declaration") {
		t.Errorf("the failure does not say where the layer's options go:\n%s", reported)
	}
}

// A space written after a comma is what an author naturally writes, and the
// arguments of a directive are separated by space — so the list ends there and
// the rest is read as another option.
func TestALisOfFieldsWrittenWithSpaces(t *testing.T) {
	_, reported := read(t, layer.Builtins(), naming("Collection"), "//forge:collection sort=Age, LastName")

	if !strings.Contains(reported, "no space between them") {
		t.Errorf("the failure does not say how to write the list:\n%s", reported)
	}
}

// silent is a layer that says nothing about itself, which is what a layer from
// outside forge does.
type silent struct{ demanding }

// A layer that says nothing is held to its schema, since a layer written
// outside forge has no answer to a question about forge's roadmap and taking
// silence for "not finished" would let every one of its options through
// unchecked.
func TestALayerThatSaysNothingAboutItself(t *testing.T) {
	l := silent{demanding{name: "Slice", options: []layer.OptionDef{
		{Key: "cap", Value: layer.ValueInt, Doc: "how many elements to allocate for"},
	}}}

	_, reported := read(t, catalog(t, l), naming("Slice"), "//forge:slice nonesuch=1")

	if !strings.Contains(reported, "FRG3006") {
		t.Errorf("a layer that said nothing was validated leniently:\n%s", reported)
	}
}

// interfaces the stand-ins have to satisfy, so that a change to the plugin
// surface is a compile failure here rather than a silent gap in what these
// tests cover.
var (
	_ layer.Layer     = demanding{}
	_ layer.Described = staged{}
	_ shape.Shape     = shape.Shape{}
)

// A layer somebody plainly meant is not also reported as unconfigured — and the
// directive that says so must not take the place of one spelled correctly. The
// two orders have to agree, since which one an author typed first is decided by
// how they came to have both: fixing a line, or taking both sides of a merge.
func TestAMistypedDirectiveBesideACorrectOne(t *testing.T) {
	orders := map[string][]string{
		"the mistyped one first": {"//forge:Collection sort=Age", "//forge:collection index=ID"},
		"the correct one first":  {"//forge:collection index=ID", "//forge:Collection sort=Age"},
	}

	for _, name := range []string{"the correct one first", "the mistyped one first"} {
		t.Run(name, func(t *testing.T) {
			set, reported := read(t, layer.Builtins(), naming("Collection"), orders[name]...)

			// The correct directive is read, whichever side of the mistyped one
			// it was written.
			if len(set) != 1 {
				t.Fatalf("read %d directives, want 1:\n%s", len(set), reported)
			}
			if entry, has := set[0].Lookup("index"); !has || entry.Value != "ID" {
				t.Errorf("the correct directive was thrown away: %v", set[0].Entries)
			}

			// And it is not itself reported as the repeat.
			if strings.Contains(reported, "FRG3005") {
				t.Errorf("a directive spelled correctly was reported as a repeat:\n%s", reported)
			}
			if !strings.Contains(reported, "FRG3004") {
				t.Errorf("the mistyped directive was not reported:\n%s", reported)
			}
		})
	}
}

// A message about a field opens with the type that does not have it, so a
// subject carrying no name of its own says which subject it is rather than
// opening with nothing.
func TestASubjectWithNoNameOfItsOwn(t *testing.T) {
	_, diags := options.Read(options.Declaration{
		Pos:        declared,
		Directives: []discover.Directive{written("//forge:collection sort=Nickname")},
		Stack:      naming("Collection"),
		Subject:    &model.Struct{Fields: []model.Field{{Name: "ID", Exported: true}}},
	}, layer.Builtins())

	rendered := diags.Render()
	if !strings.Contains(rendered, "the subject has no field Nickname") {
		t.Errorf("a subject with no name opened the message with nothing:\n%s", rendered)
	}
}

// And one with a name uses it, since that is what the author is looking at.
func TestASubjectNamedInTheMessage(t *testing.T) {
	_, reported := read(t, layer.Builtins(), naming("Collection"), "//forge:collection sort=Nickname")

	if !strings.Contains(reported, "Person has no field Nickname") {
		t.Errorf("the message does not name the subject:\n%s", reported)
	}
	if !strings.Contains(reported, "Person has ID, Age and LastName") {
		t.Errorf("the hint does not offer the fields the subject has:\n%s", reported)
	}
}

// An option written as nothing but an equals sign names nothing, and quoting
// what was parsed rather than what was written would complain about "".
func TestAnOptionWrittenAsNothingButAnEqualsSign(t *testing.T) {
	_, reported := read(t, layer.Builtins(), naming("Collection"), "//forge:collection =")

	if !strings.Contains(reported, `written as "="`) {
		t.Errorf("the failure does not quote what was written:\n%s", reported)
	}
}

// Everything after a comment marker is left alone, options included: an author
// who commented one out has said what they meant.
func TestOptionsCommentedOut(t *testing.T) {
	set, reported := read(t, layer.Builtins(), naming("Collection"),
		"//forge:collection sort=Age // index=ID later")

	if reported != "" {
		t.Errorf("an option the author commented out was read:\n%s", reported)
	}
	if len(set[0].Entries) != 1 {
		t.Errorf("read %d options, want 1", len(set[0].Entries))
	}
}

// The marker has to open an argument to count. Written against a value it is
// part of it, which is a field name nothing has and is reported as one.
func TestACommentMarkerWrittenAgainstAValue(t *testing.T) {
	_, reported := read(t, layer.Builtins(), naming("Collection"), "//forge:collection sort=Age// note")

	if !strings.Contains(reported, "Age//") {
		t.Errorf("the value was not read as written:\n%s", reported)
	}
}
