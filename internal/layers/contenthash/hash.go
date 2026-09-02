package contenthash

import (
	_ "embed"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"slices"

	"github.com/okian/forge/internal/emit"
	"github.com/okian/forge/internal/layer"
	"github.com/okian/forge/internal/layers/embedded"
	"github.com/okian/forge/internal/model"
	"github.com/okian/forge/internal/shape"
)

// container is the marker this layer claims.
const container = "Hash"

// sharedKey is what the arithmetic is contributed under.
//
// One key for the package rather than one per subject, because there is one
// copy of it however many declarations asked: a package holding two fnvUint64
// functions does not compile, and the key is what says the two contributions
// are the same thing.
const sharedKey = "hash: the arithmetic every hash is built from"

// arithmetic is the source of the functions every hash is built out of,
// embedded from the package beside this one.
//
// Embedded rather than quoted, so that what is emitted is Go this repository's
// own build compiles, its own vet reads and its own tests exercise. Code that
// is only ever a string is code nothing checks until somebody's generated file
// fails to build.
//
//go:embed shared/shared.go
var arithmetic []byte

// sharedImports names what the arithmetic imports, and what each binds.
//
// Written down rather than read off the file, so that an import added to it is
// a change somebody makes here as well — and so that a run narrows against a
// list rather than against a second parse of the same bytes.
var sharedImports = []model.Import{
	{Path: "math", Name: "math"},
}

// Layer generates a stable content hash for a value and everything it reaches.
//
// It carries no state: what a declaration decides reaches it through the
// context it is asked to generate against, so one value serves every
// declaration in a run.
type Layer struct{}

// New returns the hash layer.
func New() Layer { return Layer{} }

// Origin identifies the marker this layer claims.
func (Layer) Origin() model.TypeRef { return model.TypeRef{Pkg: model.MarkerPkg, Name: container} }

// Binds names what this layer's output imports, so that every layer of the
// stack spells its types against the same set.
//
// What the arithmetic imports, which is what the hash is written in terms of.
func (Layer) Binds() []model.Import { return slices.Clone(sharedImports) }

// Writes names the hash this layer puts on the subject.
func (Layer) Writes() []string { return []string{method} }

// Kind says where in a stack the layer may appear.
//
// An element layer: a hash is about one value rather than about a container of
// them, which is why its receiver is the subject and why two declarations over
// one subject share what it produces.
func (Layer) Kind() model.Kind { return model.KindElement }

// Stage says how far along the layer is.
func (Layer) Stage() layer.Stage { return layer.StageReady }

// Doc returns the one-line summary the list command prints.
func (Layer) Doc() string {
	return "stable content hash, which is what lets a subject with no comparable form be a set member"
}

// OptionSchema declares every option the layer accepts.
//
// One, and it is written above a field rather than on the declaration. What it
// says is that a field takes no part in the value's identity, and that is a
// fact about the field — a cached total, a timestamp of when the value was
// read, a mutex — so it belongs where somebody changing the field will see it.
func (Layer) OptionSchema() []layer.OptionDef {
	return []layer.OptionDef{
		{
			Key: optionIgnore, Scope: layer.ScopeField, Value: layer.ValueNone,
			Doc: "leave this field out of the hash, because it is not part of what the value is",
		},
	}
}

// Accepts reports whether the layer can sit on the shape beneath it.
//
// It always can. A hash is taken of whatever the subject turns out to be: a
// struct is its fields, and a name over a number is the number — which is a
// true answer rather than a missing one, and the one an enumeration will want.
func (Layer) Accepts(shape.Shape) error { return nil }

// Shape returns what the layer exposes to the layer above it.
//
// Comparable, and no methods. A container above it learns that its elements
// have a stable identity, which is what a set or a lookup map needs to know,
// and learns no method names that are not its to call: the hash goes on the
// subject, and a surface describes the declared type.
func (Layer) Shape(_ *layer.Context, below shape.Shape) shape.Shape {
	below.Caps = below.Caps.With(shape.Comparable)
	return below
}

// Generate returns the hash for the subject and everything it reaches.
func (Layer) Generate(ctx *layer.Context, _ shape.Shape) (layer.Unit, error) {
	if ctx == nil || ctx.Model == nil || ctx.Model.Subject == nil {
		// Not a diagnostic: a diagnostic points at a declaration, and the
		// declaration is what is missing. Reaching here is forge calling itself
		// wrongly rather than anybody writing anything.
		return layer.Unit{}, errors.New("hash: asked to generate without a modelled declaration")
	}

	held := ctx.Model.Subject
	if !held.Reachable() {
		return layer.Unit{}, fmt.Errorf("hash: %s cannot be named from the package being generated into",
			ctx.Model.Name)
	}

	built := &planner{into: ctx.Model.Pkg.PkgPath, bound: ctx.Bound()}
	built.plan(held)

	if err := built.diags.Err(); err != nil {
		return layer.Unit{}, err
	}

	return provided(built)
}

// provided turns a plan into the units the package emits: one per type that
// needs a hash, and one for the arithmetic they are all built out of.
//
// Keyed by the layer and the type together, because two element layers may sit
// over one subject and each contribute something about it: keyed by the type
// alone the package would keep whichever arrived first and drop the other,
// leaving generated code calling a function nothing declares.
func provided(built *planner) (layer.Unit, error) {
	out := make(map[string]layer.Unit, len(built.plans)+1)

	arithmetic, err := shared()
	if err != nil {
		return layer.Unit{}, err
	}
	out[sharedKey] = arithmetic

	for _, held := range built.written() {
		unit, err := hashFor(held, model.Through(held.of, verb, "", built.into))
		if err != nil {
			return layer.Unit{}, err
		}
		out[verb+": "+key(held.of.Type())] = unit
	}

	return layer.Unit{Provides: out}, nil
}

// shared returns the arithmetic as a contribution the package holds once.
func shared() (layer.Unit, error) {
	unit, err := embedded.Unit("shared.go", arithmetic, sharedImports)
	if err != nil {
		return layer.Unit{}, fmt.Errorf("hash: %w", err)
	}
	return unit, nil
}

// hashFor builds the declarations for one type's hash, under the name
// everything generated calls it by.
func hashFor(held *plan, name string) (layer.Unit, error) {
	w := &writer{}

	if held.attach {
		w.through(held, name)
	}
	w.hash(held, name)

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
// The failure is an error about the hash for a named type, raised where the
// layer can still be stopped, rather than a file on disk that does not build.
func parsed(source, about string) ([]ast.Decl, []*ast.CommentGroup, *token.FileSet, error) {
	fset := token.NewFileSet()

	file, err := parser.ParseFile(fset, "hash.go", "package forge\n\n"+source, parser.ParseComments)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("hash: the hash assembled for %s is not valid Go: %w", about, err)
	}

	return file.Decls, file.Comments, fset, nil
}

// needed returns the imports one type's hash uses.
//
// Gathered wide and then narrowed to what the declarations name, which is the
// bargain every generated file makes: one missing an import does not compile,
// and neither does one carrying an import it never names.
func needed(held *plan, decls []ast.Decl) []emit.Import {
	out := make([]emit.Import, 0, len(held.spelled.Imports))
	for _, one := range held.spelled.Imports {
		out = append(out, emit.Import{Path: one.Path, Name: one.Name, Aliased: one.Aliased})
	}
	for _, one := range held.fields {
		out = append(out, reaching(one.of)...)
	}
	out = append(out, reaching(held.value)...)

	return emit.Reaching(decls, out)
}

// reaching returns the imports one form's own spelling and everything under it
// bind.
//
// A hash names the types it takes apart only where it converts one, which is a
// question of what the form turned out to be rather than of what was written —
// so the candidates are gathered from every form beneath and narrowed against
// the declarations that were actually written.
func reaching(of *form) []emit.Import {
	if of == nil {
		return nil
	}

	out := make([]emit.Import, 0, len(of.spelled.Imports)+1)
	for _, one := range of.spelled.Imports {
		out = append(out, emit.Import{Path: one.Path, Name: one.Name, Aliased: one.Aliased})
	}

	out = append(out, reaching(of.key)...)
	out = append(out, reaching(of.elem)...)
	for _, one := range of.members {
		out = append(out, reaching(one.of)...)
	}

	return out
}
