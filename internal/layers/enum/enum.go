package enum

import (
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"slices"

	"github.com/okian/forge/internal/emit"
	"github.com/okian/forge/internal/layer"
	"github.com/okian/forge/internal/model"
	"github.com/okian/forge/internal/shape"
)

// container is the marker this layer claims, and verb what its contributions
// are keyed under.
const (
	container = "Enum"
	verb      = "enum"
)

// The packages generated code imports, and the names they bind.
//
// Written down rather than derived from the paths, for the reason
// [layer.Layer.Binds] gives. Both are reached by every set there is — errors
// for a name nobody declared, strconv for rendering the value that was not one
// — so what this decides is not which of them a file keeps but which names the
// subject is moved out of the way of.
var imports = []model.Import{
	{Path: "errors", Name: "errors"},
	{Path: "strconv", Name: "strconv"},
}

// Layer generates the API of a closed set.
//
// It carries no state: what a declaration decides reaches it through the
// context it is asked to generate against, so one value serves every
// declaration in a run.
type Layer struct{}

// New returns the enumeration layer.
func New() Layer { return Layer{} }

// Origin identifies the marker this layer claims.
func (Layer) Origin() model.TypeRef { return model.TypeRef{Pkg: model.MarkerPkg, Name: container} }

// Binds names what this layer's output imports, so that every layer of the
// stack spells its types against the same set.
func (Layer) Binds() []model.Import { return slices.Clone(imports) }

// Kind says where in a stack the layer may appear.
//
// An element layer: what a closed set can do is a fact about one value rather
// than about a container of them, which is why the methods go on the subject
// and why two declarations over one subject share what it produces.
func (Layer) Kind() model.Kind { return model.KindElement }

// Stage says how far along the layer is.
func (Layer) Stage() layer.Stage { return layer.StageReady }

// Doc returns the one-line summary the list command prints.
func (Layer) Doc() string {
	return "the API of a closed set, discovered from the constants declared with the subject"
}

// OptionSchema declares every option the layer accepts, which is none.
//
// What the members are is written in the constant block, which is where a
// reader of the type already looks. A declaration naming them would be the same
// list twice, and the copy that drifts is the one nobody reads.
func (Layer) OptionSchema() []layer.OptionDef { return nil }

// Accepts reports whether the layer can sit on the shape beneath it.
//
// It always can, and what decides is the subject rather than the shape. Whether
// a type is a named scalar with constants declared of it is not a capability
// and could not be one: capabilities say what a stack offers, and this is a
// question about what the subject is. It is asked where the answer is —
// [planned] refuses a subject that is not a closed set, and says which of the
// two ways it is not one.
//
// A refusal here would also be the wrong shape of refusal. What a stack is
// checked against is what it requires of what is beneath it, and there is no
// way to say "not structured": a rule written as one would be a layer refusing
// a capability rather than wanting one, which nothing else in the catalog does
// and nothing reading a catalog would expect.
func (Layer) Accepts(shape.Shape) error { return nil }

// Shape returns what the layer exposes to the layer above it.
//
// Comparable and Encodable, and no methods. A container above learns that its
// elements have a stable identity — one closed-set value equals another exactly
// when they are the same member — and that they go to text and back. What it
// does not learn is method names, because the methods go on the subject and a
// surface describes the declared type.
func (Layer) Shape(_ *layer.Context, below shape.Shape) shape.Shape {
	below.Caps = below.Caps.With(shape.Comparable, shape.Encodable)
	return below
}

// Generate returns the closed set's API.
func (Layer) Generate(ctx *layer.Context, _ shape.Shape) (layer.Unit, error) {
	if ctx == nil || ctx.Model == nil || ctx.Model.Subject == nil {
		// Not a diagnostic: a diagnostic points at a declaration, and the
		// declaration is what is missing. Reaching here is forge calling itself
		// wrongly rather than anybody writing anything.
		return layer.Unit{}, errors.New("enum: asked to generate without a modelled declaration")
	}

	held := ctx.Model.Subject
	if !held.Reachable() {
		return layer.Unit{}, fmt.Errorf("enum: %s cannot be named from the package being generated into",
			ctx.Model.Name)
	}

	// And declared on. Everything this writes goes on the subject or is named
	// after it, so a type this package cannot declare on is one the whole set
	// would have to be written somewhere it cannot be — which is a fault in the
	// declaration and is reported as one rather than as a file that does not
	// parse.
	if !held.Local(ctx.Model.Pkg.PkgPath) {
		return layer.Unit{}, elsewhere(held, ctx.Model.Pos)
	}

	built := planned(held, ctx.Model.Pkg.Fset, ctx.Model.Pkg.PkgPath, ctx.Bound(), ctx.Model.Pos)
	if err := built.diags.Err(); err != nil {
		return layer.Unit{}, err
	}

	return provided(built)
}

// provided turns a plan into the unit the package emits.
//
// Keyed by the layer and the type together, because two declarations over one
// subject reach here twice and produce the same methods twice — and a package
// holding one method twice does not compile. The key is what says the two are
// the same thing.
func provided(built *plan) (layer.Unit, error) {
	unit, err := setFor(built)
	if err != nil {
		return layer.Unit{}, err
	}

	return layer.Unit{Provides: map[string]layer.Unit{
		verb + ": " + model.TypeIdentity(built.of.Type()): unit,
	}}, nil
}

// setFor builds the declarations for one closed set.
func setFor(held *plan) (layer.Unit, error) {
	w := &writer{}
	w.set(held)

	decls, comments, fset, err := parsed(w.String(), held.spelled.Text)
	if err != nil {
		return layer.Unit{}, err
	}

	return layer.Unit{
		Decls:    decls,
		Comments: comments,
		Fset:     fset,
		Imports:  needed(held, decls),
	}, nil
}

// parsed reads assembled source back as declarations.
//
// The failure is an error about the set written for a named type, raised where
// the layer can still be stopped, rather than a file on disk that does not
// parse. Only that: a parser accepts a switch with one case twice, so what
// keeps two members of one name out of a file is the plan refusing to write
// them rather than anything caught here.
func parsed(source, about string) ([]ast.Decl, []*ast.CommentGroup, *token.FileSet, error) {
	fset := token.NewFileSet()

	file, err := parser.ParseFile(fset, "enum.go", "package forge\n\n"+source, parser.ParseComments)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("enum: the set assembled for %s is not valid Go: %w", about, err)
	}

	return file.Decls, file.Comments, fset, nil
}

// needed returns the imports one set's methods use.
//
// Narrowed against the declarations like every other layer's, though nothing is
// expected to fall out: a set reaches errors and strconv whatever its members
// are. The narrowing is here because what a layer writes is what decides its
// imports, and a list that were merely asserted to be right would be wrong the
// first time somebody changed the writer.
func needed(held *plan, decls []ast.Decl) []emit.Import {
	out := make([]emit.Import, 0, len(imports)+len(held.spelled.Imports))
	for _, one := range imports {
		out = append(out, emit.Import{Path: one.Path, Name: one.Name})
	}
	for _, one := range held.spelled.Imports {
		out = append(out, emit.Import{Path: one.Path, Name: one.Name, Aliased: one.Aliased})
	}

	return emit.Reaching(decls, out)
}
