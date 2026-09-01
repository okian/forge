package layer_test

import (
	"errors"
	"slices"
	"strings"
	"testing"

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
func (f fake) Shape(_ *layer.Context, below shape.Shape) shape.Shape {
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
	registry := layer.New()
	registry.MustRegister(
		fake{origin: marker("Collection"), kind: model.KindRefining},
		fake{origin: marker("Ring"), kind: model.KindStorage},
		fake{origin: marker("Json"), kind: model.KindElement},
	)

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

// A layer that reports no kind is refused, rather than registered and then
// ignored.
//
// Every rule about the shape of a stack is written in kinds — which may sit
// where, how many of each — so a layer that reports none is invisible to all of
// them. Worse than invisible: a container that forgot to say it was one leaves
// a decorator above it told there is nothing beneath it to wrap, which names
// the wrong layer to whoever wrote the declaration. The zero value is the
// natural way to arrive here, so it is the one worth refusing where the answer
// is a line in the layer.
func TestRegistryRefusesALayerWithNoKind(t *testing.T) {
	err := layer.New().Register(fake{origin: marker("Collection")})
	if err == nil {
		t.Fatal("a layer reporting no kind was registered")
	}
	if !strings.Contains(err.Error(), "kind") {
		t.Errorf("error %q does not say what is wrong with it", err)
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
	registry := layer.New()
	registry.MustRegister(
		fake{origin: marker("Ring"), kind: model.KindStorage},
		fake{origin: marker("Collection"), kind: model.KindRefining},
		fake{origin: marker("Json"), kind: model.KindElement},
	)

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

// A layer refusing a shape returns an ordinary error, so it composes with the
// error handling every caller already has.
func TestAcceptsComposesWithOrdinaryErrors(t *testing.T) {
	sentinel := errors.New("no")

	err := fake{accepts: sentinel}.Accepts(shape.Shape{})
	if !errors.Is(err, sentinel) {
		t.Errorf("Accepts() = %v, want the error it was given", err)
	}
}

// A layer may not take a directive forge answers itself.
//
// The two live in one flat namespace: what a word after //forge: means is
// decided by looking it up, and a layer called Skip would answer to the word
// that turns a claim off. Nothing downstream could tell which was meant, so the
// two would be told apart by whichever lookup ran first — which is a coin toss
// written into a build.
func TestRegistryRefusesALayerNamedForAReservedDirective(t *testing.T) {
	for _, directive := range model.ReservedDirectives() {
		claimed := marker(strings.ToUpper(directive[:1]) + directive[1:])

		err := layer.New().Register(fake{origin: claimed, kind: model.KindElement})
		if err == nil {
			t.Fatalf("a layer answering to //forge:%s was registered", directive)
		}
		if !strings.Contains(err.Error(), directive) {
			t.Errorf("error %q does not name the directive it collides with", err)
		}
	}
}
