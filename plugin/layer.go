package plugin

import (
	"github.com/okian/forge/internal/layer"
	"github.com/okian/forge/internal/model"
)

// Layer is the interface a layer implements: one entry in a stack, claiming one
// marker and generating for it.
//
// Its own documentation says what each method is asked and when. The package
// documentation says the order they come in.
type Layer = layer.Layer

// Kind says where in a stack a layer may appear and what its output is about.
//
// Five that a layer may report, and a sixth forge uses for the subject a stack
// is over — which is not a layer and is not one to return.
type Kind = model.Kind

// The kinds a layer may be, and the zero that is none of them.
//
// KindTransport is not an element layer, and sits at the opposite end of a
// stack: it terminates one. What is beneath it is what it carries, so nothing
// may be written over it and only one of them may appear — where an element
// layer belongs innermost, against the subject, with the containers above it.
const (
	KindInvalid   = model.KindInvalid
	KindStorage   = model.KindStorage
	KindRefining  = model.KindRefining
	KindElement   = model.KindElement
	KindDecorator = model.KindDecorator
	KindTransport = model.KindTransport
)

// Context is what a layer generates against: the declaration, its options, and
// what the run has settled that one declaration could not settle for itself.
type Context = layer.Context

// Unit is what a layer contributes: declarations, the imports they need, and
// the compile-time claims they earn.
type Unit = layer.Unit

// Assertion is one compile-time claim about what was generated, written into
// the file as a variable nobody reads.
//
// It is how a stack that stops satisfying an interface fails where it was
// generated rather than at somebody's call site.
type Assertion = layer.Assertion

// Registry holds the layers a run knows about, by the marker each claims.
//
// A layer is registered into the one a run was given rather than into one of
// its own: a stack composes across every layer the run knows, so a catalog
// holding one layer can generate for a declaration naming one layer and
// nothing else — and the storage a refining layer needs beneath it is one of
// forge's. [github.com/okian/forge/driver.Builtins] is where one comes from.
//
// The zero value is not one: a registry holds a map, and registering into an
// empty struct panics. Use NewRegistry or the driver's Builtins.
type Registry = layer.Registry

// NewRegistry returns an empty registry.
//
// Not what a run is given — a stack composes across every layer the run knows,
// so a catalog with one layer in it can generate only for a declaration naming
// one layer and nothing else. What it is for is a layer's own tests:
// registering into an empty registry says whether the layer registers at all,
// without pulling in forge's whole catalog to find out.
func NewRegistry() *Registry { return layer.New() }

// Stage says how far along a layer is.
//
// It is forge's own bookkeeping and a layer outside it has one answer:
// [StageReady]. The others describe a marker forge publishes before its
// generator is written, so that a declaration naming it type-checks and the
// report can say the work is pending rather than that the author erred.
type Stage = layer.Stage

// The stages a layer may be at.
const (
	StageReady  = layer.StageReady
	StageStub   = layer.StageStub
	StageStaged = layer.StageStaged
)

// Constructor says how the type a decorator encloses is made, where it cannot
// start as its zero value.
type Constructor = layer.Constructor

// Described is implemented by a layer that says how far along it is and what it
// is in one line, which is what the list command prints and what an explanation
// puts beside each step.
//
// Two methods, and the pair is why they are together: a report that knows what
// a layer does also wants to know whether it does it yet. A layer outside forge
// has one answer to the second — StageReady — and forge's own use the rest to
// describe a marker published before its generator was written.
//
// Optional, and worth implementing. A layer that says nothing about itself is
// reported as pending, which is what forge says about a marker whose work is
// not written — so a layer that is written and silent is described as one that
// is not.
type Described = layer.Described

// Transparent is implemented by a storage layer whose invariants the raw
// underlying type upholds on its own.
//
// A slice is transparent: the declared type really is []Person, and a caller
// may index it and range over it without going through a method. A ring buffer
// is not — its underlying form is somebody's business only through the methods
// that keep the wrap-around right — and a declaration over one is written in a
// spec file for that reason.
type Transparent = layer.Transparent

// Enclosing is implemented by a decorator that declares a type of its own and
// holds what it wraps inside it, rather than adding methods beside it.
//
// A lock encloses: the declared type becomes the lock and the representation
// becomes a field, so the layers beneath declare onto a type of the decorator's
// making. Context.Declared is what tells them which.
type Enclosing = layer.Enclosing

// Constructing is implemented by an enclosing decorator whose held type needs
// making rather than starting at its zero value.
type Constructing = layer.Constructing
