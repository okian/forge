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

	held := string(written(t, files, generate.Name()))
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

// What the file a package shares declares is checked against what the author
// declared, the same as every other file.
//
// A subject's companion type — a builder, a patch — belongs to the subject
// rather than to the declaration that asked for it, so it lands there and
// nowhere else. Without this, a name of the author's it collided with would be
// found by the compiler, in the one file nobody can fix it in.
func TestTheSharedFileIsCheckedAgainstThePackageToo(t *testing.T) {
	asked := copying(t, "Maskeds", subjectNamed(t, collidePkg, "Masked"))
	asked.Model.Stack = append(asked.Model.Stack,
		model.LayerRef{Origin: model.TypeRef{Pkg: model.MarkerPkg, Name: "Patch"}, Kind: model.KindElement})

	_, diags := generate.Package(collidePkg, "model", []generate.Request{asked}, collideConfig(t))

	if diags.Empty() {
		t.Fatal("a companion type was written over a name the package already had")
	}

	found := reported(t, diags, "FRG4013")
	if !strings.Contains(found.Message, "MaskedPatch") {
		t.Errorf("the complaint does not name the collision:\n%s", found.Message)
	}

	// And says which file it was writing, since no one declaration owns that
	// one: a report naming a declaration would be naming one of several
	// arbitrarily, and one naming none reads as a sentence with a word missing.
	if !strings.Contains(found.Message, "the file this package shares writes") {
		t.Errorf("the complaint does not say what was being written:\n%s", found.Message)
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

// A subject from a package only half the stack has heard of is written one way
// by all of it.
//
// Every layer spells the subject against what the file will bind, and before
// this the answer each of them worked that out from was its own imports. The
// collection binds the standard library's cmp and the storage beneath it does
// not, so a subject from a package called cmp was cmp2 in the methods one of
// them wrote and cmp in the other's — one path bound twice, one name bound to
// two packages, and a file refused whole over a fault in neither layer.
//
// A package called slices would not have caught it. Both layers bind that one,
// so both moved the subject out of the way of it and both moved it to the same
// place; what this needs is a name exactly one layer has heard of.
func TestASubjectFromAPackageOnlyOneLayerBinds(t *testing.T) {
	asked := colliding(t, "People")
	asked.Model.Subject = subjectFrom(t, "collidefixture/cmp")

	files, diags := generate.Package(collidePkg, "model", []generate.Request{asked}, collideConfig(t))

	if !diags.Empty() {
		t.Fatalf("a subject from a package one layer imports was refused:\n%s", diags.Render())
	}

	// And the two layers agree about what to call it, which is the thing the
	// refusal was a symptom of: the file is checked for the bare name as well,
	// since a spelling that aliased in one method and not in the other is what
	// the diagnostic was reporting.
	for _, file := range files {
		if file.Name != generate.Name() {
			continue
		}

		src := string(file.Content)
		if !strings.Contains(src, "cmp2.Person") {
			t.Errorf("the subject is not written under the name it was bound to:\n%s", src)
		}
		if strings.Contains(src, "cmp.Person") {
			t.Errorf("the subject is written under the standard library's name too:\n%s", src)
		}
		return
	}

	t.Fatal("nothing was generated for the declaration")
}

// Two declarations that reserve different names still write one foreign
// package one way.
//
// A stack does not write into one file. Its own is one; the file a package's
// subjects share is another, and the file that stands in for what the build tag
// excludes is a third — and the last two are written from every declaration at
// once. So a set of reserved names that is consistent within a stack is not
// enough: a ring reserves errors and a collection over a slice reserves cmp and
// slices, and two declarations spelling one foreign package against their own
// halves meet in the shared files with two names for it.
//
// Which is why what a layer spells against is decided for the package rather
// than for the stack. The cost is a declaration moved out of the way of a name
// its own stack never binds; what it buys is the only property that makes the
// shared files writable at all.
//
// Over the package called cmp, which only the collection binds. A subject from
// one called slices would prove nothing here: every stack reserves that name,
// because the storage a refining layer is given when none is written binds it
// and every package gets one — so two declarations would agree about it however
// the answer was arrived at, and this would pass with the scope decided per
// declaration.
func TestTwoDeclarationsThatReserveDifferentNames(t *testing.T) {
	held := standing(t, "Recent", "Ring", subjectFrom(t, "collidefixture/cmp"))
	beside := standing(t, "Places", "Collection", subjectNamed(t, "collidefixture/cmp", "Place"))

	files, diags := generate.Package(collidePkg, "model",
		[]generate.Request{held, beside}, collideConfig(t))

	if !diags.Empty() {
		t.Fatalf("two declarations over one package were refused:\n%s", diags.Render())
	}

	// And the stand-in file — the one written from both declarations — binds
	// the foreign package once. Counted rather than matched: the refusal above
	// is what a disagreement produces today, and this is what would report one
	// that stopped being refused and started being written.
	const path = `"collidefixture/cmp"`

	for _, file := range files {
		if file.Name != generate.Stubs() {
			continue
		}
		if got := strings.Count(string(file.Content), path); got != 1 {
			t.Errorf("%s binds %s %d times, want once:\n%s", file.Name, path, got, file.Content)
		}
		return
	}

	t.Fatal("no stand-in file was written for two spec declarations")
}

// standing builds one spec declaration over a subject, which is the form a
// declaration needs to be stood in for at all.
func standing(t *testing.T, declared, marker string, over *model.Struct) generate.Request {
	t.Helper()

	kind := model.KindStorage
	if marker == "Collection" {
		kind = model.KindRefining
	}

	return generate.Request{
		Model: &model.Model{
			Name: declared, Form: model.FormSpec, Subject: over,
			Pkg: collidePackage(t),
			Pos: declaredAt,
			Stack: []model.LayerRef{
				{Origin: model.TypeRef{Pkg: model.MarkerPkg, Name: marker}, Kind: kind},
			},
		},
	}
}

// colliding builds one inline declaration over the fixture's subject, under
// whatever name the caller wants it declared as.
//
// Most callers pass a name the fixture already declares, which is what makes it
// collide and what the helper is named after. One passes a name nothing has, to
// ask what a declaration does when there is nothing in its way.
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

	held := string(written(t, files, generate.Name()))
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

	held := string(written(t, files, generate.Name()))
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

	held := string(written(t, files, generate.Name()))
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
