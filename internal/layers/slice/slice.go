package slice

import (
	_ "embed"
	"errors"
	"fmt"
	"go/ast"
	"go/token"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/okian/forge/internal/diag"
	"github.com/okian/forge/internal/emit"
	"github.com/okian/forge/internal/layer"
	"github.com/okian/forge/internal/model"
	"github.com/okian/forge/internal/shape"
	"github.com/okian/forge/internal/templates"
)

// bodies is the template this layer emits, embedded from the package beside it.
//
// Embedded rather than quoted, so that what is emitted is a Go file the
// ordinary build compiles, the ordinary vet reads and this package's own tests
// exercise. A body that is only ever a string is a body nothing checks until
// somebody's generated file fails to build.
//
//go:embed tmpl/tmpl.go
var bodies []byte

// container is the type the template declares, and param the element it is
// generic over. They are the two names the rewrite is written in terms of, and
// they are written here rather than passed in because they are facts about the
// file above and change only when it does.
const (
	container = "Slice"
	param     = "T"
)

// constructorInTemplate is what the template calls its constructor. A package
// of one type calls it New; the declaration it becomes is one type among an
// author's own, where New belongs to nobody.
const constructorInTemplate = "New"

// templateImports names every package the template imports, and what a file
// importing it binds that package to.
//
// Written down rather than read off the paths, because a path does not say what
// it binds: encoding/json/v2 binds json and math/rand/v2 binds rand, so taking
// the last element under-reports exactly the names most worth knowing. And
// under-reporting is the harmful direction — a name this does not mention is a
// name the subject is not moved out of the way of, which is the collision the
// spelling exists to prevent.
//
// Neither half of it is left to be kept in step by hand. Generate refuses a
// template whose imports have grown past this list, and this package's tests
// ask the packages themselves what they bind, since a name recorded wrongly is
// the one mistake nothing derivable from a path can catch.
var templateImports = map[string]string{
	"iter":   "iter",
	"slices": "slices",
}

// taken returns what the template's imports bind, sorted so that a spelling
// built from them does not depend on a map.
//
// Path and name both, because a spelling given only names would alias a subject
// from a package the template already imports — the file would then bind one
// path twice, which is the mistake the names were passed to prevent, wearing
// the other face.
func taken() []model.Import {
	out := make([]model.Import, 0, len(templateImports))
	for path, name := range templateImports {
		out = append(out, model.Import{Path: path, Name: name})
	}

	slices.SortFunc(out, func(a, b model.Import) int { return strings.Compare(a.Path, b.Path) })
	return out
}

// Layer generates append-ordered slice storage.
//
// It carries no state. What a declaration decides reaches it through the
// context it is asked to generate against, so one value serves every
// declaration in a run and there is nothing to reset between them.
type Layer struct{}

// New returns the slice storage layer.
func New() Layer { return Layer{} }

// Origin identifies the marker this layer claims.
func (Layer) Origin() model.TypeRef { return model.TypeRef{Pkg: model.MarkerPkg, Name: container} }

// Kind says where in a stack the layer may appear.
func (Layer) Kind() model.Kind { return model.KindStorage }

// Stage says how far along the layer is.
func (Layer) Stage() layer.Stage { return layer.StageReady }

// Doc returns the one-line summary the list command prints.
func (Layer) Doc() string {
	return "append-ordered backing array, and the storage a refining layer gets when none is written"
}

// Transparent reports that the raw underlying type upholds this layer's
// invariants, because it has none: any slice of the element is a valid one, so
// a declaration over this storage may be written in an ordinary file.
func (Layer) Transparent() bool { return true }

// OptionSchema declares every option the layer accepts, which is none.
//
// A slice has nothing to configure. Capacity is a property of one value rather
// than of the type, so it belongs to whoever builds the value; ordering belongs
// to the layers above.
func (Layer) OptionSchema() []layer.OptionDef { return nil }

// Accepts reports whether the layer can sit on the shape beneath it.
//
// It always can. Storage is the bottom of a stack's representation: what is
// beneath it is the subject and whatever element layers attached to the
// subject, and none of that is something a container has to be able to do
// anything with.
func (Layer) Accepts(shape.Shape) error { return nil }

// Shape returns what the layer exposes to the layer above it.
//
// Indexed is among them and no method backs it, which is deliberate: the
// underlying type is the slice itself, so reaching an element by position is
// something the language already does and a method would only be a second way
// of writing it.
func (l Layer) Shape(_ *layer.Context, below shape.Shape) shape.Shape {
	below.Caps = below.Caps.With(shape.Sized, shape.Ordered, shape.Indexed, shape.Streamable)
	return below.WithMethods(l.methods(below.Elem)...)
}

// methods is the surface this layer emits, described for the layers above it.
//
// It is written out rather than read back from the template. The two could
// disagree, and this package's tests are what keep them from doing so — but a
// surface derived from a parse would report whatever the template happened to
// declare, including a helper that is not part of the contract, and the layers
// above are written against the contract.
func (l Layer) methods(elem model.TypeRef) []shape.Method {
	seq := "iter.Seq[" + spellElem(elem) + "]"

	return []shape.Method{
		{Name: "Len", Signature: "() int", Owner: l.Origin(), Doc: "how many elements the container holds"},
		{Name: "All", Signature: "() " + seq, Owner: l.Origin(), Doc: "walks from the first element to the last"},
		{Name: "Backward", Signature: "() " + seq, Owner: l.Origin(), Doc: "walks from the last element to the first"},
		{
			Name: "AppendSeq", Signature: "(seq " + seq + ")", Owner: l.Origin(), Pointer: true,
			Doc: "adds every element a sequence yields",
		},
		{
			Name: "Reset", Signature: "()", Owner: l.Origin(), Pointer: true,
			Doc: "empties the container, keeping the memory it has taken",
		},
	}
}

// spellElem names the element for a signature a person reads.
//
// The bare name rather than the qualified one: a shape is printed in a table
// beside the declaration it belongs to, where the package is already known. A
// stack whose subject could not be modelled has no element at all, and is
// spelled as the template spells it, which is the honest answer to a question
// with no answer yet.
func spellElem(elem model.TypeRef) string {
	if elem.Name == "" {
		return param
	}
	return elem.Name
}

// Generate returns the declarations this layer contributes.
func (l Layer) Generate(ctx *layer.Context, _ shape.Shape) (layer.Unit, error) {
	if ctx == nil || ctx.Model == nil || ctx.Model.Subject == nil {
		// Not a diagnostic: a diagnostic points at a declaration, and the
		// declaration is the thing that is missing. The pipeline never asks a
		// layer to generate for a model it does not have, so reaching here is
		// forge calling itself wrongly rather than anybody writing anything.
		return layer.Unit{}, errors.New("slice: asked to generate without a modelled declaration")
	}
	if !ctx.Model.Form.Valid() {
		// Whether the author declared the type decides whether this layer
		// declares it, and a form nobody set answers neither way. Guessing
		// either would be wrong in a package the author cannot edit: guessing
		// inline leaves methods on a type nothing declares, and guessing spec
		// declares a type they already have.
		return layer.Unit{}, fmt.Errorf("slice: asked to generate for %s, which was written in no form", ctx.Model.Name)
	}

	// Spelled against what the file will already bind, so that a subject from a
	// package called slices is written as something else rather than as a
	// second import under a name the template has.
	subject := ctx.Model.SubjectSpelling(taken())

	out, diags := l.apply(ctx, subject)
	if err := diags.Err(); err != nil {
		return layer.Unit{}, err
	}

	if wrong := accounted(out.Imports); wrong != "" {
		return layer.Unit{}, fmt.Errorf("slice: %s", wrong)
	}

	decls, err := owned(out.Decls, ctx.Model.Form, ctx.Model.Name)
	if err != nil {
		return layer.Unit{}, err
	}

	return layer.Unit{
		Decls:    decls,
		Comments: out.Comments,
		Fset:     out.Fset,
		Imports:  append(slices.Clone(out.Imports), imported(subject)...),
	}, nil
}

// apply specialises the template for one declaration.
func (Layer) apply(ctx *layer.Context, subject model.Spelling) (templates.Result, diag.Set) {
	declared := ctx.Model.Name

	return templates.Apply(
		templates.Template{Name: "slice", Source: bodies},
		templates.Rewrite{
			Param:     param,
			Subject:   subject.Text,
			Container: container,
			Declared:  declared,
			Names:     map[string]string{constructorInTemplate: constructorFor(declared)},
			Prefix:    lower(declared),
		},
		ctx.Model.Pos)
}

// accounted reports a template import nothing wrote down, or nothing.
//
// The subject is spelled before the template is read, against the names the
// list above says the file will bind. An import that is not in it is one the
// subject was not moved out of the way of, so the check is not bookkeeping: it
// is the thing that keeps the list from being a comment about a file that has
// since changed. It fails on the first run of this package's tests, which is
// where an import added to the template is cheapest to notice.
func accounted(imports []emit.Import) string {
	for _, one := range imports {
		if _, known := templateImports[one.Path]; !known {
			return "the template imports " + one.Path + ", which nothing recorded a bound name for"
		}
	}
	return ""
}

// imported returns a spelling's imports in the shape a unit carries.
//
// A name is carried only where the spelling had to invent one. An import bound
// to the name its package already declares is written without it, so that the
// ordinary case reads the way somebody would have written it by hand and the
// one line that says domain2 stands out as the thing that needed saying.
func imported(spelled model.Spelling) []emit.Import {
	out := make([]emit.Import, 0, len(spelled.Imports))
	for _, one := range spelled.Imports {
		out = append(out, emit.Import{Path: one.Path, Name: one.Name, Aliased: one.Aliased})
	}
	return out
}

// owned returns the declarations to emit for this form of declaration.
//
// The type declaration is the whole of the difference between the two forms. An
// inline declaration is one the author wrote in an ordinary file, where the
// underlying type is real and theirs; emitting it again would redeclare a type
// that is already there. A spec declaration exists only under the forgespec
// build tag, so the type in the ordinary build is this layer's to write —
// which is the storage layer's job precisely because the representation is what
// a storage layer decides.
//
// Leaving a declaration out is only safe under two conditions, and both are
// checked rather than assumed, because the template they are about is the one
// every later storage layer copies. It must declare nothing but the container,
// since a grouped declaration would take a helper out with it — the helper and
// its documentation both, out of a file whose other declarations still call it.
// And it must be the last user of no import, since an import nothing names is a
// file that does not compile.
func owned(decls []ast.Decl, form model.Form, declared string) ([]ast.Decl, error) {
	if form != model.FormInline {
		return decls, nil
	}

	kept := make([]ast.Decl, 0, len(decls))
	dropped := make([]ast.Decl, 0, 1)

	for _, decl := range decls {
		if declares(decl, declared) {
			dropped = append(dropped, decl)
			continue
		}
		kept = append(kept, decl)
	}

	for _, decl := range dropped {
		if wrong := droppable(decl, kept); wrong != "" {
			return nil, fmt.Errorf("slice: the template's declaration of %s cannot be left out: %s", container, wrong)
		}
	}
	return kept, nil
}

// droppable reports what stops a declaration from being left out of the output,
// given what stays, or nothing.
func droppable(decl ast.Decl, kept []ast.Decl) string {
	gen, ok := decl.(*ast.GenDecl)
	if !ok || gen.Tok != token.TYPE {
		return "it is not a type declaration"
	}
	if len(gen.Specs) != 1 {
		return "it declares " + strconv.Itoa(len(gen.Specs)) +
			" types in one group, and the rest are not the author's to leave out"
	}

	staying := emit.Qualifiers(kept)
	for named := range emit.Qualifiers([]ast.Decl{decl}) {
		if !staying[named] {
			return "it is the only mention of " + named + ", whose import nothing left would use"
		}
	}
	return ""
}

// declares reports whether a declaration introduces the type under this name.
func declares(decl ast.Decl, name string) bool {
	gen, ok := decl.(*ast.GenDecl)
	if !ok || gen == nil || gen.Tok != token.TYPE {
		return false
	}

	for _, spec := range gen.Specs {
		typ, ok := spec.(*ast.TypeSpec)
		if ok && typ.Name != nil && typ.Name.Name == name {
			return true
		}
	}
	return false
}

// constructorFor names the constructor after the type it builds, and gives it
// the visibility of that type: a constructor for an unexported container has no
// business being reachable from outside the package it is unexported in.
func constructorFor(declared string) string {
	if first, _ := utf8.DecodeRuneInString(declared); unicode.IsUpper(first) {
		return constructorInTemplate + declared
	}
	return lower(constructorInTemplate) + upper(declared)
}

// lower returns a name with its first letter in lower case, and upper with it
// in upper case. Between them they turn a declared name into the prefix its
// helpers take and into the tail of its constructor's name.
func lower(name string) string { return model.Lower(name) }

func upper(name string) string { return model.Upper(name) }
