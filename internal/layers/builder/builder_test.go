package builder_test

import (
	"strings"
	"testing"

	"github.com/okian/forge/internal/diag"
	"github.com/okian/forge/internal/goldentest"
)

// Every exported field gets a setter named after it, answering with the builder
// so that the next one can be written on the same expression.
func TestASetterPerField(t *testing.T) {
	held := source(t, written(t, "Person"))

	for _, want := range []string{
		"func (b *PersonBuilder) Name(v string) *PersonBuilder {\n\tb.held.Name = v\n\tb.given[0] = true\n\treturn b\n}",
		"func (b *PersonBuilder) Age(v int) *PersonBuilder {\n\tb.held.Age = v\n\tb.given[1] = true\n\treturn b\n}",
		"func (b *PersonBuilder) Nick(v string) *PersonBuilder {\n\tb.held.Nick = v\n\treturn b\n}",
		"func (b *PersonBuilder) Aliases(v []string) *PersonBuilder {\n\tb.held.Aliases = v\n\treturn b\n}",
	} {
		if !strings.Contains(held, want) {
			t.Errorf("the builder does not hold %q:\n%s", want, held)
		}
	}
}

// Both of the rules that say a field is mandatory are read, because between
// them they cover every type and neither covers all of them.
//
// A builder that read only required would demand a name and not an age, for no
// reason the author wrote down: required is what a value that can be absent
// takes, and an int is not one of those.
func TestBothRulesThatDemandAValue(t *testing.T) {
	held := source(t, written(t, "Person"))

	if !strings.Contains(held, "Build reports them: Name, Age, Email.") {
		t.Errorf("the builder does not demand what the rules say it should:\n%s", held)
	}
	for _, want := range []string{
		`if !b.given[0] {`,
		`ValidationError{Path: "Name", Rule: "required", Want: "a value"}`,
		`ValidationError{Path: "Age", Rule: "required", Want: "a value"}`,
		`ValidationError{Path: "Email", Rule: "required", Want: "a value"}`,
	} {
		if !strings.Contains(held, want) {
			t.Errorf("the builder does not hold %q:\n%s", want, held)
		}
	}
}

// A rule written after the first is found too, and so is one written after a
// comma and a space.
//
// The space is the whole of what this is about. A person writing a list writes
// one, and the check trims it because the check splits the tag itself — so a
// builder reading the tag a second way would demand a field the check enforces,
// or not, depending on a character nobody looks at. Both readers are the same
// reader, and this is what says so.
func TestARuleIsFoundHoweverItIsWritten(t *testing.T) {
	held := source(t, written(t, "Spaced"))

	for _, want := range []string{`Path: "Padded"`, `Path: "Plain"`} {
		if !strings.Contains(held, want) {
			t.Errorf("the builder does not demand %s:\n%s", want, held)
		}
	}
	if !strings.Contains(held, "reports them: Padded, Plain.") {
		t.Errorf("the builder demands a different set from the one written:\n%s", held)
	}
}

// A field no rule speaks for is offered and not demanded.
func TestWhatIsOfferedAndNotDemanded(t *testing.T) {
	held := source(t, written(t, "Person"))

	if !strings.Contains(held, "func (b *PersonBuilder) Nick(v string) *PersonBuilder {\n\tb.held.Nick = v\n\treturn b\n}") {
		t.Errorf("a field nothing demands records that it was given:\n%s", held)
	}
	if strings.Contains(held, `Path: "Nick"`) {
		t.Errorf("a field nothing demands is reported as missing:\n%s", held)
	}
}

// The types a missing field is reported through are contributed with the
// builder, and the folding only a check needs is not.
//
// That split is the whole reason the two contributions are keyed apart. A
// package with a builder and no check would otherwise be written a function
// nothing in it calls — legal Go, and a line in somebody's file that answers no
// question they have.
func TestWhatABuilderIsWrittenBeside(t *testing.T) {
	held := source(t, written(t, "Person"))

	if !strings.Contains(held, "type ValidationErrors []ValidationError") {
		t.Errorf("the builder has nothing to report a missing field through:\n%s", held)
	}
	if strings.Contains(held, "nestedValidation") {
		t.Errorf("a builder was written the folding only a check needs:\n%s", held)
	}
}

// A subject nothing is demanded of has no record of what was given, and its
// Build cannot fail.
//
// The error stays in the signature, so that a caller writes the same thing
// whether or not a rule is added to the subject tomorrow.
func TestASubjectThatDemandsNothing(t *testing.T) {
	held := source(t, written(t, "Open"))

	if strings.Contains(held, "given") {
		t.Errorf("a builder that can refuse nothing records what it was given:\n%s", held)
	}
	if !strings.Contains(held, "func (b *OpenBuilder) Build() (Open, error) { return b.held, nil }") {
		t.Errorf("the builder does not hand the value straight back:\n%s", held)
	}
	if strings.Contains(held, "ValidationErrors") {
		t.Errorf("a builder that reports nothing was written the types to report it with:\n%s", held)
	}
}

// A field the builder cannot set is left out, and the type says so rather than
// leaving a reader to notice.
func TestWhatABuilderDoesNotOffer(t *testing.T) {
	held := source(t, written(t, "Keeping"))

	if strings.Contains(held, "secret") {
		t.Errorf("a builder offers a field a caller could not have named:\n%s", held)
	}
	if !strings.Contains(held, "The unexported fields of the Keeping are not among the") {
		t.Errorf("the builder does not say what it left out:\n%s", held)
	}

	// And a subject that keeps nothing back does not say it kept something.
	if strings.Contains(source(t, written(t, "Person")), "are not among the") {
		t.Error("a builder that offers every field says it does not")
	}
}

// A setter's signature names the field's type as the file being generated into
// has to spell it.
//
// Which file that is decides the spelling, and both directions are here: a type
// beside the subject is written under its own name, and one from elsewhere
// under the package's. A setter written for the wrong file would name the local
// type as though it were somebody else's, and would not compile.
func TestASetterNamesTheTypeAsTheFileSpellsIt(t *testing.T) {
	held := source(t, written(t, "Held"))

	for _, want := range []string{
		"func (b *HeldBuilder) Where(v other.Place) *HeldBuilder {",
		"func (b *HeldBuilder) Also(v *other.Place) *HeldBuilder {",
		"func (b *HeldBuilder) Many(v map[string][]other.Place) *HeldBuilder {",
	} {
		if !strings.Contains(held, want) {
			t.Errorf("the builder does not hold %q:\n%s", want, held)
		}
	}

	beside := source(t, written(t, "Person"))
	if !strings.Contains(beside, "func (b *PersonBuilder) Home(v Address) *PersonBuilder {") {
		t.Errorf("a type declared beside the subject is not spelled as the file spells it:\n%s", beside)
	}
}

// What every subject generates compiles, which is the claim no assertion about
// a substring makes.
func TestWhatIsWrittenCompiles(t *testing.T) {
	for _, name := range []string{"Person", "Open", "Held", "Keeping"} {
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
				t.Errorf("the builder for %s does not compile: %v", name, err)
			}
		})
	}
}

// A field whose type has no name in the package being generated into has no
// setter, and is reported rather than written.
//
// It arises for a subject somewhere else: a setter's signature names the
// field's type, and a name that is unexported belongs to the package that
// declared it. Left to the compiler it is an error in a generated file, naming
// a type the author never wrote and cannot reach.
//
// What is inside an exported type is not this: a field of one is named through
// the name, so a subject holding an other.Holder is written for perfectly well
// however that type is made.
func TestAFieldWhoseTypeHasNoNameHere(t *testing.T) {
	_, err := asking(t, otherPkg, "Holder")
	if err == nil {
		t.Fatal("a setter was written with a signature nothing here could name")
	}

	reported, ok := diag.From(err)
	if !ok {
		t.Fatalf("%v is not a diagnostic", err)
	}
	if got, want := reported.Code.String(), "FRG2022"; got != want {
		t.Errorf("reported as %s, want %s: %s", got, want, reported.Message)
	}
	if !strings.Contains(reported.Message, "cannot be named from the package") {
		t.Errorf("the complaint does not say what is wrong:\n%s", reported.Message)
	}

	// And the type held from here is fine, which is what says the rule is about
	// what a signature names rather than about what a type is made of.
	if _, err := generating(t, "Naming"); err != nil {
		t.Errorf("a subject holding that type was refused too: %v", err)
	}
}

// A contradiction between what a field is and what a rule says of it is
// reported rather than resolved.
func TestWhatABuilderRefuses(t *testing.T) {
	cases := map[string]struct {
		code  string
		says  string
		hints string
	}{
		// A field a caller cannot name, and a tag saying they have to give it.
		"Demanding": {"FRG2020", "secret is unexported", "export the field"},

		// A field named after the one method a builder needs of its own.
		"Ending": {"FRG4019", "would take the name Build", "rename the field"},

		// A field the setter would copy and must not. Left alone it is a run of
		// `go vet` failing in somebody else's repository, over a line they
		// cannot edit.
		"Locked": {"FRG2022", "must not be copied", "behind a pointer"},

		// And a subject a builder would name nothing of, which is the type this
		// layer exists to avoid writing.
		"Bare": {"FRG2021", "no field a caller could give", "export a field"},
	}

	for name, want := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := generating(t, name)
			if err == nil {
				t.Fatalf("a builder was written for %s", name)
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
