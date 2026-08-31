package generate_test

import (
	"bytes"
	"go/token"
	"go/types"
	"strings"
	"testing"

	"github.com/okian/forge/internal/generate"
	"github.com/okian/forge/internal/goldentest"
	"github.com/okian/forge/internal/model"
)

// The subject a spec declaration is of, which the author writes under no tag
// because it is an ordinary type of theirs.
var subject = goldentest.Source{Name: "person.go", Content: []byte(
	"package model\n\n// Person is what the collection holds.\ntype Person struct {\n\tID int\n\tName string\n}\n")}

// The declaration itself, which the author writes under the tag so that forge
// can own the type under its complement.
//
// It declares the type rather than the stack, because what these tests are
// about is which files hold which names — and as far as scope is concerned a
// spec file is a second declaration of the name, marker or no marker.
var declared = goldentest.Source{Name: "spec.go", Content: []byte(
	"//go:build forgespec\n\npackage model\n\ntype Persons []Person\n")}

// elsewhere builds a subject declared in another package, under the name that
// package declares for itself.
//
// The two are given separately because they are separate: a package's name is
// what its clause says, and the last element of its path is only usually the
// same thing.
func elsewhere(t *testing.T, path, name string) *model.Struct {
	t.Helper()

	pkg := types.NewPackage(path, name)
	obj := types.NewTypeName(token.NoPos, pkg, "Thing", nil)

	return &model.Struct{
		Named: types.NewNamed(obj, types.NewStruct(nil, nil), nil),
		Fields: []model.Field{
			{Name: "ID", Exported: true, Type: model.Classified{Type: types.Typ[types.Int]}},
			{Name: "Name", Exported: true, Type: model.Classified{Type: types.Typ[types.String]}},
		},
	}
}

// spec generates for one declaration forge owns the type of.
func spec(t *testing.T, directives ...string) []generate.File {
	t.Helper()

	asked := request("Persons", directives...)
	asked.Model.Form = model.FormSpec

	files, diags := generate.Package(local, "model", []generate.Request{asked}, config())
	if !diags.Empty() {
		t.Fatalf("generating was refused:\n%s", diags.Render())
	}
	return files
}

// Both configurations of a package hold its whole API, so code that calls a
// generated method compiles under either.
//
// This is the whole reason the stub file exists. The file holding the real
// methods is constrained against the tag, so a build with the tag set does not
// have them — and every call site in the package stops compiling, which would
// make the configuration that exists to check the spec unable to check anything
// that uses it.
//
// The call sites are what is being tested, so they are here rather than
// implied: one inside a function, and one at package level. The second is the
// harder case and the reason a doc comment is not enough — a package-level
// initialiser is type-checked wherever it appears, so a reference from one
// fails a build that a reference from inside a function would survive.
func TestBothBuildsTypeCheckWithCallSitesPresent(t *testing.T) {
	files := spec(t, "//forge:collection sort=Name index=ID")

	calling := goldentest.Source{Name: "use.go", Content: []byte(
		"package model\n\n" +
			"// Empty is read at package level, which is where a missing method is worst.\n" +
			"var Empty = Persons(nil).Len()\n\n" +
			"// counted reads the generated API from inside a function.\n" +
			"func counted(p Persons) []string {\n\treturn p.Names()\n}\n")}

	for _, tags := range [][]string{nil, {"forgespec"}} {
		pkg := goldentest.Package{
			Path: "model", Tags: tags,
			Files: []goldentest.Source{
				subject, declared, calling,
				{Name: generate.Named("Persons"), Content: written(t, files, generate.Named("Persons")), Generated: true},
				{Name: generate.Shared(), Content: written(t, files, generate.Shared()), Generated: true},
				{Name: generate.Stubs(), Content: written(t, files, generate.Stubs()), Generated: true},
			},
		}

		if err := goldentest.Compiles(pkg); err != nil {
			t.Errorf("the package does not compile with tags %v: %v", tags, err)
		}
	}
}

// The stub file is written under the tag, which is the complement of what every
// file it stands in for carries.
//
// Were it written under anything else the two would be in scope together, and a
// package would hold every method twice.
func TestTheStubFileIsWrittenUnderTheTag(t *testing.T) {
	held := written(t, spec(t), generate.Stubs())

	if want := "//go:build forgespec\n"; !bytes.Contains(held, []byte(want)) {
		t.Errorf("the file does not carry %q:\n%s", want, held)
	}
	if bytes.Contains(held, []byte("//go:build !forgespec")) {
		t.Errorf("the file carries the constraint of the output it stands in for:\n%s", held)
	}
}

// Forge does not declare the type a second time in the stub file.
//
// Under the tag it is the author's file that declares it — that is the whole
// arrangement the two constraints produce — so a declaration here would put the
// name in scope twice and fail the build the file exists to make possible.
func TestTheStubFileLeavesTheTypeToItsAuthor(t *testing.T) {
	held := written(t, spec(t), generate.Stubs())

	if bytes.Contains(held, []byte("type Persons ")) {
		t.Errorf("the stub file declares the type the author's file declares:\n%s", held)
	}

	// The helper types are a different matter: they are forge's, they appear in
	// the signatures below, and nothing else declares them under the tag.
	if want := "type PersonsSeq struct"; !bytes.Contains(held, []byte(want)) {
		t.Errorf("the stub file does not declare %q, which its own signatures name:\n%s", want, held)
	}
}

// A stub has a body that cannot be reached and says what it is.
//
// It has to have one at all because a generic function without a body is not
// something the type-checker accepts, and a panic is the body that satisfies a
// signature with results as well as one without.
func TestAStubDoesNothing(t *testing.T) {
	held := written(t, spec(t), generate.Stubs())

	if want := `panic("forge stub")`; !bytes.Contains(held, []byte(want)) {
		t.Errorf("the file does not say %q:\n%s", want, held)
	}

	// The implementation is what the file must not carry. A stub that kept the
	// body of the method it stands in for would need everything the body needs,
	// and would be the generated file under a second name.
	if bytes.Contains(held, []byte("slices.Clone")) {
		t.Errorf("the stub file carries an implementation:\n%s", held)
	}
}

// The stub file imports what its signatures name and nothing else.
//
// A body is where most of a generated file's imports are used — a container's
// method names the package it delegates to, and its signature does not — so
// carrying the real file's imports over would leave a file importing packages
// it never mentions, which does not compile. The check is that particular
// import rather than any: iter is named by a result type and survives, slices
// is named only inside bodies and must not.
func TestTheStubFileImportsWhatItStillNames(t *testing.T) {
	held := written(t, spec(t), generate.Stubs())

	if !bytes.Contains(held, []byte(`"iter"`)) {
		t.Errorf("the file does not import iter, which its signatures name:\n%s", held)
	}
	if bytes.Contains(held, []byte(`"slices"`)) {
		t.Errorf("the file imports slices, which only the discarded bodies named:\n%s", held)
	}
}

// A declaration the author owns the type of is not stood in for.
//
// Its file carries no constraint, so it is in every build already; a stub of
// the same methods would be a second declaration of each of them, in the one
// configuration that has both files.
func TestAnInlineDeclarationGetsNoStubs(t *testing.T) {
	files, diags := generate.Package(local, "model",
		[]generate.Request{request("Persons", "//forge:collection sort=Name")}, config())
	if !diags.Empty() {
		t.Fatalf("generating was refused:\n%s", diags.Render())
	}

	for _, file := range files {
		if file.Name == generate.Stubs() {
			t.Errorf("a stub file was written for a declaration whose output carries no constraint:\n%s", file.Content)
		}
	}
}

// A package holding both forms writes stubs for the half that needs them, and
// still compiles both ways.
//
// The mixture is the ordinary case once nesting sends one declaration to a spec
// file and the others stay where they are, and it is where writing stubs for
// everything would collide: the inline declaration's methods are already in the
// build the stub file belongs to.
func TestAPackageHoldingBothForms(t *testing.T) {
	owned := request("Persons")
	owned.Model.Form = model.FormSpec

	files, diags := generate.Package(local, "model",
		[]generate.Request{owned, request("Folk")}, config())
	if !diags.Empty() {
		t.Fatalf("generating was refused:\n%s", diags.Render())
	}

	held := written(t, files, generate.Stubs())

	if !bytes.Contains(held, []byte("(c Persons)")) {
		t.Errorf("the stub file does not stand in for the declaration forge owns:\n%s", held)
	}
	if bytes.Contains(held, []byte("(c Folk)")) {
		t.Errorf("the stub file stands in for a declaration whose own file is in every build:\n%s", held)
	}

	folk := goldentest.Source{Name: "folk.go", Content: []byte(
		"package model\n\ntype Folk []Person\n")}

	for _, tags := range [][]string{nil, {"forgespec"}} {
		pkg := goldentest.Package{
			Path: "model", Tags: tags,
			Files: []goldentest.Source{
				subject, declared, folk,
				{Name: generate.Named("Persons"), Content: written(t, files, generate.Named("Persons")), Generated: true},
				{Name: generate.Named("Folk"), Content: written(t, files, generate.Named("Folk")), Generated: true},
				{Name: generate.Shared(), Content: written(t, files, generate.Shared()), Generated: true},
				{Name: generate.Stubs(), Content: held, Generated: true},
			},
		}

		if err := goldentest.Compiles(pkg); err != nil {
			t.Errorf("the package does not compile with tags %v: %v", tags, err)
		}
	}
}

// Two subjects whose packages share a name are refused rather than written.
//
// Each declaration's own file binds the name once and compiles; the file
// standing in for them all binds it twice, which does not. Nothing below the
// file can see it — a layer sees only its own contribution, and both are
// blameless — so it is caught where the names are brought together, and the
// package is written whole or not at all.
func TestTwoSubjectsWhosePackagesShareAName(t *testing.T) {
	alphas := request("Alphas")
	alphas.Model.Form = model.FormSpec
	alphas.Model.Subject = elsewhere(t, "example.com/alpha/thing", "thing")

	betas := request("Betas")
	betas.Model.Form = model.FormSpec
	betas.Model.Subject = elsewhere(t, "example.com/beta/thing", "thing")

	files, diags := generate.Package(local, "model", []generate.Request{alphas, betas}, config())

	if diags.Empty() {
		t.Fatalf("two packages bound to one name were written, into %d files", len(files))
	}
	if said := diags.Render(); !strings.Contains(said, "FRG4003") || !strings.Contains(said, "both imported as") {
		t.Errorf("the report does not name the collision:\n%s", said)
	}
	// The file itself is what was not produced. Holding the rest of the package
	// back is the caller's, which writes a package whole or not at all; what
	// matters here is that nothing was handed up to be written.
	for _, file := range files {
		if file.Name == generate.Stubs() {
			t.Errorf("a file binding one name to two packages was handed up:\n%s", file.Content)
		}
	}
}

// A declaration named after a file the package writes for itself is refused.
//
// Its file and the package's are the same file, and whichever is written second
// wins: the declaration's methods vanish from the ordinary build, and nothing
// reports it, because the name is claimed either way.
func TestADeclarationNamedAfterAFileThePackageKeeps(t *testing.T) {
	for _, declared := range []string{"Stubs", "Shared"} {
		t.Run(declared, func(t *testing.T) {
			files, diags := generate.Package(local, "model",
				[]generate.Request{request(declared)}, config())

			if diags.Empty() {
				t.Fatalf("%s was written to a file the package keeps, among %d files", declared, len(files))
			}
			if said := diags.Render(); !strings.Contains(said, "FRG4006") ||
				!strings.Contains(said, "which the package writes for itself") {
				t.Errorf("the report does not say what was collided with:\n%s", said)
			}
		})
	}
}

// The header of the file standing in for a package's output follows the
// declarations it stands in for.
//
// It is what says the file is still current, so a declaration that changed
// without changing it is a file that reports itself up to date and is not.
func TestTheStandInFileFollowsItsDeclarations(t *testing.T) {
	first := written(t, spec(t, "//forge:collection sort=Name"), generate.Stubs())
	again := written(t, spec(t, "//forge:collection sort=Name"), generate.Stubs())
	if !bytes.Equal(first, again) {
		t.Fatal("the same declaration wrote two different files")
	}

	other := written(t, spec(t, "//forge:collection sort=Name index=ID"), generate.Stubs())
	if bytes.Equal(inputs(t, first), inputs(t, other)) {
		t.Errorf("a declaration that gained an option left the header alone:\n%s", other)
	}
}

// inputs returns the line of a generated header that records what the file was
// made from.
func inputs(t *testing.T, held []byte) []byte {
	t.Helper()

	for line := range bytes.Lines(held) {
		if bytes.HasPrefix(line, []byte("// inputs ")) {
			return line
		}
	}

	t.Fatalf("no inputs line in the header:\n%s", held)
	return nil
}
