package redact

import (
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"

	"github.com/okian/forge/plugin"
)

// container is the marker this layer claims, and verb what its contributions
// are keyed under.
const (
	container = "Redact"
	verb      = "redact"
)

// method is what a type carrying one of these is asked through, which is the
// name slog looks for.
const method = "LogValue"

// slogPkg is the only package a log value names.
var slogPkg = plugin.Import{Path: "log/slog", Name: "slog"}

// Layer generates the value a subject may be logged as.
//
// It carries no state: what a declaration decides reaches it through the
// context it is asked to generate against, so one value serves every
// declaration in a run.
type Layer struct{}

// New returns the redaction layer.
func New() Layer { return Layer{} }

// Origin identifies the marker this layer claims.
func (Layer) Origin() plugin.TypeRef { return plugin.TypeRef{Pkg: plugin.MarkerPkg, Name: container} }

// Binds names what this layer's output imports, so that every layer of the
// stack spells its types against the same set.
//
// One package. A log value is built out of slog's own constructors and the
// subject's own fields, and the fields bring their packages with them through
// the spelling rather than through here.
func (Layer) Binds() []plugin.Import { return []plugin.Import{slogPkg} }

// Writes names the log value this layer puts on the subject and on everything
// it reaches.
func (Layer) Writes() []string { return []string{method} }

// Kind says where in a stack the layer may appear.
//
// An element layer: what may be logged is a fact about one value rather than
// about a container of them, which is why the method goes on the subject and
// why two declarations over one subject share what it produces.
func (Layer) Kind() plugin.Kind { return plugin.KindElement }

// Stage says how far along the layer is.
func (Layer) Stage() plugin.Stage { return plugin.StageReady }

// Doc returns the one-line summary the list command prints.
func (Layer) Doc() string {
	return "log value with redact-tagged fields masked, so logging cannot leak them"
}

// OptionSchema declares every option the layer accepts, which is none.
//
// Which fields are secret is written on the fields, in the tag the ecosystem
// already reads, where somebody changing a field will see it. A declaration
// naming them in a string would be the same fact in a second place, and the
// place a reader of the struct does not look.
func (Layer) OptionSchema() []plugin.OptionDef { return nil }

// Accepts reports whether the layer can sit on the shape beneath it.
//
// A log value is written out of the subject's fields, so a subject with none is
// nothing to write one from — and a value with no fields has nothing to hide
// either, which is the same answer arrived at from the other side.
func (Layer) Accepts(below plugin.Shape) error {
	if !below.Caps.Has(plugin.Structured) {
		return fmt.Errorf("%s needs the stack beneath it to be %s, and it is %s",
			container, plugin.Structured, below.Caps)
	}
	return nil
}

// Shape returns what the layer exposes to the layer above it.
//
// Nothing added and no methods. What this layer writes goes on the subject
// rather than on the declared type, and a container above it neither gains nor
// loses by holding elements that know what may be printed of them.
func (Layer) Shape(_ *plugin.Context, below plugin.Shape) plugin.Shape { return below }

// Generate returns the log value for the subject and everything it reaches.
func (Layer) Generate(ctx *plugin.Context, _ plugin.Shape) (plugin.Unit, error) {
	if ctx == nil || ctx.Model == nil || ctx.Model.Subject == nil {
		// Not a diagnostic: a diagnostic points at a declaration, and the
		// declaration is what is missing. Reaching here is forge calling itself
		// wrongly rather than anybody writing anything.
		return plugin.Unit{}, errors.New("redact: asked to generate without a modelled declaration")
	}

	held := ctx.Model.Subject
	if !held.Reachable() {
		return plugin.Unit{}, fmt.Errorf("redact: %s cannot be named from the package being generated into",
			ctx.Model.Name)
	}

	built := &planner{into: ctx.Model.Pkg.PkgPath, bound: ctx.Bound(), at: ctx.Model.Pos}
	built.plan(held)

	if err := built.diags.Err(); err != nil {
		return plugin.Unit{}, err
	}

	return provided(built)
}

// provided turns a plan into the units the package emits, one per type that
// needs a log value.
//
// Keyed by the layer and the type together, because two element layers may sit
// over one subject and each contribute something about it: keyed by the type
// alone the package would keep whichever arrived first and drop the other,
// leaving generated code calling a method nothing declares.
func provided(built *planner) (plugin.Unit, error) {
	out := make(map[string]plugin.Unit, len(built.plans))

	for _, held := range built.written() {
		unit, err := valueFor(held)
		if err != nil {
			return plugin.Unit{}, err
		}
		out[verb+": "+key(held.of.Type())] = unit
	}

	return plugin.Unit{Provides: out}, nil
}

// valueFor builds the declarations for one type's log value.
func valueFor(held *plan) (plugin.Unit, error) {
	w := &writer{block: plugin.Locals(binds(held)...)}
	w.value(held)

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
// The failure is an error about the log value for a named type, raised where
// the layer can still be stopped, rather than a file on disk that does not
// build.
func parsed(source, about string) ([]ast.Decl, []*ast.CommentGroup, *token.FileSet, error) {
	fset := token.NewFileSet()

	file, err := parser.ParseFile(fset, "redact.go", "package forge\n\n"+source, parser.ParseComments)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("redact: the log value assembled for %s is not valid Go: %w", about, err)
	}

	return file.Decls, file.Comments, fset, nil
}

// binds is every name already visible inside the method being written: the
// packages its spellings could name, and the receiver.
//
// Gathered wide rather than narrowed, because a local is being kept away from
// these rather than emitted for them, and a name avoided for no reason costs a
// reader nothing where one taken by mistake costs them a compile.
func binds(held *plan) []string {
	out := []string{receiverVar, slogPkg.Name}

	for _, one := range held.spelled.Imports {
		out = append(out, one.Name)
	}
	for _, field := range held.fields {
		for _, one := range field.spelled.Imports {
			out = append(out, one.Name)
		}
	}
	return out
}

// needed returns the imports one type's log value uses.
//
// slog and whatever the subject's own spelling brought with it, narrowed to
// what the declarations actually name. The narrowing is what keeps a value made
// entirely of masked fields from importing the packages of the types it no
// longer prints.
func needed(held *plan, decls []ast.Decl) []plugin.Import {
	out := make([]plugin.Import, 0, len(held.spelled.Imports)+1)
	out = append(out, plugin.Import{Path: slogPkg.Path, Name: slogPkg.Name})

	out = append(out, held.spelled.Imports...)
	for _, field := range held.fields {
		out = append(out, field.spelled.Imports...)
	}

	return plugin.Reaching(decls, out)
}
