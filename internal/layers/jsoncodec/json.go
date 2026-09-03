package jsoncodec

import (
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"slices"
	"strconv"

	"github.com/okian/forge/plugin"
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
var imports = []plugin.Import{
	{Path: "bytes", Name: "bytes"},
	{Path: "encoding/json/jsontext", Name: "jsontext"},
	{Path: "encoding/json/v2", Name: "json"},
	{Path: "errors", Name: "errors"},
	{Path: "fmt", Name: "fmt"},
	{Path: "io", Name: "io"},
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
func (Layer) Origin() plugin.TypeRef { return plugin.TypeRef{Pkg: plugin.MarkerPkg, Name: container} }

// Binds names what this layer's output imports, so that every layer of the
// stack spells its types against the same set.
//
// All of them, including the ones a given subject turns out not to need. What
// this decides is which names the subject is moved out of the way of, and one
// name too many costs an alias nothing needed; one too few costs a file that
// binds json to two packages and does not build.
func (Layer) Binds() []plugin.Import { return slices.Clone(imports) }

// Writes names the codec this layer puts on the subject.
//
// It puts the same pair on every struct the subject reaches, and says so
// nowhere: what is asked here is answered against the subject, so a neighbour
// asking about a type the subject merely holds gets nothing. Nothing is the
// right answer — such a struct is one this layer is already writing a codec
// for, and it writes that codec inline rather than delegating to a method it
// has not been told about.
//
// The container's four are not here either. Those go on the declared type,
// which a surface is the question about — and the layer cannot say whether it
// will write them until it knows whether there is a container above it to walk.
func (Layer) Writes() []string { return []string{marshalMethod, unmarshalMethod} }

// Kind says where in a stack the layer may appear.
//
// An element layer: the codec it writes is about the subject rather than about
// the container holding subjects, which is why its receiver is the subject and
// why two declarations over one subject share what it produces.
//
// The declared type gets a codec as well, and that does not make this something
// else. It is written out of the subject's codec and out of the walk the
// container exposes, and it belongs to the declaration rather than to the
// package — so it goes in the declaration's own file while the subject's goes
// in the package's shared one, which is exactly the division an element layer's
// kind describes.
func (Layer) Kind() plugin.Kind { return plugin.KindElement }

// Stage says how far along the layer is.
func (Layer) Stage() plugin.Stage { return plugin.StageReady }

// Doc returns the one-line summary the list command prints.
func (Layer) Doc() string {
	return "streaming codec over the subject's own fields, and over the container holding them"
}

// OptionSchema declares every option the layer accepts.
func (Layer) OptionSchema() []plugin.OptionDef {
	return []plugin.OptionDef{
		{
			Key: optionNames, Value: plugin.ValueEnum,
			Values: []string{styleAsIs, styleSnake, styleCamel}, Default: styleAsIs,
			Doc: "how a field with no json tag is named on the wire",
		},
		{
			Key: optionOmitZero, Value: plugin.ValueBool, Default: "false",
			Doc: "omit zero-valued fields without tagging each one",
		},
		{
			Key: optionFallback, Scope: plugin.ScopeField, Value: plugin.ValueEnum,
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
func (Layer) Accepts(below plugin.Shape) error {
	if !below.Caps.Has(plugin.Structured) {
		return fmt.Errorf("%s needs the stack beneath it to be %s, and it is %s",
			container, plugin.Structured, below.Caps)
	}
	return nil
}

// Shape returns what the layer exposes to the layer above it.
//
// Encodable, and no methods. A container above it learns that its elements can
// be encoded, which is what it needs, and learns no method names that are not
// its to call: the codec for the subject goes on the subject, and a surface
// describes the declared type.
//
// The codec for the *declared* type is not on it either, and that one is a gap
// rather than a decision. It is written and it is not describable here: a
// surface is asked for while the stack is being composed, innermost first, so
// this layer is asked before there is a container above it to be walked — and
// whether there will be one to walk is what decides whether those four methods
// exist. A layer that promised them would be describing a stack a decorator may
// yet take the walk away from.
//
// What it costs is that the explain command under-reports this layer, and that
// an author who wrote WriteTo on the declared type themselves finds out from
// the compiler rather than from forge. Closing it needs composition to settle a
// shape rather than build one in a single pass, which is a change the layers
// that mask a surface will want anyway.
func (Layer) Shape(_ *plugin.Context, below plugin.Shape) plugin.Shape {
	below.Caps = below.Caps.With(plugin.Encodable)
	return below
}

// Generate returns the codec for the subject and everything it reaches.
func (l Layer) Generate(ctx *plugin.Context, _ plugin.Shape) (plugin.Unit, error) {
	if ctx == nil || ctx.Model == nil || ctx.Model.Subject == nil {
		// Not a diagnostic: a diagnostic points at a declaration, and the
		// declaration is what is missing. Reaching here is forge calling itself
		// wrongly rather than anybody writing anything.
		return plugin.Unit{}, errors.New("json: asked to generate without a modelled declaration")
	}

	held := ctx.Model.Subject
	if !held.Reachable() {
		// A subject that cannot be named cannot be given a codec, and saying so
		// is better than writing a function whose parameter has no spelling.
		return plugin.Unit{}, fmt.Errorf("json: %s cannot be named from the package being generated into", ctx.Model.Name)
	}

	built := &planner{
		into:      ctx.Model.Pkg.PkgPath,
		bound:     ctx.Bound(),
		willWrite: ctx.Writes,
		authored:  ctx.Authored,
		style:     style(ctx.Options),
		omitZero:  omitting(ctx.Options),
	}
	root := built.plan(held)

	if err := built.diags.Err(); err != nil {
		return plugin.Unit{}, err
	}

	unit, err := provided(built)
	if err != nil {
		return plugin.Unit{}, err
	}

	over, err := streaming(ctx, root)
	if err != nil {
		return plugin.Unit{}, err
	}

	return declaring(unit, over)
}

// declaring adds the declared type's own codec to what the subject's codec
// already contributed.
//
// The two go to different places and are written by one layer, which is the
// whole of what this layer being an element layer over a container means. What
// is about the subject is handed over for the package to hold once however many
// declarations asked for it; what is about the container is this declaration's,
// and goes in this declaration's file.
func declaring(unit plugin.Unit, over stack) (plugin.Unit, error) {
	if !over.writes && !over.reads {
		// Nothing above this layer offers a walk or a sink, so the declared
		// type is not something a JSON array can be read into or written out
		// of. The subject still has its codec, which is what was asked for.
		return unit, nil
	}

	w := newWriter(over.names)
	w.container(over)

	decls, comments, fset, err := parsed(w.String(), over.declared)
	if err != nil {
		return plugin.Unit{}, err
	}

	unit.Decls, unit.Comments, unit.Fset = decls, comments, fset
	unit.Imports = spelling(over, decls)

	return unit, nil
}

// spelling returns the imports the declared type's codec uses.
//
// Gathered from what this layer binds and what the element's own spelling
// needs, then narrowed to what the declarations name — the same bargain the
// subject's codec makes, and for the same reason: a file missing an import does
// not compile, and neither does one carrying an import it never names.
func spelling(over stack, decls []ast.Decl) []plugin.Import {
	out := make([]plugin.Import, 0, len(imports)+len(over.imports))
	for _, one := range imports {
		out = append(out, plugin.Import{Path: one.Path, Name: one.Name})
	}
	out = append(out, over.imports...)

	return plugin.Reaching(decls, out)
}

// provided turns a plan into the units the package emits, one per type.
//
// Keyed by the type rather than gathered into one unit, because two
// declarations over one subject reach here twice and produce the same codec
// twice — and a package holding one function twice does not compile. The key is
// what says the two are the same thing.
func provided(built *planner) (plugin.Unit, error) {
	out := make(map[string]plugin.Unit, len(built.order))

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
			return plugin.Unit{}, err
		}
		out[contribution(held)] = unit
	}

	return plugin.Unit{Provides: out}, nil
}

// contribution names what a codec for one type is, so that two contributions
// are the same one when they are.
//
// The layer as well as the type. Two element layers may sit over one subject —
// a codec and a check, which is an ordinary stack — and each contributes
// something about it; keyed by the type alone the package would keep whichever
// arrived first and silently drop the other, leaving generated code calling a
// function nothing declares. What makes two contributions the same is that they
// are about the same type *and* come from the same layer.
func contribution(spelled string) string { return markerName + ": " + spelled }

// codecFor builds the declarations for one type's codec.
func codecFor(of *form) (plugin.Unit, error) {
	w := newWriter(naming(spelled(of)...))

	// What the prepared member names are declared under, which is the type this
	// codec is for: two subjects in one package sharing a member name each get
	// their own, and neither has to know about the other.
	w.prefix = plugin.Camel(identifier(of.typ))

	w.encoder(of)
	w.decoder(of)

	// Asked after writing rather than while planning, because whether an
	// omission can be written is a question about the sentence that would have
	// to be written — and the writer is what knows.
	if len(w.refused) > 0 {
		return plugin.Unit{}, cannotOmit(w.refused[0])
	}

	decls, comments, fset, err := parsed(w.prefacing()+w.String(), of.spelled.Text)
	if err != nil {
		return plugin.Unit{}, err
	}

	return plugin.Unit{
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
func cannotOmit(one member) plugin.Diagnostic {
	return plugin.New(codeCannotOmit, one.field.Pos,
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
func needed(of *form, decls []ast.Decl) []plugin.Import {
	out := make([]plugin.Import, 0, len(imports))
	for _, one := range imports {
		out = append(out, plugin.Import{Path: one.Path, Name: one.Name})
	}
	for _, one := range reached(of, make(map[*form]bool)) {
		out = append(out, plugin.Import{Path: one.Path, Name: one.Name, Aliased: one.Aliased})
	}

	return plugin.Reaching(decls, out)
}

// reached returns the imports of every type one codec's body spells.
//
// The walk stops at a struct other than the one it started from, because that
// struct's codec is a function of its own and spells its own types. It does not
// stop at anything else: a slice, a map, a pointer and an array are all written
// where they are used, so the types inside them are spelled here.
func reached(of *form, seen map[*form]bool) []plugin.Import {
	if of == nil || seen[of] {
		return nil
	}
	seen[of] = true

	out := append([]plugin.Import(nil), of.spelled.Imports...)

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
func style(options plugin.Options) string {
	if written, ok := options.Get(optionNames); ok && written != "" {
		return written
	}
	return styleAsIs
}

// omitting reports whether the declaration asked for a member holding its zero
// value to be left out of the document.
//
// Parsed rather than compared against the word. A boolean option is validated
// with [strconv.ParseBool], which accepts 1, t, T, TRUE, 0, f, F and FALSE as
// well as the two spellings anybody writes — so a declaration written
// omitzero=1 passes validation, and a layer comparing against "true" would
// read it as off and leave a member in a document that asked for it out.
//
// A value this cannot parse is one the option checker refused before this layer
// was asked anything, so an unparseable value here reads as unwritten. Off is
// the direction to be wrong in: a member left in says more than was asked for,
// where one left out is a field nobody knows is missing.
func omitting(options plugin.Options) bool {
	written, ok := options.Get(optionOmitZero)
	if !ok {
		return false
	}

	on, err := strconv.ParseBool(written)

	return err == nil && on
}
