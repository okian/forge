package index

import (
	_ "embed"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/okian/forge/internal/templates"
	"github.com/okian/forge/plugin"
)

// What a declaration can ask for that this layer cannot generate.
//
// The first two are about an option's value rather than about the option: the
// field exists and is spelled correctly, and what it cannot do is be a map key
// or be read from here. The next three are about options that contradict each
// other, which validation cannot see because each option alone is well formed.
// The last is not a 3xxx: two of this layer's own lookups came out under one
// name, and what that produces is a package holding one declaration twice.
var (
	codeNotKeyable            = plugin.Register(3037, "field cannot be a lookup key")
	codeFieldUnexported       = plugin.Register(3038, "field cannot be read from the generated package")
	codeSecondaryIsKey        = plugin.Register(3039, "a secondary lookup repeats the key")
	codePolicyUnneeded        = plugin.Register(3040, "a conflict policy needs a unique key")
	codeSecondariesNeedUnique = plugin.Register(3041, "secondary lookups need a unique key")

	codeLookupsCollide = plugin.Register(4103, "two lookup names are one")
)

// bodies is the template this layer emits the field-independent half of,
// embedded from the package beside it.
//
// Embedded rather than quoted, so that what is emitted is a Go file the
// ordinary build compiles, the ordinary vet reads and this package's own tests
// exercise. A body that is only ever a string is a body nothing checks until
// somebody's generated file fails to build.
//
//go:embed tmpl/tmpl.go
var bodies []byte

// container is the type the template declares, and param the element it is
// generic over. They are the two names the rewrite is written in terms of, and
// they are written here rather than passed in because they are facts about the
// file beside this one and change only when it does.
const (
	container = "Index"
	param     = "T"
)

// The names the template gives the declarations a run chooses between.
//
// The constructors and the appends are one option's two answers each: the
// layer keeps one and drops the other, and what it keeps it renames to the
// name the contract uses — so a caller writes AppendSeq whichever policy the
// declaration chose, and finds out which by whether there is an error to
// handle. The placers and Reset are placeholders: every run drops all of them
// and builds its own, because their real bodies write into maps only a
// declaration knows the keys of.
const (
	constructorPlain    = "New"
	constructorRefusing = "NewChecked"

	appendPlain    = "AppendSeq"
	appendRefusing = "AppendSeqChecked"

	placePlain    = "place"
	placeRefusing = "placeChecked"
	resetMethod   = "Reset"

	entryType = "entryOf"
	dupError  = "errDup"
)

// The options this layer reads, and the policies the conflict option names.
const (
	optionKey      = "key"
	optionUnique   = "unique"
	optionConflict = "conflict"
	optionIndex    = "index"

	conflictError   = "error"
	conflictReplace = "replace"
)

// templateImports names every package the template imports, and what a file
// importing it binds that package to.
//
// Written down rather than read off the paths, because a path does not say
// what it binds: encoding/json/v2 binds json and math/rand/v2 binds rand, so
// taking the last element under-reports exactly the names most worth knowing.
// And under-reporting is the harmful direction — a name this does not mention
// is a name the subject is not moved out of the way of, which is the collision
// the spelling exists to prevent.
var templateImports = map[string]string{
	"errors": "errors",
	"iter":   "iter",
}

// taken returns what the template's imports bind, sorted so that a spelling
// built from them does not depend on a map.
func taken() []plugin.Import {
	out := make([]plugin.Import, 0, len(templateImports))
	for path, name := range templateImports {
		out = append(out, plugin.Import{Path: path, Name: name})
	}

	slices.SortFunc(out, func(a, b plugin.Import) int { return strings.Compare(a.Path, b.Path) })
	return out
}

// Layer generates keyed storage with lookup maps over declared fields.
//
// It carries no state. What a declaration decides reaches it through the
// context it is asked to generate against, so one value serves every
// declaration in a run and there is nothing to reset between them.
type Layer struct{}

// New returns the index storage layer.
func New() Layer { return Layer{} }

// Origin identifies the marker this layer claims.
func (Layer) Origin() plugin.TypeRef { return plugin.TypeRef{Pkg: plugin.MarkerPkg, Name: container} }

// Binds names what this layer's output imports, so that every layer of the
// stack spells its types against the same set.
//
// Every import the template has, including the one a given declaration may not
// keep. What this decides is which names the subject is moved out of the way
// of, and moving it out of the way of a name the file turns out not to bind
// costs nothing; not moving it out of the way of one the file does bind is a
// package imported twice under one name.
func (Layer) Binds() []plugin.Import { return taken() }

// Writes names nothing, because everything this layer writes is about the
// container and its maps rather than about what is in them.
func (Layer) Writes() []string { return nil }

// Kind says where in a stack the layer may appear.
func (Layer) Kind() plugin.Kind { return plugin.KindStorage }

// Stage says how far along the layer is.
func (Layer) Stage() plugin.Stage { return plugin.StageReady }

// Doc returns the one-line summary the list command prints.
func (Layer) Doc() string {
	return "lookup structure over declared fields, turning a scan into a map access"
}

// Transparent reports that the raw underlying type does not uphold this
// layer's invariants.
//
// The representation is an ordered slice beside maps that have to agree with
// it: a map naming an element the order does not hold, or holding a position
// the order has moved, is a value every method reads wrongly. The language
// offers no way to stop somebody writing one, so the type is forge's rather
// than the author's, and a declaration over this storage belongs in a spec
// file.
func (Layer) Transparent() bool { return false }

// OptionSchema declares every option the layer accepts.
func (Layer) OptionSchema() []plugin.OptionDef {
	return []plugin.OptionDef{
		// Required: a lookup structure with no key is a slice wearing a
		// different name, and the field cannot be defaulted because no reading
		// of a struct says which field identifies it.
		{
			Key: optionKey, Value: plugin.ValueField, Required: true,
			Doc: "field elements are looked up and removed by",
		},
		{
			Key: optionUnique, Value: plugin.ValueBool, Default: "true",
			Doc: "whether one key reaches at most one element",
		},
		{
			Key: optionConflict, Value: plugin.ValueEnum,
			Values:  []string{conflictError, conflictReplace},
			Default: conflictError,
			Doc:     "what adding a key that is already held does, where keys are unique",
		},
		{
			Key: optionIndex, Value: plugin.ValueFields,
			Doc: "fields to generate a secondary lookup for",
		},
	}
}

// Accepts reports whether the layer can sit on the shape beneath it.
//
// It always can. Storage is the bottom of a stack's representation: what is
// beneath it is the subject and whatever element layers attached to the
// subject, and none of that is something a container has to be able to do
// anything with. The key this layer needs is declared in its own option
// rather than being a capability of the stack — which is where R7 says keys
// come from — so there is nothing here to require.
func (Layer) Accepts(plugin.Shape) error { return nil }

// Shape returns what the layer exposes to the layer above it.
//
// Sized and Streamable and nothing else. Not Ordered: removal moves the last
// element into the hole it left, so the walk order is deterministic without
// meaning anything, and a walk backwards over it would be a promise about an
// order this layer does not keep. Not Indexed: that capability says an
// element can be reached by position, which for every storage claiming it so
// far has meant the language indexes the underlying slice — and this type is
// a struct, with no positions for a layer above to generate against.
func (l Layer) Shape(ctx *plugin.Context, below plugin.Shape) plugin.Shape {
	below.Caps = below.Caps.With(plugin.Sized, plugin.Streamable)
	return below.WithMethods(l.methods(ctx, below)...)
}

// methods is the surface this layer emits, described for the layers above it.
//
// It is written out from the plan rather than read back from the template, and
// it has to be: the template declares more than any declaration gets, and the
// lookups are not in it at all. Asked without a declaration it reports the
// methods that do not depend on one — the lookups and the removal are named
// after a field and typed by it, and a rendering of a type nothing here knows
// would be a string a reader takes for source. Only the surface may be shorter
// for it; the capabilities are the same either way.
func (l Layer) methods(ctx *plugin.Context, below plugin.Shape) []plugin.Method {
	if ctx == nil || ctx.Model == nil || ctx.Model.Subject == nil {
		seq := "iter.Seq[" + spellElem(below.Elem) + "]"

		return []plugin.Method{
			{Name: "Len", Signature: "() int", Owner: l.Origin(), Pointer: true, Doc: "how many elements the container holds"},
			{Name: "All", Signature: "() " + seq, Owner: l.Origin(), Pointer: true, Doc: "walks the elements in the order they were added"},
			{Name: appendPlain, Signature: "(seq " + seq + ")", Owner: l.Origin(), Pointer: true, Doc: "adds every element a sequence yields"},
			{Name: resetMethod, Signature: "()", Owner: l.Origin(), Pointer: true, Doc: "empties the container, keeping the memory it has taken"},
		}
	}

	surface, _ := planned(ctx, below)
	return surface.surface(l.Origin(), below.Elem)
}

// spellElem names the element for a signature a person reads.
//
// The bare name rather than the qualified one: a shape is printed in a table
// beside the declaration it belongs to, where the package is already known. A
// stack whose subject could not be modelled has no element at all, and is
// spelled as the template spells it, which is the honest answer to a question
// with no answer yet.
func spellElem(elem plugin.TypeRef) string {
	if elem.Name == "" {
		return param
	}
	return elem.Name
}

// Generate returns the declarations this layer contributes.
func (l Layer) Generate(ctx *plugin.Context, below plugin.Shape) (plugin.Unit, error) {
	if ctx == nil || ctx.Model == nil || ctx.Model.Subject == nil {
		// Not a diagnostic: a diagnostic points at a declaration, and the
		// declaration is the thing that is missing. The pipeline never asks a
		// layer to generate for a model it does not have, so reaching here is
		// forge calling itself wrongly rather than anybody writing anything.
		return plugin.Unit{}, errors.New("index: asked to generate without a modelled declaration")
	}

	surface, diags := planned(ctx, below)

	clashes := surface.clashes()
	diags.Merge(&clashes)

	if err := diags.Err(); err != nil {
		return plugin.Unit{}, err
	}

	if surface.key.field == "" {
		// Validation demands the option before generation is asked, so
		// reaching here without one is the pipeline miswired rather than a
		// declaration to report about.
		return plugin.Unit{}, errors.New("index: asked to generate for a declaration with no key")
	}

	held := chosen(surface)

	out, refused := l.apply(ctx, surface, held)
	if err := refused.Err(); err != nil {
		return plugin.Unit{}, err
	}

	if wrong := accounted(out.Imports); wrong != "" {
		return plugin.Unit{}, fmt.Errorf("index: %s", wrong)
	}

	kept, err := held.applied(out.Decls, ctx.Declared())
	if err != nil {
		return plugin.Unit{}, err
	}

	built, err := surface.build()
	if err != nil {
		return plugin.Unit{}, err
	}

	// The container's own declaration first, then the template's half, then
	// the per-field methods: what the type is, what every declaration of this
	// storage has, and then what this one's fields added.
	decls := slices.Concat(built.container, kept, built.methods)

	return plugin.Unit{
		Decls:    decls,
		Comments: out.Comments,
		Fset:     out.Fset,
		Imports:  append(plugin.Reaching(decls, out.Imports), surface.imports()...),
	}, nil
}

// apply specialises the template's half of the output.
//
// The names the plan carries are the ones the prefix rule must not touch: a
// constructor and an error a caller writes out, and the entry type the built
// struct names. Everything else the template declares is a helper, and takes
// the declaration's prefix so that it cannot collide with something the
// author wrote.
func (Layer) apply(ctx *plugin.Context, surface plan, held choice) (templates.Result, plugin.Diagnostics) {
	return templates.Apply(
		templates.Template{Name: "index", Source: bodies},
		templates.Rewrite{
			Param:     param,
			Subject:   surface.subject.Text,
			Container: container,
			Declared:  ctx.Declared(),
			Names:     held.names,
			Prefix:    plugin.Camel(ctx.Declared()),
			// A walk hands its elements over from inside a closure, and the
			// closure's signature spells the subject. So the receiver is in
			// scope over a body naming the subject's type, and a subject whose
			// own name is the receiver's does not compile. The subject's name
			// is the one nobody here chooses, so this is the one that moves.
			Receiver: surface.receiver,
		},
		ctx.Model.Pos)
}

// accounted reports a template import nothing wrote down, or nothing.
//
// The subject is spelled before the template is read, against the names the
// list above says the file will bind. An import that is not in it is one the
// subject was not moved out of the way of, so the check is not bookkeeping: it
// is the thing that keeps the list from being a comment about a file that has
// since changed. It fails on the first run of this package's tests, which is
// where an import added to the template is cheapest to notice.
func accounted(imports []plugin.Import) string {
	for _, one := range imports {
		if _, known := templateImports[one.Path]; !known {
			return "the template imports " + one.Path + ", which nothing recorded a bound name for"
		}
	}
	return ""
}

// constructorFor names the constructor after the type it builds, errorFor
// names the refusal after the type that returns it, and entryFor names the
// entry type after the type whose elements it holds. Each takes the
// visibility the name deserves: the constructor and the error are the
// declaration's public face and follow it, and the entry type is
// representation, which is nobody's business outside the package.
//
// Through [plugin.Around] rather than by joining, so that the declaration's
// own name comes through exactly as its author wrote it and the seam is
// spelled the way every other seam forge writes is.
func constructorFor(declared string) string {
	return plugin.Around(plugin.Exported(declared), "new", declared)
}

func errorFor(declared string) string {
	return plugin.Around(plugin.Exported(declared), "err", declared, "duplicate")
}

func entryFor(declared string) string {
	return plugin.Around(false, "", declared, "entry")
}
