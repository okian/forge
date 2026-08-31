package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"go/token"
	"io"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"

	"github.com/okian/forge/internal/diag"
	"github.com/okian/forge/internal/discover"
	"github.com/okian/forge/internal/load"
	"github.com/okian/forge/internal/resolve"
	"github.com/okian/forge/internal/subject"
)

// stack stands in for the shared path, so that a command can be run against
// declarations that were never on disk. A fixture module per case would mean a
// load that shells out to the go command for each of them, which is a suite
// people stop running.
type stack struct {
	session    *load.Session
	refused    error
	empty      bool
	candidates []discover.Candidate
	found      []resolve.Declaration
	modelled   []request
	loaded     diag.Set
	discovered diag.Set
	resolved   diag.Set
	built      diag.Set

	// asked records what reached each stage, so that a test can say whether a
	// verb walked the path or went around it.
	asked []string

	// given records what the load was configured with, and modelling what the
	// subject builder was. Recording only that a stage ran would leave a verb
	// free to load something other than what the command line named, or to
	// build subjects for a module it is not generating for.
	given     load.Config
	modelling subject.Config
}

func (s *stack) Load(cfg load.Config) (*load.Session, error) {
	s.asked = append(s.asked, "load")
	s.given = cfg

	if s.refused != nil {
		return nil, s.refused
	}
	if s.empty {
		return nil, nil
	}
	if s.session == nil {
		s.session = &load.Session{Fset: token.NewFileSet()}
	}
	s.session.Diagnostics = s.loaded
	return s.session, nil
}

func (s *stack) Discover(*load.Session) ([]discover.Candidate, diag.Set) {
	s.asked = append(s.asked, "discover")
	return s.candidates, s.discovered
}

func (s *stack) Resolve([]discover.Candidate) ([]resolve.Declaration, diag.Set) {
	s.asked = append(s.asked, "resolve")
	return s.found, s.resolved
}

func (s *stack) Model(cfg subject.Config, declarations []resolve.Declaration) ([]request, diag.Set) {
	s.asked = append(s.asked, "model")
	s.modelling = cfg

	// What the builder said travels with the declaration it was said about, the
	// way the real stage attributes it — and a request that already carries its
	// own is left alone, so that a test can give two declarations different
	// problems and see which one is reported.
	if s.modelled != nil {
		out := make([]request, len(s.modelled))
		for i, one := range s.modelled {
			if one.Diagnostics.Empty() {
				one.Diagnostics = s.built
			}
			out[i] = one
		}
		return out, s.built
	}

	out := make([]request, 0, len(declarations))
	for _, decl := range declarations {
		out = append(out, request{Declaration: decl, Diagnostics: s.built})
	}
	return out, s.built
}

// over returns a pipeline wired to a stand-in.
func over(s *stack) pipeline {
	return pipeline{loading: s, discovering: s, resolving: s, modelling: s}
}

// quietly returns an environment that reports nowhere, for the tests that are
// about the walk rather than about what it said.
func quietly() *environment {
	return &environment{stdout: io.Discard, stderr: io.Discard}
}

// named returns the least declaration a verb can be asked about: a name, and
// nothing resolution would have had to work for.
func named(name string) resolve.Declaration {
	return resolve.Declaration{Candidate: discover.Candidate{Name: name}}
}

// loadedFrom returns a session whose packages belong to a module, which is
// where the answer to "may forge attach a method to this type" comes from.
func loadedFrom(module string) *load.Session {
	return &load.Session{
		Fset:     token.NewFileSet(),
		Packages: []*packages.Package{{Module: &packages.Module{Path: module, Main: true}}},
	}
}

// complaint returns a diagnostic set holding one thing to say.
func complaint(code diag.Code, message string) diag.Set {
	var set diag.Set
	set.Add(diag.New(code, token.Position{Filename: "model/spec.go", Line: 12, Column: 6}, "%s", message))
	return set
}

// Every stage runs, in order, and what each of them found is collected rather
// than the first of them ending the walk: an author who has made three mistakes
// should learn about three mistakes.
func TestTheWalkCollectsFromEveryStage(t *testing.T) {
	s := &stack{
		loaded:     complaint(diag.Code(5001), "the package does not build"),
		discovered: complaint(diag.Code(3001), "the directive landed on nothing"),
		resolved:   complaint(diag.Code(1007), "the stack does not resolve"),

		// A declaration for the fourth diagnostic to belong to. What the
		// subject builder says is about one declaration, so without one there
		// is nowhere for it to be said.
		found: []resolve.Declaration{named("Persons")},
		built: complaint(diag.Code(2002), "the subject is a pointer"),
	}

	found, err := over(s).follow(quietly(), load.Config{})
	if err != nil {
		t.Fatalf("the walk was refused: %v", err)
	}

	if want := []string{"load", "discover", "resolve", "model"}; strings.Join(s.asked, " ") != strings.Join(want, " ") {
		t.Errorf("the stages ran %v, want %v", s.asked, want)
	}
	// Three about the packages, and the fourth about the declaration it was
	// said about — which is where a verb answering a question about one
	// declaration looks, and where a verb acting on all of them looks too.
	if found.Diagnostics.Len() != 3 {
		t.Errorf("collected %d package diagnostics, want 3:\n%s", found.Diagnostics.Len(), found.Diagnostics.Render())
	}
	if all := found.All(); all.Len() != 4 {
		t.Errorf("collected %d diagnostics in all, want 4:\n%s", all.Len(), all.Render())
	}
}

// A load that cannot be attempted at all ends the walk. There is nothing to
// discover in a session that does not exist, and reporting the absence three
// times over would say less than reporting it once.
func TestAWalkThatCannotStart(t *testing.T) {
	s := &stack{refused: errors.New("the go command will not run")}

	if _, err := over(s).follow(quietly(), load.Config{}); err == nil {
		t.Fatal("a load that could not be attempted was walked past")
	}
	if len(s.asked) != 1 {
		t.Errorf("the walk went on after the load failed: %v", s.asked)
	}
}

// A package that does not compile is not a walk that cannot start. The
// declarations in the packages that do compile are still worth having, because
// a run that said nothing until everything built would be useless in the
// situation the author needs it.
func TestAWalkOverAPackageThatDoesNotBuild(t *testing.T) {
	s := &stack{loaded: complaint(diag.Code(5001), "the package does not build")}

	found, err := over(s).follow(quietly(), load.Config{})
	if err != nil {
		t.Fatalf("a package that does not build ended the walk: %v", err)
	}
	if len(s.asked) != 4 {
		t.Errorf("the walk stopped at %v", s.asked)
	}
	if found.Diagnostics.Empty() {
		t.Error("the build failure was not reported")
	}
}

// The verbs that resolve declarations share one path, so that none of them can
// report a stack the others would not have found.
func TestTheVerbsWalkTheSamePath(t *testing.T) {
	verbs := []struct {
		name string
		verb func(*environment, command, []string) error
	}{
		{name: "check", verb: check},
		{name: "explain", verb: explain},
		{name: "generate", verb: generate},
	}

	for _, tc := range verbs {
		name, verb := tc.name, tc.verb
		t.Run(name, func(t *testing.T) {
			s := &stack{}
			env := &environment{stdout: &bytes.Buffer{}, stderr: &bytes.Buffer{}, pipeline: over(s)}

			cmd, ok := lookup(name)
			if !ok {
				t.Fatalf("%s is not a command", name)
			}

			args := []string{"./..."}
			if name == "explain" {
				args = []string{"-t", "Persons", "."}
			}
			// What the verb answers is not the point; that it walked the same
			// path to get there is.
			_ = verb(env, cmd, args)

			if want := "load discover resolve model"; strings.Join(s.asked, " ") != want {
				t.Errorf("the stages ran %v, want %q", s.asked, want)
			}
		})
	}
}

// A load that will not run is the verb's failure too, and it is reported as
// what it is rather than as a verb this build does not have.
func TestAVerbWhoseLoadWillNotRun(t *testing.T) {
	for _, name := range []string{"check", "explain", "generate"} {
		t.Run(name, func(t *testing.T) {
			s := &stack{refused: errors.New("the go command will not run")}
			env := &environment{stdout: io.Discard, stderr: io.Discard, pipeline: over(s)}

			cmd, _ := lookup(name)
			args := []string{"./..."}
			if name == "explain" {
				args = []string{"-t", "Persons"}
			}

			err := cmd.run(env, cmd, args)

			if err == nil {
				t.Fatal("a load that will not run reported success")
			}
			if !strings.Contains(err.Error(), "go command") {
				t.Errorf("the failure does not say what went wrong: %v", err)
			}
			// Reporting it as a verb this build does not have would send somebody
			// to wait for a release that would not fix it.
			if errors.Is(err, errNotBuilt) {
				t.Errorf("a load failure was reported as a missing verb: %v", err)
			}
		})
	}
}

// Diagnostics go to stderr, in the one rendering every diagnostic uses. A run
// whose output feeds another program must not have its complaints arrive in the
// pipe.
func TestDiagnosticsGoToStandardError(t *testing.T) {
	// The two verbs that would otherwise act on the declarations stop when the
	// declarations were reported; explain answers a question about one and
	// still has that question to answer.
	for _, name := range []string{"generate", "check"} {
		t.Run(name, func(t *testing.T) {
			s := &stack{resolved: complaint(diag.Code(1007), "two storage layers in stack")}

			var out, errs bytes.Buffer
			env := &environment{stdout: &out, stderr: &errs, pipeline: over(s)}

			cmd, _ := lookup(name)
			err := cmd.run(env, cmd, []string{"./..."})

			if !errors.Is(err, errReported) {
				t.Errorf("a run that reported did not end as one: %v", err)
			}
			if !strings.Contains(errs.String(), "two storage layers in stack") {
				t.Errorf("the diagnostic is not on stderr:\n%s", errs.String())
			}
			// The complaint must not be on stdout, since a run whose output
			// feeds another program would have it arrive in the pipe. And for
			// the two verbs that answer with nothing, stdout must be empty
			// outright — a relaxation made for the verb that answers with
			// something would hide a regression in the two that do not.
			if strings.Contains(out.String(), "two storage layers in stack") {
				t.Errorf("a diagnostic arrived on stdout:\n%s", out.String())
			}
			if name != "explain" && out.Len() != 0 {
				t.Errorf("a verb that answers nothing wrote to stdout:\n%s", out.String())
			}
		})
	}
}

// A run that has already said everything it has to say does not say it again
// under a heading, or one run reads as two failures.
func TestARunThatHasAlreadyReported(t *testing.T) {
	var out, errs bytes.Buffer
	if status := status(&out, &errs, errReported); status != diag.ExitDiagnostics {
		t.Errorf("exited %d, want %d", status, diag.ExitDiagnostics)
	}
	if errs.Len() != 0 {
		t.Errorf("a run that had reported reported again:\n%s", errs.String())
	}
}

// What the command line names is what gets loaded. A verb that widened the
// scope on its own would generate into packages nobody asked about, and this is
// the only place that is decided.
func TestThePatternsReachTheLoad(t *testing.T) {
	for _, name := range []string{"check", "generate"} {
		t.Run(name, func(t *testing.T) {
			s := &stack{}
			env := &environment{stdout: io.Discard, stderr: io.Discard, pipeline: over(s)}

			cmd, _ := lookup(name)
			_ = cmd.run(env, cmd, []string{"./model", "./store"})

			if got := strings.Join(s.given.Patterns, " "); got != "./model ./store" {
				t.Errorf("loaded %q, want %q", got, "./model ./store")
			}
		})
	}

	// And the verb that asks about one declaration takes one package.
	s := &stack{}
	env := &environment{stdout: io.Discard, stderr: io.Discard, pipeline: over(s)}

	cmd, _ := lookup("explain")
	_ = cmd.run(env, cmd, []string{"-t", "Persons", "./model"})

	if got := strings.Join(s.given.Patterns, " "); got != "./model" {
		t.Errorf("loaded %q, want %q", got, "./model")
	}
}

// A command line naming no package loads the whole module, which is what a
// generator is nearly always run over — except for the verb that answers a
// question about one declaration, where the whole module is both slower and
// ambiguous.
func TestWhatIsLoadedWhenNothingIsNamed(t *testing.T) {
	// Spelled out rather than taken from the constants, since comparing a
	// default against the constant it was read from passes whatever the
	// constant becomes — and the whole module is the wrong answer for the verb
	// that asks about one declaration.
	want := map[string]string{"check": "./...", "explain": ".", "generate": "./..."}

	for _, name := range []string{"check", "explain", "generate"} {
		t.Run(name, func(t *testing.T) {
			s := &stack{}
			env := &environment{stdout: io.Discard, stderr: io.Discard, pipeline: over(s)}

			cmd, _ := lookup(name)
			var args []string
			if name == "explain" {
				args = []string{"-t", "Persons"}
			}
			_ = cmd.run(env, cmd, args)

			if got := strings.Join(s.given.Patterns, " "); got != want[name] {
				t.Errorf("loaded %q, want %q", got, want[name])
			}
		})
	}
}

// Two packages may each declare a Persons, and nothing about -t says which was
// meant.
func TestExplainingSomethingInTwoPlaces(t *testing.T) {
	s := &stack{}
	env := &environment{stdout: io.Discard, stderr: io.Discard, pipeline: over(s)}

	cmd, _ := lookup("explain")
	err := cmd.run(env, cmd, []string{"-t", "Persons", "./model", "./store"})

	if _, wrong := errors.AsType[misuse](err); !wrong {
		t.Fatalf("two packages were accepted: %v", err)
	}
	if len(s.asked) != 0 {
		t.Errorf("a load was attempted anyway: %v", s.asked)
	}
}

// The directory -C names is where patterns resolve from, and a verb that
// dropped it would load the wrong module while reporting the right one.
func TestTheDirectoryReachesTheLoad(t *testing.T) {
	s := &stack{}
	env := &environment{stdout: io.Discard, stderr: io.Discard, pipeline: over(s)}

	rest, err := global(env, []string{"-C", t.TempDir(), "check", "./..."})
	if err != nil {
		t.Fatalf("reading the flags: %v", err)
	}

	// What the verb makes of an empty run is not the subject. What is, is that
	// it loaded from where it was told to, which is answerable either way.
	_ = dispatch(env, rest)

	if s.given.Dir == "" {
		t.Fatal("the directory did not reach the load at all")
	}
	if s.given.Dir != env.dir {
		t.Errorf("loaded from %q, want %q", s.given.Dir, env.dir)
	}
}

// A loader may return nothing at all, and every stage below reads the session.
// Refusing at the seam says which stage produced nothing rather than leaving a
// dereference to say it.
func TestALoadThatProducedNoSession(t *testing.T) {
	s := &stack{empty: true}

	_, err := over(s).follow(quietly(), load.Config{})

	if err == nil {
		t.Fatal("a load that produced no session was walked past")
	}
	if !strings.Contains(err.Error(), "no session") {
		t.Errorf("the failure does not say what was missing: %v", err)
	}
	if len(s.asked) != 1 {
		t.Errorf("the walk went on: %v", s.asked)
	}
}

// Progress is what -v asks for, and it says what each stage did rather than
// only that it ran.
func TestProgressWhenAskedForIt(t *testing.T) {
	var errs bytes.Buffer
	env := &environment{stdout: io.Discard, stderr: &errs, verbose: true, pipeline: over(&stack{})}

	if _, err := env.pipeline.follow(env, env.loadConfig("./model")); err != nil {
		t.Fatalf("the walk was refused: %v", err)
	}

	for _, want := range []string{"loading ./model", "packages", "declarations", "stacks", "subjects"} {
		if !strings.Contains(errs.String(), want) {
			t.Errorf("the progress does not mention %q:\n%s", want, errs.String())
		}
	}
}

// And it is off unless asked, since a generator that narrates itself is one
// every script has to filter. Quiet wins, so that a script setting it is not
// made chatty by a flag it inherited and did not choose.
func TestProgressWhenNobodyAsked(t *testing.T) {
	cases := []struct {
		name           string
		quiet, verbose bool
	}{
		{name: "by default"},
		{name: "asked to be quiet", quiet: true},
		{name: "quiet and verbose at once", quiet: true, verbose: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var errs bytes.Buffer
			env := &environment{
				stdout: io.Discard, stderr: &errs,
				quiet: tc.quiet, verbose: tc.verbose,
				pipeline: over(&stack{}),
			}

			if _, err := env.pipeline.follow(env, env.loadConfig("./model")); err != nil {
				t.Fatalf("the walk was refused: %v", err)
			}
			if errs.Len() != 0 {
				t.Errorf("progress was reported to nobody who asked:\n%s", errs.String())
			}
		})
	}
}

// Every diagnostic reaches the reader, not the first of them. An author who has
// made three mistakes should learn about three mistakes rather than be walked
// through them one run at a time.
func TestEveryDiagnosticReachesTheReader(t *testing.T) {
	var many diag.Set
	for _, message := range []string{"the first thing", "the second thing", "the third thing"} {
		one := complaint(diag.Code(1007), message)
		many.Merge(&one)
	}

	var errs bytes.Buffer
	env := &environment{stdout: io.Discard, stderr: &errs}
	env.report(many)

	for _, want := range []string{"the first thing", "the second thing", "the third thing"} {
		if !strings.Contains(errs.String(), want) {
			t.Errorf("the report does not mention %q:\n%s", want, errs.String())
		}
	}
}

// A set with nothing in it is not a report, and printing an empty line for it
// would put one in the output of every run that went well.
func TestReportingNothing(t *testing.T) {
	var errs bytes.Buffer
	env := &environment{stdout: io.Discard, stderr: &errs}

	if env.report(diag.Set{}) {
		t.Error("an empty set was reported as something")
	}
	if errs.Len() != 0 {
		t.Errorf("an empty set wrote something:\n%s", errs.String())
	}
}

// Diagnostics go to stderr from every verb that has any, including the one
// whose answer will be machine-readable: a diagnostic on stdout would corrupt
// the JSON it is printed beside.
func TestEveryVerbSendsDiagnosticsToStandardError(t *testing.T) {
	for _, name := range []string{"check", "explain", "generate"} {
		t.Run(name, func(t *testing.T) {
			s := &stack{resolved: complaint(diag.Code(1007), "two storage layers in stack")}

			var out, errs bytes.Buffer
			env := &environment{stdout: &out, stderr: &errs, pipeline: over(s)}

			cmd, _ := lookup(name)
			args := []string{"./..."}
			if name == "explain" {
				// The verb answers a question about one declaration, so it has
				// to find one — and reports what was said about that one rather
				// than about the package it sits in.
				s.modelled = []request{{Declaration: named("Persons")}}
				s.built = complaint(diag.Code(1007), "two storage layers in stack")
				args = []string{"-t", "Persons"}
			}
			_ = cmd.run(env, cmd, args)

			if !strings.Contains(errs.String(), "two storage layers in stack") {
				t.Errorf("the diagnostic is not on stderr:\n%s", errs.String())
			}
			// The complaint must not be on stdout, since a run whose output
			// feeds another program would have it arrive in the pipe. And for
			// the two verbs that answer with nothing, stdout must be empty
			// outright — a relaxation made for the verb that answers with
			// something would hide a regression in the two that do not.
			if strings.Contains(out.String(), "two storage layers in stack") {
				t.Errorf("a diagnostic arrived on stdout:\n%s", out.String())
			}
			if name != "explain" && out.Len() != 0 {
				t.Errorf("a verb that answers nothing wrote to stdout:\n%s", out.String())
			}
		})
	}
}

// How a run ends is the whole of what a shell script can act on, so every way
// one can end has to map to the status that says so — and to the stream that
// carries the reason.
func TestHowARunCanEnd(t *testing.T) {
	answered := misuse{err: errors.New("a flag nobody defined"), answer: func(w io.Writer) {
		_, _ = io.WriteString(w, "the flags this command takes\n")
	}}

	cases := []struct {
		name   string
		err    error
		status int
		out    string
		errs   []string
		quiet  []string
	}{
		{name: "the run did what was asked", err: nil, status: diag.ExitOK},
		{
			name: "somebody asked what the commands are",
			err:  flag.ErrHelp, status: diag.ExitOK,
			out: "Commands:",
		},
		{
			name: "the input was reported on",
			err:  errReported, status: diag.ExitDiagnostics,
			// Already said, and saying it again under a heading would make one
			// run read as two failures.
			quiet: []string{"forge:", "Commands:"},
		},
		{
			name: "the run could not do what was asked",
			err:  fmt.Errorf("writing generated files %w", errNotBuilt), status: diag.ExitDiagnostics,
			errs:  []string{"forge:", "not in this build"},
			quiet: []string{"Commands:"},
		},
		{
			name: "the command line named no run",
			err:  misusedf("unknown command %q", "bogus"), status: diag.ExitUsage,
			errs: []string{"forge:", "bogus", "Commands:"},
		},
		{
			name: "one command's flags were got wrong",
			err:  answered, status: diag.ExitUsage,
			errs: []string{"forge:", "the flags this command takes"},
			// The list of commands is the answer to a wrong command, not to a
			// wrong flag of the right one.
			quiet: []string{"Commands:"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var out, errs bytes.Buffer

			if got := status(&out, &errs, tc.err); got != tc.status {
				t.Errorf("exited %d, want %d", got, tc.status)
			}
			if tc.out != "" && !strings.Contains(out.String(), tc.out) {
				t.Errorf("stdout does not hold %q:\n%s", tc.out, out.String())
			}
			if tc.out == "" && out.Len() != 0 {
				t.Errorf("a run that answered nothing wrote to stdout:\n%s", out.String())
			}
			for _, want := range tc.errs {
				if !strings.Contains(errs.String(), want) {
					t.Errorf("stderr does not hold %q:\n%s", want, errs.String())
				}
			}
			for _, gone := range tc.quiet {
				if strings.Contains(errs.String(), gone) {
					t.Errorf("stderr holds %q:\n%s", gone, errs.String())
				}
			}
		})
	}
}
