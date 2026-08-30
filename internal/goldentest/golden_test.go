package goldentest_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/okian/forge/internal/goldentest"
)

// A golden is a recorded copy of what generation produced, and holding output
// to it is what turns a layer quietly emitting something different into a diff
// somebody reads.
func TestOutputRoundTripsThroughItsGolden(t *testing.T) {
	pkg := goldentest.Package{
		Path: "model",
		Files: []goldentest.Source{subject, generated(
			"import (\n\t\"iter\"\n\t\"slices\"\n)\n\n" +
				"// Persons is a generated collection of Person.\n" +
				"type Persons []Person\n\n" +
				"// All walks the collection in order.\n" +
				"func (p Persons) All() iter.Seq[Person] { return slices.Values(p) }\n")},
	}

	goldentest.Check(t, pkg)
}

// The recorded copy is a file in the tree beside the test, which is the whole
// reason it works: it is reviewed like any other file.
func TestAGoldenIsAFileBesideTheTest(t *testing.T) {
	const name = "recorded.txt"

	want := []byte("what generation produced\n")
	goldentest.Compare(t, name, want)

	path := filepath.Join("testdata", filepath.FromSlash(t.Name()), name)
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	if string(got) != string(want) {
		t.Errorf("%s holds %q, want %q", path, got, want)
	}
}

// watcher stands in for a running test, so that what the harness says on
// failure can be read back. A harness that decides whether other tests pass is
// mostly its failure messages, and a real test will not hand those over.
type watcher struct {
	name   string
	failed bool
	said   []string
}

func (w *watcher) Helper()      {}
func (w *watcher) Name() string { return w.name }

func (w *watcher) Logf(format string, args ...any) {
	w.said = append(w.said, fmt.Sprintf(format, args...))
}

func (w *watcher) Errorf(format string, args ...any) {
	w.failed = true
	w.Logf(format, args...)
}

func (w *watcher) Fatalf(format string, args ...any) {
	w.Errorf(format, args...)
	panic(w)
}

// heard returns everything the harness said, as one text.
func (w *watcher) heard() string { return strings.Join(w.said, "\n") }

// Output that does not match what was recorded fails, and the failure says
// where the two part company rather than printing two files and leaving the
// reader to find it.
func TestOutputThatDoesNotMatchItsGolden(t *testing.T) {
	watching := recorded(t, "one\ntwo\nthree\nfour\nfive\n")

	goldentest.Compare(watching, "out.txt", []byte("one\ntwo\nCHANGED\nfour\nfive\n"))

	if !watching.failed {
		t.Fatal("output that does not match what was recorded was accepted")
	}

	// Generated files are long and mostly identical, which is where printing
	// both of them in full helps least.
	said := watching.heard()
	for _, want := range []string{"line 3", "\nrecorded:\n", "\ngenerated:\n", `> 3 | "three"`, `> 3 | "CHANGED"`} {
		if !strings.Contains(said, want) {
			t.Errorf("the failure does not mention %q:\n%s", want, said)
		}
	}

	// The two halves are labelled, and labelling them the wrong way round would
	// send the reader looking for a change in the file that did not change.
	if strings.Index(said, "three") > strings.Index(said, "CHANGED") {
		t.Errorf("what was recorded is shown as what was generated:\n%s", said)
	}
}

// The differences that cost most to read are the ones that look like no
// difference at all, so the line they disagree about is quoted.
func TestDifferencesNobodyCanSee(t *testing.T) {
	cases := map[string]struct {
		was, is string
		want    string
	}{
		"a space at the end of a line": {
			was:  "func F() {}\n",
			is:   "func F() {} \n",
			want: `"func F() {} "`,
		},
		"a tab that became spaces": {
			was:  "\tName string\n",
			is:   "    Name string\n",
			want: `"    Name string"`,
		},
		"a line ending from another platform": {
			was:  "package model\n",
			is:   "package model\r\n",
			want: `"package model\r"`,
		},
		"a name that changed only in case": {
			was:  "type Persons []Person\n",
			is:   "type persons []Person\n",
			want: `"type persons []Person"`,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			watching := recorded(t, tc.was)

			goldentest.Compare(watching, "out.txt", []byte(tc.is))

			if !watching.failed {
				t.Fatal("output that does not match what was recorded was accepted")
			}
			if !strings.Contains(watching.heard(), tc.want) {
				t.Errorf("the failure does not show %s:\n%s", tc.want, watching.heard())
			}
		})
	}
}

// One text having run out is a difference too, and a report whose two halves
// stop at different places with nothing saying why is a report about nothing.
func TestOutputThatStopsEarly(t *testing.T) {
	watching := recorded(t, "one\ntwo\n")

	goldentest.Compare(watching, "out.txt", []byte("one"))

	if !watching.failed {
		t.Fatal("output that stops early was accepted")
	}
	if !strings.Contains(watching.heard(), "the text ends here") {
		t.Errorf("nothing says one of the two ran out:\n%s", watching.heard())
	}
}

// Only the lines around the disagreement are shown. A report that printed the
// whole file would be the thing this one exists instead of.
func TestOnlyTheLinesAroundTheDisagreement(t *testing.T) {
	lines := make([]string, 0, 20)
	for i := range 20 {
		lines = append(lines, fmt.Sprintf("line %d", i+1))
	}
	was := strings.Join(lines, "\n") + "\n"

	lines[9] = "CHANGED"
	is := strings.Join(lines, "\n") + "\n"

	watching := recorded(t, was)
	goldentest.Compare(watching, "out.txt", []byte(is))

	said := watching.heard()
	for _, want := range []string{"line 7", "line 13"} {
		if !strings.Contains(said, want) {
			t.Errorf("the failure does not reach %q:\n%s", want, said)
		}
	}
	for _, gone := range []string{"line 6", "line 14"} {
		if strings.Contains(said, gone) {
			t.Errorf("the failure reaches as far as %q:\n%s", gone, said)
		}
	}
}

// A line long enough to bury the report that mentions it is shortened, since
// generated code holds lines nobody wrapped.
func TestALineTooLongToRead(t *testing.T) {
	watching := recorded(t, "// "+strings.Repeat("a", 4000)+"\n")

	goldentest.Compare(watching, "out.txt", []byte("// "+strings.Repeat("b", 4000)+"\n"))

	if !watching.failed {
		t.Fatal("output that does not match what was recorded was accepted")
	}
	if len(watching.heard()) > 4000 {
		t.Errorf("the failure is %d bytes long", len(watching.heard()))
	}
	if !strings.Contains(watching.heard(), "characters more") {
		t.Errorf("nothing says the line was shortened:\n%s", watching.heard())
	}
}

// recorded puts a golden in a directory of its own and runs the rest of the
// test from beside it, since where a golden lives is decided relative to the
// package that reads it.
func recorded(t *testing.T, content string) *watcher {
	t.Helper()

	dir := t.TempDir()
	watching := &watcher{name: "recorded"}

	path := filepath.Join(dir, "testdata", watching.name, "out.txt")
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatalf("making %s: %v", filepath.Dir(path), err)
	}
	if content != "" {
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatalf("writing %s: %v", path, err)
		}
	}

	inside(t, dir)

	return watching
}

// A golden nobody has recorded yet is written and then reported, so that a new
// layer's first run produces output to review — and does not pass while nobody
// has read it.
func TestAGoldenNobodyHasRecordedYet(t *testing.T) {
	dir := t.TempDir()
	inside(t, dir)

	watching := &watcher{name: "first"}
	goldentest.Compare(watching, "out.txt", []byte("the first thing it produced\n"))

	if !watching.failed {
		t.Errorf("a run that recorded a golden nobody has read passed:\n%s", watching.heard())
	}
	if !strings.Contains(watching.heard(), "read it and run again") {
		t.Errorf("nothing said the golden had just been recorded:\n%s", watching.heard())
	}

	path := filepath.Join(dir, "testdata", "first", "out.txt")
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	if want := "the first thing it produced\n"; string(got) != want {
		t.Errorf("%s holds %q, want %q", path, got, want)
	}

	// And the run after it, once the file has been read, passes.
	again := &watcher{name: "first"}
	goldentest.Compare(again, "out.txt", []byte("the first thing it produced\n"))
	if again.failed {
		t.Errorf("the run after the golden was recorded failed:\n%s", again.heard())
	}
}

// Both halves of a check run even when the first one failed. They answer
// different questions, and being told only that output changed — when what it
// changed into does not build — is being told the less useful half.
func TestCheckReportsBothHalves(t *testing.T) {
	watching := recorded(t, "// Code generated by forge. DO NOT EDIT.\n\npackage model\n")

	goldentest.Check(watching, goldentest.Package{
		Path: "model",
		Files: []goldentest.Source{{
			Name:      "out.txt",
			Content:   []byte("// Code generated by forge. DO NOT EDIT.\n\npackage model\n\ntype Persons []Person\n"),
			Generated: true,
		}},
	})

	said := watching.heard()
	if !strings.Contains(said, "is not what was recorded") {
		t.Errorf("the comparison did not report:\n%s", said)
	}
	if !strings.Contains(said, "undefined: Person") {
		t.Errorf("the compile gate did not run after the comparison failed:\n%s", said)
	}
}

// A package with no generated file in it compiles, records nothing and passes,
// so one forgotten field would turn the golden half off for a whole layer with
// nothing to notice.
func TestCheckWithNothingGenerated(t *testing.T) {
	watching := recorded(t, "")

	goldentest.Check(watching, goldentest.Package{
		Path:  "model",
		Files: []goldentest.Source{{Name: "out.go", Content: []byte("package model\n\ntype Persons []int\n")}},
	})

	if !watching.failed {
		t.Fatal("a package with no generated file in it passed")
	}
	if !strings.Contains(watching.heard(), "no generated file") {
		t.Errorf("the failure does not say what was missing:\n%s", watching.heard())
	}
}

// Only the generated files are recorded. A fixture is the test's own input, and
// recording it would hold the test to a copy of what it just wrote.
func TestOnlyGeneratedFilesAreRecorded(t *testing.T) {
	dir := t.TempDir()
	inside(t, dir)

	watching := &watcher{name: "recorded"}
	goldentest.Check(watching, goldentest.Package{
		Path:  "model",
		Files: []goldentest.Source{subject, generated("type Persons []Person\n")},
	})

	entries, err := os.ReadDir(filepath.Join(dir, "testdata", watching.name))
	if err != nil {
		t.Fatalf("reading the recorded goldens: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != "zz_forge_persons.go" {
		names := make([]string, 0, len(entries))
		for _, entry := range entries {
			names = append(names, entry.Name())
		}
		t.Errorf("recorded %v, want only the generated file", names)
	}
}

// A package naming one file twice is caught before either copy is recorded,
// since the run that wrote them would pass and the next one fail.
func TestCheckWithOneNameTwice(t *testing.T) {
	dir := t.TempDir()
	inside(t, dir)

	watching := &watcher{name: "twice"}
	goldentest.Check(watching, goldentest.Package{
		Path:  "model",
		Files: []goldentest.Source{generated("type A int\n"), generated("type B int\n")},
	})

	if !watching.failed {
		t.Fatal("a package naming one file twice passed")
	}
	if _, err := os.Stat(filepath.Join(dir, "testdata", watching.name)); !os.IsNotExist(err) {
		t.Error("a golden was recorded before the repeated name was noticed")
	}
}

// inside runs the rest of a test from another directory, since where a golden
// lives is decided relative to the package that reads it.
func inside(t *testing.T, dir string) {
	t.Helper()

	was, err := os.Getwd()
	if err != nil {
		t.Fatalf("reading the working directory: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("moving to %s: %v", dir, err)
	}

	t.Cleanup(func() {
		if err := os.Chdir(was); err != nil {
			t.Fatalf("moving back to %s: %v", was, err)
		}
	})
}

// Goldens are compared rather than rewritten unless somebody asks, or a suite
// meant to notice changes records every one of them instead.
func TestGoldensAreComparedByDefault(t *testing.T) {
	if goldentest.Updating() {
		t.Error("goldens are being rewritten without anybody asking")
	}
}

// A golden that cannot be read is a mistake in the tree rather than in the
// output, and it stops the test rather than being reported as a difference.
func TestAGoldenThatCannotBeRead(t *testing.T) {
	dir := t.TempDir()
	inside(t, dir)

	// A directory where the golden should be, which is readable and is not a
	// file.
	watching := &watcher{name: "unreadable"}
	if err := os.MkdirAll(filepath.Join("testdata", watching.name, "out.txt"), 0o750); err != nil {
		t.Fatalf("making the directory: %v", err)
	}

	defer func() {
		if recover() == nil {
			t.Error("a golden that cannot be read did not stop the test")
		}
		if !strings.Contains(watching.heard(), "reading") {
			t.Errorf("the failure does not say what it could not do:\n%s", watching.heard())
		}
	}()

	goldentest.Compare(watching, "out.txt", []byte("anything"))
}

// A test's name is built from whatever was handed to t.Run, and a golden nobody
// has recorded yet is written without being asked for — so a name that climbs
// out of the golden directory is a write outside the package rather than a
// comparison that goes wrong.
func TestANameThatLeavesTheGoldenDirectory(t *testing.T) {
	cases := map[string]struct{ test, name string }{
		"a test that climbs out":     {test: "TestSomething/../../../..", name: "out.txt"},
		"a golden that climbs out":   {test: "TestSomething", name: "../../../../out.txt"},
		"a golden with no name":      {test: "TestSomething", name: ""},
		"a golden somewhere else":    {test: "TestSomething", name: "/etc/passwd"},
		"a test somewhere else":      {test: "/etc", name: "passwd"},
		"a test that climbs to here": {test: "TestSomething/..", name: "out.txt"},
		"a test that goes nowhere":   {test: "TestSomething/.", name: "out.txt"},
		"a golden that goes nowhere": {test: "TestSomething", name: "./out.txt"},
	}

	for label, tc := range cases {
		t.Run(label, func(t *testing.T) {
			dir := t.TempDir()
			inside(t, dir)

			watching := &watcher{name: tc.test}

			defer func() {
				if recover() == nil {
					t.Error("a name that leaves the golden directory did not stop the test")
				}
				if !strings.Contains(watching.heard(), "does not name a file under testdata") {
					t.Errorf("the failure does not say what was wrong with it:\n%s", watching.heard())
				}
			}()

			goldentest.Compare(watching, tc.name, []byte("anything"))
		})
	}
}

// A subtest's name holds the slash that separates it from its parent, which is
// how one test's goldens end up in a directory of their own.
func TestASubtestRecordsBesideItsParent(t *testing.T) {
	dir := t.TempDir()
	inside(t, dir)

	watching := &watcher{name: "TestParent/a case"}
	goldentest.Compare(watching, "out.txt", []byte("what the subtest produced\n"))

	path := filepath.Join(dir, "testdata", "TestParent", "a case", "out.txt")
	if _, err := os.Stat(path); err != nil {
		t.Errorf("the subtest's golden is not beside its parent's: %v", err)
	}
}

// Names that climb are refused before they are joined, because joining cleans:
// two tests climbing by different amounts land on one path and then share a
// golden while each believes it has its own.
func TestNamesThatClimbToOnePlace(t *testing.T) {
	dir := t.TempDir()
	inside(t, dir)

	for _, name := range []string{"TestParent/..", "TestParent/a case/../.."} {
		t.Run(name, func(t *testing.T) {
			watching := &watcher{name: name}

			defer func() {
				if recover() == nil {
					t.Error("a name that climbs did not stop the test")
				}
			}()

			goldentest.Compare(watching, "out.txt", []byte("what this one produced\n"))
		})
	}

	if _, err := os.Stat(filepath.Join(dir, "testdata", "out.txt")); !os.IsNotExist(err) {
		t.Error("a golden was written where a climbing name would have put it")
	}
}
