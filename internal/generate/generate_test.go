package generate_test

import (
	"bytes"
	"go/token"
	"go/types"
	"slices"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"

	"github.com/okian/forge/internal/compose"
	"github.com/okian/forge/internal/discover"
	"github.com/okian/forge/internal/emit"
	"github.com/okian/forge/internal/generate"
	"github.com/okian/forge/internal/goldentest"
	"github.com/okian/forge/internal/layers"
	"github.com/okian/forge/internal/model"
)

// The package these tests generate into, and where its declarations were
// written.
const local = "example.com/model"

var declaredAt = token.Position{Filename: "model/person.go", Line: 12, Column: 6}

// subjectSource is the fixture the generated output is compiled against: the
// subject, and the declaration itself, since an inline declaration is one the
// author wrote and generation adds methods to.
const subjectSource = "package model\n\n" +
	"// Person is what the collections hold.\n" +
	"type Person struct {\n\tID int\n\tName string\n}\n\n" +
	"// Persons is a collection of them.\n" +
	"type Persons []Person\n"

// person is the model of that subject.
func person() *model.Struct {
	pkg := types.NewPackage(local, "model")
	obj := types.NewTypeName(token.NoPos, pkg, "Person", nil)

	return &model.Struct{
		Named: types.NewNamed(obj, types.NewStruct(nil, nil), nil),
		Fields: []model.Field{
			{Name: "ID", Exported: true, Type: model.Classified{Type: types.Typ[types.Int]}},
			{Name: "Name", Exported: true, Type: model.Classified{Type: types.Typ[types.String]}},
		},
	}
}

// request builds one declaration to generate for, with the directive an author
// would have written above it.
func request(declared string, directives ...string) generate.Request {
	stack := []model.LayerRef{{Origin: model.TypeRef{Pkg: model.MarkerPkg, Name: "Collection"}}}

	written := make([]discover.Directive, len(directives))
	for i, text := range directives {
		name, args, _ := strings.Cut(strings.TrimPrefix(text, "//forge:"), " ")
		written[i] = discover.Directive{
			Layer: name, Args: args, Text: text,
			ArgsOffset: len("//forge:") + len(name) + 1,
			Pos:        token.Position{Filename: declaredAt.Filename, Line: declaredAt.Line - 1, Column: 1},
		}
	}

	return generate.Request{
		Model: &model.Model{
			Name: declared, Form: model.FormInline, Subject: person(), Stack: stack,
			Pkg: &packages.Package{PkgPath: local},
			Pos: declaredAt,
		},
		Directives: written,
	}
}

// config is what a run generates with.
func config() generate.Config {
	return generate.Config{
		Catalog:   compose.Catalog{Registry: layers.Builtins(), DefaultStorage: layers.DefaultStorage()},
		Forge:     "v1.2.3",
		Markers:   "v1.2.3",
		Toolchain: "go1.27.0",
	}
}

// A package's declarations become the files they ask for, and the helpers they
// share become one more — which is the whole reason generation works a package
// at a time.
func TestWhatAPackageBecomes(t *testing.T) {
	files, diags := generate.Package(local, "model",
		[]generate.Request{request("Persons", "//forge:collection sort=Name")}, config())

	if !diags.Empty() {
		t.Fatalf("generating was refused:\n%s", diags.Render())
	}

	named := make([]string, len(files))
	for i, file := range files {
		named[i] = file.Name
	}
	if want := []string{"zz_forge_persons.go", "zz_forge_shared.go"}; !slices.Equal(named, want) {
		t.Fatalf("wrote %v, want %v", named, want)
	}

	sources := []goldentest.Source{{Name: "person.go", Content: []byte(subjectSource)}}
	for _, file := range files {
		sources = append(sources, goldentest.Source{Name: file.Name, Content: file.Content, Generated: true})
	}

	goldentest.Check(t, goldentest.Package{Path: "model", Files: sources})
}

// Two declarations in one package share the helper they both require, which is
// what requiring is for: one copy however many asked.
func TestTwoDeclarationsShareWhatTheyRequire(t *testing.T) {
	files, diags := generate.Package(local, "model",
		[]generate.Request{request("Persons"), request("Staff")}, config())

	if !diags.Empty() {
		t.Fatalf("generating was refused:\n%s", diags.Render())
	}

	shared := 0
	for _, file := range files {
		if file.Name == generate.Shared() {
			shared++
		}
	}
	if shared != 1 {
		t.Errorf("two declarations that require one helper wrote %d shared files", shared)
	}

	// And the helper is declared once inside it.
	for _, file := range files {
		if file.Name != generate.Shared() {
			continue
		}
		if got := bytes.Count(file.Content, []byte("type Seq[U any]")); got != 1 {
			t.Errorf("the shared view is declared %d times", got)
		}
	}
}

// Two declarations whose names differ only in case want one file, which is a
// collision rather than something to resolve by inventing a second spelling.
func TestTwoDeclarationsThatWantOneFile(t *testing.T) {
	_, diags := generate.Package(local, "model",
		[]generate.Request{request("Persons"), request("persons")}, config())

	reported := diags.Render()
	if !strings.Contains(reported, "FRG4006") {
		t.Fatalf("two declarations wanting one file were accepted:\n%s", reported)
	}
	for _, want := range []string{"Persons", "persons", "zz_forge_persons.go"} {
		if !strings.Contains(reported, want) {
			t.Errorf("the report does not name %s:\n%s", want, reported)
		}
	}
}

// The header records what the file was made from, so that asking whether it is
// current costs a read rather than a regeneration.
func TestWhatAFileRecordsAboutItself(t *testing.T) {
	files, _ := generate.Package(local, "model", []generate.Request{request("Persons")}, config())

	held, ok := emit.ReadHeader(files[0].Content)
	if !ok {
		t.Fatal("the file does not say it was generated")
	}
	if held.Forge != "v1.2.3" || held.Markers != "v1.2.3" {
		t.Errorf("it records %+v", held)
	}
	if held.Inputs == "" {
		t.Error("it records no fingerprint, so nothing can be checked cheaply")
	}
}

// The fingerprint is a function of what the output depends on, so anything that
// changes the output changes it — and nothing else does.
func TestWhatTheFingerprintIsAFunctionOf(t *testing.T) {
	base := request("Persons", "//forge:collection sort=Name")

	changes := map[string]func(*generate.Request, *generate.Config){
		"a declaration renamed": func(r *generate.Request, _ *generate.Config) {
			r.Model.Name = "Staff"
		},
		"a field added": func(r *generate.Request, _ *generate.Config) {
			r.Model.Subject.Fields = append(r.Model.Subject.Fields, model.Field{
				Name: "City", Exported: true, Type: model.Classified{Type: types.Typ[types.String]},
			})
		},
		"a field retyped": func(r *generate.Request, _ *generate.Config) {
			r.Model.Subject.Fields[0].Type = model.Classified{Type: types.Typ[types.String]}
		},
		"an option changed": func(r *generate.Request, _ *generate.Config) {
			r.Directives[0].Text = "//forge:collection sort=ID"
		},
		"a newer forge": func(_ *generate.Request, c *generate.Config) { c.Forge = "v2.0.0" },
		"newer markers": func(_ *generate.Request, c *generate.Config) { c.Markers = "v2.0.0" },

		// The toolchain, because the same declarations formatted by a later
		// gofmt are different bytes.
		"a newer toolchain": func(_ *generate.Request, c *generate.Config) { c.Toolchain = "go1.28.0" },
	}

	was := fingerprint(base, config())

	for name, change := range changes {
		t.Run(name, func(t *testing.T) {
			held, cfg := request("Persons", "//forge:collection sort=Name"), config()
			change(&held, &cfg)

			if got := fingerprint(held, cfg); got == was {
				t.Errorf("%s left the fingerprint at %s", name, got)
			}
		})
	}

	// And the same inputs twice are the same fingerprint, which is the half
	// that keeps a check from reporting staleness forever.
	if again := fingerprint(request("Persons", "//forge:collection sort=Name"), config()); again != was {
		t.Errorf("the same declaration fingerprinted as %s and then as %s", was, again)
	}
}

// fingerprint is what a declaration's inputs reduce to.
func fingerprint(req generate.Request, cfg generate.Config) string {
	var sum emit.Digest
	generate.Fingerprint(&sum, req, "model", cfg)

	return sum.String()
}

// The same declaration generates the same bytes, which is what lets a run skip
// a write and keeps a generated file out of every diff that did not touch it.
func TestGeneratingTwiceIsTheSameBytes(t *testing.T) {
	first, _ := generate.Package(local, "model", []generate.Request{request("Persons")}, config())
	second, _ := generate.Package(local, "model", []generate.Request{request("Persons")}, config())

	if len(first) != len(second) {
		t.Fatalf("two runs wrote %d and %d files", len(first), len(second))
	}
	for i := range first {
		if first[i].Name != second[i].Name || !bytes.Equal(first[i].Content, second[i].Content) {
			t.Errorf("%s differs between runs:\n%s", first[i].Name, first[i].Content)
		}
	}
}

// A declaration whose subject was refused is one nothing can be generated for,
// and is passed over rather than reported a second time — the refusal is
// already somebody else's report.
func TestADeclarationWithNoModel(t *testing.T) {
	files, diags := generate.Package(local, "model",
		[]generate.Request{{}, request("Persons")}, config())

	if !diags.Empty() {
		t.Errorf("a declaration that was already refused was reported again:\n%s", diags.Render())
	}
	if len(files) != 2 {
		t.Errorf("wrote %d files, want the one declaration's and the shared one", len(files))
	}
}

// A stack that does not compose is reported and generates nothing, since a
// package is written whole or not at all.
func TestADeclarationThatDoesNotCompose(t *testing.T) {
	held := request("Persons")
	held.Model.Stack = []model.LayerRef{{Origin: model.TypeRef{Pkg: model.MarkerPkg, Name: "Nonesuch"}}}

	files, diags := generate.Package(local, "model", []generate.Request{held}, config())

	if diags.Empty() {
		t.Fatal("a stack naming a marker nothing claims generated without complaint")
	}
	for _, file := range files {
		if file.Decl == "Persons" {
			t.Error("a declaration that does not compose was written anyway")
		}
	}
}

// An option the layer cannot generate from stops that declaration, and says so
// where the option is.
func TestADeclarationWhoseOptionsAreWrong(t *testing.T) {
	_, diags := generate.Package(local, "model",
		[]generate.Request{request("Persons", "//forge:collection sort=Nonesuch")}, config())

	if reported := diags.Render(); !strings.Contains(reported, "FRG3010") {
		t.Errorf("an option naming a field the subject does not have was accepted:\n%s", reported)
	}
}

// The file a package shares is a function of which helpers were asked for and
// nothing else, so a package that merely gained a declaration writes the same
// bytes and records the same fingerprint.
//
// The failure this pins is quiet: a fingerprint that counted the askers would
// rewrite the shared file every time a declaration was added or removed, and
// the diff would say nothing had changed except the line saying something had.
func TestTheSharedFileDoesNotFollowTheDeclarationCount(t *testing.T) {
	one, _ := generate.Package(local, "model", []generate.Request{request("Persons")}, config())
	two, _ := generate.Package(local, "model",
		[]generate.Request{request("Persons"), request("Staff")}, config())

	held := func(files []generate.File) []byte {
		for _, file := range files {
			if file.Name == generate.Shared() {
				return file.Content
			}
		}
		t.Fatal("no shared file was written")
		return nil
	}

	first, second := held(one), held(two)
	if !bytes.Equal(first, second) {
		t.Errorf("one declaration and two wrote different shared files:\n%s\n%s", first, second)
	}
}
