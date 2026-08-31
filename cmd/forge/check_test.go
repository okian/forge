package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/okian/forge/internal/discover"
	"github.com/okian/forge/internal/emit"
	generated "github.com/okian/forge/internal/generate"
)

// checking drives the check verb over a run, and returns what it said.
func checking(t *testing.T, s *stack, args ...string) (string, string, error) {
	t.Helper()

	out, errs := &bytes.Buffer{}, &bytes.Buffer{}
	env := &environment{stdout: out, stderr: errs, pipeline: over(s), verbose: true}

	cmd, ok := lookup("check")
	if !ok {
		t.Fatal("check is not a command")
	}

	err := cmd.run(env, cmd, args)
	return out.String(), errs.String(), err
}

// checkable generates into a directory, so that what is checked afterwards is
// what a run of forge actually leaves behind.
//
// Written rather than hand-assembled, because the whole of what this verb does
// is compare a file against what generation would put in it. A fixture whose
// header somebody typed would be testing that two strings written by the same
// hand agree.
func checkable(t *testing.T, dir string) {
	t.Helper()

	if _, said, err := running(t, generating(t, dir, "Persons"), "./..."); err != nil {
		t.Fatalf("generating the fixture: %v\n%s", err, said)
	}
}

// A tree in the state generation left it in is up to date, and the run says
// what it looked at rather than only that it is happy.
func TestATreeNothingHasTouched(t *testing.T) {
	dir := t.TempDir()
	checkable(t, dir)

	shown, said, err := checking(t, generating(t, dir, "Persons"), "./...")
	if err != nil {
		t.Fatalf("checking: %v\n%s", err, said)
	}

	if !strings.Contains(said, "1 declaration is up to date") {
		t.Errorf("the run said\n%s", said)
	}

	// Nothing on stdout, which is what makes this usable in a pipeline: the
	// answer is the exit status, and the words go where words go.
	if shown != "" {
		t.Errorf("checking wrote to stdout:\n%s", shown)
	}
}

// A declaration that changed after its file was written is reported, and the
// file is not rewritten.
//
// This is the whole verb. What decides it is the fingerprint the file records,
// so nothing here composes the stack or renders anything — and the proof that
// it did not is that the stale file is still on disk afterwards, unchanged.
func TestADeclarationThatChangedSinceItsFileWasWritten(t *testing.T) {
	dir := t.TempDir()
	checkable(t, dir)

	name := filepath.Join(dir, "zz_forge_persons.go")
	before, err := os.ReadFile(name)
	if err != nil {
		t.Fatalf("reading the generated file: %v", err)
	}

	// The same declaration with an option it did not have, which is one of the
	// things a fingerprint is a function of.
	changed := generating(t, dir, "Persons")
	changed.modelled[0].Declaration.Candidate.Directives = []discover.Directive{{
		Layer: "collection", Args: "sort=Name",
		Text: "//forge:collection sort=Name", ArgsOffset: 19,
	}}

	_, said, err := checking(t, changed, "./...")
	if err == nil {
		t.Fatal("a file that no longer matches its declaration was passed")
	}
	if !strings.Contains(said, "FRG5004") || !strings.Contains(said, "zz_forge_persons.go") {
		t.Errorf("the report does not name the file:\n%s", said)
	}

	after, err := os.ReadFile(name)
	if err != nil {
		t.Fatalf("reading the generated file: %v", err)
	}
	if !bytes.Equal(before, after) {
		t.Error("checking rewrote the file it was reporting")
	}
}

// A declaration with no file at all is reported as having none, rather than as
// having a stale one.
//
// The two want different words. A file that is out of date is one a regenerate
// will overwrite; a declaration with no file may be one somebody forgot to
// commit, and telling them their file is stale is telling them about a file
// they are looking at an empty space where.
func TestADeclarationWhoseFileIsNotThere(t *testing.T) {
	dir := t.TempDir()
	checkable(t, dir)

	if err := os.Remove(filepath.Join(dir, "zz_forge_persons.go")); err != nil {
		t.Fatalf("removing the generated file: %v", err)
	}

	_, said, err := checking(t, generating(t, dir, "Persons"), "./...")
	if err == nil {
		t.Fatal("a declaration with no generated file was passed")
	}
	if !strings.Contains(said, "FRG5005") || !strings.Contains(said, "zz_forge_persons.go") {
		t.Errorf("the report does not name what is missing:\n%s", said)
	}
	if strings.Contains(said, "FRG5004") {
		t.Errorf("a file that is not there was reported as out of date:\n%s", said)
	}
}

// A file forge will not write over is reported as that, and in the words the
// run that met it would use.
//
// It is not staleness: nothing is out of date, there is a file in the way. And
// forge cannot tell whose it is — somebody's own file under a name a
// declaration landed on, and a generated file whose header a merge or a
// licence tool removed, are the same bytes to anything reading them — so both
// ways out are offered rather than one guessed at.
func TestAFileForgeWillNotWriteOver(t *testing.T) {
	dir := t.TempDir()
	checkable(t, dir)

	name := filepath.Join(dir, "zz_forge_persons.go")
	if err := os.WriteFile(name, []byte("package model\n\n// mine\n"), 0o600); err != nil {
		t.Fatalf("writing over the generated file: %v", err)
	}

	_, said, err := checking(t, generating(t, dir, "Persons"), "./...")
	if err == nil {
		t.Fatal("somebody's own file under a generated name was passed")
	}

	for _, want := range []string{"FRG5006", "does not say forge wrote it", "rename the declaration", "delete it"} {
		if !strings.Contains(said, want) {
			t.Errorf("the report does not say %q:\n%s", want, said)
		}
	}

	// One line about it rather than two. A file in the way is not also stale,
	// and reporting both would leave an author with two things to weigh about
	// one file.
	if strings.Contains(said, "FRG5004") {
		t.Errorf("a file forge will not write over was also reported as stale:\n%s", said)
	}
}

// A declaration whose subject was refused has nothing to be out of date, and is
// not reported twice.
//
// The refusal is what the author has to fix, and a second line telling them a
// file is missing is a second thing to read about a problem they already have.
func TestADeclarationNothingCouldBeGeneratedFor(t *testing.T) {
	dir := t.TempDir()

	s := generating(t, dir, "Persons")
	s.modelled[0].Model = nil

	_, said, err := checking(t, s, "./...")
	if err != nil {
		t.Fatalf("checking: %v\n%s", err, said)
	}
	if strings.Contains(said, "FRG5005") {
		t.Errorf("a declaration nothing could be generated for was reported as missing a file:\n%s", said)
	}
}

// A file left behind by a rename is reported by the same run, because an author
// asking whether their tree is current wants both answers at once.
func TestAFileNoDeclarationAccountsFor(t *testing.T) {
	dir := t.TempDir()
	checkable(t, dir)

	left := filepath.Join(dir, "zz_forge_ancient.go")
	source := "// Code generated by forge. DO NOT EDIT.\n\npackage model\n"
	if err := os.WriteFile(left, []byte(source), 0o600); err != nil {
		t.Fatalf("making the fixture: %v", err)
	}

	run := generating(t, dir, "Persons")
	run.session.Packages[0].Errors = nil

	_, said, err := checking(t, broken(run), "./...")
	if err == nil {
		t.Fatal("a file nothing accounts for was passed")
	}
	if !strings.Contains(said, "FRG5003") || !strings.Contains(said, "zz_forge_ancient.go") {
		t.Errorf("the report does not name the leftover:\n%s", said)
	}
}

// How a file is out of date, for the headers a fingerprint cannot decide.
//
// A file can be forge's and record no fingerprint: the fields are optional, and
// an older tool wrote fewer of them. What is left to compare is the versions,
// which answer a narrower question — not "is this what these inputs produce"
// but "was this made by something else" — and answer nothing at all where the
// file records nothing to compare.
func TestHowAFileIsOutOfDate(t *testing.T) {
	cfg := generated.Config{Forge: "v2", Markers: "v2", Toolchain: "go1.27.0"}

	cases := map[string]struct {
		recorded emit.Header
		inputs   string
		says     string
	}{
		"the fingerprint agrees": {
			recorded: emit.Header{Forge: "v2", Markers: "v2", Inputs: "abc"},
			inputs:   "abc",
		},

		// And beats the versions, which is the point of having it: a file whose
		// inputs produce these bytes is current however it was made.
		"the fingerprint agrees and the versions do not": {
			recorded: emit.Header{Forge: "v1", Markers: "v1", Inputs: "abc"},
			inputs:   "abc",
		},

		"the fingerprint differs": {
			recorded: emit.Header{Forge: "v2", Markers: "v2", Inputs: "abc"},
			inputs:   "xyz",
			says:     "not what these inputs produce",
		},

		"no fingerprint, an older forge": {
			recorded: emit.Header{Forge: "v1", Markers: "v2"},
			says:     "from forge v1",
		},
		"no fingerprint, older markers": {
			recorded: emit.Header{Forge: "v2", Markers: "v1"},
			says:     "written against markers v1",
		},
		// Forge writes a fingerprint into every file it produces, so a file
		// that says forge wrote it and records none differs from what forge
		// writes — by that line at least. Passing it over would turn the check
		// off for that file for good, since nothing would ask again.
		"no fingerprint, the versions agree": {
			recorded: emit.Header{Forge: "v2", Markers: "v2"},
			says:     "missing the fingerprint",
		},
		"nothing recorded at all": {
			recorded: emit.Header{},
			says:     "missing the fingerprint",
		},
	}

	for name, want := range cases {
		t.Run(name, func(t *testing.T) {
			got := staleness(want.recorded, want.inputs, cfg)

			switch {
			case want.says == "" && got != "":
				t.Errorf("reported %q", got)
			case want.says != "" && !strings.Contains(got, want.says):
				t.Errorf("reported %q, want it to say %q", got, want.says)
			}
		})
	}
}
