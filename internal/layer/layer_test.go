package layer_test

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/okian/forge/internal/diag"
	"github.com/okian/forge/internal/layer"
	"github.com/okian/forge/internal/model"
	"github.com/okian/forge/internal/shape"
)

// fake is a layer written for the tests, which is also the smallest proof that
// the interface can be implemented from outside the package that defines it.
type fake struct {
	origin  model.TypeRef
	kind    model.Kind
	options []layer.OptionDef
	accepts error
	adds    []shape.Cap
}

func (f fake) Origin() model.TypeRef           { return f.origin }
func (f fake) Kind() model.Kind                { return f.kind }
func (f fake) OptionSchema() []layer.OptionDef { return f.options }
func (f fake) Accepts(shape.Shape) error       { return f.accepts }
func (f fake) Shape(below shape.Shape) shape.Shape {
	below.Caps = below.Caps.With(f.adds...)
	return below
}

func (f fake) Generate(*layer.Context, shape.Shape) (layer.Unit, error) {
	return layer.Unit{}, nil
}

// marker builds a reference to a marker in the package forge's own markers live
// in, which is what the registry is keyed by.
func marker(name string) model.TypeRef {
	return model.TypeRef{Pkg: model.MarkerPkg, Name: name}
}

func TestRegistryLookup(t *testing.T) {
	registry := layer.New()
	registry.MustRegister(fake{origin: marker("Collection"), kind: model.KindRefining})

	// An instantiation finds the layer its generic was registered under, which
	// is the only form resolution ever has to look up.
	instantiated := marker("Collection")
	instantiated.Args = "[example.com/domain.Person]"

	for _, ref := range []model.TypeRef{marker("Collection"), instantiated} {
		found, ok := registry.Lookup(ref)
		if !ok {
			t.Fatalf("Lookup(%v) found nothing", ref)
		}
		if got, want := found.Kind(), model.KindRefining; got != want {
			t.Errorf("Lookup(%v).Kind() = %s, want %s", ref, got, want)
		}
	}

	if _, ok := registry.Lookup(marker("Nothing")); ok {
		t.Error("Lookup found a layer nobody registered")
	}
}

// A marker no layer claims is a diagnostic somewhere else, not a failure here.
func TestRegistryKindOfAnUnclaimedMarker(t *testing.T) {
	if got := layer.New().Kind(marker("Nothing")); got != model.KindInvalid {
		t.Errorf("Kind() = %s, want %s", got, model.KindInvalid)
	}
}

// Resolution produces a stack of origins and nothing else, because a walk over
// instantiations has no business knowing what a layer means. This is where the
// two meet.
func TestRegistryResolvesAStack(t *testing.T) {
	registry := layer.Builtins()

	stack := []model.LayerRef{
		{Origin: marker("Collection")},
		{Origin: marker("Ring")},
		{Origin: marker("Json")},
		{Origin: marker("Nothing")},
	}

	resolved := registry.Resolve(stack)

	want := []model.Kind{model.KindRefining, model.KindStorage, model.KindElement, model.KindInvalid}
	for i, kind := range want {
		if got := resolved[i].Kind; got != kind {
			t.Errorf("%s resolved to %s, want %s", resolved[i].Origin.Name, got, kind)
		}
	}

	// The stack it was given is untouched, so a caller can resolve twice and
	// compare, and nothing downstream mutates what it was handed.
	for _, ref := range stack {
		if ref.Kind != model.KindInvalid {
			t.Errorf("%s was written back into the caller's stack", ref.Origin.Name)
		}
	}
}

// Two layers claiming one marker means one of them never runs, and which one
// depends on registration order. Refusing is the only answer that is not a
// silent choice.
func TestRegistryRefusesTwoLayersForOneMarker(t *testing.T) {
	registry := layer.New()
	registry.MustRegister(fake{origin: marker("Ring"), kind: model.KindStorage})

	err := registry.Register(fake{origin: marker("Ring"), kind: model.KindRefining})
	if err == nil {
		t.Fatal("a second layer claimed a marker that was taken")
	}
	if !strings.Contains(err.Error(), "Ring") {
		t.Errorf("error %q does not name the marker", err)
	}
}

// Options are addressed by the marker's name alone, so two markers named alike
// in different packages would leave one directive meaning either of them. This
// is the only point at which that can be caught: by the time a directive is
// read it looks like an ordinary option.
func TestRegistryRefusesTwoMarkersAnsweringToOneDirective(t *testing.T) {
	registry := layer.New()
	registry.MustRegister(fake{origin: marker("Ring"), kind: model.KindStorage})

	err := registry.Register(fake{
		origin: model.TypeRef{Pkg: "example.com/other", Name: "Ring"},
		kind:   model.KindStorage,
	})
	if err == nil {
		t.Fatal("two markers were allowed to answer to one directive")
	}
	if !strings.Contains(err.Error(), "//forge:ring") {
		t.Errorf("error %q does not name the directive they share", err)
	}
}

// A layer claiming nothing would be registered under the zero reference and
// found by every lookup that missed.
func TestRegistryRefusesALayerWithNoMarker(t *testing.T) {
	if err := layer.New().Register(fake{}); err == nil {
		t.Fatal("a layer with no marker was registered")
	}
}

// A layer claims the generic, not one instantiation of it. Reducing the
// reference quietly would leave the registry keyed by one form and reporting
// another, and would hide a layer that thinks it claims Collection[Person] from
// every declaration that writes Collection of something else.
func TestRegistryRefusesALayerClaimingAnInstantiation(t *testing.T) {
	instantiated := marker("Collection")
	instantiated.Args = "[example.com/domain.Person]"

	err := layer.New().Register(fake{origin: instantiated, kind: model.KindRefining})
	if err == nil {
		t.Fatal("a layer claiming an instantiation was registered")
	}
	if !strings.Contains(err.Error(), "instantiation") {
		t.Errorf("error %q does not say what is wrong with it", err)
	}
}

// Registration happens once, before anything is loaded, and a binary with two
// layers claiming one marker generates the wrong code. Failing at the first
// test beats failing in someone's repository.
func TestMustRegisterPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("MustRegister returned on a duplicate")
		}
	}()

	registry := layer.New()
	registry.MustRegister(
		fake{origin: marker("Ring"), kind: model.KindStorage},
		fake{origin: marker("Ring"), kind: model.KindStorage},
	)
}

// Anything printed from a registry has to read the same way twice.
func TestRegistryAllIsOrdered(t *testing.T) {
	registry := layer.Builtins()

	all := registry.All()
	if got, want := len(all), registry.Len(); got != want {
		t.Fatalf("All() returned %d layers, want %d", got, want)
	}

	if !slices.IsSortedFunc(all, func(a, b layer.Layer) int {
		return a.Origin().Compare(b.Origin())
	}) {
		t.Error("All() is not ordered by the marker each layer claims")
	}
}

// Every layer forge ships knows what it is before it knows how to generate,
// which is what lets the stages that reason about layers be built first.
func TestBuiltinsAreCompleteAndClassified(t *testing.T) {
	registry := layer.Builtins()

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
	registry := layer.Builtins()

	for _, name := range []string{"Set", "LRU", "Index", "Heap", "Sorted", "Page",
		"Default", "Diff", "Fault", "Binary", "Atomic", "Csv"} {
		if _, ok := registry.Lookup(marker(name)); !ok {
			t.Errorf("no layer claims the staged marker %s", name)
		}
	}
}

// Generating from a stub is a diagnostic rather than a panic or an empty unit,
// because both of those lie about what happened.
func TestAStubReportsThatItGeneratesNothing(t *testing.T) {
	found, _ := layer.Builtins().Lookup(marker("Json"))

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
	if !strings.Contains(reported.Message, "Json") {
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
	registry := layer.Builtins()

	stub, _ := registry.Lookup(marker("Json"))
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
	collection, _ := layer.Builtins().Lookup(marker("Collection"))

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
	registry := layer.Builtins()

	ring, _ := registry.Lookup(marker("Ring"))
	guarded, _ := registry.Lookup(marker("Guarded"))

	stored := ring.Shape(shape.Shape{})
	if !stored.Caps.Has(shape.Streamable, shape.Bounded, shape.Sized, shape.Ordered) {
		t.Errorf("Ring exposes %s", stored.Caps)
	}

	locked := guarded.Shape(stored)
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

// An option written for a layer and not declared by it is an error rather than
// a warning, which only works if the declarations are complete.
func TestBuiltinOptionSchemas(t *testing.T) {
	registry := layer.Builtins()

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
	other, _ := layer.Builtins().Lookup(marker("Ring"))
	if other.OptionSchema()[1].Values[0] != "overwrite" {
		t.Error("a caller rewrote what every registry built afterwards accepts")
	}
}

func TestOptionDefString(t *testing.T) {
	cases := map[string]struct {
		def  layer.OptionDef
		want string
	}{
		"no value":    {layer.OptionDef{Key: "locked"}, "locked"},
		"closed set":  {layer.OptionDef{Key: "overflow", Value: layer.ValueEnum, Values: []string{"a", "b"}}, "overflow=a|b"},
		"a field":     {layer.OptionDef{Key: "key", Value: layer.ValueField}, "key=<field>"},
		"some fields": {layer.OptionDef{Key: "sort", Value: layer.ValueFields}, "sort=<fields>"},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := tc.def.String(); got != tc.want {
				t.Errorf("String() = %q, want %q", got, tc.want)
			}
		})
	}

	if got, want := layer.ValueKind(99).String(), "value(99)"; got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
}

// What a layer is for and how far along it is are not questions generation
// asks, and are the two a catalogue is made of. A layer that can answer them
// says so by implementing them; one written elsewhere is under no obligation.
func TestLayersDescribeThemselves(t *testing.T) {
	registry := layer.Builtins()

	stages := map[string]layer.Stage{
		"Ring": layer.StageStub,
		"Csv":  layer.StageStaged,
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

	// Every layer forge ships describes itself, since the catalogue is what
	// they are for until their generators exist.
	for _, found := range registry.All() {
		if _, ok := found.(layer.Described); !ok {
			t.Errorf("%s says nothing about itself", found.Origin().Name)
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

// A layer refusing a shape returns an ordinary error, so it composes with the
// error handling every caller already has.
func TestAcceptsComposesWithOrdinaryErrors(t *testing.T) {
	sentinel := errors.New("no")

	err := fake{accepts: sentinel}.Accepts(shape.Shape{})
	if !errors.Is(err, sentinel) {
		t.Errorf("Accepts() = %v, want the error it was given", err)
	}
}
