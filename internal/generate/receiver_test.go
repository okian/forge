package generate_test

import (
	"bytes"
	"errors"
	"go/ast"
	"go/token"
	"go/types"
	"slices"
	"testing"

	"github.com/okian/forge/internal/emit"
	"github.com/okian/forge/internal/generate"
	"github.com/okian/forge/internal/goldentest"
	"github.com/okian/forge/internal/layer"
	"github.com/okian/forge/internal/layers"
	"github.com/okian/forge/internal/model"
	"github.com/okian/forge/internal/shape"
	"github.com/okian/forge/internal/templates"
)

// An element layer contributes to the subject rather than to the container, and
// where it may put that contribution is not its choice.
//
// Go lets a method be declared only in the package that declares the type, so a
// subject beside the declaration gets a method and a subject anywhere else —
// another package, another module, an instantiation — cannot. The layer emits
// the work either way; what changes is whether it goes on the subject or into a
// function beside it.
//
// The whole point of the arrangement is that nothing above notices. A container
// calls one package-level function whichever way the layer went, so a subject
// moved into another package changes what forge writes and nothing that reads
// it — which is what this checks, by generating the same declaration both ways
// and compiling both.

// marked is an element layer that attaches a method where it can and emits a
// function where it cannot, so that the two paths can be compared.
type marked struct{}

func (marked) Binds() []model.Import { return nil }
func (marked) Writes() []string      { return nil }
func (marked) Origin() model.TypeRef { return model.TypeRef{Pkg: model.MarkerPkg, Name: "Marked"} }
func (marked) Kind() model.Kind      { return model.KindElement }
func (marked) Stage() layer.Stage    { return layer.StageReady }
func (marked) Doc() string           { return "a mark on the subject, for testing where one can go" }

func (marked) Accepts(below shape.Shape) error {
	if !below.Caps.Has(shape.Structured) {
		return errUnstructured
	}
	return nil
}

func (marked) Shape(_ *layer.Context, below shape.Shape) shape.Shape { return below }
func (marked) OptionSchema() []layer.OptionDef                       { return nil }

// Generate emits the mark, and the call-through function that reaches it.
//
// Two shapes of the same contribution. Where the subject is in the package
// being written, the work is a method on it and the function forwards; where it
// is not, the function is the work. Both are named the same thing, because the
// name is what everything above uses.
func (marked) Generate(ctx *layer.Context, _ shape.Shape) (layer.Unit, error) {
	var (
		held    = ctx.Model.Subject
		into    = ctx.Model.Pkg.PkgPath
		through = model.Through(held, "mark", "Text", into)
		subject = ctx.Model.SubjectSpelling(nil)
	)

	source := "package tmpl\n\ntype Marked struct{}\n\n" +
		"// " + through + " returns what the subject is marked with.\n" +
		"func " + through + "(v " + subject.Text + ") string { return v.String() }\n"

	if held.Attachable(into) {
		source += "\n// String is the mark itself, declared on the subject.\n" +
			"func (v " + subject.Text + ") String() string { return \"marked\" }\n"
	} else {
		// Nowhere to put a method, so the function is the whole of it.
		source = "package tmpl\n\ntype Marked struct{}\n\n" +
			"// " + through + " returns what the subject is marked with.\n" +
			"func " + through + "(v " + subject.Text + ") string { return \"marked\" }\n"
	}

	out, diags := templates.Apply(
		templates.Template{Name: "marked", Source: []byte(source)},
		templates.Rewrite{
			Param: "T", Subject: subject.Text, Container: "Marked",
			Declared: ctx.Model.Name, Prefix: "marked",
			Names: verbatim(through),
		},
		ctx.Model.Pos)
	if err := diags.Err(); err != nil {
		return layer.Unit{}, err
	}

	// The container's type is the rewrite's business; this layer declares only
	// the subject's mark, so the type it was given goes back out.
	kept := make([]ast.Decl, 0, len(out.Decls))
	for _, decl := range out.Decls {
		if gen, is := decl.(*ast.GenDecl); is && gen.Tok == token.TYPE {
			continue
		}
		kept = append(kept, decl)
	}

	// Handed over as the subject's rather than as this declaration's. Two
	// declarations over one subject each reach here and each produce the same
	// thing; keyed by the subject, the package emits it once.
	mark := layer.Unit{Decls: kept, Comments: out.Comments, Fset: out.Fset, Imports: imports(subject)}

	return layer.Unit{Provides: map[string]layer.Unit{held.Ref().String(): mark}}, nil
}

// errUnstructured is what an element layer says when it is given something
// with no fields to work from.
var errUnstructured = errors.New("a mark needs a subject with fields")

// verbatim keeps the call-through function's name through the rewrite, since
// it is the one name everything above the subject agrees on.
func verbatim(name string) map[string]string { return map[string]string{name: name} }

// imports carries a spelling's imports in the shape a unit holds.
func imports(spelled model.Spelling) []emit.Import {
	return slices.Clone(spelled.Imports)
}

// declaredIn builds a subject declared in another package of the same module,
// which is the case that comes apart from the ordinary one.
func declaredIn(t *testing.T, path, name string) *model.Struct {
	t.Helper()

	pkg := types.NewPackage(path, name)
	obj := types.NewTypeName(token.NoPos, pkg, "Person", nil)

	return &model.Struct{
		Named: types.NewNamed(obj, types.NewStruct(nil, nil), nil),
		Fields: []model.Field{
			{Name: "ID", Exported: true, Type: model.Classified{Type: types.Typ[types.Int]}},
			{Name: "Name", Exported: true, Type: model.Classified{Type: types.Typ[types.String]}},
		},
	}
}

// The same declaration generates both ways, and a caller reaches the work the
// same way either way.
//
// One flag decides it, and the flag is a fact rather than a setting: whether
// the subject is declared in the package being written. What changes is where
// the work goes; what does not is that a call reaches it, which is why nothing
// above the subject has to know which way it went.
//
// The name of that call carries the subject's package where the subject is
// somewhere else, because a package can reach two structs of one name and give
// them one function between them otherwise. The layer builds the name the same
// way in every case, so this costs nothing above.
func TestWhereAnElementLayerPutsItsWork(t *testing.T) {
	registry := layers.Builtins()
	registry.MustRegister(marked{})

	cfg := over(registry)

	cases := map[string]struct {
		subject  *model.Struct
		through  string
		attaches bool
		builds   bool
	}{
		// Beside the declaration, which is the only place a method on it can
		// be declared — and the only one of these the compile gate can build,
		// since it resolves nothing outside the standard library.
		"in the package being written": {
			subject: person(), through: "markPersonText", attaches: true, builds: true,
		},

		// Same module, another package. Out of reach for the language's
		// reason: a method belongs to the file declaring the type.
		"in another package of this module": {
			subject: declaredIn(t, local+"/domain", "domain"), through: "markDomainPersonText",
		},

		// And another module, which is out of reach for two reasons at once.
		"in another module": {
			subject: declaredIn(t, "other.example/lib", "lib"), through: "markLibPersonText",
		},
	}

	for name, want := range cases {
		t.Run(name, func(t *testing.T) {
			asked := request("Persons")
			asked.Model.Subject = want.subject
			asked.Model.Form = model.FormSpec

			// The element layer innermost, which is where the rules put one:
			// it attaches to the subject, so nothing may stand between them.
			asked.Model.Stack = []model.LayerRef{
				{Origin: model.TypeRef{Pkg: model.MarkerPkg, Name: "Slice"}, Kind: model.KindStorage},
				{Origin: model.TypeRef{Pkg: model.MarkerPkg, Name: "Marked"}, Kind: model.KindElement},
			}

			files, diags := generate.Package(local, "model", []generate.Request{asked}, cfg)
			if !diags.Empty() {
				t.Fatalf("generating was refused:\n%s", diags.Render())
			}

			// Across the package rather than in the declaration's own file:
			// what an element layer writes belongs to the subject, so it lands
			// where a package keeps what its declarations share.
			held := everything(files)

			// The call-through function, whichever way the layer went. It is
			// the whole of what anything above the subject knows about: one
			// function, written the same way in all three, so nothing above
			// has to ask which way the layer went.
			//
			// Its name carries the subject's package where the subject is not
			// this one's, because a package may reach two structs of one name
			// and two functions of one name is a package that does not build.
			if want := "func " + want.through + "("; !bytes.Contains(held, []byte(want)) {
				t.Errorf("the output does not hold %q:\n%s", want, held)
			}

			// And a method on the subject only where one could be declared.
			if got := bytes.Contains(held, []byte("String() string")); got != want.attaches {
				t.Errorf("a method on the subject present=%v, want %v:\n%s", got, want.attaches, held)
			}

			if want.builds {
				compiled(t, files)
			}
		})
	}
}

// everything joins what a package was written, for an assertion about the
// package rather than about one file of it.
func everything(files []generate.File) []byte {
	var out []byte
	for _, file := range files {
		out = append(out, file.Content...)
	}
	return out
}

// compiled builds what was generated for a subject in the package being
// written.
//
// Only that case. A subject anywhere else means generated code importing the
// package it lives in, and the compile gate resolves nothing outside the
// standard library — so what those cases are held to is what was emitted, which
// is where the difference between the two paths is anyway.
func compiled(t *testing.T, files []generate.File) {
	t.Helper()

	sources := []goldentest.Source{
		// The subject, and the declaration under the tag: a spec declaration's
		// type is forge's to write in the ordinary build, so nothing here
		// declares it a second time.
		{Name: "person.go", Content: []byte("package model\n\ntype Person struct {\n\tID int\n\tName string\n}\n")},
		{Name: "spec.go", Content: []byte("//go:build forgespec\n\npackage model\n\ntype Persons []Person\n")},

		// And a call site, so that what is compiled is somebody using the
		// call-through rather than a package that merely holds it.
		{Name: "using.go", Content: []byte("package model\n\nfunc marked(v Person) string { return markPersonText(v) }\n")},
	}

	for _, file := range files {
		sources = append(sources, goldentest.Source{Name: file.Name, Content: file.Content, Generated: true})
	}

	for _, tags := range [][]string{nil, {"forgespec"}} {
		if err := goldentest.Compiles(goldentest.Package{Path: "model", Tags: tags, Files: sources}); err != nil {
			t.Errorf("what was generated does not compile with tags %v: %v", tags, err)
		}
	}
}

// Two declarations over one subject get one copy of what the element layer
// contributed to it.
//
// The case the arrangement exists for. What an element layer writes belongs to
// the subject, so two containers holding the same subject both ask for it and
// both are given it — and a package holding two declarations of one function
// does not compile. Handing it over as the subject's rather than as the
// declaration's is what lets the package keep one.
func TestTwoDeclarationsOverOneSubject(t *testing.T) {
	registry := layers.Builtins()
	registry.MustRegister(marked{})

	cfg := over(registry)

	asked := make([]generate.Request, 0, 2)
	for _, name := range []string{"Persons", "Staff"} {
		one := request(name)
		one.Model.Form = model.FormSpec
		one.Model.Stack = []model.LayerRef{
			{Origin: model.TypeRef{Pkg: model.MarkerPkg, Name: "Slice"}, Kind: model.KindStorage},
			{Origin: model.TypeRef{Pkg: model.MarkerPkg, Name: "Marked"}, Kind: model.KindElement},
		}
		asked = append(asked, one)
	}

	files, diags := generate.Package(local, "model", asked, cfg)
	if !diags.Empty() {
		t.Fatalf("generating was refused:\n%s", diags.Render())
	}

	// Once in the file the ordinary build reads. The file standing in under the
	// tag holds it again, which is what standing in is: the two are never in
	// scope together.
	for _, file := range files {
		held := bytes.Count(file.Content, []byte("func markPersonText("))
		if held != 1 {
			t.Errorf("%s holds the subject's function %d times", file.Name, held)
		}
	}

	sources := []goldentest.Source{
		{Name: "person.go", Content: []byte("package model\n\ntype Person struct {\n\tID int\n\tName string\n}\n")},
		{Name: "spec.go", Content: []byte("//go:build forgespec\n\npackage model\n\ntype Persons []Person\ntype Staff []Person\n")},
		{Name: "using.go", Content: []byte("package model\n\nfunc marked(v Person) string { return markPersonText(v) }\n")},
	}
	for _, file := range files {
		sources = append(sources, goldentest.Source{Name: file.Name, Content: file.Content, Generated: true})
	}

	for _, tags := range [][]string{nil, {"forgespec"}} {
		if err := goldentest.Compiles(goldentest.Package{Path: "model", Tags: tags, Files: sources}); err != nil {
			t.Errorf("two declarations over one subject do not compile with tags %v: %v", tags, err)
		}
	}
}
