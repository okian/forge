package layer

import (
	"fmt"
	"go/token"
	"slices"

	"github.com/okian/forge/internal/diag"
	"github.com/okian/forge/internal/model"
	"github.com/okian/forge/internal/shape"
)

// Diagnostics this package reports.
//
// The number is high in its range on purpose. A code is permanent, and this one
// describes a condition that disappears as the catalog is filled in, so it
// should not sit in the low numbers that emission and collision rules — the
// range's real subject — will want.
var codeNotImplemented = diag.Register(4900, "layer generates nothing yet")

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
	stage  Stage

	// transparent records that the raw underlying type upholds this layer's
	// invariants, so a stack containing it may be written outside a spec file.
	// The zero value is the safe answer: a layer that has not thought about it
	// is opaque, and the cost of that is a declaration that has to move rather
	// than a value that can be corrupted through the type it is declared as.
	transparent bool

	requires []shape.Cap
	adds     []shape.Cap
	masks    []shape.Cap
	options  []OptionDef
	doc      string
}

// Origin identifies the marker this layer claims.
func (s *stub) Origin() model.TypeRef { return s.origin }

// Kind says where in a stack the layer may appear.
func (s *stub) Kind() model.Kind { return s.kind }

// Stage says how far along the layer is.
func (s *stub) Stage() Stage { return s.stage }

// Doc returns the one-line summary the list command prints.
func (s *stub) Doc() string { return s.doc }

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
func (s *stub) OptionSchema() []OptionDef {
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
func (s *stub) Shape(below shape.Shape) shape.Shape {
	below.Caps = below.Caps.With(s.adds...).Without(s.masks...)
	return below
}

// Generate reports that this layer has no generator yet.
//
// It is a diagnostic rather than a panic or an empty unit because both of those
// lie: a panic says forge is broken when the declaration is merely early, and
// an empty unit says the declaration was generated when nothing was written.
func (s *stub) Generate(ctx *Context, _ shape.Shape) (Unit, error) {
	var where token.Position
	if ctx != nil && ctx.Model != nil {
		where = ctx.Model.Pos
	}

	return Unit{}, diag.New(codeNotImplemented, where,
		"layer %s generates nothing yet", s.origin.Name).
		WithHint("%s", s.stageHint())
}

// stageHint says what an author can do about a layer that generates nothing.
func (s *stub) stageHint() string {
	if s.stage == StageStaged {
		return "this marker is declared so that a declaration naming it type-checks; the layer itself is not in this release"
	}
	return "the marker and the composition rules for this layer are in place; its generator is not written yet"
}

// marker builds a reference to one of the markers forge declares.
func marker(name string) model.TypeRef {
	return model.TypeRef{Pkg: model.MarkerPkg, Name: name}
}

// DefaultStorage is the storage a refining layer gets when a declaration is
// written with none beneath it, so that Collection[Person] resolves as though
// Collection[Slice[Person]] had been written.
//
// It is exported because the stage that inserts it has to name it, and a marker
// name written out again there would be a second copy of a fact this catalog
// owns. A function rather than a variable, so that nothing importing this
// package can quietly make it something else.
func DefaultStorage() model.TypeRef { return marker("Slice") }

// builtins is every layer forge knows about, in the order the catalog reads.
//
// The staged ones are here for the same reason their markers are declared at
// all: a declaration naming one type-checks, so it reaches generation, and
// being told the layer is not in this release is a better answer than being
// told nothing claims a marker forge plainly ships.
var builtins = []*stub{
	{
		origin: marker("Slice"), kind: model.KindStorage, stage: StageStub, transparent: true,
		adds: []shape.Cap{shape.Sized, shape.Ordered, shape.Indexed, shape.Streamable},
		doc:  "append-ordered backing array, and the storage a refining layer gets when none is written",
	},
	{
		origin: marker("Ring"), kind: model.KindStorage, stage: StageStub,
		adds: []shape.Cap{shape.Sized, shape.Ordered, shape.Streamable, shape.Bounded},
		options: []OptionDef{
			// Not required: a ring whose capacity is not declared takes it at
			// construction, which is the form the worked example in the
			// documentation uses and the one a caller sizing a buffer from
			// configuration needs.
			{Key: "cap", Value: ValueInt, Doc: "how many elements the buffer holds, when that is fixed at build time rather than passed to the constructor"},
			{
				Key: "overflow", Value: ValueEnum, Values: []string{"overwrite", "error"}, Default: "overwrite",
				Doc: "what a push does when the buffer is full",
			},
		},
		doc: "fixed-capacity circular buffer, so a long-running producer cannot grow memory without bound",
	},
	{
		origin: marker("Collection"), kind: model.KindRefining, stage: StageStub, transparent: true,
		requires: []shape.Cap{shape.Streamable},
		options: []OptionDef{
			{Key: "sort", Value: ValueFields, Doc: "fields to generate a sorted view for"},
			{Key: "index", Value: ValueFields, Doc: "fields to generate a lookup map for"},
			{Key: "seq", Value: ValueString, Doc: "name for the generated sequence view, when the default collides"},
		},
		doc: "query, projection and sort methods built from the subject's fields",
	},
	{
		origin: marker("Json"), kind: model.KindElement, stage: StageStub,
		requires: []shape.Cap{shape.Structured}, adds: []shape.Cap{shape.Encodable},
		options: []OptionDef{
			{
				Key: "names", Value: ValueEnum, Values: []string{"asis", "snake", "camel"}, Default: "asis",
				Doc: "how a field with no json tag is named on the wire",
			},
			{
				Key: "omitzero", Value: ValueBool, Default: "false",
				Doc: "omit zero-valued fields without tagging each one",
			},
			{
				Key: "fallback", Scope: ScopeField, Value: ValueEnum, Values: []string{"stdlib"},
				Doc: "encode a field forge cannot see through reflectively, and mark that it did",
			},
		},
		doc: "streaming codec with no reflection, driven by the subject's json tags",
	},
	{
		origin: marker("Validate"), kind: model.KindElement, stage: StageStub,
		requires: []shape.Cap{shape.Structured},
		doc:      "rules read from the subject's validate tags, checked in declaration order",
	},
	{
		origin: marker("Clone"), kind: model.KindElement, stage: StageStub,
		options: []OptionDef{
			{
				Key: "aliasing", Value: ValueEnum, Values: []string{"copy", "share"}, Default: "copy",
				Doc: "whether a pointer, slice or map is copied or shared with the original",
			},
		},
		doc: "deep copy over everything reachable from the subject, with an explicit aliasing policy",
	},
	{
		origin: marker("Hash"), kind: model.KindElement, stage: StageStub,
		adds: []shape.Cap{shape.Comparable},
		doc:  "stable content hash, which is what lets a subject with no comparable form be a set member",
	},
	{
		origin: marker("Builder"), kind: model.KindElement, stage: StageStub,
		requires: []shape.Cap{shape.Structured},
		doc:      "fluent builder whose required fields are enforced at Build rather than at each setter",
	},
	{
		origin: marker("Patch"), kind: model.KindElement, stage: StageStub,
		requires: []shape.Cap{shape.Structured},
		doc:      "field-mask companion type, so that absent and zero stay distinguishable",
	},
	{
		origin: marker("Redact"), kind: model.KindElement, stage: StageStub,
		requires: []shape.Cap{shape.Structured},
		doc:      "log value with redact-tagged fields masked, so logging cannot leak them",
	},
	{
		origin: marker("Enum"), kind: model.KindElement, stage: StageStub,
		adds: []shape.Cap{shape.Comparable, shape.Encodable},
		doc:  "the API of a closed set, discovered from the constants declared with the subject",
	},
	{
		origin: marker("Guarded"), kind: model.KindDecorator, stage: StageStub,
		adds:  []shape.Cap{shape.Concurrent},
		masks: []shape.Cap{shape.Streamable, shape.Indexed},
		options: []OptionDef{
			{
				Key: "encode", Value: ValueEnum, Values: []string{"snapshot", "locked"}, Default: "snapshot",
				Doc: "whether encoding copies first or holds the lock for the length of the write",
			},
			{
				Key: "expose", Value: ValueEnum, Values: []string{"locker"},
				Doc: "expose the lock as a sync.Locker, which the layer exists to make unnecessary",
			},
		},
		doc: "read-write lock with scoped access, replacing iteration so that re-entry cannot be written",
	},

	// Staged: the marker is declared, the layer is not in this release.
	{
		origin: marker("Set"), kind: model.KindStorage, stage: StageStaged,
		requires: []shape.Cap{shape.Comparable}, adds: []shape.Cap{shape.Sized, shape.Streamable},
		options: []OptionDef{{Key: "key", Value: ValueField, Doc: "field to deduplicate on, instead of the subject's content hash"}},
		doc:     "at most one element per distinct key",
	},
	{
		origin: marker("LRU"), kind: model.KindStorage, stage: StageStaged,
		requires: []shape.Cap{shape.Keyed}, adds: []shape.Cap{shape.Sized, shape.Bounded, shape.Streamable},
		options: []OptionDef{
			{Key: "key", Value: ValueField, Doc: "field elements are keyed by"},
			{Key: "cap", Value: ValueInt, Doc: "how many elements are held before the least recently used is evicted"},
		},
		doc: "bounded map with recency eviction",
	},
	{
		origin: marker("Index"), kind: model.KindStorage, stage: StageStaged,
		requires: []shape.Cap{shape.Keyed}, adds: []shape.Cap{shape.Sized, shape.Indexed, shape.Streamable},
		options: []OptionDef{
			{Key: "key", Value: ValueField, Doc: "field to look elements up by"},
			{Key: "unique", Value: ValueBool, Default: "true", Doc: "whether one key reaches at most one element"},
		},
		doc: "lookup structure over a declared field, turning a scan into a map access",
	},
	{
		origin: marker("Heap"), kind: model.KindStorage, stage: StageStaged,
		requires: []shape.Cap{shape.Keyed}, adds: []shape.Cap{shape.Sized, shape.Streamable},
		options: []OptionDef{{Key: "key", Value: ValueField, Doc: "field the priority order is taken from"}},
		doc:     "priority order by a declared key",
	},
	{
		origin: marker("Sorted"), kind: model.KindRefining, stage: StageStaged,
		requires: []shape.Cap{shape.Ordered},
		options:  []OptionDef{{Key: "key", Value: ValueFields, Doc: "fields the order is maintained by"}},
		doc:      "order maintained on insert rather than on demand",
	},
	{
		origin: marker("Page"), kind: model.KindRefining, stage: StageStaged,
		requires: []shape.Cap{shape.Sized, shape.Ordered},
		doc:      "offset and cursor windowing over a large collection",
	},
	{
		origin: marker("Default"), kind: model.KindElement, stage: StageStaged,
		requires: []shape.Cap{shape.Structured},
		doc:      "the subject's default tag values, applied before the rules that assume them",
	},
	{
		origin: marker("Diff"), kind: model.KindElement, stage: StageStaged,
		requires: []shape.Cap{shape.Structured},
		doc:      "what differs between two values, as a list of changes rather than a boolean",
	},
	{
		origin: marker("Fault"), kind: model.KindElement, stage: StageStaged,
		doc: "the error protocol for a subject that models a failure, requested and never inferred",
	},
	{
		origin: marker("Binary"), kind: model.KindElement, stage: StageStaged,
		requires: []shape.Cap{shape.Structured}, adds: []shape.Cap{shape.Encodable},
		doc: "compact binary codec, with an appender for callers who own the buffer",
	},
	{
		origin: marker("Atomic"), kind: model.KindDecorator, stage: StageStaged,
		adds: []shape.Cap{shape.Concurrent},
		doc:  "copy-on-write publication, so a read is one atomic load",
	},
	{
		origin: marker("Csv"), kind: model.KindTransport, stage: StageStaged,
		requires: []shape.Cap{shape.Structured, shape.Streamable}, adds: []shape.Cap{shape.Encodable},
		doc: "the whole stack as CSV, mapping the subject's fields to a header row",
	},
}

// Builtins returns a registry holding every layer forge ships.
//
// A fresh registry each time rather than one shared value: a caller that adds a
// layer of its own is doing that for one run, and a registry that outlived the
// run would carry it into the next.
func Builtins() *Registry {
	r := New()
	for _, l := range builtins {
		r.MustRegister(l)
	}
	return r
}
