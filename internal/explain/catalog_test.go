package explain_test

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/okian/forge/internal/explain"
	"github.com/okian/forge/internal/layer"
	"github.com/okian/forge/internal/layers"
	"github.com/okian/forge/internal/model"
	"github.com/okian/forge/internal/shape"
)

// held returns the layer the catalog describes under a name, failing the test
// if the registry does not hold one.
func held(t *testing.T, c explain.Catalog, name string) explain.Registered {
	t.Helper()

	for _, one := range c.Layers {
		if one.Name == name {
			return one
		}
	}

	t.Fatalf("the catalog does not hold %s", name)
	return explain.Registered{}
}

// The catalog holds every layer the registry does, in the order the registry
// gives them.
//
// Ordered because it is printed: a listing that came back in a different order
// each run is one nobody can diff, and the registry is where that order is
// already decided.
func TestTheCatalogHoldsWhatTheRegistryHolds(t *testing.T) {
	registry := layers.Builtins()
	got := explain.Layers(registry)

	if len(got.Layers) != registry.Len() {
		t.Fatalf("the catalog describes %d layers and the registry holds %d", len(got.Layers), registry.Len())
	}

	for i, one := range registry.All() {
		if got.Layers[i].Name != one.Origin().Name {
			t.Errorf("layer %d is %s, and the registry's is %s", i, got.Layers[i].Name, one.Origin().Name)
		}
	}

	if explain.Layers(nil).Layers != nil {
		t.Error("a catalog of no registry describes something")
	}
}

// What a layer requires, adds and masks is asked of the layer, so the answer is
// the one composition will give.
//
// Checked against layers whose answers are known from the other end: a
// collection needs to be able to walk what it sits on and contributes no
// capability of its own; a ring contributes four and needs none; a lock takes
// iteration away, which is the only reason withdrawal is in the vocabulary.
func TestWhatTheCatalogSaysAboutAShape(t *testing.T) {
	got := explain.Layers(layers.Builtins())

	cases := map[string]struct{ requires, adds, masks []string }{
		"Collection": {requires: []string{"Streamable"}},
		"Ring":       {adds: []string{"Sized", "Ordered", "Streamable", "Bounded"}},
		"Guarded":    {requires: []string{"Streamable"}, adds: []string{"Concurrent"}, masks: []string{"Indexed", "Streamable"}},
	}

	for name, want := range cases {
		t.Run(name, func(t *testing.T) {
			one := held(t, got, name)

			for _, pair := range []struct {
				what string
				got  []string
				want []string
			}{
				{"requires", one.Requires, want.requires},
				{"adds", one.Adds, want.adds},
				{"masks", one.Masks, want.masks},
			} {
				if !same(pair.got, pair.want) {
					t.Errorf("%s %v, want %v", pair.what, pair.got, pair.want)
				}
			}
		})
	}
}

// same reports whether two lists hold the same names, whatever order they came
// back in.
func same(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}

	for _, one := range want {
		if !contains(got, one) {
			return false
		}
	}
	return true
}

func contains(of []string, name string) bool {
	for _, one := range of {
		if one == name {
			return true
		}
	}
	return false
}

// Where a declaration may be written comes from the layer, since that is the
// question a reader of the listing is answering.
//
// Getting it backwards is the worst thing this can print. A layer whose
// invariants a raw write would break, listed as writable in an ordinary file,
// sends somebody to write one — and what they get is a value corrupted at run
// time with nothing to point at.
func TestWhereTheCatalogSaysADeclarationGoes(t *testing.T) {
	got := explain.Layers(layers.Builtins())

	if !held(t, got, "Slice").Transparent {
		t.Error("a slice is reported as needing a spec file")
	}
	if held(t, got, "Ring").Transparent {
		t.Error("a ring is reported as writable in an ordinary file")
	}
}

// Each cell of a rendered row holds what the column above it says it does.
//
// Read as a row rather than as text somewhere in the output. A listing checked
// by looking for a word anywhere in it passes when two columns are swapped,
// when a stage is printed under the wrong name, and when the mark for "no
// default" is printed where a default was — each of which is the table telling
// somebody something untrue, in the confident voice of a table.
func TestWhatARenderedRowHolds(t *testing.T) {
	registry := layer.New()
	registry.MustRegister(sample{})

	var b strings.Builder
	if err := explain.Layers(registry).Text(&b); err != nil {
		t.Fatalf("rendering: %v", err)
	}
	got := b.String()

	for _, want := range []string{
		// Layer, kind, stage, where a declaration goes, and what it is for.
		"Sample  storage  stub  spec file  what a sample is for",

		// And the shape it works in, in the order the heading names.
		"Sample  Structured  Sized  Ordered",

		// And each option, where it is written, and what it is worth unwritten.
		"Sample  choice=one|two  the declaration  one  which one",
		"Sample  marked=<string>  a field  required  which field",
	} {
		if !strings.Contains(collapsed(got), collapsed(want)) {
			t.Errorf("the tables hold no row %q:\n%s", want, got)
		}
	}
}

// collapsed squeezes the runs of spaces a table aligns with, so that a row can
// be written down without counting them.
func collapsed(of string) string { return strings.Join(strings.Fields(of), " ") }

// sample is a layer with a different answer to every question the tables ask,
// so that a cell in the wrong column holds something visibly wrong.
type sample struct{}

func (sample) Binds() []model.Import { return nil }
func (sample) Origin() model.TypeRef { return model.TypeRef{Pkg: model.MarkerPkg, Name: "Sample"} }
func (sample) Kind() model.Kind      { return model.KindStorage }
func (sample) Stage() layer.Stage    { return layer.StageStub }
func (sample) Doc() string           { return "what a sample is for" }

func (sample) Accepts(below shape.Shape) error {
	if !below.Caps.Has(shape.Structured) {
		return errNotStructured
	}
	return nil
}

func (sample) Shape(_ *layer.Context, below shape.Shape) shape.Shape {
	below.Caps = below.Caps.With(shape.Sized).Without(shape.Ordered)
	return below
}

func (sample) OptionSchema() []layer.OptionDef {
	return []layer.OptionDef{
		{Key: "choice", Value: layer.ValueEnum, Values: []string{"one", "two"}, Default: "one", Doc: "which one"},
		{Key: "marked", Value: layer.ValueString, Scope: layer.ScopeField, Required: true, Doc: "which field"},
	}
}

func (sample) Generate(*layer.Context, shape.Shape) (layer.Unit, error) {
	return layer.Unit{}, nil
}

// errNotStructured is what a layer that needs a structured subject says when it
// is not given one.
var errNotStructured = errors.New("a sample needs a subject with fields")

// Every option a layer accepts is listed, with what it is worth when nobody
// writes it.
func TestWhatTheCatalogSaysAboutOptions(t *testing.T) {
	one := held(t, explain.Layers(layers.Builtins()), "Ring")

	if len(one.Options) != 2 {
		t.Fatalf("a ring accepts %d options", len(one.Options))
	}

	overflow := one.Options[1]
	if overflow.Key != "overflow" {
		t.Errorf("the second option is %s", overflow.Key)
	}
	if overflow.Written != "overflow=overwrite|error" {
		t.Errorf("it is written %q", overflow.Written)
	}
	if !same(overflow.Values, []string{"overwrite", "error"}) {
		t.Errorf("it accepts %v", overflow.Values)
	}
	if overflow.Default != "overwrite" {
		t.Errorf("its default is %q", overflow.Default)
	}
}

// A layer that answers nothing is described rather than skipped, and nothing it
// does not answer is invented for it.
//
// The layers forge ships all answer; one written elsewhere need not, and a
// listing that dropped it would be a listing of the layers forge happens to
// have written rather than of the ones a declaration may name.
func TestACatalogEntryForALayerThatSaysNothing(t *testing.T) {
	registry := layer.New()
	registry.MustRegister(unspoken{})

	one := held(t, explain.Layers(registry), "Quiet")

	if one.Kind != model.KindStorage.String() {
		t.Errorf("its kind is %q", one.Kind)
	}
	if one.Stage != "ready" {
		t.Errorf("a layer with no roadmap is staged as %q, and forge has no answer to give for it", one.Stage)
	}
	if one.Doc == "" {
		t.Error("it is described by nothing at all")
	}
	if len(one.Options) != 0 {
		t.Errorf("it accepts %v", one.Options)
	}
}

// unspoken is a layer from outside forge: it answers what generation needs and
// nothing about forge's own roadmap.
type unspoken struct{}

func (unspoken) Binds() []model.Import { return nil }
func (unspoken) Origin() model.TypeRef { return model.TypeRef{Pkg: model.MarkerPkg, Name: "Quiet"} }
func (unspoken) Kind() model.Kind      { return model.KindStorage }
func (unspoken) Accepts(shape.Shape) error {
	return nil
}
func (unspoken) Shape(_ *layer.Context, below shape.Shape) shape.Shape { return below }
func (unspoken) OptionSchema() []layer.OptionDef                       { return nil }
func (unspoken) Generate(*layer.Context, shape.Shape) (layer.Unit, error) {
	return layer.Unit{}, nil
}

// What a program reads back out of the document. Named rather than written
// where it is read, because these fields are the interface: a test that spelled
// them inline would spell them again wherever the next one reads them.
type document struct {
	Layers []documentLayer `json:"layers"`
}

type documentLayer struct {
	Name     string           `json:"name"`
	Requires []string         `json:"requires"`
	Adds     []string         `json:"adds"`
	Masks    []string         `json:"masks"`
	Options  []documentOption `json:"options"`
}

type documentOption struct {
	Key    string   `json:"key"`
	Values []string `json:"values"`
}

// The document a program reads carries every field, present even when empty.
//
// A tool should not have to tell "requires nothing" from "the field was
// omitted", and somebody diffing two runs should get a diff of what changed
// rather than of which fields happened to be written.
func TestTheCatalogAsADocument(t *testing.T) {
	var b strings.Builder
	if err := explain.Layers(layers.Builtins()).JSON(&b); err != nil {
		t.Fatalf("rendering: %v", err)
	}

	var got document
	if err := json.Unmarshal([]byte(b.String()), &got); err != nil {
		t.Fatalf("reading it back: %v\n%s", err, b.String())
	}

	if len(got.Layers) == 0 {
		t.Fatal("the document holds no layers")
	}

	for _, one := range got.Layers {
		for _, pair := range []struct {
			what string
			held []string
		}{{"requires", one.Requires}, {"adds", one.Adds}, {"masks", one.Masks}} {
			if pair.held == nil {
				t.Errorf("%s has a null %s rather than an empty one", one.Name, pair.what)
			}
		}
	}

	// And the parts of an option a program would otherwise take back apart.
	for _, one := range got.Layers {
		if one.Name != "Ring" {
			continue
		}
		for _, opt := range one.Options {
			if opt.Key == "overflow" && !same(opt.Values, []string{"overwrite", "error"}) {
				t.Errorf("overflow accepts %v", opt.Values)
			}
		}
	}
}

// A layer that cannot be asked is described by what it did answer, rather than
// taking the listing down with it.
//
// A layer is the part of this somebody outside forge writes, so a listing that
// ended in a stack trace would tell a reader nothing about the other twenty
// — and the reader most likely to be running it is the one whose layer is the
// broken one.
func TestALayerThatCannotBeAsked(t *testing.T) {
	cases := map[string]layer.Layer{
		"one that will not say what it accepts": awkward{refuses: true},
		"one that will not say what it exposes": awkward{silent: true},
		"one that will not say either":          awkward{refuses: true, silent: true},
	}

	for name, one := range cases {
		t.Run(name, func(t *testing.T) {
			registry := layer.New()
			registry.MustRegister(one)

			got := held(t, explain.Layers(registry), "Awkward")

			if got.Name != "Awkward" {
				t.Errorf("it is described as %q", got.Name)
			}
			if got.Kind != model.KindStorage.String() {
				t.Errorf("its kind is %q", got.Kind)
			}
		})
	}
}

// A layer that refuses every shape is reported as not having answered, rather
// than as needing nothing.
//
// It refuses for a reason this cannot see — it reads the surface beneath it, or
// it is a marker forge has not committed to — and every answer taken from a
// refusal is then wrong: needing nothing is wrong, and needing all ten is
// wrong. Saying so is the only true thing available.
func TestALayerThatRefusesEveryShape(t *testing.T) {
	for name, one := range map[string]layer.Layer{
		"one that says no":      stubborn{},
		"one that will not say": awkward{refuses: true},
	} {
		t.Run(name, func(t *testing.T) {
			registry := layer.New()
			registry.MustRegister(one)

			got := held(t, explain.Layers(registry), one.Origin().Name)

			if got.Probed {
				t.Error("it is reported as having answered")
			}
			if len(got.Requires) != 0 {
				t.Errorf("it is reported as needing %v", got.Requires)
			}

			// And the table says so rather than showing the mark that means an
			// empty answer, which is the whole point of telling them apart.
			var b strings.Builder
			if err := explain.Layers(registry).Text(&b); err != nil {
				t.Fatalf("rendering: %v", err)
			}
			if !strings.Contains(b.String(), "?") {
				t.Errorf("the table does not say the answer is unknown:\n%s", b.String())
			}
		})
	}
}

// stubborn is a layer that refuses every shape by saying so, rather than by
// falling over.
type stubborn struct{}

func (stubborn) Binds() []model.Import { return nil }
func (stubborn) Origin() model.TypeRef { return model.TypeRef{Pkg: model.MarkerPkg, Name: "Stubborn"} }
func (stubborn) Kind() model.Kind      { return model.KindDecorator }
func (stubborn) Accepts(shape.Shape) error {
	return errNotStructured
}
func (stubborn) Shape(_ *layer.Context, below shape.Shape) shape.Shape { return below }
func (stubborn) OptionSchema() []layer.OptionDef                       { return nil }
func (stubborn) Generate(*layer.Context, shape.Shape) (layer.Unit, error) {
	return layer.Unit{}, nil
}

// An option that has to be written says so where its default would go, since
// that is the column somebody reads to find out whether they can leave it out.
func TestAnOptionThatHasToBeWritten(t *testing.T) {
	registry := layer.New()
	registry.MustRegister(awkward{demands: true})

	one := held(t, explain.Layers(registry), "Awkward")
	if len(one.Options) != 1 || !one.Options[0].Required {
		t.Fatalf("it accepts %v", one.Options)
	}

	var b strings.Builder
	if err := explain.Layers(registry).Text(&b); err != nil {
		t.Fatalf("rendering: %v", err)
	}
	if !strings.Contains(b.String(), "required") {
		t.Errorf("the table does not say the option has to be written:\n%s", b.String())
	}
}

// awkward is a layer that will not answer the questions a listing asks, in
// whichever combination a case wants.
type awkward struct {
	refuses bool
	silent  bool
	demands bool
}

func (awkward) Binds() []model.Import { return nil }
func (awkward) Origin() model.TypeRef { return model.TypeRef{Pkg: model.MarkerPkg, Name: "Awkward"} }
func (awkward) Kind() model.Kind      { return model.KindStorage }

func (a awkward) Accepts(shape.Shape) error {
	if a.refuses {
		panic("this layer will not say")
	}
	return nil
}

func (a awkward) Shape(_ *layer.Context, below shape.Shape) shape.Shape {
	if a.silent {
		panic("this layer will not say")
	}
	below.Caps = below.Caps.With(shape.Sized)
	return below
}

func (a awkward) OptionSchema() []layer.OptionDef {
	if !a.demands {
		return nil
	}
	return []layer.OptionDef{{
		Key: "key", Value: layer.ValueString, Required: true,
		Doc: "the field this is about",
	}}
}

func (awkward) Generate(*layer.Context, shape.Shape) (layer.Unit, error) {
	return layer.Unit{}, nil
}

// A catalogue where nothing is configurable has no table of options, rather
// than a heading over nothing.
func TestACatalogWithNothingToConfigure(t *testing.T) {
	registry := layer.New()
	registry.MustRegister(unspoken{})

	var b strings.Builder
	if err := explain.Layers(registry).Text(&b); err != nil {
		t.Fatalf("rendering: %v", err)
	}

	if strings.Contains(b.String(), "Options") {
		t.Errorf("a catalogue with no options has a table of them:\n%s", b.String())
	}
}
