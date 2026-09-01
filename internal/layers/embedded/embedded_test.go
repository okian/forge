package embedded_test

import (
	"go/ast"
	"go/token"
	"strings"
	"testing"

	"github.com/okian/forge/internal/layers/embedded"
	"github.com/okian/forge/internal/model"
)

// A file's declarations come across, and its package clause and imports do not.
//
// Both halves matter. The declarations are what the other package needs; the
// package clause names this repository's package and the imports are re-derived
// from what the declarations turn out to name, so carrying either would put a
// name that means something here into a file that is not ours.
func TestWhatComesAcrossAndWhatDoesNot(t *testing.T) {
	unit, err := embedded.Unit("shared.go", []byte(
		"// Package helper is not the package this lands in.\npackage helper\n\n"+
			"import \"math\"\n\n"+
			"// Half is what a caller is here for.\nfunc Half(f float64) float64 { return math.Abs(f) / 2 }\n"),
		[]model.Import{{Path: "math", Name: "math"}})
	if err != nil {
		t.Fatalf("carrying a file across: %v", err)
	}

	if len(unit.Decls) != 1 {
		t.Fatalf("carried %d declarations, want the one that is not an import", len(unit.Decls))
	}
	if _, is := unit.Decls[0].(*ast.FuncDecl); !is {
		t.Errorf("carried a %T, want the function", unit.Decls[0])
	}

	// The comment on it travels too, which is the whole reason the file set
	// comes with the declarations: a printer finds a comment by position.
	if len(unit.Comments) == 0 || unit.Fset == nil {
		t.Error("the declarations arrived without their comments or the positions that place them")
	}
}

// Only the imports the declarations actually name are bound.
//
// Gathered wide and narrowed, which is the bargain every generated file makes:
// one missing an import does not compile, and neither does one carrying an
// import it never names.
func TestOnlyWhatIsNamedIsBound(t *testing.T) {
	unit, err := embedded.Unit("shared.go",
		[]byte("package helper\n\nimport \"math\"\n\nfunc Twice(n int) int { return n * 2 }\n"),
		[]model.Import{{Path: "math", Name: "math"}, {Path: "strings", Name: "strings"}})
	if err != nil {
		t.Fatalf("carrying a file across: %v", err)
	}

	if len(unit.Imports) != 0 {
		t.Errorf("bound %v, and the declarations name none of them", unit.Imports)
	}
}

// An import the file gained and the list did not is refused here, where it is a
// failing test in this repository — rather than emitted into somebody's package
// as a file naming something it never imported.
func TestAnImportNothingRecordedANameFor(t *testing.T) {
	_, err := embedded.Unit("shared.go",
		[]byte("package helper\n\nimport \"strings\"\n\nfunc Up(s string) string { return strings.ToUpper(s) }\n"),
		[]model.Import{{Path: "math", Name: "math"}})

	if err == nil {
		t.Fatal("a file importing something unaccounted for was carried across")
	}
	for _, want := range []string{"shared.go", "strings", "bound name"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the failure does not mention %q: %v", want, err)
		}
	}
}

// A file that does not parse is a failure named after the file, raised where
// the layer can still be stopped.
func TestAFileThatDoesNotParse(t *testing.T) {
	_, err := embedded.Unit("shared.go", []byte("package helper\n\nfunc Broken( {\n"), nil)

	if err == nil {
		t.Fatal("a file that is not Go was carried across")
	}
	if !strings.Contains(err.Error(), "shared.go does not parse") {
		t.Errorf("the failure does not say what went wrong: %v", err)
	}
}

// A file with nothing in it but its package clause and imports is a helper that
// would contribute nothing, which is a mistake rather than an empty answer.
func TestAFileThatDeclaresNothing(t *testing.T) {
	_, err := embedded.Unit("shared.go", []byte("package helper\n\nimport \"math\"\n"),
		[]model.Import{{Path: "math", Name: "math"}})

	if err == nil {
		t.Fatal("a file declaring nothing was carried across")
	}
	if !strings.Contains(err.Error(), "declares nothing") {
		t.Errorf("the failure does not say what went wrong: %v", err)
	}
}

// The name a package is bound to travels with it, because a file names a
// package by what it bound rather than by the last element of its path.
func TestAPackageKeepsTheNameItWasBoundTo(t *testing.T) {
	unit, err := embedded.Unit("shared.go",
		[]byte("package helper\n\nimport js \"encoding/json/v2\"\n\n"+
			"func Bytes(v any) ([]byte, error) { return js.Marshal(v) }\n"),
		[]model.Import{{Path: "encoding/json/v2", Name: "js", Aliased: true}})
	if err != nil {
		t.Fatalf("carrying a file across: %v", err)
	}

	if len(unit.Imports) != 1 {
		t.Fatalf("bound %v, want the one the declarations name", unit.Imports)
	}
	if got := unit.Imports[0]; got.Name != "js" || !got.Aliased {
		t.Errorf("the package is bound as %+v, want the name the file wrote", got)
	}
}

// The positions the comments carry resolve against the file set that came with
// them, which is what places a comment when the declarations are printed.
func TestThePositionsResolveAgainstWhatCameWithThem(t *testing.T) {
	unit, err := embedded.Unit("shared.go",
		[]byte("package helper\n\n// Twice doubles.\nfunc Twice(n int) int { return n * 2 }\n"), nil)
	if err != nil {
		t.Fatalf("carrying a file across: %v", err)
	}

	at := unit.Fset.Position(unit.Comments[0].Pos())
	if at.Filename != "shared.go" || at.Line != 3 {
		t.Errorf("the comment is at %s, want shared.go:3", at)
	}
	if nowhere := unit.Fset.Position(token.NoPos); nowhere.IsValid() {
		t.Error("a position that is not one resolved to somewhere")
	}
}
