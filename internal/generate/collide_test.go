package generate_test

import (
	"go/token"
	"go/types"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"

	"github.com/okian/forge/internal/diag"
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

	var found []diag.Diagnostic
	for _, one := range diags.All() {
		if one.Code.String() == code {
			found = append(found, one)
		}
	}

	if len(found) != 1 {
		t.Fatalf("%d diagnostics carry %s, want one:\n%s", len(found), code, diags.Render())
	}
	return found[0]
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

	loaded := loadCollide(t)
	obj := collidePackage(t).Types.Scope().Lookup("Person")
	if obj == nil {
		t.Fatalf("%s declares no Person", collidePkg)
	}

	held, is := types.Unalias(obj.Type()).(*types.Named)
	if !is {
		t.Fatalf("Person is a %T, want a named type", obj.Type())
	}

	built, problems := subjects.New(subjects.Config{
		Fset:      loaded.Fset,
		Owned:     loaded.Owned(),
		Docs:      loaded.FieldDocs(),
		Generated: loaded.Generated(),
	}).Build(held, subjects.At(token.Position{Filename: "model.go"}))
	if !problems.Empty() {
		t.Fatalf("modelling Person: %s", problems.Render())
	}

	return built
}
