package clone

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

// container is the marker this layer claims.
const container = "Clone"

// imports names the packages a copy may reach, and what each binds.
//
// Written down rather than derived from the paths, and gathered wide: which of
// them a copy reaches depends on what the subject holds, and the list is
// narrowed to what the declarations actually name before it reaches a file.
var imports = []model.Import{
	{Path: "maps", Name: "maps"},
	{Path: "slices", Name: "slices"},
}

// Layer generates a copy of a value that shares nothing with it.
//
// It carries no state: what a declaration decides reaches it through the
// context it is asked to generate against, so one value serves every
// declaration in a run.
type Layer struct{}

// New returns the clone layer.
func New() Layer { return Layer{} }

// Origin identifies the marker this layer claims.
func (Layer) Origin() model.TypeRef { return model.TypeRef{Pkg: model.MarkerPkg, Name: container} }

// Binds names what this layer's output imports, so that every layer of the
// stack spells its types against the same set.
//
// Both are named whether or not the subject in hand holds a map or a slice,
// because what this decides is which names the subject is moved out of the way
// of — and a subject from a package called slices has to be moved out of the
// way of the one a copy would reach for.
func (Layer) Binds() []model.Import { return slices.Clone(imports) }

// Kind says where in a stack the layer may appear.
//
// An element layer: a copy is about one value rather than about a container of
// them, which is why its receiver is the subject and why two declarations over
// one subject share what it produces.
func (Layer) Kind() model.Kind { return model.KindElement }

// Stage says how far along the layer is.
func (Layer) Stage() layer.Stage { return layer.StageReady }

// Doc returns the one-line summary the list command prints.
func (Layer) Doc() string {
	return "deep copy over everything reachable from the subject, with an explicit aliasing policy"
}

// OptionSchema declares every option the layer accepts.
func (Layer) OptionSchema() []layer.OptionDef {
	return []layer.OptionDef{
		{
			Key: optionAliasing, Value: layer.ValueEnum,
			Values: []string{aliasingCopy, aliasingShare}, Default: aliasingCopy,
			Doc: "whether a pointer, slice or map is copied or shared with the original",
		},
		{
			Key: optionAliasing, Scope: layer.ScopeField, Value: layer.ValueEnum,
			Values: []string{aliasingShare},
			Doc:    "carry this field across as it is rather than copying what it refers to",
		},
	}
}

// Accepts reports whether the layer can sit on the shape beneath it.
//
// It always can. A copy is written for whatever the subject turns out to be,
// and a subject with no fields is copied by being assigned — which is a true
// answer rather than a missing one.
func (Layer) Accepts(shape.Shape) error { return nil }

// Shape returns what the layer exposes to the layer above it.
//
// Nothing added and no methods. What this layer writes goes on the subject
// rather than on the declared type, and there is no capability for "copies
// itself": a container above it neither gains nor loses anything by holding
// elements that can be copied.
func (Layer) Shape(_ *layer.Context, below shape.Shape) shape.Shape { return below }

// Generate returns the copy for the subject and everything it reaches.
func (Layer) Generate(ctx *layer.Context, _ shape.Shape) (layer.Unit, error) {
	if ctx == nil || ctx.Model == nil || ctx.Model.Subject == nil {
		// Not a diagnostic: a diagnostic points at a declaration, and the
		// declaration is what is missing. Reaching here is forge calling itself
		// wrongly rather than anybody writing anything.
		return layer.Unit{}, errors.New("clone: asked to generate without a modelled declaration")
	}

	held := ctx.Model.Subject
	if !held.Reachable() {
		return layer.Unit{}, fmt.Errorf("clone: %s cannot be named from the package being generated into",
			ctx.Model.Name)
	}

	built := &planner{into: ctx.Model.Pkg.PkgPath, bound: ctx.Bound(), sharing: sharing(ctx.Options)}
	built.plan(held)

	if err := built.diags.Err(); err != nil {
		return layer.Unit{}, err
	}

	return provided(built)
}

// sharing reports whether the declaration asked for references to be carried
// across rather than copied.
//
// The default is applied here as well as declared in the schema, because a
// layer is asked to generate for declarations that were never through options
// validation — the layer's own tests, and any caller assembling a model by
// hand — and one that read the option raw would copy for them and share for
// everybody else.
func sharing(options model.Options) bool {
	held, written := options.Get(optionAliasing)
	return written && held == aliasingShare
}

// provided turns a plan into the units the package emits, one per type.
//
// Keyed by the layer and the type together, because two element layers may sit
// over one subject and each contribute something about it: keyed by the type
// alone the package would keep whichever arrived first and drop the other.
func provided(built *planner) (layer.Unit, error) {
	out := make(map[string]layer.Unit, len(built.plans))

	for _, held := range built.written() {
		unit, err := copyFor(held, model.Through(held.of, verb, "", built.into))
		if err != nil {
			return layer.Unit{}, err
		}
		out[verb+": "+key(held.of.Type())] = unit
	}

	return layer.Unit{Provides: out}, nil
}

// copyFor builds the declarations for one type's copy, under the name
// everything generated calls it by.
func copyFor(held *plan, name string) (layer.Unit, error) {
	w := &writer{}

	if held.attach {
		w.through(held, name)
	}
	w.copy(held, name)

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
// The failure is an error about the copy for a named type, raised where the
// layer can still be stopped, rather than a file on disk that does not build.
func parsed(source, about string) ([]ast.Decl, []*ast.CommentGroup, *token.FileSet, error) {
	fset := token.NewFileSet()

	file, err := parser.ParseFile(fset, "clone.go", "package forge\n\n"+source, parser.ParseComments)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("clone: the copy assembled for %s is not valid Go: %w", about, err)
	}

	return file.Decls, file.Comments, fset, nil
}

// needed returns the imports one type's copy uses.
//
// Everything the copy might name and then only what it does. A copy names the
// types it builds again — the slice it makes, the map it makes, the pointer's
// element it declares — so the candidates are gathered from the forms rather
// than from a fixed list, and narrowed against the declarations that were
// actually written.
func needed(held *plan, decls []ast.Decl) []emit.Import {
	out := make([]emit.Import, 0, len(held.spelled.Imports)+len(imports))
	for _, one := range imports {
		out = append(out, emit.Import{Path: one.Path, Name: one.Name})
	}
	for _, one := range held.spelled.Imports {
		out = append(out, emit.Import{Path: one.Path, Name: one.Name, Aliased: one.Aliased})
	}
	for _, one := range held.fields {
		out = append(out, reaching(one.of)...)
	}

	return emit.Reaching(decls, out)
}

// reaching returns the imports one form's own spelling and everything under it
// bind.
func reaching(of *form) []emit.Import {
	if of == nil {
		return nil
	}

	out := make([]emit.Import, 0, len(of.spelled.Imports)+1)
	for _, one := range of.spelled.Imports {
		out = append(out, emit.Import{Path: one.Path, Name: one.Name, Aliased: one.Aliased})
	}

	return append(out, reaching(of.elem)...)
}
