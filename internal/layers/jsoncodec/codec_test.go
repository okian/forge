package jsoncodec_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/okian/forge/internal/emit"
	"github.com/okian/forge/internal/layers/jsoncodec"
	"github.com/okian/forge/internal/load"
	"github.com/okian/forge/internal/subject"
	"github.com/okian/forge/plugin"
)

// modelPkg is the fixture package the subjects are declared in.
const modelPkg = "codecfixture/model"

// What the generated codec is checked against is the standard library reading
// and writing the same values reflectively.
//
// Not a golden file. A golden file says the output has not changed, which is a
// different claim from the output being right — and the thing that makes a
// codec right is that it agrees with the one everybody else is using. Any
// disagreement about a name, an order, a number's precision or a null is a
// value that goes out of one program and arrives wrong in another.
//
// The comparison is possible because a defined type does not inherit methods:
// `type twin Subject` has the same fields and no codec, so the standard library
// reflects over it while the subject itself dispatches to what forge wrote.
func TestTheCodecAgreesWithTheStandardLibrary(t *testing.T) {
	if testing.Short() {
		t.Skip("compiling and running a generated module is not a short test")
	}

	dir := t.TempDir()
	built := generated(t)

	// The fixture's own source, beside the codec written for it.
	copied(t, filepath.Join("testdata", "codec"), dir)
	write(t, filepath.Join(dir, "model", "zz_codec.go"), built)
	write(t, filepath.Join(dir, "model", "agreement_test.go"), agreement)

	cmd := exec.CommandContext(t.Context(), "go", "test", "./...")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOFLAGS=-mod=mod")

	if out, err := cmd.CombinedOutput(); err != nil {
		t.Errorf("the generated codec does not agree with the standard library:\n%s", out)
	}
}

// generated returns the codec forge writes for every subject in the fixture.
func generated(t *testing.T) []byte {
	t.Helper()

	loaded := loadFixture(t)
	builder := subject.New(subject.Config{
		Fset:  loaded.Fset,
		Owned: loaded.Owned(),
		Docs:  loaded.FieldDocs(),
	})

	file := emit.File{Package: "model"}
	seen := make(map[string]bool)

	for _, name := range subjects(t, loaded) {
		unit := codec(t, builder, loaded, name)

		// Two subjects reach one struct — Nested and Embedded both reach
		// Address — and the package holds one codec for it. Keyed the same way
		// the emitter keys it, which is what makes this a rehearsal of what a
		// package really does rather than a second arrangement of it.
		for key, held := range unit.Provides {
			if seen[key] {
				continue
			}
			seen[key] = true

			file.Sections = append(file.Sections, emit.Section{
				Decls: held.Decls, Comments: held.Comments, Fset: held.Fset,
			})
			file.Imports = append(file.Imports, held.Imports...)
		}
	}

	out, err := file.Render()
	if err != nil {
		t.Fatalf("rendering the codec: %v", err)
	}
	return out
}

// codec asks the layer for one subject's codec.
func codec(t *testing.T, builder *subject.Builder, loaded *load.Session, name string) plugin.Unit {
	t.Helper()

	built, problems := builder.Build(named(t, loaded, name), subject.Site{})
	if !problems.Empty() {
		t.Fatalf("modelling %s: %s", name, problems.Render())
	}

	pkg, _ := loaded.Package(modelPkg)
	unit, err := jsoncodec.New().Generate(&plugin.Context{
		Model: &plugin.Model{
			Name: name, Form: plugin.FormInline, Subject: built,
			Pkg: pkg, Pos: token.Position{Filename: "person.go"},
		},
	}, plugin.Shape{})
	if err != nil {
		t.Fatalf("generating a codec for %s: %v", name, err)
	}

	return unit
}

// subjects names the fixture's structs, in a fixed order so that what is
// rendered does not depend on how a scope iterated.
func subjects(t *testing.T, loaded *load.Session) []string {
	t.Helper()

	pkg, ok := loaded.Package(modelPkg)
	if !ok {
		t.Fatalf("the fixture has no package %s", modelPkg)
	}

	var out []string
	for _, name := range pkg.Types.Scope().Names() {
		if named := pkg.Types.Scope().Lookup(name); named != nil && structural(named.Type()) {
			out = append(out, name)
		}
	}
	slices.Sort(out)

	if len(out) == 0 {
		t.Fatal("the fixture declares no structs")
	}
	return out
}

// copied writes a directory's Go files and go.mod into another directory.
func copied(t *testing.T, from, to string) {
	t.Helper()

	err := filepath.WalkDir(from, func(path string, entry os.DirEntry, err error) error {
		switch {
		case err != nil:
			return err
		case entry.IsDir():
			return os.MkdirAll(filepath.Join(to, rel(t, from, path)), 0o755)
		case !strings.HasSuffix(path, ".go") && filepath.Base(path) != "go.mod":
			return nil
		}

		held, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(to, rel(t, from, path)), held, 0o644)
	})
	if err != nil {
		t.Fatalf("copying the fixture: %v", err)
	}
}

// rel returns a path relative to a root.
func rel(t *testing.T, root, path string) string {
	t.Helper()

	out, err := filepath.Rel(root, path)
	if err != nil {
		t.Fatalf("relating %s to %s: %v", path, root, err)
	}
	return out
}

// write puts a file where the generated module expects it.
func write(t *testing.T, path string, content []byte) {
	t.Helper()

	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
}

// Every subject the fixture declares is compared against the standard library,
// and every twin written for one reaches a comparison.
//
// It exists because neither is visible from a passing run. A subject nobody
// compares is still generated — [subjects] enumerates the package — so it is
// still compiled, and a suite that only compiles what it generates reports
// success for a codec nothing has ever read the output of. A twin declared and
// never compared looks exactly like one that is, from anywhere except here.
//
// That is not hypothetical, twice over. Seven twins were once added with their
// comparisons, and the comparisons did not land: the file compiled, the suite
// was green, and seven shapes went unchecked. The first attempt at this test
// then counted how often a name appeared, which a twin holding a field of its
// own type satisfies by being declared — so the check passed for exactly the
// shape it was written to catch.
//
// What is asked instead is reachability: a name has to be written somewhere in a
// function that compares something, or in a helper such a function names.
// Counting cannot express that and parsing can, so this parses.
//
// The whole function body rather than the comparison's arguments, because a
// value is rarely written into the call — a case list is a slice literal in a
// range statement, and a twin whose fields are structs is built into a local
// first. That makes the rule generous in one direction: a shape named as a
// field value inside another comparison's case list counts as reached, so a
// shape compared only that way could lose its own comparison unnoticed. The
// generous direction is the safe one for a guard whose failure mode is a test
// nobody wrote, and the strict reading was tried first — it reported eleven
// shapes as uncompared that are compared on every run.
func TestEveryShapeIsCompared(t *testing.T) {
	reached := compared(t)

	for _, name := range twins(t) {
		if !reached[name] {
			t.Errorf("%s is declared and never reaches a comparison", name)
		}
	}

	for _, name := range subjects(t, loadFixture(t)) {
		if !reached[name] {
			t.Errorf("%s is generated and never reaches a comparison", name)
		}
	}

	// And both kinds of comparison are still being made.
	//
	// The rules above are about shapes, and a shape is usually compared by
	// several tests — so losing one of them costs no shape and nothing above
	// would say a word. What the second kind covers is not a shape but a
	// direction: what a reader does with a document nobody here generated,
	// which is the half a round trip cannot reach and where every reader
	// failure this codec has had was caught.
	file := comparison(t)
	for name := range comparing {
		if !someFunction(file, name) {
			t.Errorf("nothing calls %s, so that kind of comparison is no longer made", name)
		}
	}
}

// someFunction reports whether anything in the file calls a function by name.
func someFunction(file *ast.File, name string) bool {
	found := false

	ast.Inspect(file, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		if named, is := call.Fun.(*ast.Ident); is && named.Name == name {
			found = true
		}
		return !found
	})
	return found
}

// compared returns every name a function that compares something writes.
//
// The function rather than the call, because a value is rarely written into the
// call: a case list is a slice literal in a range statement, a twin whose fields
// are structs is built into a local first, and a twin built by a helper is named
// only inside it. All three are the same claim — this function is about these
// shapes — and the function is the smallest thing that holds it.
//
// Helpers are followed one level, which is as deep as the fixture goes. A
// helper called from a comparison is part of that comparison; one called from
// nowhere is not, which is what makes a deleted comparison visible even when the
// helper it used survives.
func compared(t *testing.T) map[string]bool {
	t.Helper()

	file := comparison(t)

	helpers := make(map[string]*ast.FuncDecl)
	for _, decl := range file.Decls {
		if held, ok := decl.(*ast.FuncDecl); ok && held.Recv == nil {
			helpers[held.Name.Name] = held
		}
	}

	out := make(map[string]bool)
	for _, decl := range file.Decls {
		held, ok := decl.(*ast.FuncDecl)
		if !ok || !compares(held) {
			continue
		}

		for name := range mentioned(held) {
			out[name] = true
			if inner, is := helpers[name]; is && inner != held {
				for reached := range mentioned(inner) {
					out[reached] = true
				}
			}
		}
	}

	if len(out) == 0 {
		t.Fatal("the comparison file calls agree nowhere, so it compares nothing")
	}
	return out
}

// comparing names the functions that put forge's answer beside the standard
// library's. A function calling one of them is comparing something.
//
// Both of them, because they compare different things and the second is the one
// worth losing least: agree writes a value and reads back what it wrote, and
// readAlike reads a document nobody generated. A guard that knew only about the
// first would let the test covering hand-written input be deleted without
// noticing — which is the failure this whole test exists to prevent, one
// function along from where it was first found.
var comparing = map[string]bool{"agree": true, "readAlike": true}

// compares reports whether a function compares something.
func compares(held *ast.FuncDecl) bool {
	found := false

	ast.Inspect(held, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		if named, is := call.Fun.(*ast.Ident); is && comparing[named.Name] {
			found = true
		}
		return !found
	})
	return found
}

// mentioned returns every identifier written anywhere inside a node.
func mentioned(node ast.Node) map[string]bool {
	out := make(map[string]bool)

	ast.Inspect(node, func(held ast.Node) bool {
		if named, ok := held.(*ast.Ident); ok {
			out[named.Name] = true
		}
		return true
	})
	return out
}

// twins returns the twin types the comparison file declares.
func twins(t *testing.T) []string {
	t.Helper()

	var out []string
	for _, decl := range comparison(t).Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.TYPE {
			continue
		}
		for _, spec := range gen.Specs {
			if held, is := spec.(*ast.TypeSpec); is && strings.HasSuffix(held.Name.Name, "Twin") {
				out = append(out, held.Name.Name)
			}
		}
	}

	if len(out) == 0 {
		t.Fatal("the comparison file declares no twins, so it cannot be comparing anything")
	}
	return out
}

// comparison parses the file the comparisons are written in.
func comparison(t *testing.T) *ast.File {
	t.Helper()

	file, err := parser.ParseFile(token.NewFileSet(), "agreement.go", agreement, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parsing the comparison file: %v", err)
	}
	return file
}
