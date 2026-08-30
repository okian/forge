package goldentest

import (
	"errors"
	"flag"
	"fmt"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/ctrlflow"
	"golang.org/x/tools/go/analysis/passes/inspect"
)

// says records what the harness said, standing in for a running test.
type says struct {
	name   string
	failed bool
	said   []string
}

func (s *says) Helper() {}
func (s *says) Name() string {
	if s.name == "" {
		return "said"
	}
	return s.name
}

func (s *says) Logf(format string, args ...any) {
	s.said = append(s.said, fmt.Sprintf(format, args...))
}

func (s *says) Errorf(format string, args ...any) {
	s.failed = true
	s.Logf(format, args...)
}

func (s *says) Fatalf(format string, args ...any) {
	s.Errorf(format, args...)
	panic(s)
}

// heard returns everything the harness said, as one text.
func (s *says) heard() string { return strings.Join(s.said, "\n") }

// only swaps the analyses for the rest of a test, so that what the driver does
// with an analysis it cannot run can be seen without waiting for somebody to
// add one.
func only(t *testing.T, swapped ...*analysis.Analyzer) {
	t.Helper()

	was := analyses
	analyses = swapped
	t.Cleanup(func() { analyses = was })
}

// compiling is the smallest package the driver will accept, for tests about the
// driver rather than about the code it reads.
var compiling = Package{
	Path:  "model",
	Files: []Source{{Name: "model.go", Content: []byte("package model\n\ntype Persons []int\n")}},
}

// An analysis that cannot run is a check that has quietly stopped checking,
// which is worse than one that fails: the suite goes green and covers less than
// it says it does.
func TestAnAnalysisThatCannotRun(t *testing.T) {
	only(t, &analysis.Analyzer{
		Name: "broken",
		Doc:  "an analysis that cannot run",
		Run:  func(*analysis.Pass) (any, error) { return nil, errors.New("it did not work") },
	})

	err := Compiles(compiling)

	if err == nil {
		t.Fatal("an analysis that could not run was passed over")
	}
	for _, want := range []string{"broken", "could not run", "it did not work"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the failure does not mention %q:\n%v", want, err)
		}
	}
}

// An analysis that panics would otherwise take the test binary down and be
// reported as a crash in whichever layer's package happened to be running.
func TestAnAnalysisThatPanics(t *testing.T) {
	only(t, &analysis.Analyzer{
		Name: "collapsing",
		Doc:  "an analysis that panics",
		Run:  func(*analysis.Pass) (any, error) { panic("it fell over") },
	})

	err := Compiles(compiling)

	if err == nil {
		t.Fatal("an analysis that panicked was passed over")
	}
	for _, want := range []string{"collapsing", "panicked", "it fell over"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the failure does not mention %q:\n%v", want, err)
		}
	}
}

// The driver is deliberately small, and what an analysis might otherwise reach
// for is absent rather than stubbed — so an analysis that needs more is named
// here rather than dereferencing nothing part way through somebody's package.
func TestAnAnalysisThisDriverCannotSupport(t *testing.T) {
	cases := map[string]struct {
		analyzer *analysis.Analyzer
		fragment string
	}{
		"one that needs a result nothing produces": {
			analyzer: &analysis.Analyzer{
				Name:     "demanding",
				Doc:      "an analysis that needs more than a walk",
				Requires: []*analysis.Analyzer{inspect.Analyzer, ctrlflow.Analyzer},
				Run:      func(*analysis.Pass) (any, error) { return nil, nil },
			},
			fragment: "ctrlflow",
		},
		"one that carries facts between packages": {
			analyzer: &analysis.Analyzer{
				Name:      "remembering",
				Doc:       "an analysis that carries facts",
				FactTypes: []analysis.Fact{&remembered{}},
				Run:       func(*analysis.Pass) (any, error) { return nil, nil },
			},
			fragment: "facts",
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			only(t, tc.analyzer)

			err := Compiles(compiling)

			if err == nil {
				t.Fatal("an analysis this driver cannot support was run anyway")
			}
			if !strings.Contains(err.Error(), tc.fragment) {
				t.Errorf("the failure does not say what was missing:\n%v", err)
			}
		})
	}
}

// remembered is a fact, which is what an analysis carries from one package into
// the analysis of another.
type remembered struct{}

func (*remembered) AFact() {}

// Every analysis in the set has to be one this driver can run, or the set is a
// crash waiting for the input that reaches the code which asks.
func TestEveryAnalysisCanRunHere(t *testing.T) {
	for _, analyzer := range analyses {
		if why := unsupported(analyzer); why != "" {
			t.Errorf("%s is in the set and cannot run here: %s", analyzer.Name, why)
		}
	}
	if err := analysis.Validate(analyses); err != nil {
		t.Errorf("the set does not hold together: %v", err)
	}
}

// An analysis can read the file it is reporting about, and a driver that cannot
// hand one over dereferences nothing.
func TestAnAnalysisReadingTheFileItReportsOn(t *testing.T) {
	var read []byte
	var failed error

	only(t, &analysis.Analyzer{
		Name: "reading",
		Doc:  "an analysis that reads its own file",
		Run: func(pass *analysis.Pass) (any, error) {
			read, _ = pass.ReadFile("model.go")
			_, failed = pass.ReadFile("nowhere.go")
			return nil, nil
		},
	})

	if err := Compiles(compiling); err != nil {
		t.Fatalf("a package that compiles was refused: %v", err)
	}
	if !strings.Contains(string(read), "type Persons []int") {
		t.Errorf("the analysis read %q", read)
	}
	if failed == nil {
		t.Error("a file that is not in the package was handed over anyway")
	}
}

// Rewriting is what the flag is for, and it has to overwrite what is recorded
// rather than compare against it — otherwise the only way to accept a
// deliberate change would be to delete the file first.
func TestUpdatingRewritesWhatWasRecorded(t *testing.T) {
	dir := t.TempDir()
	moveTo(t, dir)

	path := filepath.Join(goldenDir, "said", "out.txt")
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatalf("making %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte("what was recorded\n"), 0o600); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}

	asking(t)

	watching := &says{}
	Compare(watching, "out.txt", []byte("what it produces now\n"))

	if watching.failed {
		t.Errorf("rewriting reported a difference: %v", watching.heard())
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	if want := "what it produces now\n"; string(got) != want {
		t.Errorf("%s holds %q, want %q", path, got, want)
	}
}

// moveTo runs the rest of a test from another directory, since where a golden
// lives is decided relative to the package that reads it.
//
// t.Chdir moves back when the test ends, and refuses to run under t.Parallel —
// which is the guarantee that matters here, because the working directory is
// process-wide and one test's move would otherwise be another's surprise.
func moveTo(t *testing.T, dir string) {
	t.Helper()

	t.Chdir(dir)
}

// asking turns rewriting on for the rest of a test.
func asking(t *testing.T) {
	t.Helper()

	f := flag.Lookup(updateName)
	if f == nil {
		t.Fatalf("no %s flag is registered", updateName)
	}

	was := f.Value.String()
	if err := f.Value.Set("true"); err != nil {
		t.Fatalf("setting the flag: %v", err)
	}
	t.Cleanup(func() {
		if err := f.Value.Set(was); err != nil {
			t.Fatalf("putting the flag back: %v", err)
		}
	})

	if !Updating() {
		t.Fatal("the flag was set and nothing noticed")
	}
}

// The flag is off unless somebody types it. A suite meant to notice changes
// that defaulted to rewriting would record every one of them instead, and pass.
func TestTheFlagIsOffUnlessAsked(t *testing.T) {
	f := flag.Lookup(updateName)
	if f == nil {
		t.Fatalf("no %s flag is registered", updateName)
	}
	if f.DefValue != "false" {
		t.Errorf("%s defaults to %q", updateName, f.DefValue)
	}
}

// A package that already has an -update flag keeps its own, since registering a
// second one is a panic at init in any test binary that links both — which is
// every package adopting this one that had already rolled its own comparison.
func TestTheFlagIsOnlyRegisteredOnce(t *testing.T) {
	if flag.Lookup(updateName) == nil {
		t.Fatalf("no %s flag is registered", updateName)
	}

	// Registering unconditionally is what panics, so running the registration
	// again is the whole of the test: it has to notice the flag already there.
	register()
}

// A golden that cannot be written is a mistake in the tree, and a suite that
// carried on would report every later run against a file that is not there.
func TestAGoldenThatCannotBeWritten(t *testing.T) {
	dir := t.TempDir()

	// A file where the directory of goldens should be, so that making the
	// directory cannot succeed.
	if err := os.WriteFile(filepath.Join(dir, goldenDir), []byte("not a directory"), 0o600); err != nil {
		t.Fatalf("writing the obstruction: %v", err)
	}

	watching := &says{}

	defer func() {
		if recover() == nil {
			t.Error("a golden that could not be written did not stop the test")
		}
		if !strings.Contains(watching.heard(), "making") {
			t.Errorf("the failure does not say what it could not do: %v", watching.heard())
		}
	}()

	record(watching, filepath.Join(dir, goldenDir, "said", "out.txt"), []byte("anything"))
}

// And one that cannot be written where the directory is fine is the same kind
// of mistake, reported the same way.
func TestAGoldenThatCannotBeWrittenToItsPath(t *testing.T) {
	dir := t.TempDir()

	// A directory where the file should be.
	path := filepath.Join(dir, goldenDir, "said", "out.txt")
	if err := os.MkdirAll(path, 0o750); err != nil {
		t.Fatalf("making the obstruction: %v", err)
	}

	watching := &says{}

	defer func() {
		if recover() == nil {
			t.Error("a golden that could not be written did not stop the test")
		}
		if !strings.Contains(watching.heard(), "writing") {
			t.Errorf("the failure does not say what it could not do: %v", watching.heard())
		}
	}()

	record(watching, path, []byte("anything"))
}

// A path that names a standard library package is one the go command would
// resolve from GOROOT, and everything else is somebody's module.
func TestWhatCountsAsTheStandardLibrary(t *testing.T) {
	cases := map[string]bool{
		"iter":             true,
		"encoding/json/v2": true,
		"unsafe":           true,

		// A dot in the first element is what tells a module path from a
		// standard one, wherever else in the path a dot turns up.
		"example.com/somewhere":          false,
		"github.com/okian/forge":         false,
		"golang.org/x/tools/go/packages": false,
		"gopkg.in/yaml.v3":               false,

		// Under GOROOT and still not importable from outside the tree it
		// belongs to, which is where a fast path would reach for.
		"internal/abi":                    false,
		"crypto/internal/fips140":         false,
		"vendor/golang.org/x/net/http2":   false,
		"cmd/vendor/rsc.io/markdown":      false,
		"encoding/json/internal/jsonopts": false,

		// Not paths at all.
		"":                         false,
		"/absolute/path":           false,
		"iter/../golang.org/x/mod": false,
	}

	for path, want := range cases {
		if got := standard(path); got != want {
			t.Errorf("standard(%q) is %v, want %v", path, got, want)
		}
	}
}

// The importer is shared across the process, so it has to survive being asked
// the same question twice and being asked for something it will refuse.
func TestTheImporterAnswersTwice(t *testing.T) {
	first, err := stdlib.Import("iter")
	if err != nil {
		t.Fatalf("resolving iter: %v", err)
	}
	second, err := stdlib.Import("iter")
	if err != nil {
		t.Fatalf("resolving iter again: %v", err)
	}
	if first != second {
		t.Error("the importer resolved one package twice over")
	}

	if _, err := stdlib.Import("example.com/somewhere"); err == nil {
		t.Error("the importer resolved something outside the standard library")
	}
}

// Two findings in one place still need an order, and the analyses fire in
// whichever order the set happens to list them.
func TestFindingsInOneSpot(t *testing.T) {
	same := func(name string, messages ...string) *analysis.Analyzer {
		return &analysis.Analyzer{
			Name: name,
			Doc:  "an analysis that reports in one place",
			Run: func(pass *analysis.Pass) (any, error) {
				for _, message := range messages {
					pass.Report(analysis.Diagnostic{Pos: pass.Files[0].Package, Message: message})
				}
				return nil, nil
			},
		}
	}
	only(t, same("zulu", "second", "first"), same("alpha", "only"))

	err := Compiles(compiling)
	if err == nil {
		t.Fatal("output the analyses object to was accepted")
	}

	report := err.Error()
	for was, then := range map[string]string{"alpha": "zulu", "first": "second"} {
		if strings.Index(report, was) > strings.Index(report, then) {
			t.Errorf("%q is reported after %q:\n%v", was, then, err)
		}
	}
}

// Two findings on one line are told apart by where along it they are, since a
// generated line can hold a whole method and a reader still has to find the
// part being complained about.
func TestFindingsOnOneLine(t *testing.T) {
	only(t, &analysis.Analyzer{
		Name: "counting",
		Doc:  "an analysis that reports twice along one line",
		Run: func(pass *analysis.Pass) (any, error) {
			at := pass.Files[0].Package
			pass.Report(analysis.Diagnostic{Pos: at + 4, Message: "further along"})
			pass.Report(analysis.Diagnostic{Pos: at, Message: "at the start"})
			return nil, nil
		},
	})

	err := Compiles(compiling)
	if err == nil {
		t.Fatal("output the analyses object to was accepted")
	}
	if strings.Index(err.Error(), "at the start") > strings.Index(err.Error(), "further along") {
		t.Errorf("the later column is reported first:\n%v", err)
	}
}

// A parse hands over a list of what it could not read, and anything else
// arriving here is still worth printing.
func TestWhatAParseSaid(t *testing.T) {
	if got := spread(errors.New("something else entirely")); len(got) != 1 || got[0] != "something else entirely" {
		t.Errorf("spread said %q", got)
	}
}

// What the type-checker reports carries a position, and something arriving
// without one keeps its message rather than being dropped for lacking a place
// to be reported at.
func TestAFailureWithNowhereToBe(t *testing.T) {
	failure := reported(token.NewFileSet(), errors.New("nowhere in particular"))

	if failure.Msg != "nowhere in particular" {
		t.Errorf("the failure says %q", failure.Msg)
	}
	if got := ordered([]types.Error{failure}); len(got) != 1 || !strings.Contains(got[0], "nowhere in particular") {
		t.Errorf("ordering it said %q", got)
	}

	// And one built without a file set at all still has to be orderable, since
	// resolving a position against nothing is a crash rather than an answer.
	if got := ordered([]types.Error{{Msg: "from nowhere at all"}, failure}); len(got) != 2 {
		t.Errorf("ordering said %q", got)
	}
}

// Nothing sets the flag in a binary that never registered one, and a value that
// is not a bool is not an instruction to rewrite anything.
func TestTheFlagIsNotThereToBeRead(t *testing.T) {
	was := flag.CommandLine
	t.Cleanup(func() { flag.CommandLine = was })

	flag.CommandLine = flag.NewFlagSet("empty", flag.ContinueOnError)
	if Updating() {
		t.Error("goldens are being rewritten with no flag registered at all")
	}

	flag.CommandLine = flag.NewFlagSet("odd", flag.ContinueOnError)
	flag.String(updateName, "", "a flag of another kind entirely")
	if err := flag.Set(updateName, "whenever"); err != nil {
		t.Fatalf("setting the flag: %v", err)
	}
	if Updating() {
		t.Error("goldens are being rewritten because a flag of another kind holds a word")
	}
}
