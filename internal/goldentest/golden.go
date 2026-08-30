package goldentest

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// updateName is the flag that rewrites goldens instead of comparing against
// them.
//
// A flag rather than an environment variable, because it is typed once and read
// in the same breath as the test it applies to:
//
//	go test ./internal/layers/slice -run Golden -update
const updateName = "update"

// A package that already defines -update keeps its own, and this one reads
// through it, so that one flag has one meaning however a binary was assembled.
//
// The guard only helps between packages neither of which imports the other,
// because Go initialises what a package imports before the package itself: a
// test that imports this one always finds the flag already registered, and its
// own unconditional flag.Bool still panics. Adopting this package therefore
// means deleting the -update the adopting package had, which is the right
// direction anyway — two flags of one name would have to agree about what they
// mean.
func init() { register() }

// register defines the flag unless somebody already has.
func register() {
	if flag.Lookup(updateName) == nil {
		flag.Bool(updateName, false, "rewrite golden files instead of comparing against them")
	}
}

// Updating reports whether goldens are being rewritten rather than compared.
//
// Read through the flag rather than from a captured pointer, since the flag may
// belong to another package.
func Updating() bool {
	f := flag.Lookup(updateName)
	if f == nil {
		return false
	}
	set, err := strconv.ParseBool(f.Value.String())
	return err == nil && set
}

// T is the part of a running test this package needs.
//
// A named interface rather than *testing.T, because a harness that decides
// whether other tests pass has to be held to something itself — and most of
// what it is for is what it says when output has changed, which cannot be read
// back out of a real test. The testing package's own TB cannot be implemented
// from outside it by design, so this names the five methods used.
type T interface {
	Helper()
	Name() string
	Logf(format string, args ...any)
	Errorf(format string, args ...any)
	Fatalf(format string, args ...any)
}

// goldenDir is where a test's recorded output lives, relative to the package
// that runs it.
const goldenDir = "testdata"

// Compare holds bytes to the copy recorded for this test, and records them the
// first time or when asked to.
//
// A first run records what it produced and then fails. Recording is what makes
// a new layer's first review show the whole of its new output instead of a file
// nobody looked at; failing is what stops that from happening silently, since
// a passing run says nothing a person will read and a golden nobody has ever
// compared against is not evidence of anything. Rerunning is the whole of the
// fix, once somebody has read the file.
func Compare(t T, name string, got []byte) {
	t.Helper()

	path, err := where(t.Name(), name)
	if err != nil {
		t.Fatalf("%v", err)
		return
	}

	if Updating() {
		record(t, path, got)
		return
	}

	want, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		record(t, path, got)
		t.Errorf("%s had not been recorded, and now holds what this run produced; read it and run again", path)
		return
	}
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
		return
	}

	if !bytes.Equal(got, want) {
		// Naming the flag in the failure, because the reader who has just
		// decided the change is deliberate is one keystroke from accepting it
		// and will otherwise go looking for how.
		t.Errorf("%s is not what was recorded:\n%s\nif the change is deliberate, rerun with -%s",
			path, difference("recorded", string(want), "generated", string(got)), updateName)
	}
}

// where places a test's golden under the golden directory, refusing a name that
// would leave it.
//
// A test's name reaches this function unfiltered — testing builds it from
// whatever was handed to t.Run — and a golden nobody has recorded yet is
// written without being asked for. So a subtest called "../../.." is not a
// comparison that goes wrong, it is a write outside the package, and refusing
// it costs one Join and one check.
func where(test, name string) (string, error) {
	if !addressable(test) || !addressable(name) {
		return "", fmt.Errorf("%s in test %s does not name a file under %s", name, test, goldenDir)
	}
	return filepath.Join(goldenDir, filepath.FromSlash(test), filepath.FromSlash(name)), nil
}

// addressable reports whether a part names somewhere below where it starts —
// and exactly one such place.
//
// Judged before the join rather than after, because joining cleans. A part that
// climbs cancels against the one before it, so two tests climbing by different
// amounts land on one path and then share a golden while each believes it has
// its own. filepath.IsLocal refuses the ones that climb clear of the tree and
// allows the rest: TestParent/.. is local, it simply is not TestParent. So an
// element that names somewhere other than a step down — a climb, or a step
// nowhere — is refused on its own account.
func addressable(part string) bool {
	if part == "" || !filepath.IsLocal(filepath.FromSlash(part)) {
		return false
	}
	for element := range strings.SplitSeq(filepath.ToSlash(part), "/") {
		if element == "." || element == ".." {
			return false
		}
	}
	return true
}

// record writes a golden, making the directory it belongs in.
func record(t T, path string, content []byte) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatalf("making %s: %v", filepath.Dir(path), err)
		return
	}
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
}

// difference renders the first line two texts disagree about, with the lines
// around it, under the names of where each came from.
//
// Whole-file output is what a diff tool is for, and a test that prints two
// files leaves the reader to find the disagreement themselves. Generated files
// are long and mostly identical, which is the case where that costs most.
//
// Named by the caller because more than one pair of texts passes through here,
// and a formatter's opinion of a file printed under the word "recorded" sends
// the reader looking for a golden that was never involved.
func difference(wasName, was, isName, is string) string {
	wasLines, isLines := strings.Split(was, "\n"), strings.Split(is, "\n")

	at := 0
	for at < len(wasLines) && at < len(isLines) && wasLines[at] == isLines[at] {
		at++
	}

	var b strings.Builder
	b.WriteString("first difference at line ")
	b.WriteString(strconv.Itoa(at + 1))
	b.WriteString("\n\n")
	b.WriteString(wasName)
	b.WriteString(":\n")
	writeAround(&b, wasLines, at)
	b.WriteString("\n")
	b.WriteString(isName)
	b.WriteString(":\n")
	writeAround(&b, isLines, at)

	return b.String()
}

// around is how many lines either side of a disagreement are shown.
const around = 3

// widest is how many characters of one line are shown before the rest is
// summarised, since generated code can hold a line long enough to bury the
// report that mentions it.
const widest = 200

// writeAround writes the lines around one, numbered, marking the one they
// disagree about.
func writeAround(b *strings.Builder, lines []string, at int) {
	for i := max(at-around, 0); i < min(at+around+1, len(lines)); i++ {
		marker := "  "
		if i == at {
			marker = "> "
		}
		b.WriteString(marker)
		b.WriteString(strconv.Itoa(i + 1))
		b.WriteString(" | ")
		b.WriteString(shown(lines[i], i == at))
		b.WriteByte('\n')
	}

	// One text having run out is a difference too, and the alternative is a
	// report whose two halves stop at different places with nothing saying why.
	if at >= len(lines) {
		b.WriteString("> ")
		b.WriteString(strconv.Itoa(at + 1))
		b.WriteString(" | (nothing; the text ends here)\n")
	}
}

// shown renders one line of a report, quoting it when it is the line the two
// texts disagree about and shortening it when it is too long to read.
//
// Quoted only for the disagreement. Quoting everything would make the context
// unreadable, and quoting nothing hides the differences that are hardest to see
// and easiest to introduce: a trailing space, a tab that became spaces, a
// carriage return that a terminal then draws the rest of the line over the top
// of.
//
// Characters throughout, both to decide and to cut. Measuring in one unit and
// cutting in the other would announce a clip that had not happened on any line
// whose characters run wider than a byte — which the em dashes in this
// package's own comments do. What was left out is said outside the quotes,
// since inside them it reads as part of the line and takes the closing quote
// with it.
func shown(line string, quote bool) string {
	quoting := func(text string) string {
		if quote {
			return strconv.Quote(text)
		}
		return text
	}

	kept := []rune(line)
	if len(kept) <= widest {
		return quoting(line)
	}
	return fmt.Sprintf("%s… and %d characters more", quoting(string(kept[:widest])), len(kept)-widest)
}
