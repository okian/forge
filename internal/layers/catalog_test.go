package layers_test

import (
	"slices"
	"testing"

	"github.com/okian/forge/internal/layer"
	"github.com/okian/forge/internal/layers"
	"github.com/okian/forge/internal/model"
	"github.com/okian/forge/internal/shape"
)

// entry is one row of the catalog, written out as the documentation writes it.
type entry struct {
	kind        model.Kind
	stage       layer.Stage
	transparent bool
	requires    []shape.Cap
	adds        []shape.Cap
	masks       []shape.Cap

	// writes names what the layer puts on the subject, which is what a
	// neighbour declaration's output reads it by.
	writes []string
}

// catalog is every layer forge knows about and everything it declares about
// itself.
//
// The data is the whole of what this package delivers until generators exist,
// so it is written down twice on purpose: once as the thing under test and once
// here, from the documentation, so that a row edited in one place and not the
// other fails rather than redefines what forge is.
var catalog = map[string]entry{
	"Slice": {
		kind: model.KindStorage, stage: layer.StageReady, transparent: true,
		adds: []shape.Cap{shape.Sized, shape.Ordered, shape.Indexed, shape.Streamable},
	},
	"Ring": {
		kind: model.KindStorage, stage: layer.StageReady,
		adds: []shape.Cap{shape.Sized, shape.Ordered, shape.Streamable, shape.Bounded},
	},
	"Collection": {
		kind: model.KindRefining, stage: layer.StageReady, transparent: true,
		requires: []shape.Cap{shape.Streamable},
	},
	"Json": {
		kind: model.KindElement, stage: layer.StageReady,
		requires: []shape.Cap{shape.Structured}, adds: []shape.Cap{shape.Encodable},
		writes: []string{"AppendJSON", "MarshalJSON", "UnmarshalJSON", "UnmarshalJSONBorrowed"},
	},
	"Validate": {
		kind: model.KindElement, stage: layer.StageReady,
		requires: []shape.Cap{shape.Structured},
		writes:   []string{"Validate"},
	},
	"Clone": {kind: model.KindElement, stage: layer.StageReady, writes: []string{"Clone"}},
	"Hash": {
		kind: model.KindElement, stage: layer.StageReady,
		adds: []shape.Cap{shape.Comparable}, writes: []string{"Hash"},
	},
	"Builder": {
		kind: model.KindElement, stage: layer.StageReady,
		requires: []shape.Cap{shape.Structured},
	},
	"Patch": {
		kind: model.KindElement, stage: layer.StageReady,
		requires: []shape.Cap{shape.Structured},
	},
	"Redact": {
		kind: model.KindElement, stage: layer.StageReady,
		requires: []shape.Cap{shape.Structured},
		writes:   []string{"LogValue"},
	},
	"Enum": {
		kind: model.KindElement, stage: layer.StageReady,
		adds: []shape.Cap{shape.Comparable, shape.Encodable},
		// The text codec is the half a neighbour's codec reads it by; the
		// parser and the list of members are functions rather than methods.
		writes: []string{"String", "Valid", "MarshalText", "AppendText", "UnmarshalText"},
	},
	"Guarded": {
		kind: model.KindDecorator, stage: layer.StageReady,
		requires: []shape.Cap{shape.Streamable},
		adds:     []shape.Cap{shape.Concurrent},
		masks:    []shape.Cap{shape.Streamable, shape.Indexed},
	},
	"Map": {
		kind: model.KindBridge, stage: layer.StageReady,
	},

	"Set": {
		kind: model.KindStorage, stage: layer.StageStaged,
		requires: []shape.Cap{shape.Comparable}, adds: []shape.Cap{shape.Sized, shape.Streamable},
	},
	"LRU": {
		kind: model.KindStorage, stage: layer.StageStaged,
		requires: []shape.Cap{shape.Keyed}, adds: []shape.Cap{shape.Sized, shape.Bounded, shape.Streamable},
	},
	"Index": {
		kind: model.KindStorage, stage: layer.StageStaged,
		requires: []shape.Cap{shape.Keyed}, adds: []shape.Cap{shape.Sized, shape.Indexed, shape.Streamable},
	},
	"Heap": {
		kind: model.KindStorage, stage: layer.StageStaged,
		requires: []shape.Cap{shape.Keyed}, adds: []shape.Cap{shape.Sized, shape.Streamable},
	},
	"Sorted": {
		kind: model.KindRefining, stage: layer.StageStaged,
		requires: []shape.Cap{shape.Ordered},
	},
	"Page": {
		kind: model.KindRefining, stage: layer.StageStaged,
		requires: []shape.Cap{shape.Sized, shape.Ordered},
	},
	"Default": {
		kind: model.KindElement, stage: layer.StageStaged,
		requires: []shape.Cap{shape.Structured},
	},
	"Diff": {
		kind: model.KindElement, stage: layer.StageStaged,
		requires: []shape.Cap{shape.Structured},
	},
	"Fault": {kind: model.KindElement, stage: layer.StageStaged},
	"Binary": {
		kind: model.KindElement, stage: layer.StageStaged,
		requires: []shape.Cap{shape.Structured}, adds: []shape.Cap{shape.Encodable},
	},
	"Atomic": {
		kind: model.KindDecorator, stage: layer.StageStaged,
		adds: []shape.Cap{shape.Concurrent},
	},
	"Csv": {
		kind: model.KindTransport, stage: layer.StageStaged,
		requires: []shape.Cap{shape.Structured, shape.Streamable}, adds: []shape.Cap{shape.Encodable},
	},
}

// allCaps is every capability there is, which is what makes a check over a
// catalog row two-directional: a row is compared against all ten rather than
// against its own list, so an entry nobody wrote is caught as well as one
// nobody remembered.
var allCaps = shape.Every().All()

// everything is a shape holding every capability, which is what makes a layer's
// masks visible: what it takes away is what is missing from what it exposes
// over a stack that had all of it.
//
// Taken from the package that declares them rather than listed here. A list
// written out would have to be added to every time a capability is, and the
// test that noticed would be this one — which is the test that would then be
// checking nine of ten layers against ten of eleven capabilities and reporting
// that everything agreed.
func everything() shape.Shape {
	return shape.Shape{Caps: shape.Every()}
}

// Every layer is checked against the catalog it is supposed to implement, in
// every column: where it may appear, what it needs, what it contributes, what
// it withdraws, and how far along it is.
func TestTheCatalogIsWhatItSaysItIs(t *testing.T) {
	registry := layers.Builtins()

	if got, want := registry.Len(), len(catalog); got != want {
		t.Fatalf("the registry holds %d layers and the catalog names %d", got, want)
	}

	for name, want := range catalog {
		t.Run(name, func(t *testing.T) {
			found, ok := registry.Lookup(marker(name))
			if !ok {
				t.Fatalf("no layer claims %s", name)
			}

			if got := found.Kind(); got != want.kind {
				t.Errorf("kind is %s, want %s", got, want.kind)
			}
			if got := layer.TransparentLayer(found); got != want.transparent {
				t.Errorf("transparent is %v, want %v", got, want.transparent)
			}
			if got := found.(layer.Described).Stage(); got != want.stage {
				t.Errorf("stage is %s, want %s", got, want.stage)
			}

			// What it needs, asked of all ten rather than of the row's own
			// list. Looping over what the row claims would catch a requirement
			// that went missing and not one that appeared: a Slice that
			// quietly required Comparable would refuse every subject with no
			// stable identity, and no test built that way would notice.
			for _, capability := range allCaps {
				below := everything()
				below.Caps = below.Caps.Without(capability)

				refused := found.Accepts(below) != nil
				if want := slices.Contains(want.requires, capability); refused != want {
					t.Errorf("without %s: refused=%v, want %v", capability, refused, want)
				}
			}

			// What it adds: visible over a stack with nothing.
			added := found.Shape(nil, shape.Shape{}).Caps.All()
			if !slices.Equal(added, sorted(want.adds)) {
				t.Errorf("adds %v, want %v", added, sorted(want.adds))
			}

			// What it withdraws, compared as a set for the same reason.
			exposed := found.Shape(nil, everything()).Caps
			masked := everything().Caps.Without(exposed.All()...).All()
			if !slices.Equal(masked, sorted(want.masks)) {
				t.Errorf("withdraws %v, want %v", masked, sorted(want.masks))
			}

			// And what it puts on the subject, which is the one thing here that
			// another declaration's output reads: a layer that stopped naming a
			// method it still writes would have a neighbour's codec write the
			// form underneath a type instead of asking it.
			if got := found.Writes(); !slices.Equal(got, want.writes) {
				t.Errorf("writes %v, want %v", got, want.writes)
			}
		})
	}
}

// sorted returns the capabilities in the order a set reports them, so that a
// catalog row can be written in the order the documentation writes it.
func sorted(members []shape.Cap) []shape.Cap {
	if len(members) == 0 {
		return nil
	}
	return shape.Set(members...).All()
}

// An element layer cannot be transparent whatever it claims, and that is a fact
// about how its marker is written rather than about its invariants: a marker
// that cannot be a defined slice is a phantom struct, so a declaration holding
// one has an underlying type of struct{} and never []Person. Deciding it once
// keeps the rule from resting on twelve catalog rows each remembering to stay
// quiet.
func TestNoElementLayerIsTransparent(t *testing.T) {
	claiming := transparentFake{fake: fake{origin: marker("Json"), kind: model.KindElement}}

	if layer.TransparentLayer(claiming) {
		t.Error("an element layer claimed transparency and was believed")
	}

	// The same claim from a layer that could be transparent is taken.
	claiming.kind = model.KindStorage
	if !layer.TransparentLayer(claiming) {
		t.Error("a storage layer claimed transparency and was not believed")
	}

	// And a layer that says nothing is opaque, which is the safe direction.
	if layer.TransparentLayer(fake{origin: marker("Ring"), kind: model.KindStorage}) {
		t.Error("a layer that declared nothing was taken to be transparent")
	}
}

// transparentFake is a layer from outside that claims its invariants survive
// raw access to the type underneath it.
type transparentFake struct{ fake }

func (transparentFake) Transparent() bool { return true }

// The default storage has to be nameable by the stage that inserts it, or that
// stage keeps a second copy of a fact this catalog owns.
func TestTheDefaultStorageIsRegistered(t *testing.T) {
	found, ok := layers.Builtins().Lookup(layers.DefaultStorage())
	if !ok {
		t.Fatalf("no layer claims the default storage %s", layers.DefaultStorage())
	}
	if got, want := found.Kind(), model.KindStorage; got != want {
		t.Errorf("the default storage is a %s, want a %s", got, want)
	}
	if !layer.TransparentLayer(found) {
		t.Error("the default storage is opaque, which would make every inline declaration illegal")
	}
}
