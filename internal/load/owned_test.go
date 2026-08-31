package load_test

import (
	"testing"

	"github.com/okian/forge/internal/load"
)

// Which packages the module owns is not which packages share its path.
//
// The two look like one question and come apart wherever a module has another
// nested inside it: the nested module's packages sit under the outer one's
// import path and belong to somebody else. It matters because the answer
// decides whether forge may attach a method to a type, and attaching one to a
// type in a module forge does not own is a compile error in generated code —
// in a file the author did not write, about a type they did nothing wrong with.
func TestWhichPackagesTheModuleOwns(t *testing.T) {
	held := loadFixture(t, "nested", "./...").Owned()

	for _, want := range []string{"nestedfixture/model", "nestedfixture/domain"} {
		if !held[want] {
			t.Errorf("%s belongs to the module and is not among %v", want, held)
		}
	}

	// Under the same path, and somebody else's.
	if held["nestedfixture/inner"] {
		t.Errorf("a nested module's package is counted as this module's: %v", held)
	}
}

// A package reached only because something imports it is answered for too.
//
// The half a walk of the roots alone would miss. A subject may be declared in a
// package no pattern named — the declaration's own package merely imports it —
// and whether *that* package is the module's is exactly the question being
// asked about it.
func TestAPackageReachedOnlyByImport(t *testing.T) {
	// One pattern, naming one package, which imports the other two.
	held := loadFixture(t, "nested", "./model").Owned()

	if !held["nestedfixture/domain"] {
		t.Errorf("a package reached only by import is not among what the module owns: %v", held)
	}
	if held["nestedfixture/inner"] {
		t.Errorf("a nested module's package reached by import is counted as this module's: %v", held)
	}
}

// A session that is not there owns nothing, and says so rather than failing.
func TestWhatNoSessionOwns(t *testing.T) {
	var absent *load.Session

	if held := absent.Owned(); len(held) != 0 {
		t.Errorf("a session that is not there owns %v", held)
	}
}
