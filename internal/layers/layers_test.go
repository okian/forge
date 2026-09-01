package layers_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/okian/forge/internal/diag"
	"github.com/okian/forge/internal/layer"
	"github.com/okian/forge/internal/layers"
	"github.com/okian/forge/internal/model"
	"github.com/okian/forge/internal/shape"
)

// marker builds a reference to a marker in the package forge's own markers live
// in, which is what the registry is keyed by.
func marker(name string) model.TypeRef {
	return model.TypeRef{Pkg: model.MarkerPkg, Name: name}
}

// fake is a layer from outside the catalog, which is what a check that the
// catalog demands nothing of a third party is written against.
type fake struct {
	origin model.TypeRef
	kind   model.Kind
}

func (f fake) Origin() model.TypeRef           { return f.origin }
func (f fake) Kind() model.Kind                { return f.kind }
func (f fake) OptionSchema() []layer.OptionDef { return nil }
func (f fake) Accepts(shape.Shape) error       { return nil }
func (f fake) Shape(_ *layer.Context, below shape.Shape) shape.Shape {
	return below
}

func (f fake) Generate(*layer.Context, shape.Shape) (layer.Unit, error) {
	return layer.Unit{}, nil
}

// Every layer forge ships knows what it is before it knows how to generate,
// which is what lets the stages that reason about layers be built first.
func TestBuiltinsAreCompleteAndClassified(t *testing.T) {
	registry := layers.Builtins()

	v1 := map[string]model.Kind{
		"Slice":      model.KindStorage,
		"Ring":       model.KindStorage,
		"Collection": model.KindRefining,
		"Json":       model.KindElement,
		"Validate":   model.KindElement,
		"Clone":      model.KindElement,
		"Hash":       model.KindElement,
		"Builder":    model.KindElement,
		"Patch":      model.KindElement,
		"Redact":     model.KindElement,
		"Enum":       model.KindElement,
		"Guarded":    model.KindDecorator,
	}

	for name, kind := range v1 {
		found, ok := registry.Lookup(marker(name))
		if !ok {
			t.Errorf("no layer claims %s", name)
			continue
		}
		if got := found.Kind(); got != kind {
			t.Errorf("%s is a %s, want a %s", name, got, kind)
		}
		if !found.Kind().Valid() {
			t.Errorf("%s has no kind", name)
		}
	}
}

// A staged marker is declared so that a declaration naming it type-checks. It
// has to be claimed too, or the answer to naming one is silence about a marker
// forge plainly ships.
func TestStagedMarkersAreClaimedToo(t *testing.T) {
	registry := layers.Builtins()

	for _, name := range []string{
		"Set", "LRU", "Index", "Heap", "Sorted", "Page",
		"Default", "Diff", "Fault", "Binary", "Atomic", "Csv",
	} {
		if _, ok := registry.Lookup(marker(name)); !ok {
			t.Errorf("no layer claims the staged marker %s", name)
		}
	}
}

// Generating from a stub is a diagnostic rather than a panic or an empty unit,
// because both of those lie about what happened.
func TestAStubReportsThatItGeneratesNothing(t *testing.T) {
	found, _ := layers.Builtins().Lookup(marker("Builder"))

	unit, err := found.Generate(&layer.Context{
		Model: &model.Model{Name: "Persons"},
	}, shape.Shape{})

	if err == nil {
		t.Fatal("a stub generated without complaint")
	}
	if len(unit.Decls) != 0 {
		t.Errorf("a stub returned %d declarations", len(unit.Decls))
	}

	reported, ok := diag.From(err)
	if !ok {
		t.Fatalf("error %v is not a diagnostic", err)
	}
	if got, want := reported.Code.String(), "FRG4900"; got != want {
		t.Errorf("code is %s, want %s", got, want)
	}
	if !strings.Contains(reported.Message, "Builder") {
		t.Errorf("message %q does not name the layer", reported.Message)
	}
	if reported.Hint == "" {
		t.Error("the diagnostic carries no hint")
	}
}

// The hint distinguishes a layer whose generator is merely unwritten from a
// marker forge has not committed to, because what an author can do about the
// two is different.
func TestAStagedLayerSaysSoDifferently(t *testing.T) {
	registry := layers.Builtins()

	stub, _ := registry.Lookup(marker("Validate"))
	staged, _ := registry.Lookup(marker("Csv"))

	_, stubErr := stub.Generate(nil, shape.Shape{})
	_, stagedErr := staged.Generate(nil, shape.Shape{})

	first, _ := diag.From(stubErr)
	second, _ := diag.From(stagedErr)

	if first.Hint == second.Hint {
		t.Errorf("both hints read %q", first.Hint)
	}
	if !strings.Contains(second.Hint, "not in this release") {
		t.Errorf("the staged hint %q does not say the layer is absent", second.Hint)
	}
	// Generating without a context is a programming error, not a crash: the
	// diagnostic simply has nowhere to point.
	if first.Pos.Line != 0 {
		t.Errorf("a generate with no model reported at %s", first.Pos)
	}
}

// A layer says what it needs of the stack beneath it, and what it says is a
// sentence rather than a code: it is handed a shape and knows nothing about the
// declaration that produced it.
func TestALayerRefusesAShapeItCannotSitOn(t *testing.T) {
	collection, _ := layers.Builtins().Lookup(marker("Collection"))

	err := collection.Accepts(shape.Shape{Caps: shape.Set(shape.Sized)})
	if err == nil {
		t.Fatal("Collection accepted a stack with nothing to stream over")
	}
	if _, ok := diag.From(err); ok {
		t.Error("the error carries a diagnostic, which would carry a position the layer cannot know")
	}
	if !strings.Contains(err.Error(), "Streamable") {
		t.Errorf("error %q does not name what is missing", err)
	}

	if err := collection.Accepts(shape.Shape{Caps: shape.Set(shape.Streamable)}); err != nil {
		t.Errorf("Collection refused a streamable stack: %v", err)
	}
}

// What a layer exposes upward is what was beneath it, plus what it adds, minus
// what it takes away — and a lock takes iteration away, which is the whole
// reason withdrawal is in the vocabulary.
func TestALayerReportsTheShapeItExposes(t *testing.T) {
	registry := layers.Builtins()

	ring, _ := registry.Lookup(marker("Ring"))
	guarded, _ := registry.Lookup(marker("Guarded"))

	stored := ring.Shape(nil, shape.Shape{})
	if !stored.Caps.Has(shape.Streamable, shape.Bounded, shape.Sized, shape.Ordered) {
		t.Errorf("Ring exposes %s", stored.Caps)
	}

	locked := guarded.Shape(nil, stored)
	if !locked.Caps.Has(shape.Concurrent) {
		t.Errorf("Guarded exposes %s, want it concurrent", locked.Caps)
	}
	if locked.Caps.Has(shape.Streamable) {
		t.Errorf("Guarded exposes %s, want iteration withdrawn", locked.Caps)
	}
	// Withdrawing is not forgetting: what the lock did not touch is still there.
	if !locked.Caps.Has(shape.Sized) {
		t.Errorf("Guarded exposes %s, want the rest of the stack kept", locked.Caps)
	}
}

// A lock takes the methods away as well as the capability.
//
// The capability is what the layers above are checked against; the methods are
// what a caller reaches and what a reader is shown. A lock that withdrew only
// the first would leave the declared type listing a walk that a caller could
// still call, over data the lock exists to protect — and the shape saying so is
// the only place anything would notice before the race did.
func TestALockTakesTheWalkAwayAndNotOnlyTheCapability(t *testing.T) {
	registry := layers.Builtins()

	storage, _ := registry.Lookup(marker("Slice"))
	guarded, _ := registry.Lookup(marker("Guarded"))

	stored := storage.Shape(nil, shape.Shape{})
	if !slices.Contains(stored.Names(), "All") {
		t.Fatalf("the storage beneath exposes %v, and there is no walk to withdraw", stored.Names())
	}

	locked := guarded.Shape(nil, stored)
	for _, gone := range []string{"All", "Backward", "AppendSeq"} {
		if _, found := locked.Method(gone); found {
			t.Errorf("Guarded exposes %v, and %s reaches the data the lock protects",
				locked.Names(), gone)
		}
	}

	// The cheap read is not iteration and stays, which is what makes this a
	// withdrawal rather than a clearing.
	if _, found := locked.Method("Len"); !found {
		t.Errorf("Guarded exposes %v, want what it did not withdraw kept", locked.Names())
	}
}

// An option written for a layer and not declared by it is an error rather than
// a warning, which only works if the declarations are complete.
func TestBuiltinOptionSchemas(t *testing.T) {
	registry := layers.Builtins()

	ring, _ := registry.Lookup(marker("Ring"))
	schema := ring.OptionSchema()

	keys := make([]string, len(schema))
	for i, def := range schema {
		keys[i] = def.Key
	}
	if want := []string{"cap", "overflow"}; !slices.Equal(keys, want) {
		t.Fatalf("Ring accepts %v, want %v", keys, want)
	}

	// A ring whose capacity is not declared takes it at construction, which is
	// the form the worked example uses, so requiring the option would make that
	// example an error.
	if schema[0].Required {
		t.Error("cap is required, which would make the directive-free worked example an error")
	}
	if got, want := schema[0].String(), "cap=<int>"; got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
	if got, want := schema[1].String(), "overflow=overwrite|error"; got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
	if got, want := schema[1].Default, "overwrite"; got != want {
		t.Errorf("the default is %q, want %q", got, want)
	}

	// The schema is the layer's, and a caller that edits what it was handed must
	// not be editing the layer. The accepted values matter more than the key
	// here: they are behind a second pointer, so a copy of the slice alone
	// leaves them shared with a catalog every registry is built from — and one
	// caller sorting them in place would rewrite what forge accepts for the
	// rest of the process.
	schema[0].Key = "edited"
	schema[1].Values[0] = "edited"

	fresh := ring.OptionSchema()
	if fresh[0].Key != "cap" {
		t.Error("a caller rewrote the layer's own option name")
	}
	if fresh[1].Values[0] != "overwrite" {
		t.Errorf("a caller rewrote the layer's own accepted values: %v", fresh[1].Values)
	}
	// And the catalog behind it, not only this registry's view of it.
	other, _ := layers.Builtins().Lookup(marker("Ring"))
	if other.OptionSchema()[1].Values[0] != "overwrite" {
		t.Error("a caller rewrote what every registry built afterwards accepts")
	}
}

// What a layer is for and how far along it is are not questions generation
// asks, and are the two a catalogue is made of. A layer that can answer them
// says so by implementing them; one written elsewhere is under no obligation.
func TestLayersDescribeThemselves(t *testing.T) {
	registry := layers.Builtins()

	stages := map[string]layer.Stage{
		"Slice": layer.StageReady,
		"Json":  layer.StageReady,
		"Hash":  layer.StageReady,
		"Patch": layer.StageStub,
		"Csv":   layer.StageStaged,
	}

	for name, want := range stages {
		found, _ := registry.Lookup(marker(name))

		described, ok := found.(layer.Described)
		if !ok {
			t.Fatalf("%s says nothing about itself", name)
		}
		if got := described.Stage(); got != want {
			t.Errorf("%s is %s, want %s", name, got, want)
		}
		if described.Doc() == "" {
			t.Errorf("%s has no summary", name)
		}
	}

	// Every layer forge ships describes itself, in one line, because that line
	// is what the list command prints and what explain reports as a step's
	// effect. A layer with none reports "pending" about work that is done.
	for _, found := range registry.All() {
		described, ok := found.(layer.Described)
		if !ok {
			t.Errorf("%s says nothing about itself", found.Origin().Name)
			continue
		}
		if described.Doc() == "" {
			t.Errorf("%s has no summary", found.Origin().Name)
		}
	}

	// A layer written elsewhere is under no such obligation.
	if _, ok := any(fake{}).(layer.Described); ok {
		t.Error("a layer from outside was required to describe itself")
	}

	if got, want := layer.Stage(99).String(), "stage(99)"; got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
	if got, want := layer.StageReady.String(), "ready"; got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
}

// The catalog's field-scoped options are the ones that say "not this one".
//
// Each marks an exception to what the layer otherwise does — the codec's
// reflective boundary, the copy's shared reference, the field that is not part
// of what a value is — and an exception is per field by nature: turning
// reflection on for every field at once, or sharing every reference at once, is
// the opposite of marking one out. Anything a layer does uniformly belongs on
// the declaration, and this is what keeps the two kinds from drifting into each
// other.
func TestWhichOptionsAreAboutFields(t *testing.T) {
	var about []string

	for _, l := range layers.Builtins().All() {
		for _, def := range l.OptionSchema() {
			if def.Scope == layer.ScopeField {
				about = append(about, l.Origin().Name+"."+def.Key)
			}
		}
	}

	want := []string{"Clone.aliasing", "Hash.ignore", "Json.fallback"}
	if strings.Join(about, ", ") != strings.Join(want, ", ") {
		t.Errorf("the options about fields are %v, want %v", about, want)
	}
}

// An option a layer cannot generate without, and that belongs on a field, would
// be demanded in the one place it is refused. Nothing in the catalog does that,
// and a layer that did could not be configured at all.
func TestNoOptionIsBothRequiredAndAboutAField(t *testing.T) {
	for _, l := range layers.Builtins().All() {
		for _, def := range l.OptionSchema() {
			if def.Required && def.Scope == layer.ScopeField {
				t.Errorf("%s.%s is required and belongs on a field, so it can be neither written nor left out",
					l.Origin().Name, def.Key)
			}
		}
	}
}

// A schema is copied on the way out, so a caller reading one cannot change what
// forge accepts for the rest of the process — including the scope.
func TestAScopeSurvivesBeingRead(t *testing.T) {
	json, ok := layers.Builtins().Lookup(model.TypeRef{Pkg: model.MarkerPkg, Name: "Json"})
	if !ok {
		t.Fatal("the catalog has no Json layer")
	}

	first := json.OptionSchema()
	for i := range first {
		first[i].Scope = layer.ScopeDeclaration
	}

	for _, def := range json.OptionSchema() {
		if def.Key == "fallback" && def.Scope != layer.ScopeField {
			t.Error("a caller rewrote what the catalog accepts")
		}
	}
}
