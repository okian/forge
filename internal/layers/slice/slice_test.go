package slice_test

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"slices"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"

	"github.com/okian/forge/internal/diag"
	"github.com/okian/forge/internal/emit"
	"github.com/okian/forge/internal/goldentest"
	"github.com/okian/forge/internal/layer"
	"github.com/okian/forge/internal/layers/slice"
	"github.com/okian/forge/internal/merge"
	"github.com/okian/forge/internal/model"
	"github.com/okian/forge/internal/shape"
)

// where the declaration these tests generate for was written, and the package
// it was written in. Generated code lands in that package, which is what
// decides whether the subject is spelled bare or qualified.
var (
	declaredAt = token.Position{Filename: "model/person.go", Line: 10, Column: 6}
	local      = "example.com/model"
)

// subjectSource is the fixture the generated output is compiled against. It is
// the whole of what the output needs: a storage layer stores what it is given
// and never reads it, so two fields are two fields.
const subjectSource = "package model\n\n" +
	"type Person struct {\n\tName string\n\tAge  int\n}\n"

// person is the subject the declarations below are specialised to.
func person(pkgPath, pkgName string) *model.Struct {
	pkg := types.NewPackage(pkgPath, pkgName)
	obj := types.NewTypeName(token.NoPos, pkg, "Person", nil)

	return &model.Struct{
		Named: types.NewNamed(obj, types.NewStruct(nil, nil), nil),
		Fields: []model.Field{
			{Name: "Name", Exported: true},
			{Name: "Age", Exported: true},
		},
	}
}

// declaration builds what a layer is asked to generate against, the way the
// pipeline builds it.
func declaration(name string, form model.Form, subject *model.Struct) *layer.Context {
	return &layer.Context{
		Model: &model.Model{
			Name:    name,
			Form:    form,
			Subject: subject,
			Stack:   []model.LayerRef{{Origin: slice.New().Origin(), Kind: model.KindStorage}},
			Pkg:     &packages.Package{PkgPath: local},
			Pos:     declaredAt,
		},
	}
}

// inline is the ordinary case: a declaration in an ordinary file, whose
// underlying type the author already wrote.
func inline() *layer.Context {
	return declaration("Persons", model.FormInline, person(local, "model"))
}

// generate asks the layer for its unit, failing the test if it refuses.
func generate(t *testing.T, ctx *layer.Context) layer.Unit {
	t.Helper()

	unit, err := slice.New().Generate(ctx, shape.Shape{})
	if err != nil {
		t.Fatalf("the layer refused to generate: %v", err)
	}
	return unit
}

// generated renders a unit as the file it will be written to, through the same
// merge and emit steps generation uses.
func generated(t *testing.T, ctx *layer.Context) []byte {
	t.Helper()

	merged := merge.Units(generate(t, ctx))

	out, err := emit.File{
		Package:  "model",
		Decl:     ctx.Model.Name,
		Pos:      ctx.Model.Pos,
		Imports:  merged.Imports,
		Sections: merged.Sections,
	}.Render()
	if err != nil {
		t.Fatalf("rendering the file: %v", err)
	}
	return out
}

// An inline declaration is one the author wrote in an ordinary file, where the
// underlying type is real and theirs. Generation adds methods to it and does
// not redeclare it — which is the half of this that a golden alone cannot
// check, since the fixture would compile either way if the author's file were
// left out of it.
func TestTheInlineFormIsGivenMethodsAndNotADeclaration(t *testing.T) {
	out := generated(t, inline())

	if bytes.Contains(out, []byte("type Persons")) {
		t.Errorf("the author's own declaration was written a second time:\n%s", out)
	}

	goldentest.Check(t, goldentest.Package{
		Path: "model",
		Files: []goldentest.Source{
			{Name: "person.go", Content: []byte(subjectSource + "\ntype Persons []Person\n")},
			{Name: "zz_forge_persons.go", Content: out, Generated: true},
		},
	})
}

// A spec declaration exists only under the forgespec build tag, so the type in
// the ordinary build is this layer's to write. The representation is what a
// storage layer decides, which is why the declaration is its to emit rather
// than something assembled elsewhere from what it said.
//
// The recorded output carries no build constraint. A layer produces
// declarations and says what they need; which file they land in and what
// guards it is decided once for the whole run, by the stage that assembles a
// file out of every layer's contribution — so a constraint written here would
// be one layer's opinion about a file it shares.
func TestTheSpecFormIsGivenTheDeclarationToo(t *testing.T) {
	out := generated(t, declaration("Persons", model.FormSpec, person(local, "model")))

	if !bytes.Contains(out, []byte("type Persons []Person")) {
		t.Errorf("no declaration was written for a form that has none:\n%s", out)
	}

	goldentest.Check(t, goldentest.Package{
		Path: "model",
		Files: []goldentest.Source{
			{Name: "person.go", Content: []byte(subjectSource)},
			{Name: "zz_forge_persons.go", Content: out, Generated: true},
		},
	})
}

// What the layer tells the layers above it is on the declared type has to be
// what it puts there. The two are written separately — one is a report and one
// is a template — and nothing but this holds them together.
func TestTheSurfaceIsWhatIsEmitted(t *testing.T) {
	ctx := inline()
	exposed := slice.New().Shape(shape.Subject(ctx.Model.Subject))

	var promised []string
	for _, method := range exposed.Surface {
		promised = append(promised, method.Name)
	}
	slices.Sort(promised)

	emitted := methodsOn(t, generated(t, ctx), "Persons")
	if !slices.Equal(emitted, promised) {
		t.Errorf("the surface says %v and the output holds %v", promised, emitted)
	}

	// The signatures are read beside the declaration, where the package is
	// already known, so the element is named and not qualified.
	all, _ := exposed.Method("All")
	if want := "() iter.Seq[Person]"; all.Signature != want {
		t.Errorf("All reads %q, want %q", all.Signature, want)
	}
	if all.Owner != slice.New().Origin() {
		t.Errorf("All is owned by %s, want the layer that emits it", all.Owner)
	}
}

// method is one method the generated source declares: its name, and whether it
// took the receiver by pointer — which decides whether a value of the type has
// it at all.
type method struct {
	name    string
	pointer bool
}

// methodDecls returns the methods the source declares on a type, in the order
// they were written.
func methodDecls(t *testing.T, source []byte, receiver string) []method {
	t.Helper()

	file, err := parser.ParseFile(token.NewFileSet(), "generated.go", source, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("the generated file does not parse: %v\n%s", err, source)
	}

	var found []method
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Recv == nil || len(fn.Recv.List) != 1 {
			continue
		}

		name, pointer := receiverName(fn.Recv.List[0].Type)
		if name == receiver {
			found = append(found, method{name: fn.Name.Name, pointer: pointer})
		}
	}

	return found
}

// methodsOn returns the names of the methods the source declares on a type,
// sorted, so that a surface and an output can be compared as sets.
func methodsOn(t *testing.T, source []byte, receiver string) []string {
	t.Helper()

	decls := methodDecls(t, source, receiver)

	found := make([]string, len(decls))
	for i, one := range decls {
		found[i] = one.name
	}

	slices.Sort(found)
	return found
}

// receiverName returns the type a receiver names, and whether it named it
// through a pointer.
func receiverName(expr ast.Expr) (string, bool) {
	pointer := false
	if star, ok := expr.(*ast.StarExpr); ok {
		expr, pointer = star.X, true
	}
	if name, ok := expr.(*ast.Ident); ok {
		return name.Name, pointer
	}
	return "", pointer
}

// A shape whose subject could not be modelled still has to answer, because the
// explain command asks it about declarations that did not resolve.
func TestASurfaceOverNoSubject(t *testing.T) {
	exposed := slice.New().Shape(shape.Shape{})

	all, ok := exposed.Method("All")
	if !ok {
		t.Fatal("a shape over no subject exposes no methods")
	}
	if want := "() iter.Seq[T]"; all.Signature != want {
		t.Errorf("All reads %q, want %q", all.Signature, want)
	}
}

// The subject is spelled the way the file being written has to spell it, which
// for a type from another package is qualified — and the file then has to
// import it, or it is a name nothing binds.
func TestASubjectFromAnotherPackage(t *testing.T) {
	ctx := declaration("Persons", model.FormInline, person("example.com/domain", "domain"))
	unit := generate(t, ctx)

	out := generated(t, ctx)
	if !bytes.Contains(out, []byte("domain.Person")) {
		t.Errorf("the subject is not spelled for the package it is written in:\n%s", out)
	}
	if !slices.Contains(unit.Imports, emit.Import{Path: "example.com/domain"}) {
		t.Errorf("the unit imports %v, and none of them is the subject's package", unit.Imports)
	}
}

// A subject from a package named like one the template imports would leave the
// file importing two things under one name — which does not compile, in a file
// the author cannot edit, over a collision they caused by naming a package
// slices. It is bound to a name of its own instead, and spelled by that.
//
// Recorded but not compiled. The compile gate builds one package and refuses
// every import outside the standard library, which is the whole subject of this
// case, so what holds the output here is a copy somebody reads — and the file
// it records is the one worth reading, since nothing about this fix is visible
// in a signature.
func TestASubjectWhosePackageNameIsAlreadyTaken(t *testing.T) {
	ctx := declaration("Persons", model.FormSpec, person("example.com/util/slices", "slices"))
	unit := generate(t, ctx)

	out := generated(t, ctx)
	for _, want := range []string{
		`slices2 "example.com/util/slices"`,
		"type Persons []slices2.Person",
		"slices.Clone(elems)",
	} {
		if !bytes.Contains(out, []byte(want)) {
			t.Errorf("the output does not hold %q:\n%s", want, out)
		}
	}
	if !slices.Contains(unit.Imports, emit.Import{Path: "example.com/util/slices", Name: "slices2"}) {
		t.Errorf("the unit imports %v, and none of them binds the subject's package", unit.Imports)
	}

	goldentest.Compare(t, "zz_forge_persons.go", out)
}

// A form nobody set answers neither of the two questions this layer asks, and
// guessing either is wrong in a package the author cannot edit: guessing inline
// leaves methods on a type nothing declares, and guessing spec declares a type
// they already have.
func TestGeneratingForADeclarationWrittenInNoForm(t *testing.T) {
	ctx := declaration("Persons", model.FormInvalid, person(local, "model"))

	if _, err := slice.New().Generate(ctx, shape.Shape{}); err == nil {
		t.Fatal("a declaration written in no form was generated for")
	} else if !strings.Contains(err.Error(), "Persons") {
		t.Errorf("the error %q does not name the declaration", err)
	}
}

// Reading a container does not need a pointer to one, so a value of the
// declared type has every method that only reads it — which is what lets it
// satisfy an interface, and what lets a container a function returned be read
// without being stored first. Only the method that changes it takes a pointer.
func TestWhichMethodsAValueHas(t *testing.T) {
	ctx := inline()

	receivers := map[string]bool{}
	for _, method := range methodDecls(t, generated(t, ctx), "Persons") {
		receivers[method.name] = method.pointer
	}

	want := map[string]bool{"Len": false, "All": false, "Backward": false, "AppendSeq": true}
	for name, pointer := range want {
		if got, has := receivers[name]; !has {
			t.Errorf("no method %s was emitted", name)
		} else if got != pointer {
			t.Errorf("%s takes a pointer receiver = %v, want %v", name, got, pointer)
		}
	}

	// And the surface says the same thing, since a decorator wrapping one of
	// these has to declare the receiver it wraps.
	exposed := slice.New().Shape(shape.Subject(ctx.Model.Subject))
	for _, method := range exposed.Surface {
		if method.Pointer != want[method.Name] {
			t.Errorf("the surface says %s takes a pointer receiver = %v", method.Name, method.Pointer)
		}
	}
}

// The constructor returns a value rather than a pointer, so what it returns can
// be ranged over and passed where a slice is wanted — which is what the type's
// own documentation promises.
func TestTheConstructorReturnsAValue(t *testing.T) {
	if out := generated(t, inline()); !bytes.Contains(out, []byte("func NewPersons(elems ...Person) Persons")) {
		t.Errorf("the constructor does not return a value:\n%s", out)
	}
}

// A constructor for an unexported container has no business being reachable
// from outside the package the container is unexported in.
func TestTheConstructorFollowsTheTypesVisibility(t *testing.T) {
	cases := map[string]string{
		"Persons": "func NewPersons(elems ...Person) Persons",
		"persons": "func newPersons(elems ...Person) persons",
	}

	for declared, want := range cases {
		t.Run(declared, func(t *testing.T) {
			out := generated(t, declaration(declared, model.FormSpec, person(local, "model")))
			if !bytes.Contains(out, []byte(want)) {
				t.Errorf("no constructor reads %q:\n%s", want, out)
			}
		})
	}
}

// The same declaration generates the same bytes, which is what lets generation
// skip a write and keeps a generated file out of every diff that did not touch
// it.
func TestGeneratingTwiceIsTheSameBytes(t *testing.T) {
	if first, second := generated(t, inline()), generated(t, inline()); !bytes.Equal(first, second) {
		t.Errorf("two runs differ:\n%s", first)
	}
}

// A layer asked to generate for nothing has nothing to point a diagnostic at,
// so it says so as an ordinary error: the declaration is the thing that is
// missing.
func TestGeneratingWithoutADeclaration(t *testing.T) {
	cases := map[string]*layer.Context{
		"no context": nil,
		"no model":   {},
		"no subject": {Model: &model.Model{Name: "Persons"}},
	}

	for name, ctx := range cases {
		t.Run(name, func(t *testing.T) {
			unit, err := slice.New().Generate(ctx, shape.Shape{})
			if err == nil {
				t.Fatal("the layer generated without a declaration")
			}
			if len(unit.Decls) != 0 {
				t.Errorf("it returned %d declarations as well", len(unit.Decls))
			}
			if !strings.Contains(err.Error(), "slice") {
				t.Errorf("the error %q does not say which layer it is from", err)
			}
		})
	}
}

// A declared name a printer cannot write is refused rather than written into a
// file to see what happens, and the refusal points at the declaration that
// asked rather than at generated code nobody can edit.
func TestADeclarationThatCannotBeWritten(t *testing.T) {
	ctx := declaration("not an identifier", model.FormSpec, person(local, "model"))

	_, err := slice.New().Generate(ctx, shape.Shape{})
	if err == nil {
		t.Fatal("a name that is not one was written")
	}

	reported, ok := diag.From(err)
	if !ok {
		t.Fatalf("the error %v is not a diagnostic, so nothing says where it is about", err)
	}
	if reported.Pos != declaredAt {
		t.Errorf("it points at %s, want the declaration at %s", reported.Pos, declaredAt)
	}
}
