package generate_test

import (
	"bytes"
	"testing"

	"github.com/okian/forge/internal/generate"
	"github.com/okian/forge/internal/goldentest"
	"github.com/okian/forge/internal/model"
)

// A declaration forge owns the type of is written under the complement of the
// tag the author's is written under.
//
// The two are declarations of one name, and exactly one may be in scope. The
// spec sits under a tag so the compiler still checks it — a renamed subject is
// a build failure rather than a stale comment — and the generated file sits
// under the negation so the ordinary build gets the real type. Without the
// constraint the package holds the name twice and does not compile at all,
// which is a failure the author cannot read as forge's.
func TestWhatASpecDeclarationBecomes(t *testing.T) {
	asked := request("Persons", "//forge:collection sort=Name")
	asked.Model.Form = model.FormSpec

	files, diags := generate.Package(local, "model", []generate.Request{asked}, config())
	if !diags.Empty() {
		t.Fatalf("generating was refused:\n%s", diags.Render())
	}

	held := written(t, files, generate.Named("Persons"))

	// The type itself, which the author's file declares only under the tag:
	// forge owns it here, and its underlying form comes from the storage layer
	// at the bottom of the stack.
	if want := "type Persons []Person"; !bytes.Contains(held, []byte(want)) {
		t.Errorf("the file does not declare the type as %q:\n%s", want, held)
	}

	// And the constraint, asked of the go command rather than read.
	//
	// What the file carries has to be the *complement* of the tag the spec is
	// written under, and a substring of it is not: a constraint naming a tag
	// nobody sets — one letter's difference — is a constraint that is always
	// true, and would put the generated type in every build beside the author's
	// own. So the package is compiled both ways, and what is asserted is that
	// exactly one declaration of the name is in scope each time.
	//
	// The stand-in spec declares the type rather than the stack, because what
	// is under test is the constraint and not the marker: as far as scope is
	// concerned, a spec file is a second declaration of the name.
	spec := goldentest.Source{Name: "spec.go", Content: []byte(
		"//go:build forgespec\n\npackage model\n\ntype Persons []Person\n")}

	// The subject alone. What the fixture for an inline declaration compiles
	// against holds the declaration too, because there the author wrote it —
	// here they wrote it in the file above, under the tag.
	subject := goldentest.Source{Name: "person.go", Content: []byte(
		"package model\n\n// Person is what the collection holds.\ntype Person struct {\n\tID int\n\tName string\n}\n")}

	for _, tags := range [][]string{nil, {"forgespec"}} {
		pkg := goldentest.Package{
			Path: "model", Tags: tags,
			Files: []goldentest.Source{
				subject,
				{Name: generate.Named("Persons"), Content: held, Generated: true},
				{Name: generate.Shared(), Content: written(t, files, generate.Shared()), Generated: true},
				spec,
			},
		}

		if err := goldentest.Compiles(pkg); err != nil {
			t.Errorf("the package does not compile with tags %v: %v", tags, err)
		}
	}
}

// A declaration the author owns the type of carries no constraint, and forge
// does not declare the type a second time.
//
// Their file is under no tag, so methods written under one would be missing
// from the build there is; and the type is already theirs, so a second
// declaration would be the package holding one name twice.
func TestWhatAnInlineDeclarationBecomes(t *testing.T) {
	files, diags := generate.Package(local, "model",
		[]generate.Request{request("Persons", "//forge:collection sort=Name")}, config())
	if !diags.Empty() {
		t.Fatalf("generating was refused:\n%s", diags.Render())
	}

	held := written(t, files, generate.Named("Persons"))

	if bytes.Contains(held, []byte("//go:build")) {
		t.Errorf("the file carries a build constraint:\n%s", held)
	}
	if bytes.Contains(held, []byte("type Persons []Person")) {
		t.Errorf("forge declared a type the author already declared:\n%s", held)
	}
}

// The helpers a package shares carry no constraint either.
//
// The ordinary build needs them, because an inline declaration's file calls
// them and carries no tag of its own. The tagged build does not need them yet,
// and will: what mirrors the generated API there is not written, and when it is
// it will be written against these same helpers. Until then they are a generic
// type nothing under the tag names, which costs that build nothing — and a
// helper constrained to one tag would be a helper missing from the other.
func TestTheSharedFileCarriesNoConstraint(t *testing.T) {
	asked := request("Persons", "//forge:collection sort=Name")
	asked.Model.Form = model.FormSpec

	files, diags := generate.Package(local, "model", []generate.Request{asked}, config())
	if !diags.Empty() {
		t.Fatalf("generating was refused:\n%s", diags.Render())
	}

	if held := written(t, files, generate.Shared()); bytes.Contains(held, []byte("//go:build")) {
		t.Errorf("the shared file carries a build constraint:\n%s", held)
	}
}

// Moving a declaration between the two forms changes what is written for it.
//
// The form reaches the fingerprint, so this holds however the two files come to
// differ — which is the point: a run that found the fingerprints equal would
// leave the ordinary build with whichever file was there first, and for one of
// the two directions that is a build with no type in it at all.
func TestMovingADeclarationBetweenFormsChangesWhatIsWritten(t *testing.T) {
	inline := request("Persons", "//forge:collection sort=Name")

	spec := request("Persons", "//forge:collection sort=Name")
	spec.Model.Form = model.FormSpec

	one, _ := generate.Package(local, "model", []generate.Request{inline}, config())
	two, _ := generate.Package(local, "model", []generate.Request{spec}, config())

	if bytes.Equal(written(t, one, generate.Named("Persons")), written(t, two, generate.Named("Persons"))) {
		t.Error("a declaration written each way produced one file")
	}
}

// written returns the content of the file of that name, failing if the run did
// not write one.
func written(t *testing.T, files []generate.File, name string) []byte {
	t.Helper()

	for _, file := range files {
		if file.Name == name {
			return file.Content
		}
	}

	t.Fatalf("no %s among the files written", name)
	return nil
}
