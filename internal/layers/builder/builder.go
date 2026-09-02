package builder

import (
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"

	"github.com/okian/forge/internal/layers/failures"
	"github.com/okian/forge/plugin"
)

// container is the marker this layer claims.
const container = "Builder"

// Layer generates a way to make a value one field at a time.
//
// It carries no state: what a declaration decides reaches it through the
// context it is asked to generate against, so one value serves every
// declaration in a run.
type Layer struct{}

// New returns the builder layer.
func New() Layer { return Layer{} }

// Origin identifies the marker this layer claims.
func (Layer) Origin() plugin.TypeRef { return plugin.TypeRef{Pkg: plugin.MarkerPkg, Name: container} }

// Binds names what this layer's output imports, so that every layer of the
// stack spells its types against the same set.
//
// The failure vocabulary a refused build reports in, which is all a builder
// needs beyond the subject's own types: a setter per field and a Build reach
// for nothing else.
func (Layer) Binds() []plugin.Import { return failures.Binds() }

// Writes names nothing, because a builder puts its methods on the builder.
//
// The setters and Build go on a type this layer invents, which nothing outside
// it holds as a field — so there is no neighbour with a question about them.
// What the subject gains is a constructor, and a constructor is a function.
func (Layer) Writes() []string { return nil }

// Kind says where in a stack the layer may appear.
//
// An element layer: a builder makes one value rather than a container of them,
// which is why what it writes is about the subject and why two declarations
// over one subject share it.
func (Layer) Kind() plugin.Kind { return plugin.KindElement }

// Stage says how far along the layer is.
func (Layer) Stage() plugin.Stage { return plugin.StageReady }

// Doc returns the one-line summary the list command prints.
func (Layer) Doc() string {
	return "fluent builder whose required fields are enforced at Build rather than at each setter"
}

// OptionSchema declares every option the layer accepts, which is none.
//
// What a builder demands is decided by the tags on the fields, and that is the
// point rather than an omission: which fields a value has to carry is the same
// decision the rules on it record, and a second way of writing it down on the
// declaration would be a second thing to keep in step.
func (Layer) OptionSchema() []plugin.OptionDef { return nil }

// Accepts reports whether the layer can sit on the shape beneath it.
//
// A builder is written out of the subject's fields, so a subject with none is
// nothing to write one from — a type with one method and no setters is a
// constructor spelled the long way.
func (Layer) Accepts(below plugin.Shape) error {
	if !below.Caps.Has(plugin.Structured) {
		return fmt.Errorf("%s needs the stack beneath it to be %s, and it is %s",
			container, plugin.Structured, below.Caps)
	}
	return nil
}

// Shape returns what the layer exposes to the layer above it.
//
// Nothing added and no methods. What this layer writes is a type beside the
// subject rather than anything on the declared type, and a container above it
// neither gains nor loses by holding elements that can be built one field at a
// time.
func (Layer) Shape(_ *plugin.Context, below plugin.Shape) plugin.Shape { return below }

// Generate returns the builder for the subject.
func (Layer) Generate(ctx *plugin.Context, _ plugin.Shape) (plugin.Unit, error) {
	if ctx == nil || ctx.Model == nil || ctx.Model.Subject == nil {
		// Not a diagnostic: a diagnostic points at a declaration, and the
		// declaration is what is missing. Reaching here is forge calling itself
		// wrongly rather than anybody writing anything.
		return plugin.Unit{}, errors.New("builder: asked to generate without a modelled declaration")
	}

	// No question here about whether a method can be attached to the subject.
	// What a builder writes is a type of its own that holds one, so a subject
	// that is an instantiation of a generic, or one from another module, is
	// written for like any other — what decides is whether its fields can be
	// named, and that is asked of each of them.
	built := planned(ctx.Model.Subject, ctx.Model.Pkg.PkgPath, ctx.Bound())
	if err := built.diags.Err(); err != nil {
		return plugin.Unit{}, err
	}

	return provided(built)
}

// provided turns a plan into the units the package emits: the builder, and the
// types it reports a missing field through.
//
// Keyed rather than gathered into one unit, because two declarations over one
// subject reach here twice and produce the same builder twice — and a package
// holding one type twice does not compile. The key is what says the two are the
// same thing.
func provided(built *plan) (plugin.Unit, error) {
	out := make(map[string]plugin.Unit, 2)

	if built.demanded > 0 {
		// Only where something can be missing. A subject no field of which has
		// to be given is one whose Build never reports anything, and a package
		// written the types for that would hold a vocabulary nothing in it
		// speaks.
		reported, err := failures.Unit()
		if err != nil {
			return plugin.Unit{}, err
		}
		out[failures.Key] = reported
	}

	unit, err := builderFor(built)
	if err != nil {
		return plugin.Unit{}, err
	}
	out[verb+": "+plugin.TypeIdentity(built.of.Type())] = unit

	return plugin.Unit{Provides: out}, nil
}

// builderFor builds the declarations for one subject's builder.
func builderFor(held *plan) (plugin.Unit, error) {
	w := &writer{}
	w.builder(held)

	decls, comments, fset, err := parsed(w.String(), held.spelled.Text)
	if err != nil {
		return plugin.Unit{}, err
	}

	return plugin.Unit{
		Decls:    decls,
		Comments: comments,
		Fset:     fset,
		Imports:  needed(held, decls),
	}, nil
}

// parsed reads assembled source back as declarations.
//
// The failure is an error about the builder for a named type, raised where the
// layer can still be stopped, rather than a file on disk that does not build.
func parsed(source, about string) ([]ast.Decl, []*ast.CommentGroup, *token.FileSet, error) {
	fset := token.NewFileSet()

	file, err := parser.ParseFile(fset, "builder.go", "package forge\n\n"+source, parser.ParseComments)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("builder: the builder assembled for %s is not valid Go: %w", about, err)
	}

	return file.Decls, file.Comments, fset, nil
}

// needed returns the imports a builder uses.
//
// Every field's own spelling, because a setter names the field's type in its
// signature — and then only what the declarations turn out to name, which is
// the bargain every generated file makes: one missing an import does not
// compile, and neither does one carrying an import it never names.
func needed(held *plan, decls []ast.Decl) []plugin.Import {
	out := make([]plugin.Import, 0, len(held.spelled.Imports)+len(held.fields))
	out = append(out, held.spelled.Imports...)
	for _, field := range held.fields {
		out = append(out, field.spelled.Imports...)
	}

	return plugin.Reaching(decls, out)
}
