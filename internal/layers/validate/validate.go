package validate

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
const container = "Validate"

// imports names every package a generated check may reach, and what each binds.
//
// Written down rather than derived from the paths, and gathered wide: what a
// check reaches depends on which rules were written, and the list is narrowed
// to what the declarations actually name before it reaches a file.
var imports = []model.Import{
	{Path: "regexp", Name: "regexp"},
}

// Layer generates a value's own check of the rules written on its fields.
//
// It carries no state: what a declaration decides reaches it through the
// context it is asked to generate against, so one value serves every
// declaration in a run.
type Layer struct{}

// New returns the validation layer.
func New() Layer { return Layer{} }

// Origin identifies the marker this layer claims.
func (Layer) Origin() model.TypeRef { return model.TypeRef{Pkg: model.MarkerPkg, Name: container} }

// Kind says where in a stack the layer may appear.
//
// An element layer: the check is about one value rather than about a container
// of them, which is why its receiver is the subject and why two declarations
// over one subject share what it produces.
func (Layer) Kind() model.Kind { return model.KindElement }

// Stage says how far along the layer is.
func (Layer) Stage() layer.Stage { return layer.StageReady }

// Doc returns the one-line summary the list command prints.
func (Layer) Doc() string {
	return "rules read from the subject's validate tags, checked in declaration order"
}

// OptionSchema declares every option the layer accepts, which is none.
//
// Everything this layer does is decided by the tags on the fields, and that is
// the point rather than an omission: a rule belongs beside the field it is
// about, where somebody changing the field will see it, rather than on a
// declaration that names the field in a string.
func (Layer) OptionSchema() []layer.OptionDef { return nil }

// Accepts reports whether the layer can sit on the shape beneath it.
//
// A check is written out of the subject's fields, so a subject with none is
// nothing to write one from.
func (Layer) Accepts(below shape.Shape) error {
	if !below.Caps.Has(shape.Structured) {
		return fmt.Errorf("%s needs the stack beneath it to be %s, and it is %s",
			container, shape.Structured, below.Caps)
	}
	return nil
}

// Shape returns what the layer exposes to the layer above it.
//
// Nothing added and no methods. What this layer writes goes on the subject
// rather than on the declared type, and there is no capability for "checks
// itself": a container above it neither gains nor loses anything by holding
// elements that can be asked whether they are in order.
func (Layer) Shape(_ *layer.Context, below shape.Shape) shape.Shape { return below }

// Generate returns the check for the subject and everything it reaches.
func (Layer) Generate(ctx *layer.Context, _ shape.Shape) (layer.Unit, error) {
	if ctx == nil || ctx.Model == nil || ctx.Model.Subject == nil {
		// Not a diagnostic: a diagnostic points at a declaration, and the
		// declaration is what is missing. Reaching here is forge calling itself
		// wrongly rather than anybody writing anything.
		return layer.Unit{}, errors.New("validate: asked to generate without a modelled declaration")
	}

	held := ctx.Model.Subject
	if !held.Reachable() {
		return layer.Unit{}, fmt.Errorf("validate: %s cannot be named from the package being generated into",
			ctx.Model.Name)
	}

	built := &planner{into: ctx.Model.Pkg.PkgPath}
	built.plan(held)

	if err := built.diags.Err(); err != nil {
		return layer.Unit{}, err
	}

	return provided(built)
}

// provided turns a plan into the units the package emits: one per type that
// needs a check, and one for the failures they all report through.
//
// Keyed by the type rather than gathered into one unit, because two
// declarations over one subject reach here twice and produce the same check
// twice — and a package holding one method twice does not compile. The key is
// what says the two are the same thing.
func provided(built *planner) (layer.Unit, error) {
	out, err := reporting()
	if err != nil {
		return layer.Unit{}, err
	}

	for _, held := range built.written() {
		unit, err := checkFor(held, model.Through(held.of, verb, "", built.into))
		if err != nil {
			return layer.Unit{}, err
		}
		out[contribution(key(held.of.Type()))] = unit
	}

	return layer.Unit{Provides: out}, nil
}

// contribution names what a check for one type is, so that two contributions
// are the same one when they are.
//
// The layer as well as the type. Two element layers may sit over one subject —
// a check and a codec, which is an ordinary stack — and each contributes
// something about it; keyed by the type alone the package would keep whichever
// arrived first and silently drop the other, leaving generated code calling a
// function nothing declares.
func contribution(spelled string) string { return verb + ": " + spelled }

// checkFor builds the declarations for one type's check, under the name
// everything generated calls it by.
func checkFor(held *plan, name string) (layer.Unit, error) {
	w := &writer{}
	w.patterns(held)

	if held.attach {
		w.through(held, name)
	}
	w.check(held, name)

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
// The assembly is text because what it assembles is a function of conditions,
// which is many times its own size as a tree. What that costs is the
// possibility of writing something that is not Go, and this is where that cost
// is paid: the failure is an error about the check for a named type, raised
// where the layer can still be stopped, rather than a file on disk that does
// not build.
func parsed(source, about string) ([]ast.Decl, []*ast.CommentGroup, *token.FileSet, error) {
	fset := token.NewFileSet()

	file, err := parser.ParseFile(fset, "validate.go", "package forge\n\n"+source, parser.ParseComments)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("validate: the check assembled for %s is not valid Go: %w", about, err)
	}

	return file.Decls, file.Comments, fset, nil
}

// needed returns the imports one type's check uses.
//
// Gathered wide and then narrowed to what the declarations name, which is the
// bargain every generated file makes: one missing an import does not compile,
// and neither does one carrying an import it never names.
func needed(held *plan, decls []ast.Decl) []emit.Import {
	out := make([]emit.Import, 0, len(imports)+len(held.spelled.Imports))
	for _, one := range imports {
		out = append(out, emit.Import{Path: one.Path, Name: one.Name})
	}
	for _, one := range held.spelled.Imports {
		out = append(out, emit.Import{Path: one.Path, Name: one.Name, Aliased: one.Aliased})
	}
	for _, one := range held.of.Fields {
		out = append(out, spelt(held, one)...)
	}

	return emit.Reaching(decls, out)
}

// spelt returns the imports a field's own spelling binds, which a zero written
// out as a composite literal names.
func spelt(held *plan, field model.Field) []emit.Import {
	if field.Type.Type == nil {
		return nil
	}

	out := make([]emit.Import, 0, 2)
	for _, one := range model.Spell(field.Type.Type, held.spelled.Local, nil).Imports {
		out = append(out, emit.Import{Path: one.Path, Name: one.Name, Aliased: one.Aliased})
	}
	return out
}
