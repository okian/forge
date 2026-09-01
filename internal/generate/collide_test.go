package generate_test

import (
	"go/token"
	"go/types"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"

	"github.com/okian/forge/internal/diag"
	"github.com/okian/forge/internal/discover"
	"github.com/okian/forge/internal/generate"
	"github.com/okian/forge/internal/load"
	"github.com/okian/forge/internal/model"
	subjects "github.com/okian/forge/internal/subject"
)

// collidePkg is the fixture package that already declares things forge would
// otherwise write into it.
const collidePkg = "collidefixture/model"

// A name the package already declares is reported rather than written over.
//
// Never an overwrite, and never a silent skip either. A file forge writes is a
// file forge owns, and a name in it the author also declared is two
// declarations in one package — the build fails, and it fails naming the
// generated file, which is the one place nobody can fix it.
func TestAGeneratedNameThePackageAlreadyHas(t *testing.T) {
	_, diags := generate.Package(collidePkg, "model",
		[]generate.Request{colliding(t, "Taken")}, collideConfig(t))

	if diags.Empty() {
		t.Fatal("a declaration wrote over a name the package already had")
	}

	found := reported(t, diags, "FRG4013")
	if !strings.Contains(found.Message, "TakenSeq") {
		t.Errorf("the complaint does not name the collision:\n%s", found.Message)
	}
	if !strings.Contains(found.Message, "already declares at") {
		t.Errorf("the complaint does not say where the other one is:\n%s", found.Message)
	}
	if found.Hint == "" {
		t.Error("the complaint says nothing to do about it")
	}
}

// A method the author wrote in place of a generated one is kept, and the
// generated one is not written.
//
// It is the override mechanism, and it is silent on purpose: somebody who wrote
// the method meant to, and a diagnostic about it would be a diagnostic about
// doing the thing the design invites.
func TestAMethodTheAuthorWroteIsTheOneThatIsKept(t *testing.T) {
	files, diags := generate.Package(collidePkg, "model",
		[]generate.Request{colliding(t, "Overridden")}, collideConfig(t))

	if !diags.Empty() {
		t.Fatalf("an override was reported rather than honoured:\n%s", diags.Render())
	}

	held := string(written(t, files, generate.Named("Overridden")))
	if strings.Contains(held, ") Len() int {") {
		t.Errorf("the generated method was written beside the author's:\n%s", held)
	}

	// And everything else the layer writes is still there, so the override took
	// one method rather than the file.
	if !strings.Contains(held, ") All() iter.Seq[Person] {") {
		t.Errorf("overriding one method dropped the rest:\n%s", held)
	}
}

// An override that no longer answers what the layers above it were written
// against is reported.
//
// The one thing an override may not do. A method written in place of a
// generated one is still the method the stack promised, and one that answers
// with something else turns a stack that composed into a package that does not
// build — reported against generated code, which is the one file nobody can fix
// it in.
func TestAnOverrideThatBreaksTheContract(t *testing.T) {
	_, diags := generate.Package(collidePkg, "model",
		[]generate.Request{colliding(t, "Contradicting")}, collideConfig(t))

	if diags.Empty() {
		t.Fatal("an override that answers with the wrong type was allowed")
	}

	found := reported(t, diags, "FRG4011")
	for _, want := range []string{"Len", "string", "int"} {
		if !strings.Contains(found.Message, want) {
			t.Errorf("the complaint does not mention %q:\n%s", want, found.Message)
		}
	}
	if found.Hint == "" {
		t.Error("the complaint says nothing to do about it")
	}
}

// Two generated declarations that fold onto one name are reported rather than
// written.
//
// A name an element layer writes is built from the type it is for and the
// package that declares it, so that a subject reaching an Address from two
// packages gets two functions. Folding two things into one identifier has
// collisions somewhere however it is written, and this is one: a local
// DomainPerson and a domain.Person reach the same name.
//
// Nothing else would catch it. The package's own names are checked against what
// is generated, and two layers writing one method on one type are checked
// against each other — but two element layers over two subjects write
// package-level functions that neither of them can see the other's. Without
// this the author gets a redeclaration inside a file that says DO NOT EDIT.
func TestTwoGeneratedDeclarationsOfOneName(t *testing.T) {
	_, diags := generate.Package(collidePkg, "model", []generate.Request{
		copying(t, "Locals", subjectNamed(t, collidePkg, "DomainPerson")),
		copying(t, "Foreigners", subjectFrom(t, "collidefixture/domain")),
	}, collideConfig(t))

	if diags.Empty() {
		t.Fatal("two declarations of one name were written")
	}

	found := reported(t, diags, "FRG4018")
	if !strings.Contains(found.Message, "cloneDomainPerson") {
		t.Errorf("the complaint does not name the collision:\n%s", found.Message)
	}
	if found.Hint == "" {
		t.Error("the complaint says nothing to do about it")
	}
}

// Two generated files of one build are one namespace, and a name written into
// both is reported.
//
// Neither file can see the other. What an element layer writes goes into the
// file a package shares, and what a container layer writes goes into the
// declaration's own — so a declaration named after something the shared file
// holds is a collision nothing but the package can find.
func TestOneNameInTwoGeneratedFiles(t *testing.T) {
	asked := copying(t, "ValidationErrors", collideSubject(t))
	asked.Model.Stack = append(asked.Model.Stack,
		model.LayerRef{Origin: model.TypeRef{Pkg: model.MarkerPkg, Name: "Validate"}, Kind: model.KindElement})

	_, diags := generate.Package(collidePkg, "model", []generate.Request{asked}, collideConfig(t))

	if diags.Empty() {
		t.Fatal("one name was written into two files of one build")
	}

	found := reported(t, diags, "FRG4018")
	if !strings.Contains(found.Message, "ValidationErrors") {
		t.Errorf("the complaint does not name the collision:\n%s", found.Message)
	}
}

// One of those two on its own is not a collision, which is what says the check
// is about two names meeting rather than about the name.
func TestOneGeneratedDeclarationOfThatNameIsFine(t *testing.T) {
	_, diags := generate.Package(collidePkg, "model", []generate.Request{
		copying(t, "Foreigners", subjectFrom(t, "collidefixture/domain")),
	}, collideConfig(t))

	if !diags.Empty() {
		t.Errorf("one declaration was reported as colliding with itself:\n%s", diags.Render())
	}
}

// copying builds one spec declaration that copies a subject, which is the shape
// an element layer's contribution to a package takes.
func copying(t *testing.T, declared string, over *model.Struct) generate.Request {
	t.Helper()

	return generate.Request{
		Model: &model.Model{
			Name: declared, Form: model.FormSpec, Subject: over,
			Pkg: collidePackage(t),
			Pos: declaredAt,
			// The element layer innermost, which is where the rules put one: it
			// attaches to the subject, so nothing may stand between them.
			Stack: []model.LayerRef{
				{Origin: model.TypeRef{Pkg: model.MarkerPkg, Name: "Slice"}, Kind: model.KindStorage},
				{Origin: model.TypeRef{Pkg: model.MarkerPkg, Name: "Clone"}, Kind: model.KindElement},
			},
		},
	}
}

// What a previous run wrote is not what the author declared, so a package is
// not reported as colliding with its own output.
//
// It is the difference between a check that works once and one that works. A
// generated file is loaded with the package it belongs to, so without this
// every run after the first would report the whole of its last output as a
// collision with itself.
func TestAPackageDoesNotCollideWithItsOwnOutput(t *testing.T) {
	// The fixture holds no generated file, so what a previous run wrote is
	// said rather than found: what is being tested is what the check does with
	// the answer, and a fixture carrying a generated copy would be a second
	// place for the two to disagree about which file that is.
	cfg := collideConfig(t)
	cfg.Generated = func(token.Pos) bool { return true }

	_, diags := generate.Package(collidePkg, "model",
		[]generate.Request{colliding(t, "Taken")}, cfg)

	if !diags.Empty() {
		t.Errorf("a package was reported as colliding with what a previous run wrote:\n%s", diags.Render())
	}
}

// reported returns the one diagnostic carrying a code, and fails the test where
// there is not exactly one.
func reported(t *testing.T, diags diag.Set, code string) diag.Diagnostic {
	t.Helper()

	found := reportedAll(t, diags, code)
	if len(found) != 1 {
		t.Fatalf("%d diagnostics carry %s, want one:\n%s", len(found), code, diags.Render())
	}
	return found[0]
}

// reportedAll returns every diagnostic carrying a code, for a test about how
// many times something is said rather than about what it says.
func reportedAll(t *testing.T, diags diag.Set, code string) []diag.Diagnostic {
	t.Helper()

	var found []diag.Diagnostic
	for _, one := range diags.All() {
		if one.Code.String() == code {
			found = append(found, one)
		}
	}
	return found
}

// colliding builds one inline declaration over the fixture's subject, named
// after a type the fixture already declares.
func colliding(t *testing.T, declared string) generate.Request {
	t.Helper()

	return generate.Request{
		Model: &model.Model{
			Name: declared, Form: model.FormInline, Subject: collideSubject(t),
			Pkg: collidePackage(t),
			Pos: declaredAt,
			// One layer, because an inline declaration is of one: the storage
			// beneath it is filled in while composing.
			Stack: []model.LayerRef{
				{Origin: model.TypeRef{Pkg: model.MarkerPkg, Name: "Collection"}, Kind: model.KindRefining},
			},
		},
	}
}

// collideConfig is what the fixture generates with: the ordinary configuration,
// with nothing yet generated.
func collideConfig(t *testing.T) generate.Config {
	t.Helper()

	cfg := config()
	cfg.Generated = loadCollide(t).Generated()

	return cfg
}

// collideFixture holds the load, which is read and never written.
var collideFixture *load.Session

// loadCollide loads the fixture module once for the whole file.
func loadCollide(t *testing.T) *load.Session {
	t.Helper()

	if collideFixture != nil {
		return collideFixture
	}

	dir, err := filepath.Abs(filepath.Join("testdata", "collide"))
	if err != nil {
		t.Fatalf("resolving the fixture: %v", err)
	}

	loaded, err := load.Load(load.Config{Dir: dir})
	if err != nil {
		t.Fatalf("loading the fixture: %v", err)
	}
	if !loaded.Diagnostics.Empty() {
		t.Fatalf("the fixture does not load clean:\n%s", loaded.Diagnostics.Render())
	}

	collideFixture = loaded
	return loaded
}

// collidePackage returns the fixture package the declarations are generated
// into.
func collidePackage(t *testing.T) *packages.Package {
	t.Helper()

	pkg, ok := loadCollide(t).Package(collidePkg)
	if !ok {
		t.Fatalf("the fixture has no package %s", collidePkg)
	}
	return pkg
}

// collideSubject models the fixture's subject.
func collideSubject(t *testing.T) *model.Struct {
	t.Helper()
	return subjectFrom(t, collidePkg)
}

// subjectFrom models the Person a given fixture package declares, so that a
// declaration can be generated over a subject that is not the local package's.
func subjectFrom(t *testing.T, path string) *model.Struct {
	t.Helper()
	return subjectNamed(t, path, "Person")
}

// subjectNamed models one named type of one fixture package.
func subjectNamed(t *testing.T, path, name string) *model.Struct {
	t.Helper()

	loaded := loadCollide(t)

	pkg, ok := loaded.Package(path)
	if !ok {
		t.Fatalf("the fixture has no package %s", path)
	}

	obj := pkg.Types.Scope().Lookup(name)
	if obj == nil {
		t.Fatalf("%s declares no %s", path, name)
	}

	held, is := types.Unalias(obj.Type()).(*types.Named)
	if !is {
		t.Fatalf("%s is a %T, want a named type", name, obj.Type())
	}

	built, problems := subjects.New(subjects.Config{
		Fset:      loaded.Fset,
		Owned:     loaded.Owned(),
		Docs:      loaded.FieldDocs(),
		Generated: loaded.Generated(),
	}).Build(held, subjects.At(token.Position{Filename: "model.go"}))
	if !problems.Empty() {
		t.Fatalf("modelling %s: %s", name, problems.Render())
	}

	return built
}

// A claim about an author's own method is spelled the way the package spells
// it.
//
// The walk is where this shows, since its signature names the element type. A
// method forge wrote is spelled by forge and cannot be got wrong; one the
// author wrote is read back out of the type checker, which knows types by the
// package they came from rather than by the file the claim is written in.
func TestAClaimAboutTheAuthorsOwnMethod(t *testing.T) {
	files, diags := generate.Package(collidePkg, "model",
		[]generate.Request{colliding(t, "Walked")}, collideConfig(t))

	if !diags.Empty() {
		t.Fatalf("an author's own walk was reported:\n%s", diags.Render())
	}

	held := string(written(t, files, generate.Named("Walked")))
	if !strings.Contains(held, "var _ func(*Walked) iter.Seq[Person] = (*Walked).All") {
		t.Errorf("the author's walk is not claimed as the package spells it:\n%s", claimed(held))
	}
}

// A claim about an author's walk over an element from another package is
// written once, in one language.
//
// Two renderers meet here and only this case tells them apart. What the author
// declared is read out of the type checker, which knows a type by the package
// it came from; the element a claim is written with is spelled for the file the
// claim goes in. Where the element is local both arrive at Person and agree by
// accident. Where it is not, one of them can say domain.Person and the other
// something else, and the disagreement would be reported as though the walk
// were over the wrong type.
func TestAClaimAboutAWalkOverAnotherPackagesElement(t *testing.T) {
	asked := colliding(t, "Elsewhere")
	asked.Model.Subject = subjectFrom(t, "collidefixture/domain")

	files, diags := generate.Package(collidePkg, "model",
		[]generate.Request{asked}, collideConfig(t))

	if !diags.Empty() {
		t.Fatalf("a walk over another package's element was reported:\n%s", diags.Render())
	}

	held := string(written(t, files, generate.Named("Elsewhere")))
	if !strings.Contains(held, "var _ func(*Elsewhere) iter.Seq[domain.Person] = (*Elsewhere).All") {
		t.Errorf("the walk is not claimed as the file writes its element:\n%s", claimed(held))
	}
}

// And where the element had to be renamed, the claim and the method are still
// read as being about one type.
//
// The collection imports the standard library's slices, so a subject from a
// package of that name cannot keep it and the spelling binds it under another.
// That binding is carried with the spelling rather than with the declarations,
// so a comparison that read only the declarations would write the element one
// way, read the author's method the other way, and refuse a build that is
// perfectly correct.
func TestAClaimAboutAWalkOverARenamedElement(t *testing.T) {
	asked := colliding(t, "Renamed")
	asked.Model.Subject = subjectFrom(t, "collidefixture/slices")

	files, diags := generate.Package(collidePkg, "model",
		[]generate.Request{asked}, collideConfig(t))

	if !diags.Empty() {
		t.Fatalf("a walk over a renamed element was reported:\n%s", diags.Render())
	}

	held := string(written(t, files, generate.Named("Renamed")))
	if !strings.Contains(held, "var _ func(*Renamed) iter.Seq[slices2.Person] = (*Renamed).All") {
		t.Errorf("the walk is not claimed under the name the element was bound to:\n%s", claimed(held))
	}
}

// A walk that answers with something other than the declaration's elements is
// reported rather than claimed.
//
// It is the one contract break the surface check cannot reach: a surface spells
// the walk's result as iter.Seq[Person], which is written for a person to read
// rather than to be lined up against the type checker, so FRG4011 falls back to
// arity and this passes it. Synthesis holds both spellings at once and is the
// only stage that can tell — and writing the claim anyway would hand the author
// a package that does not build, pointing at a file they may not edit.
func TestAWalkOverSomethingElse(t *testing.T) {
	_, diags := generate.Package(collidePkg, "model",
		[]generate.Request{colliding(t, "Wandering")}, collideConfig(t))

	if diags.Empty() {
		t.Fatal("a walk over the wrong thing was claimed without a word")
	}

	found := reported(t, diags, "FRG4017")
	for _, want := range []string{"Wandering", "iter.Seq[string]", "Person"} {
		if !strings.Contains(found.Message, want) {
			t.Errorf("the complaint does not mention %q:\n%s", want, found.Message)
		}
	}
	if found.Hint == "" {
		t.Error("the complaint says nothing to do about it")
	}
}

// And skipping that walk is not also answered with "there is no walk".
//
// The run is refused either way, so this is about what the author reads while
// they fix it. Two complaints about one line, the second saying the opposite of
// the first, is worse than one.
func TestSkippingAWalkOverSomethingElse(t *testing.T) {
	asked := colliding(t, "Wandering")
	asked.Directives = []discover.Directive{{
		Layer: "skip", Args: "All", Text: "//forge:skip All",
		ArgsOffset: len("//forge:skip "),
		Pos:        token.Position{Filename: "model.go", Line: 1, Column: 1},
	}}

	_, diags := generate.Package(collidePkg, "model",
		[]generate.Request{asked}, collideConfig(t))

	if got := reportedAll(t, diags, "FRG3019"); len(got) != 0 {
		t.Errorf("skipping a walk that was reported is also called unclaimed:\n%s", diags.Render())
	}
	if got := reportedAll(t, diags, "FRG4017"); len(got) != 1 {
		t.Errorf("the walk itself was reported %d times, want once:\n%s", len(got), diags.Render())
	}
}

// claimed returns the part of a generated file that makes its claims, so that a
// failure shows them rather than the whole file.
func claimed(held string) string {
	at := strings.Index(held, "var _ func(")
	if at < 0 {
		return held
	}
	return held[at:]
}
