package load_test

import (
	"go/token"
	"testing"

	"golang.org/x/tools/go/packages"

	"github.com/okian/forge/internal/load"
)

// Whether forge may attach a method to a type is decided by which module the
// type belongs to, so the answer to "which module is this" is one every element
// layer depends on being right.
func TestTheModuleBeingGeneratedFor(t *testing.T) {
	session, err := load.Load(load.Config{Dir: fixture(t, "realistic"), Patterns: []string{"./..."}})
	if err != nil {
		t.Fatalf("loading: %v", err)
	}

	if want := "realisticfixture"; session.Module() != want {
		t.Errorf("the module is %q, want %q", session.Module(), want)
	}
}

// A package from a module that is not the one being generated for is not the
// answer. Whether forge may attach a method to a type is decided by this, so a
// dependency's module reported as the main one would have every local type
// judged foreign and every foreign one judged local.
func TestOnlyTheModuleBeingGeneratedFor(t *testing.T) {
	session := &load.Session{
		Fset: token.NewFileSet(),
		Packages: []*packages.Package{
			{PkgPath: "example.com/dependency", Module: &packages.Module{Path: "example.com/dependency"}},
			{PkgPath: "example.com/mine/model", Module: &packages.Module{Path: "example.com/mine", Main: true}},
		},
	}

	if want := "example.com/mine"; session.Module() != want {
		t.Errorf("the module is %q, want %q", session.Module(), want)
	}
}

// And a load holding nothing but dependencies has no module of its own to
// report, which is the honest answer rather than the first path in the list.
func TestAModuleThatIsNobodyMain(t *testing.T) {
	session := &load.Session{
		Fset:     token.NewFileSet(),
		Packages: []*packages.Package{{Module: &packages.Module{Path: "example.com/dependency"}}},
	}

	if session.Module() != "" {
		t.Errorf("a dependency was reported as the module being generated for: %q", session.Module())
	}
}

// A load that never happened has no module, and answering with a guess would be
// worse than answering with nothing: every type would be judged against it.
func TestTheModuleOfNoLoadAtAll(t *testing.T) {
	var none *load.Session
	if none.Module() != "" {
		t.Errorf("a load that never happened reports module %q", none.Module())
	}

	if empty := (&load.Session{}); empty.Module() != "" {
		t.Errorf("a load with no packages reports module %q", empty.Module())
	}
}
