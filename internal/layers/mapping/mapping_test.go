package mapping

import (
	_ "embed"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/okian/forge/internal/emit"
	"github.com/okian/forge/plugin"
)

// reference is the test that runs inside the generated module, comparing every
// constructor against the mapping a person would have written.
//
// Embedded from a file rather than quoted, so that it is Go this repository's
// own tools read: a comparison held only as a string is one nothing checks
// until the day it fails to compile inside a temporary directory.
//
//go:embed testdata/reference.go.txt
var reference []byte

// gate lists the pairs the compiled module exercises: every rung of the
// ladder, both source kinds, both hints, the ignore, and the local unexported
// member.
var gate = []pair{
	{pkg: modelPkg, source: "User", target: "Person"},
	{pkg: modelPkg, source: "Reader", target: "Card"},
	{pkg: modelPkg, source: "User", target: "Renamed", hint: "renamedFromUser"},
	{pkg: modelPkg, source: "User", target: "Converted", hint: "convertedFromUser"},
	{pkg: modelPkg, source: "Entitled", target: "Titled"},
	{pkg: modelPkg, source: "Terse", target: "Sparse", ignore: "Note"},
}

// What the generated constructors are checked against is the mapping a person
// would have written, compiled and run.
//
// Not a golden file. A golden file says the output has not changed, which is a
// different claim from the output being right — and what makes a constructor
// right is that it moves the values the author meant to move.
func TestTheConstructorsAgreeWithTheHandWrittenOnes(t *testing.T) {
	if testing.Short() {
		t.Skip("compiling and running a generated module is not a short test")
	}

	dir := t.TempDir()
	built := generated(t)

	// The fixture's own source, beside the constructors written for it.
	copied(t, filepath.Join("testdata", "mapping"), dir)
	write(t, filepath.Join(dir, "model", "zz_map.go"), built)
	write(t, filepath.Join(dir, "model", "reference_test.go"), reference)

	cmd := exec.CommandContext(t.Context(), "go", "test", "./...")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOFLAGS=-mod=mod")

	if out, err := cmd.CombinedOutput(); err != nil {
		t.Errorf("the generated constructors do not agree with the hand-written ones:\n%s", out)
	}
}

// generated returns the file forge writes for every pair in the gate.
func generated(t *testing.T) []byte {
	t.Helper()

	loaded := loadFixture(t)
	file := emit.File{Package: "model"}

	for _, p := range gate {
		unit, err := New().Generate(contextFor(t, loaded, p), plugin.Shape{})
		if err != nil {
			t.Fatalf("generating the constructor for %s: %v", p.target, err)
		}

		file.Sections = append(file.Sections, emit.Section{
			Decls: unit.Decls, Comments: unit.Comments, Fset: unit.Fset,
		})
		file.Imports = append(file.Imports, unit.Imports...)
	}

	out, err := file.Render()
	if err != nil {
		t.Fatalf("rendering the constructors: %v", err)
	}
	return out
}

// copied writes a directory's Go files and go.mod into another directory.
func copied(t *testing.T, from, to string) {
	t.Helper()

	err := filepath.WalkDir(from, func(path string, entry os.DirEntry, err error) error {
		switch {
		case err != nil:
			return err
		case entry.IsDir():
			return os.MkdirAll(filepath.Join(to, rel(t, from, path)), 0o755)
		case !strings.HasSuffix(path, ".go") && filepath.Base(path) != "go.mod":
			return nil
		}

		held, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(to, rel(t, from, path)), held, 0o644)
	})
	if err != nil {
		t.Fatalf("copying the fixture: %v", err)
	}
}

// rel is the fixture-relative path of one of its files.
func rel(t *testing.T, from, path string) string {
	t.Helper()

	out, err := filepath.Rel(from, path)
	if err != nil {
		t.Fatalf("relativising %s: %v", path, err)
	}
	return out
}

// write puts a file where the generated module expects it.
func write(t *testing.T, path string, content []byte) {
	t.Helper()

	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
}
