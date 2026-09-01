package patch_test

import (
	"strings"
	"testing"

	"github.com/okian/forge/internal/diag"
	"github.com/okian/forge/internal/goldentest"
)

// Every exported field becomes a pointer, because a pointer is how Go says
// there is something here about a value that has a zero.
//
// Without it a handler cannot tell a field somebody cleared from a field they
// never mentioned: both arrive as the zero value, and the handler either
// overwrites what it was not asked to or asks for everything back.
func TestAPointerPerField(t *testing.T) {
	held := source(t, written(t, "Person"))

	for _, want := range []string{
		"Name *string `json:\"name\"`",
		"Age *int `json:\"age,omitempty\"`",
		"Aliases *[]string `json:\"aliases\" validate:\"max=8\"`",
		"Ratio *float64",
	} {
		if !strings.Contains(held, want) {
			t.Errorf("the patch does not hold %q:\n%s", want, held)
		}
	}
}

// A patch's members are named as the subject's are, because the two go over the
// same wire.
//
// It is the difference between a partial update and a no-op nobody notices. A
// codec reads a json tag for a member's name, so a patch whose fields carried
// none would be read under the field's own name while the subject was written
// under the tag's — and a request sent with the names a reply came back under
// would name nothing the patch recognised, decode into a patch that sets
// nothing, and change nothing at all without reporting anything.
func TestAPatchIsNamedAsTheSubjectIs(t *testing.T) {
	held := source(t, written(t, "Person"))

	if !strings.Contains(held, "`json:\"name\"`") {
		t.Errorf("the patch does not carry the subject's tags:\n%s", held)
	}

	// Every tag the field carried, in the order it was written: a patch that
	// kept the one it knew about would be deciding which of somebody's tags
	// matter.
	if !strings.Contains(held, "`json:\"aliases\" validate:\"max=8\"`") {
		t.Errorf("the patch kept some of a field's tags and not others:\n%s", held)
	}

	// And a field with no tag gets none, rather than an empty one.
	if strings.Contains(held, "Ratio *float64 ``") {
		t.Errorf("a field with no tag was given an empty one:\n%s", held)
	}
}

// A field that is already a pointer becomes a pointer to one, because there is
// no third state to collapse the two into.
//
// The outer one says whether the patch mentions the field at all, and the inner
// one is the value, which may itself be absent. A patch that used one pointer
// for both could not say "set this to nothing".
func TestAPointerFieldKeepsItsOwn(t *testing.T) {
	held := source(t, written(t, "Indirect"))

	for _, want := range []string{"Count **int", "Where **other.Place"} {
		if !strings.Contains(held, want) {
			t.Errorf("the patch does not hold %q:\n%s", want, held)
		}
	}
	if !strings.Contains(held, "into.Count = *p.Count") {
		t.Errorf("a pointer field is not written through:\n%s", held)
	}
}

// Apply writes the fields that are there and leaves the rest, which is the
// whole of what a partial update means.
func TestApplyWritesWhatIsThereAndNothingElse(t *testing.T) {
	held := source(t, written(t, "Person"))

	if !strings.Contains(held, "func (p PersonPatch) Apply(into *Person) {") {
		t.Errorf("the patch has no Apply:\n%s", held)
	}
	for _, want := range []string{
		"if p.Name != nil {\n\t\tinto.Name = *p.Name\n\t}",
		"if p.Age != nil {\n\t\tinto.Age = *p.Age\n\t}",
	} {
		if !strings.Contains(held, want) {
			t.Errorf("the patch does not hold %q:\n%s", want, held)
		}
	}
	if strings.Contains(held, "else") {
		t.Errorf("a field the patch says nothing about is written anyway:\n%s", held)
	}
}

// IsZero holds when no field is set, which is a patch nobody asked for anything
// by — and is the name a codec looks for when it is told to leave zero values
// out.
func TestWhenAPatchAsksForNothing(t *testing.T) {
	held := source(t, written(t, "Person"))

	if !strings.Contains(held, "func (p PersonPatch) IsZero() bool {") {
		t.Errorf("the patch cannot say it asks for nothing:\n%s", held)
	}
	for _, want := range []string{"p.Name == nil &&", "p.Ratio == nil"} {
		if !strings.Contains(held, want) {
			t.Errorf("the patch does not hold %q:\n%s", want, held)
		}
	}
}

// A field a patch cannot be filled in with is left out, and the type says so
// rather than leaving a reader to notice.
func TestWhatAPatchDoesNotCarry(t *testing.T) {
	held := source(t, written(t, "Keeping"))

	if strings.Contains(held, "secret") {
		t.Errorf("a patch carries a field nobody outside could have filled in:\n%s", held)
	}
	if !strings.Contains(held, "The unexported fields of the Keeping are not among them") {
		t.Errorf("the patch does not say what it left out:\n%s", held)
	}

	// And a subject that keeps nothing back does not say it kept something.
	if strings.Contains(source(t, written(t, "Person")), "are not among them") {
		t.Error("a patch that carries every field says it does not")
	}
}

// A patch's own fields name their types as the file being generated into has to
// spell them.
//
// Which file that is decides the spelling, and both directions are here: a type
// beside the subject is written under its own name, and one from elsewhere
// under the package's. A patch written for the wrong file would name the local
// type as though it were somebody else's, and would not compile.
func TestAPatchNamesTypesAsTheFileSpellsThem(t *testing.T) {
	held := source(t, written(t, "Held"))

	for _, want := range []string{
		"Where *other.Place",
		"Many *map[string][]other.Place",
		"Fixed *[2]other.Place",
	} {
		if !strings.Contains(held, want) {
			t.Errorf("the patch does not hold %q:\n%s", want, held)
		}
	}

	if beside := source(t, written(t, "Person")); !strings.Contains(beside, "Home *Address") {
		t.Errorf("a type declared beside the subject is not spelled as the file spells it:\n%s", beside)
	}
}

// What every subject generates compiles, which is the claim no assertion about
// a substring makes.
func TestWhatIsWrittenCompiles(t *testing.T) {
	for _, name := range []string{"Person", "Indirect", "Held", "Keeping"} {
		t.Run(name, func(t *testing.T) {
			sources := []goldentest.Source{
				{Name: "model.go", Content: fixtureSource(t)},
				{Name: "zz_forge.go", Content: []byte(source(t, written(t, name))), Generated: true},
			}

			held := goldentest.Package{
				Path:     modelPkg,
				Files:    sources,
				Requires: []goldentest.Package{besideFixture(t)},
			}
			if err := goldentest.Compiles(held); err != nil {
				t.Errorf("the patch for %s does not compile: %v", name, err)
			}
		})
	}
}

// What a patch cannot carry is reported rather than written.
func TestWhatAPatchRefuses(t *testing.T) {
	cases := map[string]struct {
		code  string
		says  string
		hints string
	}{
		// A field named after one of the two methods a patch needs of its own.
		"Naming": {"FRG4020", "one of its own methods", "rename the field"},
		"Asking": {"FRG4020", "one of its own methods", "rename the field"},

		// A field Apply would copy and must not. Left alone it is a run of
		// `go vet` failing in somebody else's repository, over a line they
		// cannot edit.
		"Locked": {"FRG2024", "must not be copied", "behind a pointer"},

		// And a subject a patch could name nothing of, which is a request
		// nobody can make.
		"Bare": {"FRG2023", "no field a caller could change", "export a field"},
	}

	for name, want := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := generating(t, name)
			if err == nil {
				t.Fatalf("a patch was written for %s", name)
			}

			reported, ok := diag.From(err)
			if !ok {
				t.Fatalf("%v is not a diagnostic", err)
			}
			if got := reported.Code.String(); got != want.code {
				t.Errorf("%s is reported as %s, want %s: %s", name, got, want.code, reported.Message)
			}
			if !strings.Contains(reported.Message, want.says) {
				t.Errorf("the complaint about %s does not mention %q:\n%s", name, want.says, reported.Message)
			}
			if !strings.Contains(reported.Hint, want.hints) {
				t.Errorf("the hint for %s does not say %q:\n%s", name, want.hints, reported.Hint)
			}
		})
	}
}

// A field whose type has no name in the package being generated into is
// reported rather than declared.
//
// It arises for a subject somewhere else: a patch declares a field of the same
// type, and a name that is unexported belongs to the package that declared it.
// Left to the compiler it is an error in a generated file, naming a type the
// author never wrote and cannot reach.
func TestAFieldWhoseTypeHasNoNameHere(t *testing.T) {
	_, err := asking(t, otherPkg, "Holder")
	if err == nil {
		t.Fatal("a patch declared a field nothing here could name")
	}

	reported, ok := diag.From(err)
	if !ok {
		t.Fatalf("%v is not a diagnostic", err)
	}
	if got, want := reported.Code.String(), "FRG2024"; got != want {
		t.Errorf("reported as %s, want %s: %s", got, want, reported.Message)
	}
	if !strings.Contains(reported.Message, "cannot be named from the package") {
		t.Errorf("the complaint does not say what is wrong:\n%s", reported.Message)
	}
}

// A subject in another package is written for like any other, which is what
// says the gate is about what a field's type can be named as rather than about
// where the subject lives.
func TestASubjectSomewhereElse(t *testing.T) {
	unit, err := asking(t, otherPkg, "Place")
	if err != nil {
		t.Fatalf("a subject of another package was refused: %v", err)
	}

	held := source(t, unit)
	if !strings.Contains(held, "type OtherPlacePatch struct {") {
		t.Errorf("the patch is not named after the package the subject came from:\n%s", held)
	}
	if !strings.Contains(held, "func (p OtherPlacePatch) Apply(into *other.Place) {") {
		t.Errorf("the patch does not write over the subject:\n%s", held)
	}
}
