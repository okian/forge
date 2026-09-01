package load_test

import (
	"go/token"
	"go/types"
	"testing"

	"github.com/okian/forge/internal/load"
)

// The load tells forge's own work apart from the author's.
//
// It has to, and nothing else can. Generated files are loaded with the package
// — they must be, or a call site naming a generated type would stop the load —
// so a stage asking go/types what a type declares is told about the last run as
// though somebody had typed it. A layer that declines to redeclare what the
// author already wrote would then decline to write what it wrote itself, on
// every run after the first.
func TestWhatAPreviousRunWroteIsNotTheAuthorsWork(t *testing.T) {
	session := loadFixture(t, "regenerated")

	if !session.Diagnostics.Empty() {
		t.Fatalf("the fixture does not load clean:\n%s", session.Diagnostics.Render())
	}

	generated := session.Generated()
	person := named(t, session, "regeneratedfixture/model", "Person")

	found := map[string]bool{}
	for method := range person.Methods() {
		found[method.Name()] = generated(method.Pos())
	}

	if len(found) != 2 {
		t.Fatalf("the fixture's subject has %d methods, want the author's and the generator's", len(found))
	}
	if found["Rename"] {
		t.Error("a method the author wrote is reported as generated")
	}
	if !found["Describe"] {
		t.Error("a method a previous run wrote is reported as the author's")
	}
}

// A load with nothing generated in it reports nothing generated, which is the
// answer for the run before the first one.
func TestNothingIsGeneratedBeforeAnythingHasBeen(t *testing.T) {
	session := loadFixture(t, "clean")

	generated := session.Generated()
	person := named(t, session, "cleanfixture/model", "Person")

	if generated(person.Obj().Pos()) {
		t.Error("a hand-written subject is reported as generated")
	}
}

// A position from nowhere belongs to no file, so nothing generated it.
//
// It is what a caller holding a type built rather than parsed has — the
// stages that construct a model in a test, and any layer asking about a
// predeclared type — and answering it with a lookup against an empty filename
// would depend on whatever the map happened to hold.
func TestAPositionFromNowhereIsNobodysWork(t *testing.T) {
	session := loadFixture(t, "regenerated")

	if session.Generated()(token.NoPos) {
		t.Error("a position from nowhere is reported as generated")
	}
}

// named returns the named type a fixture package declares under this name.
func named(t *testing.T, session *load.Session, path, name string) *types.Named {
	t.Helper()

	pkg, ok := session.Package(path)
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
	return held
}
