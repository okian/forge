package main

import (
	"bytes"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/okian/forge/internal/diag"
	"github.com/okian/forge/internal/emit"
	generated "github.com/okian/forge/internal/generate"
)

// A file already holding exactly what would be written is left alone.
//
// Rewriting it with the same bytes would change its modification time, which
// every build cache and every watcher reads as a change — so a run that
// generated nothing new would rebuild the world, and the run after it would do
// it again.
func TestWhatWritingAFileDoes(t *testing.T) {
	dir := t.TempDir()

	// Carrying the line that says forge wrote it, because that is what forge
	// writes and what deciding to write over one asks about.
	file := generated.File{
		Name:    "zz_forge_persons.go",
		Content: []byte(emit.Generated + "\n\npackage model\n"),
	}

	// Not there: written, and reported as new.
	if did, err := place(dir, file); err != nil || did != created {
		t.Fatalf("writing a file that was not there gave %v, %v", did, err)
	}

	// There and the same: left alone.
	before := modified(t, filepath.Join(dir, file.Name))
	if did, err := place(dir, file); err != nil || did != unchanged {
		t.Fatalf("writing a file that was already right gave %v, %v", did, err)
	}
	if after := modified(t, filepath.Join(dir, file.Name)); !after.Equal(before) {
		t.Error("a file holding what would be written was rewritten anyway")
	}

	// There and different: written, and reported as changed.
	file.Content = []byte(emit.Generated + "\n\npackage model\n\ntype Persons []Person\n")
	if did, err := place(dir, file); err != nil || did != updated {
		t.Fatalf("writing a file that had changed gave %v, %v", did, err)
	}

	held, err := os.ReadFile(filepath.Join(dir, file.Name))
	if err != nil {
		t.Fatalf("reading it back: %v", err)
	}
	if !bytes.Equal(held, file.Content) {
		t.Errorf("it holds %q", held)
	}
}

// modified is when a file was last written.
func modified(t *testing.T, path string) time.Time {
	t.Helper()

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	return info.ModTime()
}

// What would change is reported without anything being written, which is what
// both the flags that hold a run back are for.
func TestWhatWouldChange(t *testing.T) {
	dir := t.TempDir()
	file := generated.File{Name: "zz_forge_persons.go", Content: []byte("package model\n")}

	// A file that is not there is arriving whole.
	text, err := difference(dir, file)
	if err != nil {
		t.Fatalf("difference: %v", err)
	}
	if !strings.Contains(text, "+package model") {
		t.Errorf("a file arriving reads\n%s", text)
	}
	if _, err := os.Stat(filepath.Join(dir, file.Name)); err == nil {
		t.Error("reporting what would change wrote it")
	}

	// One that is there and holds it differs in nothing.
	if _, err := place(dir, file); err != nil {
		t.Fatalf("writing: %v", err)
	}
	if text, err = difference(dir, file); err != nil || text != "" {
		t.Errorf("a file already right differs by %q (%v)", text, err)
	}
}

// A run that could not read what is there says so rather than overwriting it,
// since a file forge cannot read is one it cannot know it owns.
func TestAFileThatCannotBeRead(t *testing.T) {
	dir := t.TempDir()

	// A directory where a file should be: readable as an entry and not as a
	// file, which is the shape of every unreadable path a test can make
	// without depending on being able to change permissions.
	if err := os.Mkdir(filepath.Join(dir, "zz_forge_persons.go"), 0o755); err != nil {
		t.Fatalf("making the fixture: %v", err)
	}

	file := generated.File{Name: "zz_forge_persons.go", Content: []byte("package model\n")}

	if _, err := place(dir, file); err == nil {
		t.Error("a path that is not a file was written to without complaint")
	}
	if _, err := difference(dir, file); err == nil {
		t.Error("a path that is not a file was compared without complaint")
	}
}

// What a run did is counted in the words that fit what it was allowed to do.
func TestWhatARunSaysItDid(t *testing.T) {
	cases := map[string]struct {
		changed int
		hold    bool
		want    string
	}{
		"one written":     {1, false, "wrote 1 file"},
		"several written": {3, false, "wrote 3 files"},
		"none written":    {0, false, "wrote 0 files"},
		"one held back":   {1, true, "would write 1 file"},
		"several held":    {2, true, "would write 2 files"},
	}

	for name, tc := range cases {
		if got := counted(tc.changed, tc.hold); got != tc.want {
			t.Errorf("%s reads %q, want %q", name, got, tc.want)
		}
	}
}

// A file that does not say forge wrote it is not written over.
//
// The names forge chooses are unusual and prefixed to be, but a declaration can
// land on one somebody already used — and a generator that silently replaces
// somebody's file has done the one thing nothing downstream recovers from.
func TestAFileThatIsNotForgesIsNotWrittenOver(t *testing.T) {
	dir := t.TempDir()
	name := "zz_forge_persons.go"
	path := filepath.Join(dir, name)

	mine := []byte("package model\n\n// Mine.\nconst Mine = 1\n")
	if err := os.WriteFile(path, mine, 0o600); err != nil {
		t.Fatalf("making the fixture: %v", err)
	}

	file := generated.File{
		Name:    name,
		Content: []byte(emit.Generated + "\n\npackage model\n"),
		Pos:     token.Position{Filename: "model/person.go", Line: 8, Column: 6},
	}

	did, err := place(dir, file)
	if err == nil {
		t.Fatal("somebody's own file was written over")
	}
	if did != unchanged {
		t.Errorf("the write reported %v", did)
	}

	held, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatalf("reading it back: %v", readErr)
	}
	if !bytes.Equal(held, mine) {
		t.Errorf("the file holds %q", held)
	}

	// Reported as a diagnostic rather than as a failure to write, so that it
	// points at the declaration and carries the two ways out.
	said, ok := diag.From(err)
	if !ok {
		t.Fatalf("the refusal is not a diagnostic: %v", err)
	}
	if got := said.Render(); !strings.Contains(got, "FRG5006") ||
		!strings.Contains(got, "rename the declaration") {
		t.Errorf("the refusal reads:\n%s", got)
	}
}

// An empty file is written over, because it holds nothing to lose.
//
// It is the shape a generated file takes when a merge or a tool truncates it,
// and refusing would leave somebody with a file forge cannot repair and no way
// to ask it to.
func TestAnEmptyFileIsWrittenOver(t *testing.T) {
	dir := t.TempDir()
	name := "zz_forge_persons.go"

	if err := os.WriteFile(filepath.Join(dir, name), []byte("\n\n"), 0o600); err != nil {
		t.Fatalf("making the fixture: %v", err)
	}

	file := generated.File{Name: name, Content: []byte(emit.Generated + "\n\npackage model\n")}
	if did, err := place(dir, file); err != nil || did != updated {
		t.Fatalf("writing over an empty file gave %v, %v", did, err)
	}
}
