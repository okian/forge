package collection

import (
	_ "embed"
	"errors"
	"fmt"
	"go/ast"
	"go/types"
	"slices"
	"strings"

	"github.com/okian/forge/internal/shared/seq"
	"github.com/okian/forge/internal/templates"
	"github.com/okian/forge/plugin"
)

// What a declaration can ask for that this layer cannot generate.
//
// The first three are about an option's value rather than about the option: the
// field exists and is spelled correctly, and what it cannot do is be read from
// here, be ordered, or be a map key. Validation checks that an option naming a
// field names one; whether the field will do is a question only the layer that
// would generate from it can answer.
var (
	codeSortNotOrdered  = plugin.Register(3013, "field cannot be ordered")
	codeIndexNotKeyable = plugin.Register(3014, "field cannot be a map key")
	codeFieldUnexported = plugin.Register(3015, "field cannot be read from the generated package")

	// The last is not a 3xxx. An option may have named one of the two names and
	// no option can un-name the other, and what it produces is a package
	// holding one declaration twice.
	codeNamesCollide = plugin.Register(4101, "two generated names are one")

	// And the one case of it a rule can settle rather than refuse: two fields
	// whose projections come out with one name, where the field spelled like
	// the name keeps it and the other is projected under its own name with
	// Values after it. Reported all the same, because the fallback is readable
	// rather than good and the pair is almost never deliberate.
	codeProjectionsShareAName = plugin.Register(4102, "two fields project to one name")
)

// bodies is the template this layer emits the field-independent half of.
//
//go:embed tmpl/tmpl.go
var bodies []byte

// What the template calls the things the rewrite is written in terms of.
const (
	container = "Collection"
	param     = "T"

	// projecting, keying and ordering are the helpers, and are methods rather
	// than package-level names so that nothing here is taken in the author's
	// package. They keep the names the template gives them: a method lives on
	// its receiver's type, where a second declaration's helpers cannot reach.
	projecting = "project"
	keying     = "keyed"
	ordering   = "ordered"

	// walking is the storage layer's, declared by the template so its bodies
	// compile and emitted by nobody here.
	walking = "All"
)

// templateImports names every package the template imports, and what a file
// importing it binds each of them to. Generation refuses one that is not here,
// so the list cannot go stale without saying so.
var templateImports = map[string]string{
	"cmp":    "cmp",
	"iter":   "iter",
	"slices": "slices",
}

// The names a method calls the collection and one of its elements are asked
// for rather than written down here. See [plan.receiver] and [naming].

// Layer generates a query surface from the subject's fields.
type Layer struct{}

// New returns the collection layer.
func New() Layer { return Layer{} }

// Origin identifies the marker this layer claims.
func (Layer) Origin() plugin.TypeRef { return plugin.TypeRef{Pkg: plugin.MarkerPkg, Name: container} }

// Kind says where in a stack the layer may appear.
func (Layer) Kind() plugin.Kind { return plugin.KindRefining }

// Stage says how far along the layer is.
func (Layer) Stage() plugin.Stage { return plugin.StageReady }

// Doc returns the one-line summary the list command prints.
func (Layer) Doc() string {
	return "query, projection and sort methods built from the subject's fields"
}

// Transparent reports that the raw underlying type upholds this layer's
// invariants, because it adds none: everything it generates reads what is
// beneath it and returns something new.
func (Layer) Transparent() bool { return true }

// OptionSchema declares every option the layer accepts.
func (Layer) OptionSchema() []plugin.OptionDef {
	return []plugin.OptionDef{
		{Key: "sort", Value: plugin.ValueFields, Doc: "fields to generate a sorted view for"},
		{Key: "index", Value: plugin.ValueFields, Doc: "fields to generate a lookup map for"},
		{Key: "seq", Value: plugin.ValueString, Doc: "name for the generated sequence view, when the default collides"},
	}
}

// Accepts reports whether the layer can sit on the shape beneath it.
//
// It needs to be able to walk what is beneath it and nothing else. Everything
// it generates is a question asked of the elements in order, which is what
// being streamable means — asking for a length as well would narrow it to the
// storages that count without buying anything the answers need.
func (Layer) Accepts(below plugin.Shape) error {
	if !below.Caps.Has(plugin.Streamable) {
		return fmt.Errorf("%s needs the stack beneath it to be %s, and it is %s",
			container, plugin.Streamable, below.Caps)
	}
	return nil
}

// Shape returns what the layer exposes to the layer above it.
//
// It adds no capability. What it adds is surface, and the whole of that surface
// depends on the declaration: a projection per field, a sorted view per
// declared sort key, a lookup per declared index key, each named after the
// field it reads and the view named after the declared type. Given the
// declaration this reports all of them, spelled as they will be emitted, so a
// decorator above can wrap them and collision detection can see them.
//
// Given no declaration it reports the one method that does not depend on one,
// and reports it without a signature rather than guessing: the result type is
// named after the declared type, and a rendering of a name nothing here knows
// is a string a reader would take for source.
//
// Diagnostics from reading the declaration are dropped here and reported at
// generation, which is where they belong. A shape is a description, and a
// caller asking what a layer would emit for a declaration that cannot be
// generated wants the description it can have rather than a second copy of the
// reason — which the run is about to print anyway.
func (l Layer) Shape(ctx *plugin.Context, below plugin.Shape) plugin.Shape {
	if ctx == nil || ctx.Model == nil || ctx.Model.Subject == nil {
		return below.WithMethods(plugin.Method{
			Name:  "Seq",
			Owner: l.Origin(),
			Doc:   "Seq returns a lazy view over the elements, named after the declared type.",
		})
	}

	surface, _ := planned(ctx, below)

	return below.WithMethods(surface.surface(l.Origin())...)
}

// Generate returns the declarations this layer contributes.
func (l Layer) Generate(ctx *plugin.Context, below plugin.Shape) (plugin.Unit, error) {
	if ctx == nil || ctx.Model == nil || ctx.Model.Subject == nil {
		return plugin.Unit{}, errors.New("collection: asked to generate without a modelled declaration")
	}

	surface, diags := planned(ctx, below)

	clashes := surface.clashes()
	diags.Merge(&clashes)

	if err := diags.Err(); err != nil {
		return plugin.Unit{}, err
	}

	out, refused := l.apply(ctx, surface)
	if err := refused.Err(); err != nil {
		return plugin.Unit{}, err
	}

	if wrong := accounted(out.Imports); wrong != "" {
		return plugin.Unit{}, fmt.Errorf("collection: %s", wrong)
	}

	built, err := surface.build()
	if err != nil {
		return plugin.Unit{}, err
	}

	shared, err := helpers(out.Decls, surface)
	if err != nil {
		return plugin.Unit{}, err
	}

	// Built first, then the helpers they call, which is the order a reader
	// wants: what the declaration gained, and then how it works.
	decls := slices.Concat(built, shared)

	return plugin.Unit{
		Decls:    decls,
		Comments: out.Comments,
		Fset:     out.Fset,
		Imports:  append(plugin.Reaching(decls, out.Imports), imported(surface.imports())...),
		Requires: []plugin.TypeRef{seq.Ref(surface.pkg)},
	}, nil
}

// apply specialises the template's half of the output.
func (Layer) apply(ctx *plugin.Context, surface plan) (templates.Result, plugin.Diagnostics) {
	return templates.Apply(
		templates.Template{Name: "collection", Source: bodies},
		templates.Rewrite{
			Param:     param,
			Subject:   surface.subject.Text,
			Container: container,
			Declared:  ctx.Declared(),
			Prefix:    plugin.Camel(ctx.Declared()),
			// The same name the built half uses, and for the same reason: a
			// helper's body spells the subject wherever the template wrote its
			// type parameter, so a subject called c inside a method whose
			// receiver is c does not compile.
			Receiver: surface.receiver,
		},
		ctx.Model.Pos)
}

// helpers returns the template's declarations that this declaration reaches.
//
// A stack that names no sort has no use for the sorting helper, and emitting it
// anyway would put a method on the author's type that nothing calls — small,
// and the kind of thing that accumulates once twelve layers are doing it.
//
// The container's own declaration and the storage layer's walk go too. The
// template declares both so its bodies compile; neither is this layer's to
// emit, and emitting either would redeclare something that is already there.
func helpers(decls []ast.Decl, surface plan) ([]ast.Decl, error) {
	wanted := map[string]bool{
		projecting: len(surface.projections) > 0,
		keying:     len(surface.indexes) > 0,
		ordering:   len(surface.sorts) > 0,
	}

	out := make([]ast.Decl, 0, len(decls))
	for _, decl := range decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Recv == nil {
			// The container's type declaration, and anything else the template
			// declares at package level, which this layer does not own.
			continue
		}

		if _, known := wanted[fn.Name.Name]; !known && fn.Name.Name != walking {
			// A method nothing here has heard of, which is a template that grew
			// one and a layer that would drop it without a word. The same
			// answer an import nobody recorded gets, for the same reason: what
			// either produces is a file missing something it calls.
			return nil, fmt.Errorf("collection: the template declares %s, which nothing here emits or leaves out",
				fn.Name.Name)
		}
		if wanted[fn.Name.Name] {
			out = append(out, decl)
		}
	}
	return out, nil
}

// accounted reports a template import nothing wrote down, or nothing.
func accounted(imports []plugin.Import) string {
	for _, one := range imports {
		if _, known := templateImports[one.Path]; !known {
			return "the template imports " + one.Path + ", which nothing recorded a bound name for"
		}
	}
	return ""
}

// Binds names what this layer's output imports, so that every layer of the
// stack spells its types against the same set.
//
// Path and name both. A field typed iter.Seq joins the import the template
// already has rather than being aliased away from it — an alias there would
// leave one path bound twice across the file, since the storage layer beneath
// binds it under its own name.
func (Layer) Binds() []plugin.Import { return taken() }

// Writes names nothing, because everything this layer writes is about the
// container rather than about what is in it.
func (Layer) Writes() []string { return nil }

// taken returns what the template's imports bind, sorted so that what is built
// from them does not depend on the order a map was walked in.
func taken() []plugin.Import {
	out := make([]plugin.Import, 0, len(templateImports))
	for path, name := range templateImports {
		out = append(out, plugin.Import{Path: path, Name: name})
	}

	slices.SortFunc(out, func(a, b plugin.Import) int { return strings.Compare(a.Path, b.Path) })
	return out
}

// imported returns what a spelling depends on in the shape a unit carries.
//
// The name comes across whether or not it will be written. What is written is
// decided by Aliased, and the name itself is what says which package a
// qualified identifier in the file refers to — a question asked of the file
// later, by anything working out which of its imports it still needs.
func imported(needed []plugin.Import) []plugin.Import {
	out := make([]plugin.Import, 0, len(needed))
	out = append(out, needed...)
	return out
}

// viewName is what the view over a declaration's elements is called: the
// declared name with Seq after it, unless the declaration said otherwise
// because that name was taken.
func viewName(declared, asked string) string {
	if asked != "" {
		return asked
	}
	return declared + "Seq"
}

// orderable reports whether a field can be compared for order, which is what
// sorting by it needs.
//
// The underlying type decides, because that is what the constraint says: a
// named type over an integer is as ordered as the integer is. A boolean, a
// complex number, a struct and a slice are not, and neither is anything an
// interface hides.
func orderable(t types.Type) bool {
	basic, ok := t.Underlying().(*types.Basic)
	return ok && basic.Info()&types.IsOrdered != 0
}

// keyable reports whether a field can be a map key, which is what indexing by
// it needs. A slice, a map and a function cannot; a struct of them cannot
// either, which is why this asks the type rather than its shape.
func keyable(t types.Type) bool { return types.Comparable(t) }
