package cli

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"

	"github.com/okian/forge/internal/load"
	"github.com/okian/forge/internal/model"
)

// diagnosing drives the doctor verb over a run and returns what it wrote.
func diagnosing(t *testing.T, dir string, s *stack, args ...string) (string, error) {
	t.Helper()

	out, errs := &bytes.Buffer{}, &bytes.Buffer{}
	env := &environment{stdout: out, stderr: errs, dir: dir, pipeline: over(s)}

	cmd, ok := lookup("doctor")
	if !ok {
		t.Fatal("doctor is not a command")
	}

	err := cmd.run(env, cmd, args)
	return out.String(), err
}

// A tree in the state generation left it in is reported healthy, and every
// check says what it looked at rather than only the ones that failed.
//
// Reporting what passed is the point of the verb. Somebody running it is asking
// whether their setup is right, and a report listing only problems leaves them
// unable to tell a healthy setup from one this did not examine.
func TestWhatADiagnosisReportsAboutAHealthyTree(t *testing.T) {
	dir := t.TempDir()
	checkable(t, dir)

	shown, err := diagnosing(t, dir, generating(t, dir, "Persons"))
	if err != nil {
		t.Fatalf("diagnosing: %v\n%s", err, shown)
	}

	for _, want := range []string{"toolchain", "markers", "1 declaration"} {
		if !strings.Contains(shown, want) {
			t.Errorf("the report says nothing about %q:\n%s", want, shown)
		}
	}
	if strings.Contains(shown, "wrong") {
		t.Errorf("a healthy tree was reported as broken:\n%s", shown)
	}

	// And nothing about an editor, because nothing here is written under the
	// tag: advice about a problem a tree does not have is advice somebody has
	// to read and discard.
	if strings.Contains(shown, "editor") {
		t.Errorf("a tree with no spec declaration was told about the build tag:\n%s", shown)
	}
}

// A package whose generated files are missing or out of date is reported, and
// the run ends badly.
//
// The two are told apart because what to do about them is the same and what
// happened is not: a file that was never written is very often one somebody
// forgot to commit, and a file that is out of date is one the last run of
// generate would have fixed.
func TestWhatADiagnosisReportsAboutAStaleTree(t *testing.T) {
	cases := map[string]struct {
		spoil func(t *testing.T, dir string)
		says  string
	}{
		"nothing generated": {
			spoil: func(t *testing.T, dir string) {
				t.Helper()
				if err := os.Remove(filepath.Join(dir, "zz_forge_persons.go")); err != nil {
					t.Fatalf("removing the generated file: %v", err)
				}
			},
			says: "FRG5005",
		},
		"out of date": {
			spoil: func(t *testing.T, dir string) {
				t.Helper()
				name := filepath.Join(dir, "zz_forge_persons.go")
				held, err := os.ReadFile(name)
				if err != nil {
					t.Fatalf("reading the generated file: %v", err)
				}
				// The fingerprint the file records, made not to match.
				spoiled := bytes.Replace(held, []byte("// inputs "), []byte("// inputs 0"), 1)
				if err := os.WriteFile(name, spoiled, 0o600); err != nil {
					t.Fatalf("writing it back: %v", err)
				}
			},
			says: "FRG5004",
		},
	}

	for name, want := range cases {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			checkable(t, dir)
			want.spoil(t, dir)

			shown, err := diagnosing(t, dir, generating(t, dir, "Persons"))

			if err == nil {
				t.Fatalf("a tree that needs generating was reported healthy:\n%s", shown)
			}
			if !strings.Contains(shown, want.says) {
				t.Errorf("the report does not say %q:\n%s", want.says, shown)
			}
			if !strings.Contains(shown, "run forge generate") {
				t.Errorf("the report does not say what to do:\n%s", shown)
			}
		})
	}
}

// The editor setting is reported, written when asked, and never written over.
//
// Never written over because it is not forge's file. Somebody's editor
// configuration holds whatever else they have set, and a tool that replaced it
// with the one line forge cares about would take away things it never looked
// at — so where there is anything there at all, this says what to add and stops.
func TestWhatADiagnosisDoesAboutTheEditor(t *testing.T) {
	t.Run("says what is missing", func(t *testing.T) {
		dir := t.TempDir()

		held := generating(t, dir, "Persons")
		held.modelled[0].Declaration.Candidate.Form = model.FormSpec

		shown, _ := diagnosing(t, dir, held)

		if !strings.Contains(shown, "nothing here analyses the tagged build") {
			t.Errorf("the report does not say the editor is unset:\n%s", shown)
		}
		if !strings.Contains(shown, "--write-editor-config") {
			t.Errorf("the report does not offer to set it:\n%s", shown)
		}
	})

	t.Run("writes it when asked", func(t *testing.T) {
		dir := t.TempDir()

		if shown, _ := diagnosing(t, dir, &stack{}, "--write-editor-config"); !strings.Contains(shown, "wrote") {
			t.Errorf("the report does not say it wrote anything:\n%s", shown)
		}

		held, err := os.ReadFile(filepath.Join(dir, ".vscode", "settings.json"))
		if err != nil {
			t.Fatalf("reading what was written: %v", err)
		}
		if !strings.Contains(string(held), "-tags=forgespec") {
			t.Errorf("what was written does not set the tag:\n%s", held)
		}

		// And a second run sees its own work rather than offering again.
		if shown, _ := diagnosing(t, dir, &stack{}, "--write-editor-config"); !strings.Contains(shown, "already analyses") {
			t.Errorf("a run after writing does not see it:\n%s", shown)
		}
	})

	t.Run("leaves somebody else's alone", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, ".vscode", "settings.json")

		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatalf("making the fixture: %v", err)
		}
		mine := []byte("{\n  \"editor.formatOnSave\": true\n}\n")
		if err := os.WriteFile(path, mine, 0o600); err != nil {
			t.Fatalf("making the fixture: %v", err)
		}

		shown, _ := diagnosing(t, dir, &stack{}, "--write-editor-config")
		if !strings.Contains(shown, "left alone") {
			t.Errorf("the report does not say it declined:\n%s", shown)
		}

		held, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("reading it back: %v", err)
		}
		if !bytes.Equal(held, mine) {
			t.Errorf("somebody's own settings were changed to:\n%s", held)
		}
	})
}

// A toolchain older than what the output needs is reported as something that
// will not work, and anything this cannot read is taken to be new enough.
//
// The release candidates are the cases worth having. Go spells one go1.26rc1,
// with no separator a general version parser finds, so anything that took the
// version apart itself would read every one of them as unreadable — and then,
// by the rule below, as new enough. go1.20beta1 is seven releases under the
// floor and would have passed.
func TestWhichToolchainsAreOldEnough(t *testing.T) {
	cases := map[string]bool{
		"go1.27.0": true,
		"go1.28":   true,
		"go1.27":   true,

		"go1.26.5":    false,
		"go1.9":       false,
		"go1.26rc1":   false,
		"go1.20beta1": false,

		// A release candidate of the floor itself is new enough, which is Go's
		// own answer: a //go:build go1.27 constraint is satisfied by go1.27rc1,
		// and the language features are there.
		"go1.27rc1": true,

		// Not a version this can read, which is somebody running a toolchain
		// they built. Taken as new enough, because the alternative is telling
		// them it is too old on the strength of a string forge could not read.
		"devel +abc123": true,
		"":              true,
		"go1.27.1-x":    true,
	}

	for held, want := range cases {
		t.Run(held, func(t *testing.T) {
			if got := newEnough(held); got != want {
				t.Errorf("newEnough(%q) = %v, want %v", held, got, want)
			}
		})
	}
}

// A module whose go directive is older than the floor is reported, whatever the
// compiler running forge is.
//
// The one that usually bites. The go command refuses a construct newer than the
// directive names however new the toolchain is, so a module saying go 1.24
// cannot build what forge writes on a 1.27 compiler — and a report that asked
// only about the compiler would call that setup healthy.
func TestAModuleWhoseGoDirectiveIsTooOld(t *testing.T) {
	found := resolved{Session: &load.Session{Packages: []*packages.Package{
		{PkgPath: "example.com/old", Module: &packages.Module{Path: "example.com/old", GoVersion: "1.24.0"}},
		{PkgPath: "example.com/new", Module: &packages.Module{Path: "example.com/new", GoVersion: "1.27.0"}},
	}}}

	var said []string
	for _, one := range toolchain(found) {
		if one.how == faulty {
			said = append(said, one.said)
		}
	}

	if len(said) != 1 {
		t.Fatalf("reported %d modules as too old: %v", len(said), said)
	}
	if !strings.Contains(said[0], "example.com/old") || !strings.Contains(said[0], "go1.24") {
		t.Errorf("it said %q", said[0])
	}
}

// A settings file is read for the tag rather than for the word, since a file
// that merely mentions it does not set it.
func TestWhichSettingsAnalyseTheTaggedBuild(t *testing.T) {
	cases := map[string]bool{
		`{"gopls": {"build.buildFlags": ["-tags=forgespec"]}}`:             true,
		`{"gopls": {"build.buildFlags": ["-tags=integration,forgespec"]}}`: true,
		`{"gopls": {"build.buildFlags": ["-tags=integration"]}}`:           false,
		`{"files.exclude": {"**/*.forgespec.bak": true}}`:                  false,
		`{"// forgespec is what forge calls it": 1}`:                       false,
		``: false,
	}

	for held, want := range cases {
		t.Run(held, func(t *testing.T) {
			if got := analysed([]byte(held)); got != want {
				t.Errorf("analysed = %v, want %v", got, want)
			}
		})
	}
}

// The verb takes no arguments, since what it reports is about the tree it is
// run in rather than about packages somebody names.
func TestDiagnosingWithAnArgument(t *testing.T) {
	_, err := diagnosing(t, t.TempDir(), &stack{}, "./...")

	if err == nil {
		t.Fatal("an argument was accepted")
	}
	if !strings.Contains(err.Error(), "takes no arguments") {
		t.Errorf("it was refused as %q", err)
	}
}

// stamped gives this binary a version for the length of one test.
//
// A test binary records none, which is the honest answer for one — so a build
// that does have a version is something a test has to supply, and skew is not
// visible without one.
func stamped(t *testing.T, path, version string) {
	t.Helper()

	held := build
	t.Cleanup(func() { build = held })

	build = func() (*debug.BuildInfo, bool) {
		return &debug.BuildInfo{Main: debug.Module{Path: path, Version: version}}, true
	}
}

// depending builds a run whose packages import a module, the way a load of a
// module that depends on the markers reports one.
func depending(path, version string, swapped bool) resolved {
	held := &packages.Module{Path: path, Version: version}
	if swapped {
		held.Replace = &packages.Module{Path: "/somewhere/on/disk"}
	}

	return resolved{Session: &load.Session{Packages: []*packages.Package{{
		PkgPath: "example.com/model",
		Imports: map[string]*packages.Package{path: {PkgPath: path, Module: held}},
	}}}}
}

// The version of the markers the code is written against is read from what the
// packages import, and skew against what this forge resolves is reported.
//
// It is worth reporting because the failure it causes points somewhere else. A
// spec file naming a marker this build has never heard of is reported as a
// marker nothing claims, which reads as a misspelling — and the author checks
// the spelling, which is right, and is not what is wrong.
func TestWhatIsSaidAboutTheMarkers(t *testing.T) {
	_, bundled, _ := versions()
	path, _, _ := strings.Cut(bundled, " ")

	cases := map[string]struct {
		found  resolved
		stamps string
		says   string
	}{
		"nothing depends on them": {
			found: resolved{Session: &load.Session{}},
			says:  "nothing here depends on",
		},
		"replaced": {
			found: depending(path, "v1.2.3", true),
			says:  "is replaced",
		},

		// A binary with no version of its own, which is what go run and a
		// build from a dirty tree both produce — and what this test is, so
		// the version has to be given rather than assumed.
		"a build with no version": {
			found: depending(path, "v9.9.9", false),
			says:  "records no version of its own",
		},

		"a version this build does not have": {
			found: depending(path, "v9.9.9", false), stamps: "v1.0.0",
			says: "written against v9.9.9 and this forge resolves",
		},
		"the version this build has": {
			found: depending(path, "v1.0.0", false), stamps: "v1.0.0",
			says: "written against v1.0.0, which this forge resolves",
		},
	}

	for name, want := range cases {
		t.Run(name, func(t *testing.T) {
			if want.stamps != "" {
				stamped(t, path, want.stamps)
			}

			got := markers(want.found)

			if !strings.Contains(got.said, want.says) {
				t.Errorf("it said %q, wanting it to say %q", got.said, want.says)
			}
			if got.how == faulty {
				t.Errorf("a question about versions was reported as something that stops working: %q", got.said)
			}
		})
	}

	// A module depending on some other module says nothing about the markers.
	if got := markers(depending("example.com/unrelated", "v1.0.0", false)); !strings.Contains(got.said, "nothing here depends on") {
		t.Errorf("an unrelated dependency was read as the markers: %q", got.said)
	}
}

// Whether generated files are committed is asked of git, and where there is
// nobody to ask the report says nothing rather than claiming they are.
//
// Nothing, because being unable to ask is not evidence. Somebody working in a
// directory that is not a repository has no version control to be wrong about,
// and a line telling them their files are committed would be a claim forge did
// not earn — which is worse than the silence it replaces, since it reads as a
// check that passed.
func TestWhetherGeneratedFilesAreCommitted(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git to ask")
	}

	dir := t.TempDir()
	for _, name := range []string{"zz_forge_persons.go", "zz_forge_shared.go"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("package model\n"), 0o600); err != nil {
			t.Fatalf("making the fixture: %v", err)
		}
	}

	pkg := packaged{path: "example.com/model", dir: dir}

	// Not a repository: nothing to ask, so nothing is said.
	if said, _ := tracked(pkg); said != "" {
		t.Errorf("a directory with no repository was reported on: %q", said)
	}

	for _, args := range [][]string{{"init"}, {"config", "user.email", "t@example.com"}, {"config", "user.name", "t"}} {
		out, err := exec.CommandContext(t.Context(), "git", append([]string{"-C", dir}, args...)...).CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}

	// A repository that knows neither file.
	said, how := tracked(pkg)
	if !strings.Contains(said, "does not track") || how != worth {
		t.Errorf("untracked files were reported as %q at %v", said, how)
	}
	for _, name := range []string{"zz_forge_persons.go", "zz_forge_shared.go"} {
		if !strings.Contains(said, name) {
			t.Errorf("the report does not name %s: %q", name, said)
		}
	}

	// And one that knows both.
	if out, err := exec.CommandContext(t.Context(), "git", "-C", dir, "add", ".").CombinedOutput(); err != nil {
		t.Fatalf("git add: %v\n%s", err, out)
	}
	if said, how := tracked(pkg); !strings.Contains(said, "every generated file") || how != 0 {
		t.Errorf("tracked files were reported as %q at %v", said, how)
	}
}

// The report reaches the table, since a finding nothing prints is one nobody
// acts on.
func TestTheGitAnswerReachesTheReport(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git to ask")
	}

	dir := t.TempDir()
	checkable(t, dir)

	for _, args := range [][]string{{"init"}, {"config", "user.email", "t@example.com"}, {"config", "user.name", "t"}} {
		out, err := exec.CommandContext(t.Context(), "git", append([]string{"-C", dir}, args...)...).CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}

	shown, _ := diagnosing(t, dir, generating(t, dir, "Persons"))
	if !strings.Contains(shown, "does not track") {
		t.Errorf("the report says nothing about what git has:\n%s", shown)
	}
}
