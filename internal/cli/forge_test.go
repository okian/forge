package cli

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"runtime/debug"
	"strconv"
	"strings"
	"testing"

	"github.com/okian/forge/internal/diag"
	"github.com/okian/forge/internal/model"
)

// The stand-in stages let a verb be tested without a module on disk, which is
// most of what these tests want — and would let the whole tool be wired to
// nothing at all. This is the one that runs the real ones, over a module that
// really does hold declarations, through the command line somebody would type.
//
// What it asserts is what the stages *found*, not that they ran: a pipeline
// wired to stubs reports the same statuses and the same "not in this build" as
// a wired one, and only the counts tell them apart.
func TestForgeOverAModuleOnDisk(t *testing.T) {
	fixture := fixtureAt(t, "resolve", "stacks")

	got := forge("-v", "-C", fixture, "check", "./...")

	if got.status != diag.ExitDiagnostics {
		t.Errorf("exited %d, want %d:\n%s", got.status, diag.ExitDiagnostics, got.err)
	}

	// Every stage found something. A stage wired to nothing reports zero and
	// changes no status, so these numbers are the only thing that says the tool
	// is connected to the packages it claims to read.
	for _, want := range []string{"loaded", "found", "resolved", "modelled"} {
		if !found(got.err, want) {
			t.Errorf("the %s stage found nothing:\n%s", want, got.err)
		}
	}

	// The fixture holds subjects that cannot be modelled, on purpose, so the
	// walk reaches the far end and reports — which is a stronger claim than
	// reaching it and finding nothing to say.
	if !strings.Contains(got.err, "FRG2") {
		t.Errorf("the subjects were not modelled at all:\n%s", got.err)
	}

	// And the refusal is drawn, which only happens if the stage that knows the
	// stack hands it to the stage that does not. A diagnostic naming the type
	// it refused leaves the reader to find it among four nested layers.
	if !strings.Contains(got.err, "Collection[*Person]\n") {
		t.Errorf("the refusal does not draw the declaration:\n%s", got.err)
	}
	if !strings.Contains(got.err, "^^^^^^^") {
		t.Errorf("the refusal draws no caret:\n%s", got.err)
	}
}

// found reports whether a progress line beginning with a word carries a count
// above zero.
func found(progress, stage string) bool {
	for line := range strings.SplitSeq(progress, "\n") {
		if !strings.HasPrefix(line, stage+" ") {
			continue
		}
		count, _, _ := strings.Cut(strings.TrimPrefix(line, stage+" "), " ")
		if n, err := strconv.Atoi(count); err == nil && n > 0 {
			return true
		}
	}
	return false
}

// And a module whose declarations are wrong reports them, with the code and the
// position, through the command line. Nothing but the real stages produces one.
func TestForgeOverDeclarationsThatAreWrong(t *testing.T) {
	fixture := fixtureAt(t, "discover", "decls")

	got := forge("-C", fixture, "check", "./...")

	if got.status != diag.ExitDiagnostics {
		t.Errorf("exited %d, want %d:\n%s", got.status, diag.ExitDiagnostics, got.err)
	}
	if !strings.Contains(got.err, "FRG") {
		t.Errorf("no diagnostic reached the command line:\n%s", got.err)
	}
	if !strings.Contains(got.err, ".go:") {
		t.Errorf("the diagnostic carries no position:\n%s", got.err)
	}
	if !strings.Contains(got.err, "hint:") {
		t.Errorf("the diagnostic arrived without the line that says what to do:\n%s", got.err)
	}
	if got.out != "" {
		t.Errorf("a diagnostic arrived on stdout:\n%s", got.out)
	}
}

// fixtureAt returns a module another package keeps for its own tests, which is
// where the declarations an author would really write already live.
func fixtureAt(t *testing.T, parts ...string) string {
	t.Helper()

	at := append([]string{"..", "..", "internal"}, parts[0], "testdata")
	at = append(at, parts[1:]...)

	where, err := filepath.Abs(filepath.Join(at...))
	if err != nil {
		t.Fatalf("finding the fixture: %v", err)
	}
	if _, err := os.Stat(filepath.Join(where, "go.mod")); err != nil {
		t.Fatalf("the fixture is not a module: %v", err)
	}
	return where
}

// No verb is unfinished, and the machinery for saying so still works.
//
// The list was how a build said which of its own verbs it could not do; it is
// empty now, and the assertion that matters has turned over: every verb runs.
// A stub added later fails this rather than shipping as a command that looks
// like the others and does nothing — which is the failure the machinery was
// written for, and which is only worth having if something notices.
func TestNoVerbIsUnfinished(t *testing.T) {
	for _, cmd := range commands {
		t.Run(cmd.name, func(t *testing.T) {
			held := &stack{}
			env := &environment{
				stdout: &bytes.Buffer{}, stderr: &bytes.Buffer{},
				dir: t.TempDir(), pipeline: over(held),
			}

			// Whatever else a verb makes of an empty run is not the subject.
			// What is, is that it did not decline to try.
			if err := cmd.run(env, cmd, nil); errors.Is(err, errNotBuilt) {
				t.Errorf("%s is a command and this build cannot do it: %v", cmd.name, err)
			}
		})
	}
}

// Every command refuses a flag it does not define, because a flag somebody
// typed was meant to change something and silently doing nothing is worse than
// refusing.
func TestEveryCommandRefusesAFlagItDoesNotHave(t *testing.T) {
	for _, cmd := range commands {
		t.Run(cmd.name, func(t *testing.T) {
			got := forge(cmd.name, "-nonesuch")

			if got.status != diag.ExitUsage {
				t.Errorf("exited %d, want %d", got.status, diag.ExitUsage)
			}
			if !strings.Contains(got.err, "nonesuch") {
				t.Errorf("the failure does not name the flag:\n%s", got.err)
			}
		})
	}
}

// Explaining every declaration in a package is what the other verbs are for.
// This one answers a question about one declaration, and without a name there
// is no question.
func TestExplainingNothingInParticular(t *testing.T) {
	got := forge("explain", ".")

	if got.status != diag.ExitUsage {
		t.Errorf("exited %d, want %d", got.status, diag.ExitUsage)
	}
	if !strings.Contains(got.err, "-t") {
		t.Errorf("the failure does not say what to type:\n%s", got.err)
	}
}

// An empty command name resembles every command and none of them, so it gets
// no guess.
func TestAnEmptyCommandName(t *testing.T) {
	got := forge("")

	if got.status != diag.ExitUsage {
		t.Errorf("exited %d, want %d", got.status, diag.ExitUsage)
	}
	if strings.Contains(got.err, "did you mean") {
		t.Errorf("an empty name was guessed at:\n%s", got.err)
	}
}

// Three versions that can disagree, and each disagreement means something
// different: which generator wrote a file, which declarations it can resolve,
// and which language the output has to compile under.
func TestTheVersionsThatCanDisagree(t *testing.T) {
	got := forge("version")

	if got.status != diag.ExitOK {
		t.Errorf("exited %d, want %d:\n%s", got.status, diag.ExitOK, got.err)
	}

	lines := strings.Split(strings.TrimSpace(got.out), "\n")
	if len(lines) != 3 {
		t.Fatalf("reported %d lines, want 3:\n%s", len(lines), got.out)
	}
	for i, want := range []string{"forge ", "markers ", "go "} {
		if !strings.HasPrefix(lines[i], want) {
			t.Errorf("line %d is %q, want it to start %q", i+1, lines[i], want)
		}
	}
	if !strings.Contains(lines[1], "github.com/okian/forge") {
		t.Errorf("the markers line does not name what a spec file imports: %q", lines[1])
	}
	if got.err != "" {
		t.Errorf("version wrote to stderr:\n%s", got.err)
	}
}

// A binary with no build information is one somebody assembled by hand, and
// saying so is more use than reporting a version invented here.
func TestAVersionNobodyStamped(t *testing.T) {
	was := build
	build = func() (*debug.BuildInfo, bool) { return nil, false }
	t.Cleanup(func() { build = was })

	got := forge("version")

	if got.status != diag.ExitOK {
		t.Errorf("exited %d, want %d:\n%s", got.status, diag.ExitOK, got.err)
	}
	if !strings.Contains(got.out, unknown) {
		t.Errorf("an unstamped build did not say so:\n%s", got.out)
	}
}

// And one stamped with nothing in the fields that matter reads the same way,
// rather than reporting an empty string as a version.
//
// The path is not one of those fields. It is the constant a spec file names, so
// it is right whatever the build recorded; the version beside it is what goes
// missing.
func TestAVersionStampedWithNothing(t *testing.T) {
	was := build
	build = func() (*debug.BuildInfo, bool) { return &debug.BuildInfo{}, true }
	t.Cleanup(func() { build = was })

	self, markers, toolchain := versions()

	if self != unknown {
		t.Errorf("forge reports %q", self)
	}
	if want := model.MarkerPkg + " " + unknown; markers != want {
		t.Errorf("markers report %q, want %q", markers, want)
	}
	if toolchain == "" {
		t.Error("the toolchain reports nothing at all")
	}
}

// The toolchain a binary was built with is the one stamped into it, not the one
// the runtime happens to report — they differ whenever a binary outlives the
// toolchain that produced it, which is the case this line exists for.
func TestTheToolchainThatBuiltIt(t *testing.T) {
	was := build
	build = func() (*debug.BuildInfo, bool) {
		return &debug.BuildInfo{
			GoVersion: "go1.99.0",
			Main:      debug.Module{Path: model.MarkerPkg, Version: "v1.2.3"},
		}, true
	}
	t.Cleanup(func() { build = was })

	self, markers, toolchain := versions()

	if self != "v1.2.3" {
		t.Errorf("forge reports %q", self)
	}
	if want := model.MarkerPkg + " v1.2.3"; markers != want {
		t.Errorf("markers report %q, want %q", markers, want)
	}
	if toolchain != "go1.99.0" {
		t.Errorf("the toolchain reports %q, want the one stamped into the binary", toolchain)
	}
}

// A binary somebody linked a layer into reports the markers it generates
// against, not the module it happens to be.
//
// The case the marker line exists for and the one it used to get wrong. A
// plugin binary's own module is the layer's, and the markers a spec file names
// are still forge's — so a header recording the running module would claim a
// file was written against markers that module does not declare. The freshness
// check compares that line, which made every file such a binary wrote look to
// forge's own command like the work of different tooling.
func TestAVersionFromABinaryThatLinkedALayer(t *testing.T) {
	cases := map[string]struct {
		deps []*debug.Module
		want string
	}{
		"forge as an ordinary requirement": {
			deps: []*debug.Module{
				{Path: "example.com/other", Version: "v0.4.0"},
				{Path: model.MarkerPkg, Version: "v1.2.3"},
			},
			want: "v1.2.3",
		},
		"forge replaced by a directory, as a development build has it": {
			deps: []*debug.Module{{
				Path:    model.MarkerPkg,
				Version: "v0.0.0",
				Replace: &debug.Module{Path: model.MarkerPkg},
			}},
			want: unknown,
		},
		"forge replaced by another version": {
			deps: []*debug.Module{{
				Path:    model.MarkerPkg,
				Version: "v1.0.0",
				Replace: &debug.Module{Path: model.MarkerPkg, Version: "v1.4.0"},
			}},
			want: "v1.4.0",
		},
		"a binary that does not link the markers at all": {
			deps: []*debug.Module{{Path: "example.com/other", Version: "v0.4.0"}},
			want: unknown,
		},
	}

	for name, held := range cases {
		t.Run(name, func(t *testing.T) {
			was := build
			build = func() (*debug.BuildInfo, bool) {
				return &debug.BuildInfo{
					Main: debug.Module{Path: "example.com/mylayers", Version: "v0.9.0"},
					Deps: held.deps,
				}, true
			}
			t.Cleanup(func() { build = was })

			self, markers, _ := versions()

			// The binary is still itself, which is the other half: the two
			// lines are two facts and only one of them moved.
			if self != "v0.9.0" {
				t.Errorf("forge reports %q, want the running binary's own version", self)
			}
			if want := model.MarkerPkg + " " + held.want; markers != want {
				t.Errorf("markers report %q, want %q", markers, want)
			}
		})
	}
}
