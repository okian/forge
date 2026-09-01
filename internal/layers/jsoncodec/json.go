package jsoncodec

import (
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"

	"github.com/okian/forge/internal/diag"
	"github.com/okian/forge/internal/emit"
	"github.com/okian/forge/internal/layer"
	"github.com/okian/forge/internal/model"
	"github.com/okian/forge/internal/shape"
)

// The marker this layer claims, and the name a directive writes it under.
const (
	container  = "Json"
	markerName = "json"
)

// optionNames is the declaration option deciding how an untagged field is
// named. The other declaration option, omitzero, is spelled exactly as the tag
// option of the same meaning and is defined with it.
const optionNames = "names"

// The packages generated code imports, and the names they bind.
//
// Written down rather than derived from the paths, because a path does not say
// what it binds: encoding/json/v2 binds json, which is exactly the name the
// last element of the path does not give.
var imports = []model.Import{
	{Path: "bytes", Name: "bytes"},
	{Path: "encoding/json/jsontext", Name: "jsontext"},
	{Path: "encoding/json/v2", Name: "json"},
	{Path: "fmt", Name: "fmt"},
	{Path: "maps", Name: "maps"},
	{Path: "slices", Name: "slices"},
}

// Layer generates a streaming JSON codec for a subject.
//
// It carries no state: what a declaration decides reaches it through the
// context it is asked to generate against, so one value serves every
// declaration in a run.
type Layer struct{}

// New returns the JSON codec layer.
func New() Layer { return Layer{} }

// Origin identifies the marker this layer claims.
func (Layer) Origin() model.TypeRef { return model.TypeRef{Pkg: model.MarkerPkg, Name: container} }

// Kind says where in a stack the layer may appear.
//
// An element layer: what it writes is about the subject rather than about the
// container holding subjects, which is why its receiver is the subject and why
// two declarations over one subject share what it produces.
func (Layer) Kind() model.Kind { return model.KindElement }

// Stage says how far along the layer is.
func (Layer) Stage() layer.Stage { return layer.StageReady }

// Doc returns the one-line summary the list command prints.
func (Layer) Doc() string {
	return "streaming codec over the subject's own fields, driven by its json tags"
}

// OptionSchema declares every option the layer accepts.
func (Layer) OptionSchema() []layer.OptionDef {
	return []layer.OptionDef{
		{
			Key: optionNames, Value: layer.ValueEnum,
			Values: []string{styleAsIs, styleSnake, styleCamel}, Default: styleAsIs,
			Doc: "how a field with no json tag is named on the wire",
		},
		{
			Key: optionOmitZero, Value: layer.ValueBool, Default: "false",
			Doc: "omit zero-valued fields without tagging each one",
		},
		{
			Key: optionFallback, Scope: layer.ScopeField, Value: layer.ValueEnum,
			Values: []string{fallbackValue},
			Doc:    "encode a field forge cannot see through reflectively, and mark that it did",
		},
	}
}

// Accepts reports whether the layer can sit on the shape beneath it.
//
// A codec is written out of the subject's fields, so a subject with none is
// nothing to write one from. What is missing is named beside what is there,
// because a capability list is what the author can act on: it says which layer
// beneath would supply it.
func (Layer) Accepts(below shape.Shape) error {
	if !below.Caps.Has(shape.Structured) {
		return fmt.Errorf("%s needs the stack beneath it to be %s, and it is %s",
			container, shape.Structured, below.Caps)
	}
	return nil
}

// Shape returns what the layer exposes to the layer above it.
//
// Encodable, and no methods. What this layer writes goes on the subject rather
// than on the declared type, and a surface describes the declared type — so a
// container above it learns that its elements can be encoded, which is what it
// needs, and does not learn method names that are not its to call.
func (Layer) Shape(_ *layer.Context, below shape.Shape) shape.Shape {
	below.Caps = below.Caps.With(shape.Encodable)
	return below
}

// Generate returns the codec for the subject and everything it reaches.
func (l Layer) Generate(ctx *layer.Context, _ shape.Shape) (layer.Unit, error) {
	if ctx == nil || ctx.Model == nil || ctx.Model.Subject == nil {
		// Not a diagnostic: a diagnostic points at a declaration, and the
		// declaration is what is missing. Reaching here is forge calling itself
		// wrongly rather than anybody writing anything.
		return layer.Unit{}, errors.New("json: asked to generate without a modelled declaration")
	}

	held := ctx.Model.Subject
	if !held.Reachable() {
		// A subject that cannot be named cannot be given a codec, and saying so
		// is better than writing a function whose parameter has no spelling.
		return layer.Unit{}, fmt.Errorf("json: %s cannot be named from the package being generated into", ctx.Model.Name)
	}

	built := &planner{
		into:     ctx.Model.Pkg.PkgPath,
		style:    style(ctx.Options),
		omitZero: flag(ctx.Options, optionOmitZero),
	}
	built.plan(held)

	if err := built.diags.Err(); err != nil {
		return layer.Unit{}, err
	}

	return provided(built)
}

// provided turns a plan into the units the package emits, one per type.
//
// Keyed by the type rather than gathered into one unit, because two
// declarations over one subject reach here twice and produce the same codec
// twice — and a package holding one function twice does not compile. The key is
// what says the two are the same thing.
func provided(built *planner) (layer.Unit, error) {
	out := make(map[string]layer.Unit, len(built.order))

	for _, held := range built.order {
		one := built.forms[held]
		if one.how != writtenStruct {
			// A struct is the only thing reached by calling a function. Every
			// other shape is written where it is used — a scalar as one token,
			// a composite as the loop around what it holds, a type with its own
			// codec by its own method — so a function for one would be a
			// declaration nothing calls, in a file the author has to keep.
			continue
		}

		unit, err := codecFor(one)
		if err != nil {
			return layer.Unit{}, err
		}
		out[held] = unit
	}

	return layer.Unit{Provides: out}, nil
}

// codecFor builds the declarations for one type's codec.
func codecFor(of *form) (layer.Unit, error) {
	w := newWriter()
	w.encoder(of)
	w.decoder(of)

	// Asked after writing rather than while planning, because whether an
	// omission can be written is a question about the sentence that would have
	// to be written — and the writer is what knows.
	if len(w.refused) > 0 {
		return layer.Unit{}, cannotOmit(w.refused[0])
	}

	decls, comments, fset, err := parsed(w.String(), of.spelled.Text)
	if err != nil {
		return layer.Unit{}, err
	}

	return layer.Unit{
		Decls:    decls,
		Comments: comments,
		Fset:     fset,
		Imports:  needed(of, decls),
	}, nil
}

// parsed reads assembled source back as declarations.
//
// The assembly is text because what it assembles is a function with loops and
// branches in it, which is many times its own size as a tree. What that costs is
// the possibility of writing something that is not Go, and this is where that
// cost is paid: the failure is an error about the codec for a named type, raised
// where the layer can still be stopped, rather than a file on disk that does not
// build.
func parsed(source, about string) ([]ast.Decl, []*ast.CommentGroup, *token.FileSet, error) {
	fset := token.NewFileSet()

	file, err := parser.ParseFile(fset, "json.go", "package forge\n\n"+source, parser.ParseComments)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("json: the codec assembled for %s is not valid Go: %w", about, err)
	}

	return file.Decls, file.Comments, fset, nil
}

// cannotOmit reports a member whose omission could not be written.
//
// Only omitzero reaches here. It asks what the Go value is, which is a question
// about a type — and a type whose parts are not reachable from here has no
// answer to give. omitempty asks what the JSON would be, which is a question
// about output, and output can always be had by producing it: a member nothing
// can answer for in advance is written into a buffer and dropped if it came
// back empty.
func cannotOmit(one member) diag.Diagnostic {
	return diag.New(codeCannotOmit, one.field.Pos,
		"%s is asked to be omitted at its zero value, which cannot be tested for here",
		one.field.Name).
		WithHint("%s", zeroHint)
}

// needed returns the imports one type's codec uses.
//
// Gathered wide and then narrowed to what the declarations name. Which packages
// a codec reaches depends on what it encodes — a struct of two strings calls
// into jsontext and nothing else, one holding a map reaches maps and slices,
// one holding a type from another package names that package — and neither
// direction is safe to guess at: a file missing an import does not compile, and
// so does one carrying an import it never names.
//
// Gathering has to be wide because an import cannot be added later: the narrow
// step can only drop candidates, so anything left out here is a package the
// file will not bind however plainly the body names it. Narrowing has to happen
// because gathering wide is deliberately generous — a member written by a codec
// of its own is reached without being spelled, so its package is a candidate
// that nothing names.
//
// Asked of the written declarations rather than tracked while writing them,
// because the declarations are what will be in the file and a tally kept
// alongside is a second account of it that can fall behind.
func needed(of *form, decls []ast.Decl) []emit.Import {
	out := make([]emit.Import, 0, len(imports))
	for _, one := range imports {
		out = append(out, emit.Import{Path: one.Path, Name: one.Name})
	}
	for _, one := range reached(of, make(map[*form]bool)) {
		out = append(out, emit.Import{Path: one.Path, Name: one.Name, Aliased: one.Aliased})
	}

	return emit.Reaching(decls, out)
}

// reached returns the imports of every type one codec's body spells.
//
// The walk stops at a struct other than the one it started from, because that
// struct's codec is a function of its own and spells its own types. It does not
// stop at anything else: a slice, a map, a pointer and an array are all written
// where they are used, so the types inside them are spelled here.
func reached(of *form, seen map[*form]bool) []model.Import {
	if of == nil || seen[of] {
		return nil
	}
	seen[of] = true

	out := append([]model.Import(nil), of.spelled.Imports...)

	for _, one := range of.members {
		held := one.of
		out = append(out, held.spelled.Imports...)
		if held.how != writtenStruct {
			out = append(out, reached(&held, seen)...)
		}
		for _, guard := range one.guards {
			out = append(out, guard.imports...)
		}
	}

	for _, part := range []*form{of.key, of.elem} {
		if part == nil {
			continue
		}
		out = append(out, part.spelled.Imports...)
		if part.how != writtenStruct {
			out = append(out, reached(part, seen)...)
		}
	}

	return out
}

// style returns the naming style the declaration asked for.
func style(options model.Options) string {
	if written, ok := options.Get(optionNames); ok && written != "" {
		return written
	}
	return styleAsIs
}

// flag returns whether a boolean option was written as true.
func flag(options model.Options, key string) bool {
	written, ok := options.Get(key)
	return ok && written == "true"
}
