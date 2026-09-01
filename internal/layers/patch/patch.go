package patch

import (
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"

	"github.com/okian/forge/internal/emit"
	"github.com/okian/forge/internal/layer"
	"github.com/okian/forge/internal/model"
	"github.com/okian/forge/internal/shape"
)

// container is the marker this layer claims.
const container = "Patch"

// Layer generates the shape a partial update takes.
//
// It carries no state: what a declaration decides reaches it through the
// context it is asked to generate against, so one value serves every
// declaration in a run.
type Layer struct{}

// New returns the patch layer.
func New() Layer { return Layer{} }

// Origin identifies the marker this layer claims.
func (Layer) Origin() model.TypeRef { return model.TypeRef{Pkg: model.MarkerPkg, Name: container} }

// Kind says where in a stack the layer may appear.
//
// An element layer: a patch is about one value rather than about a container of
// them, which is why what it writes is about the subject and why two
// declarations over one subject share it.
func (Layer) Kind() model.Kind { return model.KindElement }

// Stage says how far along the layer is.
func (Layer) Stage() layer.Stage { return layer.StageReady }

// Doc returns the one-line summary the list command prints.
func (Layer) Doc() string {
	return "a companion type of pointers, so that a field cleared and a field not mentioned stay apart"
}

// OptionSchema declares every option the layer accepts, which is none.
//
// A patch carries the fields the subject has, and which of them a caller wants
// to change is decided at run time by whoever fills one in — there is nothing
// left for a declaration to say.
func (Layer) OptionSchema() []layer.OptionDef { return nil }

// Accepts reports whether the layer can sit on the shape beneath it.
//
// A patch is written out of the subject's fields, so a subject with none is
// nothing to write one from. Which fields those turn out to be is decided later
// and reported there: a shape says whether the subject has fields at all, and
// whether any of them is one a caller could change is a question about the
// fields themselves.
func (Layer) Accepts(below shape.Shape) error {
	if !below.Caps.Has(shape.Structured) {
		return fmt.Errorf("%s needs the stack beneath it to be %s, and it is %s",
			container, shape.Structured, below.Caps)
	}
	return nil
}

// Shape returns what the layer exposes to the layer above it.
//
// Nothing added and no methods. What this layer writes is a type beside the
// subject rather than anything on the declared type, and a container above it
// neither gains nor loses by holding elements a caller can describe a partial
// change to.
func (Layer) Shape(_ *layer.Context, below shape.Shape) shape.Shape { return below }

// Generate returns the patch for the subject.
func (Layer) Generate(ctx *layer.Context, _ shape.Shape) (layer.Unit, error) {
	if ctx == nil || ctx.Model == nil || ctx.Model.Subject == nil {
		// Not a diagnostic: a diagnostic points at a declaration, and the
		// declaration is what is missing. Reaching here is forge calling itself
		// wrongly rather than anybody writing anything.
		return layer.Unit{}, errors.New("patch: asked to generate without a modelled declaration")
	}

	// No question here about whether a method can be attached to the subject.
	// What a patch writes is a type of its own that describes one, so a subject
	// that is an instantiation of a generic, or one from another module, is
	// written for like any other — what decides is whether its fields can be
	// named, and that is asked of each of them.
	built := planned(ctx.Model.Subject, ctx.Model.Pkg.PkgPath)
	if err := built.diags.Err(); err != nil {
		return layer.Unit{}, err
	}

	return provided(built)
}

// provided turns a plan into the unit the package emits.
//
// Keyed rather than handed over as this declaration's own, because two
// declarations over one subject reach here twice and produce the same patch
// twice — and a package holding one type twice does not compile. The key is
// what says the two are the same thing.
func provided(built *plan) (layer.Unit, error) {
	unit, err := patchFor(built)
	if err != nil {
		return layer.Unit{}, err
	}

	return layer.Unit{Provides: map[string]layer.Unit{
		verb + ": " + model.TypeIdentity(built.of.Type()): unit,
	}}, nil
}

// patchFor builds the declarations for one subject's patch.
func patchFor(held *plan) (layer.Unit, error) {
	w := &writer{}
	w.patch(held)

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
// The failure is an error about the patch for a named type, raised where the
// layer can still be stopped, rather than a file on disk that does not build.
func parsed(source, about string) ([]ast.Decl, []*ast.CommentGroup, *token.FileSet, error) {
	fset := token.NewFileSet()

	file, err := parser.ParseFile(fset, "patch.go", "package forge\n\n"+source, parser.ParseComments)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("patch: the patch assembled for %s is not valid Go: %w", about, err)
	}

	return file.Decls, file.Comments, fset, nil
}

// needed returns the imports a patch uses.
//
// Every field's own spelling, because the patch names each field's type in its
// own declaration — and then only what the declarations turn out to name, which
// is the bargain every generated file makes: one missing an import does not
// compile, and neither does one carrying an import it never names.
func needed(held *plan, decls []ast.Decl) []emit.Import {
	out := make([]emit.Import, 0, len(held.spelled.Imports)+len(held.fields))
	for _, one := range held.spelled.Imports {
		out = append(out, emit.Import{Path: one.Path, Name: one.Name, Aliased: one.Aliased})
	}
	for _, field := range held.fields {
		for _, one := range field.spelled.Imports {
			out = append(out, emit.Import{Path: one.Path, Name: one.Name, Aliased: one.Aliased})
		}
	}

	return emit.Reaching(decls, out)
}
