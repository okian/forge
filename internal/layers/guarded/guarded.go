package guarded

import (
	"fmt"

	"github.com/okian/forge/plugin"
)

// container is the marker this layer claims.
const container = "Guarded"

// The methods this layer calls on the stack beneath it.
//
// The walk is what a snapshot is made of and what a write with the lock held is
// written over, so a stack without it is refused rather than generated against.
// The count is called only where the stack says it can be counted, and it goes
// back out under the name it came in under — a length behind a lock is still a
// length, and renaming it would be the lock claiming to have changed something
// it did not.
const (
	walkMethod = "All"
	length     = "Len"
)

// How the results of those two are written where a surface spells them.
//
// The walk's is completed with the declaration's own element, since a surface
// writes its element by the bare name it reads best under and this layer
// refuses a subject the package cannot write that way at all. The count's is
// the whole of it as it stands.
const (
	sequenceOpens = "iter.Seq["
	countResult   = "int"
)

// The methods this layer adds, named once because two things read them: the
// surface it reports and the writer that emits them.
const (
	writeScope = "Do"
	readScope  = "RDo"
	snapshot   = "Snapshot"
)

// The packages the generated code names.
var (
	stdSync     = plugin.Import{Path: "sync", Name: "sync"}
	stdSlices   = plugin.Import{Path: "slices", Name: "slices"}
	stdIter     = plugin.Import{Path: "iter", Name: "iter"}
	stdJSONText = plugin.Import{Path: "encoding/json/jsontext", Name: "jsontext"}
)

// Layer generates a read-write lock around the stack beneath it.
//
// It carries no state: what a declaration decides reaches it through the
// context it is asked to generate against, so one value serves every
// declaration in a run.
type Layer struct{}

// New returns the guarded layer.
func New() Layer { return Layer{} }

// Origin identifies the marker this layer claims.
func (Layer) Origin() plugin.TypeRef { return plugin.TypeRef{Pkg: plugin.MarkerPkg, Name: container} }

// Binds names what this layer's output imports, so that every layer of the
// stack spells its types against the same set.
//
// All four, and none of them conditional on what a given declaration turns out
// to need. The lock's own two — sync for the mutex the declared type holds, and
// slices for the snapshot a read hands back — are written for every
// declaration; the codec's is written only where there is a codec beneath, and
// the walk's only where a forwarded signature names one. What this decides is
// which names a subject is moved out of the way of, so the conditional ones are
// named anyway: an unused reservation costs an alias nobody needed, and a
// missing one costs a file that binds a name to two packages.
//
// This is the list, and [imports] is it in the shape a unit carries. [naming]
// answers a narrower question — what a forwarded signature reaches for — and is
// a subset on purpose.
func (Layer) Binds() []plugin.Import {
	return []plugin.Import{stdJSONText, stdIter, stdSlices, stdSync}
}

// Writes names nothing, because a lock is about who may reach the container
// rather than about what is in it.
func (Layer) Writes() []string { return nil }

// Kind says where in a stack the layer may appear.
//
// A decorator: it wraps a representation rather than being one, and what it
// wraps has to already exist for there to be anything to lock.
func (Layer) Kind() plugin.Kind { return plugin.KindDecorator }

// Stage says how far along the layer is.
func (Layer) Stage() plugin.Stage { return plugin.StageReady }

// Doc returns the one-line summary the list command prints.
func (Layer) Doc() string {
	return "read-write lock with scoped access, replacing iteration so that re-entry cannot be written"
}

// OptionSchema declares every option the layer accepts.
func (Layer) OptionSchema() []plugin.OptionDef {
	return []plugin.OptionDef{
		{
			Key: optionEncode, Value: plugin.ValueEnum,
			Values: []string{"snapshot", encodeLocked}, Default: "snapshot",
			Doc: "whether encoding copies first or holds the lock for the length of the write",
		},
		{
			Key: optionExpose, Value: plugin.ValueEnum, Values: []string{exposeLocker},
			Doc: "expose the lock as a sync.Locker, which the layer exists to make unnecessary",
		},
	}
}

// Accepts says what the layer needs of the stack beneath it.
//
// A walk, which is what a snapshot is made of. It is the one thing this cannot
// do without: the methods it takes away are replaced by scoped access and by a
// copy, and a stack that cannot be walked has no copy to give — so the lock
// would take iteration away and offer nothing back.
func (Layer) Accepts(below plugin.Shape) error {
	if !below.Caps.Has(plugin.Streamable) {
		return fmt.Errorf("%s needs the stack beneath it to be %s, and it is %s",
			container, plugin.Streamable, below.Caps)
	}
	return nil
}

// Encloses returns the name for what sits beneath the lock.
//
// What is beneath moves to a type of its own, because a lock that left it where
// it was would be a lock anybody could walk around: a method on the declared
// type is reachable by whoever holds one, and the whole arrangement is that the
// unlocked methods are not.
//
// Unexported, and named for what the lock holds. Everything derived from it
// reads accordingly — a constructor called newSessionsHeld, a view over
// sessionsHeld — and none of those names is one an author writes, because none
// of them is reachable without going through the lock.
func (Layer) Encloses(declared string) string {
	if declared == "" {
		return ""
	}
	return plugin.Around(false, "", declared, "held")
}

// Constructor returns the function that makes one of these, where there is one.
//
// A lock over a container that is ready as its zero value is ready as its zero
// value too, and says nothing here. A lock over one that has to be made writes
// a way in, and this is that way in described for whatever might hold *this* —
// which is the same forwarding one level up, and is why an enclosing decorator
// answers the question as well as asking it.
//
// Nothing composes that way today, and this does not on its own make it work:
// composing asks each enclosing layer this question with a context that has not
// been told what *it* holds, because the walk that works those out runs
// outermost first and the inner answer is not ready when the outer one is
// asked. So a second enclosing layer would still find nothing here. What this
// is is the half that belongs to this layer, written where it belongs, so that
// whoever adds the second one finds the question already answered on this side.
func (l Layer) Constructor(ctx *plugin.Context) (plugin.Constructor, bool) {
	made, needs := ctx.Holds()
	if !needs {
		return plugin.Constructor{}, false
	}

	return plugin.Constructor{
		Name:    constructorFor(ctx.Declared()),
		Params:  made.Params,
		Args:    made.Args,
		Pointer: true,
	}, true
}

// Shape returns what the layer exposes to the layer above it.
//
// Concurrent, because that is what it makes the stack; without Streamable and
// Indexed, because reaching an element directly is what it took away.
//
// And with a surface of its own rather than the one beneath it filtered. Every
// method the stack below declared is on the type this encloses, so none of them
// is on the declared type any more — not the walk it withdrew and not the rest
// either. A surface that kept them would be describing a type where they are
// not, which is worse than describing one method wrongly: a decorator above
// would wrap methods that do not exist, and collision detection would report a
// clash with something nothing declares.
//
// What replaces them is scoped access. It is what a caller reaches the whole of
// what is below through, and it is why taking the surface away is not taking
// the API away.
func (l Layer) Shape(ctx *plugin.Context, below plugin.Shape) plugin.Shape {
	held := below
	held.Caps = held.Caps.With(plugin.Concurrent).Without(plugin.Streamable, plugin.Indexed)
	held.Surface = nil

	return held.WithMethods(l.methods(ctx, below)...)
}

// methods is the surface this layer emits, described for the layers above it.
func (l Layer) methods(ctx *plugin.Context, below plugin.Shape) []plugin.Method {
	view := viewName(ctx)

	out := []plugin.Method{
		{
			Name: writeScope, Signature: "(f func(v " + view + "))", Owner: l.Origin(), Pointer: true,
			Doc: "runs a function with the write lock held, over everything beneath the lock",
		},
		{
			Name: readScope, Signature: "(f func(v " + view + "))", Owner: l.Origin(), Pointer: true,
			Doc: "runs a function with the read lock held, over everything beneath the lock",
		},
		{
			Name: snapshot, Signature: "() []" + elem(ctx), Owner: l.Origin(), Pointer: true,
			Doc: "copies the elements under the read lock, so that what is walked is nobody else's",
		},
	}

	// A length is one number read and handed back, so it is reached directly
	// rather than through a scope: there is nothing a caller can hold open, and
	// a closure for it would teach them that scopes are ceremony.
	if below.Caps.Has(plugin.Sized) {
		out = append(out, plugin.Method{
			Name: length, Signature: "() int", Owner: l.Origin(), Pointer: true,
			Doc: "how many elements the container holds, read under the read lock",
		})
	}

	// The codec for the container, which this layer writes rather than wraps:
	// one is written over a walk, and the walk is what this took away, so the
	// layer that writes codecs was handed a stack with nothing to walk and
	// wrote none. Reported here as well as written, because a surface is what a
	// layer above reads and what collision detection compares against, and a
	// method emitted without being described is one they would both miss.
	if encodes(below) {
		how := "from a copy taken under the read lock"
		if lockedWrite(ctx) {
			how = "with the read lock held for the length of the write"
		}

		out = append(out, plugin.Method{
			Name: marshalMethod, Signature: "(enc *jsontext.Encoder) error", Owner: l.Origin(), Pointer: true,
			Doc: "writes the container as a JSON array, " + how,
		})
	}

	if locker(ctx) {
		out = append(out,
			plugin.Method{
				Name: "Lock", Signature: "()", Owner: l.Origin(), Pointer: true,
				Doc: "takes the write lock, for a caller who asked for the lock to be exposed",
			},
			plugin.Method{
				Name: "Unlock", Signature: "()", Owner: l.Origin(), Pointer: true,
				Doc: "releases the write lock",
			},
		)
	}

	return out
}

// encodes reports whether the elements beneath the lock can be written as JSON.
//
// Asked of the capability rather than of a method, because the codec for an
// element goes on the element and a shape describes the declared type: nothing
// on the surface beneath this layer names it. What Encodable says is that some
// layer in the stack gave the elements a codec, and the entry point a codec is
// written under is the one thing every element layer that claims it agrees on.
func encodes(below plugin.Shape) bool { return below.Caps.Has(plugin.Encodable) }
