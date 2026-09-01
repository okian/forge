package generate_test

import (
	"strings"
	"testing"

	"github.com/okian/forge/internal/diag"
	"github.com/okian/forge/internal/generate"
	"github.com/okian/forge/internal/goldentest"
	"github.com/okian/forge/internal/model"
)

// A declaration claims what its methods add up to, and says so where the
// compiler will check it.
//
// The claim is worth making for two reasons and both are about somebody else: a
// stack that stops satisfying an interface fails in the generated file rather
// than at a caller's line, and a reader who is not going to read forty methods
// can read what they come to.
func TestWhatADeclarationClaims(t *testing.T) {
	held := claiming(t, "Persons")

	// Each named with the half of the type that satisfies it. Writing a
	// document needs nothing of the container but its elements and is declared
	// on the value; reading one into it assigns, so it is declared on the
	// pointer — and a claim naming the value for that one would not compile,
	// while one naming the pointer for the others would understate what the
	// type does.
	for _, want := range []string{
		"_ io.WriterTo          = *new(Persons)",
		"_ io.ReaderFrom        = (*Persons)(nil)",
		"_ json.MarshalerTo     = *new(Persons)",
		"_ json.UnmarshalerFrom = (*Persons)(nil)",
	} {
		if !strings.Contains(held, want) {
			t.Errorf("the output does not claim %q:\n%s", want, held)
		}
	}
}

// The walk is claimed as a method expression rather than as an interface.
//
// A method expression is resolved when the package is built and calls nothing.
// An assertion of the same thing would have to name a value of the type and
// initialise it, which costs an allocation and a line of package
// initialisation for a claim that is checked either way.
func TestTheWalkIsClaimedWithoutBeingCalled(t *testing.T) {
	held := claiming(t, "Persons")

	if !strings.Contains(held, "var _ func(*Persons) iter.Seq[Person] = (*Persons).All") {
		t.Errorf("the walk's signature is not claimed:\n%s", held)
	}
	if strings.Contains(held, "iter.Seq[Person] = *new(Persons)") {
		t.Errorf("the walk names a value where a method expression would do:\n%s", held)
	}
}

// A claim is made from what was written rather than from what the stack said it
// would write.
//
// The difference is the whole reason the check reads the declarations: a claim
// made from the shape would be a claim about a layer's description of itself,
// and a generated file that failed to compile would be the first anybody heard
// of the two disagreeing.
func TestNothingIsClaimedThatWasNotWritten(t *testing.T) {
	// A declaration with no codec writes no WriteTo, so it claims neither of
	// the interfaces a codec earns — while still claiming the walk.
	held := claimingStack(t, "Persons", collectionOnly())

	for _, want := range []string{"io.WriterTo", "json.MarshalerTo"} {
		if strings.Contains(held, want) {
			t.Errorf("a declaration with no codec claims %s:\n%s", want, held)
		}
	}

	// The var block goes with them, rather than being written empty. Asserted
	// because the absences above would hold just as well of an implementation
	// that claimed nothing at all, and this is the line that tells the two
	// apart.
	if strings.Contains(held, "= (*Persons)(nil)") {
		t.Errorf("a declaration with no codec claims an interface anyway:\n%s", held)
	}
	if !strings.Contains(held, "(*Persons).All") {
		t.Errorf("a declaration that walks does not claim its walk:\n%s", held)
	}
}

// The walk is claimed with the element the declaration holds, taken from the
// subject rather than from the method it is about.
//
// A claim read back off the thing it claims is true whatever that thing does. A
// container whose storage regressed to walking something else would satisfy an
// assertion built that way, and the reader would be told a signature that was
// checked against itself.
func TestTheWalkIsClaimedWithTheElement(t *testing.T) {
	held := claiming(t, "Persons")

	if !strings.Contains(held, "iter.Seq[Person] = (*Persons).All") {
		t.Errorf("the walk is not claimed over the element the declaration holds:\n%s", held)
	}
}

// A skip turns off exactly one claim.
func TestASkipTurnsOffOneClaim(t *testing.T) {
	held := claimingWith(t, "Persons", "//forge:skip io.WriterTo")

	if strings.Contains(held, "io.WriterTo") {
		t.Errorf("a skipped interface is claimed anyway:\n%s", held)
	}
	for _, want := range []string{"io.ReaderFrom", "json.MarshalerTo", "json.UnmarshalerFrom"} {
		if !strings.Contains(held, want) {
			t.Errorf("skipping one claim dropped %s:\n%s", want, held)
		}
	}
}

// And what is left after a skip still compiles.
//
// The imports are the reason to ask. Each claim brings the import its interface
// is written under, and a claim that was turned off must not bring one: an
// import nothing in the file names is not a warning in Go, it is a package that
// does not build — and the file it would be in is the one nobody may edit.
func TestWhatIsLeftAfterASkipCompiles(t *testing.T) {
	cases := map[string][]string{
		"one of two over a package": {"//forge:skip io.WriterTo"},
		"the walk":                  {"//forge:skip All"},

		// Both of them, which is the case that can go wrong. The codec's own
		// methods name jsontext and never encoding/json/v2, so the only thing
		// in the file asking for that package is the pair of claims — and a
		// file that imports it once they are gone does not build.
		"everything one package gave": {
			"//forge:skip json.MarshalerTo",
			"//forge:skip json.UnmarshalerFrom",
		},
	}

	for name, written := range cases {
		t.Run(name, func(t *testing.T) {
			files, diags := generating(t, "Persons", everyLayer(), written...)
			if !diags.Empty() {
				t.Fatalf("generating was refused:\n%s", diags.Render())
			}

			compiling(t, files, "Persons")
		})
	}
}

// And the walk can be turned off by name, since it is claimed like the rest.
func TestTheWalkCanBeSkipped(t *testing.T) {
	held := claimingWith(t, "Persons", "//forge:skip All")

	if strings.Contains(held, "(*Persons).All") {
		t.Errorf("a skipped walk is claimed anyway:\n%s", held)
	}
	if !strings.Contains(held, "io.WriterTo") {
		t.Errorf("skipping the walk dropped the interfaces:\n%s", held)
	}
}

// A skip that turns nothing off is reported.
//
// Silence would be worse than the mistake: an author who wrote it believes the
// declaration does not claim something, and is wrong about a different thing
// than they think — either the name is misspelled or the claim was never there.
func TestASkipThatTurnsNothingOff(t *testing.T) {
	cases := map[string]struct{ written, says string }{
		"an interface nothing claims": {"//forge:skip io.Closer", "does not claim io.Closer"},
		"a misspelling":               {"//forge:skip io.WriteTo", "does not claim io.WriteTo"},
		"nothing at all":              {"//forge:skip", "names nothing to skip"},
	}

	for name, want := range cases {
		t.Run(name, func(t *testing.T) {
			_, diags := generating(t, "Persons", everyLayer(), want.written)
			if diags.Empty() {
				t.Fatal("a skip that turns nothing off was passed over")
			}

			found := reported(t, diags, "FRG3019")
			if !strings.Contains(found.Message, want.says) {
				t.Errorf("the complaint does not say %q:\n%s", want.says, found.Message)
			}
			if found.Hint == "" {
				t.Error("the complaint says nothing to do about it")
			}
		})
	}
}

// The same interface skipped twice is reported.
//
// The second turns nothing off, which is the same thing wrong with a skip that
// names an interface nobody claimed. Left alone it reads as two decisions where
// there is one, and an author deleting what looks like the only skip would find
// the claim still missing.
func TestOneInterfaceSkippedTwice(t *testing.T) {
	_, diags := generating(t, "Persons", everyLayer(),
		"//forge:skip io.WriterTo", "//forge:skip io.WriterTo")

	if diags.Empty() {
		t.Fatal("a repeated skip was passed over")
	}

	found := reported(t, diags, "FRG3020")
	if !strings.Contains(found.Message, "io.WriterTo") {
		t.Errorf("the complaint does not name the skip it repeats:\n%s", found.Message)
	}
	if !strings.Contains(found.Hint, "the first is at") {
		t.Errorf("the complaint does not say where the first one is:\n%s", found.Hint)
	}
}

// A skip that names nothing is still only reported once as a repeat.
//
// Two of the same mistake is one mistake and one repeat, whichever mistake it
// is. Reporting the second as a fresh case of the first would say the same
// sentence twice about one line the author has already been told about.
func TestOneMistakenSkipWrittenTwice(t *testing.T) {
	_, diags := generating(t, "Persons", everyLayer(),
		"//forge:skip io.Closer", "//forge:skip io.Closer")

	if got := len(reportedAll(t, diags, "FRG3019")); got != 1 {
		t.Errorf("a skip written twice was reported as unclaimed %d times, want 1", got)
	}
	if got := len(reportedAll(t, diags, "FRG3020")); got != 1 {
		t.Errorf("the repeat was reported %d times, want 1", got)
	}
}

// A skip names an interface the way Go names it.
//
// The author is configuring the run that writes the file, so they cannot know
// what that file will call a package: holding them to an alias a layer picked
// would be asking for the answer before the question. Forge's own spelling
// always works, and the file's own spelling works too, since a reader who has
// the file in front of them will reach for what they see.
func TestWhatSpellingASkipUses(t *testing.T) {
	held := claimingWith(t, "Persons", "//forge:skip io.WriterTo")

	if strings.Contains(held, "io.WriterTo") {
		t.Errorf("a skip written the way Go names the interface turned nothing off:\n%s", held)
	}
}

// A complaint about a skip points at the name it is about.
//
// The directive is a line and the mistake is a word in it. An editor that jumps
// to the reported column lands on the mistake or on the comment marker, and the
// difference is whether reading the message is the last step or the first.
func TestWhereASkipIsReported(t *testing.T) {
	written := "//forge:skip io.Closer"

	_, diags := generating(t, "Persons", everyLayer(), written)

	found := reported(t, diags, "FRG3019")
	if got, want := found.Pos.Column, 1+strings.Index(written, "io.Closer"); got != want {
		t.Errorf("the complaint points at column %d, want %d (the name it is about)", got, want)
	}
}

// claiming generates a declaration over every element layer and returns its
// file.
func claiming(t *testing.T, declared string) string {
	t.Helper()
	return claimingStack(t, declared, everyLayer())
}

// claimingWith does the same with a directive written on the declaration.
func claimingWith(t *testing.T, declared, directive string) string {
	t.Helper()

	files, diags := generating(t, declared, everyLayer(), directive)
	if !diags.Empty() {
		t.Fatalf("generating was refused:\n%s", diags.Render())
	}
	return string(written(t, files, generate.Named(declared)))
}

// claimingStack generates over a given stack and returns the declaration's
// file.
func claimingStack(t *testing.T, declared string, stack []model.LayerRef) string {
	t.Helper()

	files, diags := generating(t, declared, stack)
	if !diags.Empty() {
		t.Fatalf("generating was refused:\n%s", diags.Render())
	}
	return string(written(t, files, generate.Named(declared)))
}

// generating builds one spec declaration over a stack, with whatever directives
// were written on it.
func generating(t *testing.T, declared string, stack []model.LayerRef, directives ...string) ([]generate.File, diag.Set) {
	t.Helper()

	asked := request(declared, directives...)
	asked.Model.Form = model.FormSpec
	asked.Model.Stack = stack

	return generate.Package(local, "model", []generate.Request{asked}, config())
}

// compiling type-checks a package's whole output, as the go command would.
//
// Every file forge wrote goes in, because what is under test here is a file
// that has to agree with itself: an import each claim brings and a name only
// some other file declares are both things a golden comparison reads straight
// past and a compiler does not.
func compiling(t *testing.T, files []generate.File, declared string) {
	t.Helper()

	sources := []goldentest.Source{{
		Name: "person.go",
		Content: []byte("package model\n\n// Person is what the collection holds.\n" +
			"type Person struct {\n\tID int\n\tName string\n}\n"),
	}}

	for _, file := range files {
		sources = append(sources, goldentest.Source{
			Name: file.Name, Content: file.Content, Generated: true,
		})
	}

	// The ordinary build rather than the one the spec is written under, since
	// the claims are in the file that build compiles and the declaration they
	// are about is one forge owns there.
	if err := goldentest.Compiles(goldentest.Package{Path: "model", Files: sources}); err != nil {
		t.Fatalf("what forge wrote for %s does not compile: %v", declared, err)
	}
}

// everyLayer is the stack the claims are exercised over: a container with a
// codec beneath it, which is what earns every interface this build can decide
// about.
func everyLayer() []model.LayerRef {
	return []model.LayerRef{
		{Origin: model.TypeRef{Pkg: model.MarkerPkg, Name: "Collection"}, Kind: model.KindRefining},
		{Origin: model.TypeRef{Pkg: model.MarkerPkg, Name: "Slice"}, Kind: model.KindStorage},
		{Origin: model.TypeRef{Pkg: model.MarkerPkg, Name: "Json"}, Kind: model.KindElement},
	}
}

// collectionOnly is the same stack without the codec, so that nothing writes a
// WriteTo and nothing claims one.
func collectionOnly() []model.LayerRef {
	return []model.LayerRef{
		{Origin: model.TypeRef{Pkg: model.MarkerPkg, Name: "Collection"}, Kind: model.KindRefining},
		{Origin: model.TypeRef{Pkg: model.MarkerPkg, Name: "Slice"}, Kind: model.KindStorage},
	}
}
