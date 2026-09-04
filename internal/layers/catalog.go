package layers

import (
	"fmt"
	"go/token"
	"slices"

	"github.com/okian/forge/internal/diag"
	"github.com/okian/forge/internal/layer"
	"github.com/okian/forge/internal/model"
	"github.com/okian/forge/internal/shape"
)

// NotImplemented is what a layer whose generator is not written reports.
//
// The number is high in its range on purpose. A code is permanent, and this one
// describes a condition that disappears as the catalog is filled in, so it
// should not sit in the low numbers that emission and collision rules — the
// range's real subject — will want.
//
// Exported because it is the one refusal that is not a fault in the
// declaration: it says forge has not got there yet, and a verb describing a
// stack rather than writing one has somewhere better to put that than in a list
// of what the author did wrong.
var NotImplemented = diag.Register(4900, "layer generates nothing yet")

// stub is a layer that knows everything about itself except how to generate.
//
// Every layer forge ships starts as one. What a layer *is* — the marker it
// claims, its kind, its options, the capabilities it needs and adds — is what
// resolution, composition, explain and list read, and all of that is decided
// before a line of its output is designed. Writing it down first is what lets
// those stages be built and tested against the whole catalog rather than
// against whichever layer happens to exist.
type stub struct {
	origin model.TypeRef
	kind   model.Kind
	stage  layer.Stage

	// transparent records that the raw underlying type upholds this layer's
	// invariants, so a stack containing it may be written outside a spec file.
	// The zero value is the safe answer: a layer that has not thought about it
	// is opaque, and the cost of that is a declaration that has to move rather
	// than a value that can be corrupted through the type it is declared as.
	transparent bool

	requires []shape.Cap
	adds     []shape.Cap
	masks    []shape.Cap

	// withdraws names the methods this layer takes off the declared type, which
	// is the half of masking capabilities cannot express.
	//
	// A lock takes the walk away and offers scoped access instead, and a
	// capability alone cannot say that: Streamable going missing tells a layer
	// above that it may not be written against a walk, and tells nothing at all
	// to the reader looking at a list of methods that still holds All. What is
	// named here is what a stack that had it no longer has, so a stack that
	// never had it is unaffected and nothing has to check first.
	withdraws []string

	options []layer.OptionDef
	doc     string
}

// Origin identifies the marker this layer claims.
func (s *stub) Origin() model.TypeRef { return s.origin }

// Kind says where in a stack the layer may appear.
func (s *stub) Kind() model.Kind { return s.kind }

// Stage says how far along the layer is.
func (s *stub) Stage() layer.Stage { return s.stage }

// Doc returns the one-line summary the list command prints.
func (s *stub) Doc() string { return s.doc }

// Binds names what this layer's output imports, which for a layer that writes
// nothing is nothing.
//
// A stub answering with the imports its generator will one day want would move
// every subject out of the way of names no file binds, so a stack containing
// one would generate differently from the same stack without it — for a layer
// that contributes not one line. What it will bind is written down when it is
// written.
func (s *stub) Binds() []model.Import { return nil }

// Writes names nothing, for the reason [stub.Binds] names nothing.
//
// A marker whose generator is unwritten puts no method anywhere. Naming what it
// will one day write would have a neighbour's codec reach for a method nobody
// has written yet, in a package that then names one nothing declares — which is
// a worse answer than the number the codec would otherwise have written.
func (s *stub) Writes() []string { return nil }

// Transparent reports whether the raw underlying type upholds this layer's
// invariants on its own.
func (s *stub) Transparent() bool { return s.transparent }

// OptionSchema declares every option the layer accepts.
//
// The copy goes one level deeper than the slice, because a shallow one would
// leave every returned definition's accepted values pointing at the catalog's
// own. The catalog is a package-level value handed to every registry ever
// built, so one caller sorting or rewriting what it was given would rewrite
// what forge accepts for the life of the process.
func (s *stub) OptionSchema() []layer.OptionDef {
	out := slices.Clone(s.options)
	for i := range out {
		out[i].Values = slices.Clone(out[i].Values)
	}
	return out
}

// Accepts reports whether the layer can sit on the shape beneath it.
//
// The first missing capability is the one reported. Listing all of them would
// read as a longer list of things to fix, when in practice the second is
// usually a consequence of the first: a stack with nothing to stream over
// rarely has an order either.
func (s *stub) Accepts(below shape.Shape) error {
	for _, required := range s.requires {
		if !below.Caps.Has(required) {
			return fmt.Errorf("%s needs the stack beneath it to be %s, and it is %s",
				s.origin.Name, required, below.Caps)
		}
	}
	return nil
}

// Shape returns what the layer exposes upward: what was beneath it, plus what
// it adds, minus what it takes away.
//
// Both halves of taking away. A capability withdrawn stops the layers above
// being written against it; a method withdrawn stops a caller reaching it, and
// stops it being listed as something the declared type has. A stub reports the
// second even though it emits nothing, because what it exposes is a description
// of the layer rather than of its output — and the description is what every
// other stage is written against until the generator arrives.
func (s *stub) Shape(_ *layer.Context, below shape.Shape) shape.Shape {
	below.Caps = below.Caps.With(s.adds...).Without(s.masks...)
	return below.Without(s.withdraws...)
}

// Generate reports that this layer has no generator yet.
//
// It is a diagnostic rather than a panic or an empty unit because both of those
// lie: a panic says forge is broken when the declaration is merely early, and
// an empty unit says the declaration was generated when nothing was written.
func (s *stub) Generate(ctx *layer.Context, _ shape.Shape) (layer.Unit, error) {
	var where token.Position
	if ctx != nil && ctx.Model != nil {
		where = ctx.Model.Pos
	}

	return layer.Unit{}, diag.New(NotImplemented, where,
		"layer %s generates nothing yet", s.origin.Name).
		WithHint("%s", s.stageHint())
}

// stageHint says what an author can do about a layer that generates nothing.
func (s *stub) stageHint() string {
	if s.stage == layer.StageStaged {
		return "this marker is declared so that a declaration naming it type-checks; the layer itself is not in this release"
	}
	return "the marker and the composition rules for this layer are in place; its generator is not written yet"
}

// marker builds a reference to one of the markers forge declares.
func marker(name string) model.TypeRef {
	return model.TypeRef{Pkg: model.MarkerPkg, Name: name}
}

// declared is every layer forge knows about, in the order the catalog reads.
//
// The staged ones are here for the same reason their markers are declared at
// all: a declaration naming one type-checks, so it reaches generation, and
// being told the layer is not in this release is a better answer than being
// told nothing claims a marker forge plainly ships.
var declared = []*stub{
	// Staged: the marker is declared, the layer is not in this release.
	{
		origin: marker("Set"), kind: model.KindStorage, stage: layer.StageStaged,
		requires: []shape.Cap{shape.Comparable}, adds: []shape.Cap{shape.Sized, shape.Streamable},
		options: []layer.OptionDef{{Key: "key", Value: layer.ValueField, Doc: "field to deduplicate on, instead of the subject's content hash"}},
		doc:     "at most one element per distinct key",
	},
	{
		origin: marker("LRU"), kind: model.KindStorage, stage: layer.StageStaged,
		requires: []shape.Cap{shape.Keyed}, adds: []shape.Cap{shape.Sized, shape.Bounded, shape.Streamable},
		options: []layer.OptionDef{
			{Key: "key", Value: layer.ValueField, Doc: "field elements are keyed by"},
			{Key: "cap", Value: layer.ValueInt, Doc: "how many elements are held before the least recently used is evicted"},
		},
		doc: "bounded map with recency eviction",
	},
	{
		origin: marker("Heap"), kind: model.KindStorage, stage: layer.StageStaged,
		requires: []shape.Cap{shape.Keyed}, adds: []shape.Cap{shape.Sized, shape.Streamable},
		options: []layer.OptionDef{{Key: "key", Value: layer.ValueField, Doc: "field the priority order is taken from"}},
		doc:     "priority order by a declared key",
	},
	{
		origin: marker("Sorted"), kind: model.KindRefining, stage: layer.StageStaged,
		requires: []shape.Cap{shape.Ordered},
		options:  []layer.OptionDef{{Key: "key", Value: layer.ValueFields, Doc: "fields the order is maintained by"}},
		doc:      "order maintained on insert rather than on demand",
	},
	{
		origin: marker("Page"), kind: model.KindRefining, stage: layer.StageStaged,
		requires: []shape.Cap{shape.Sized, shape.Ordered},
		doc:      "offset and cursor windowing over a large collection",
	},
	{
		origin: marker("Default"), kind: model.KindElement, stage: layer.StageStaged,
		requires: []shape.Cap{shape.Structured},
		doc:      "the subject's default tag values, applied before the rules that assume them",
	},
	{
		origin: marker("Diff"), kind: model.KindElement, stage: layer.StageStaged,
		requires: []shape.Cap{shape.Structured},
		doc:      "what differs between two values, as a list of changes rather than a boolean",
	},
	{
		origin: marker("Fault"), kind: model.KindElement, stage: layer.StageStaged,
		doc: "the error protocol for a subject that models a failure, requested and never inferred",
	},
	{
		origin: marker("Binary"), kind: model.KindElement, stage: layer.StageStaged,
		requires: []shape.Cap{shape.Structured}, adds: []shape.Cap{shape.Encodable},
		doc: "compact binary codec, with an appender for callers who own the buffer",
	},
	{
		origin: marker("Atomic"), kind: model.KindDecorator, stage: layer.StageStaged,
		adds: []shape.Cap{shape.Concurrent},
		doc:  "copy-on-write publication, so a read is one atomic load",
	},
	{
		origin: marker("Csv"), kind: model.KindTransport, stage: layer.StageStaged,
		requires: []shape.Cap{shape.Structured, shape.Streamable}, adds: []shape.Cap{shape.Encodable},
		doc: "the whole stack as CSV, mapping the subject's fields to a header row",
	},
}
