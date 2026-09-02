package forge_test

import (
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// The changelog is the one document here that nothing generates and nothing
// else checks.
//
// The rest of the prose in this tree is held to something: a doc comment is
// read beside the code it is about, the diagnostics index is rendered from the
// registry, and the worked examples are regenerated and compared. A convention
// with none of that behind it is a convention that lasts until the first
// hurried release, so the shape is checked here — the file exists, it opens
// with a place for what is not released yet, and every version heading is one a
// reader can order against the others. Whether an entry is *true* is what
// review is for.
//
// This package rather than a directory of its own, because the changelog is
// about the module and this is the module's own test. It reads a file beside it
// and needs nothing loaded.
const changelog = "CHANGELOG.md"

// unreleased is where a change goes before it has a version, and heading is
// what one looks like once it has.
//
// A version is `## v1.2.3 — 2026-09-02`, with the date the release was cut. The
// date is what makes the file readable at a distance: a reader deciding whether
// to upgrade wants to know how old the version they are on is, and a list of
// bare numbers does not say.
const unreleased = "## Unreleased"

var heading = regexp.MustCompile(
	`^## v(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(?:-([0-9A-Za-z.-]+))? — \d{4}-\d{2}-\d{2}$`)

// release is a version heading, parsed so that two of them can be ordered.
type release struct {
	// major, minor and patch are the numbers, and pre is what follows a hyphen.
	major, minor, patch int
	pre                 string

	// line is the heading as it was written, for a message to quote back.
	line string
}

// parsed reads a version heading, and reports whether the line was one.
func parsed(line string) (release, bool) {
	held := heading.FindStringSubmatch(line)
	if held == nil {
		return release{}, false
	}

	out := release{pre: held[4], line: line}
	for at, into := range []*int{&out.major, &out.minor, &out.patch} {
		number, err := strconv.Atoi(held[at+1])
		if err != nil {
			// Unreachable: the pattern above matches digits only. A version
			// long enough to overflow an int is not one anybody wrote.
			return release{}, false
		}
		*into = number
	}

	return out, true
}

// same reports whether two headings name one version, whatever dates they
// carry.
//
// The dates are not compared on purpose: a version listed twice is a mistake
// whether or not the two entries agree about when it went out, and comparing
// them would let the commoner form of the mistake through.
func (r release) same(other release) bool {
	return r.major == other.major && r.minor == other.minor &&
		r.patch == other.patch && r.pre == other.pre
}

// after reports whether this release is the later of the two.
//
// Numerically, and not as text. A changelog listing v0.10.0 above v0.9.0 is
// correct and in the right order, and a comparison on the written line would
// refuse it — for the tenth release of a project that is on its ninth, which is
// the one moment the check is load-bearing.
//
// A pre-release sorts below the version it leads to, which is what semantic
// versioning says and the opposite of what the text does: a space sorts below a
// hyphen, so "v1.0.0 — …" reads as earlier than "v1.0.0-rc.1 — …". Two
// pre-releases of one version are compared as text, which is right for rc.1
// against rc.2 and gives up on rc.9 against rc.10 — a project that gets there
// has a bigger problem than its changelog.
func (r release) after(other release) bool {
	switch {
	case r.major != other.major:
		return r.major > other.major
	case r.minor != other.minor:
		return r.minor > other.minor
	case r.patch != other.patch:
		return r.patch > other.patch
	case (r.pre == "") != (other.pre == ""):
		return r.pre == ""
	default:
		return r.pre > other.pre
	}
}

// The changelog is shaped the way a reader and a release both need.
func TestTheChangelogIsShapedForBothItsReaders(t *testing.T) {
	held, err := os.ReadFile(changelog)
	if err != nil {
		t.Fatalf("reading %s: %v", changelog, err)
	}

	lines := strings.Split(string(held), "\n")

	if lines[0] != "# Changelog" {
		t.Errorf("%s opens %q, want a title", changelog, lines[0])
	}

	// Unreleased first, and exactly once. It is where an entry is written in
	// the commit that earns it, so a file without one has nowhere to put the
	// next change and a file with two has two places to look.
	var (
		found    int
		versions []release
	)

	for at, line := range lines {
		switch {
		case line == unreleased:
			found++

			if len(versions) > 0 {
				t.Errorf("%s:%d: %s comes after a version, and belongs above them all",
					changelog, at+1, unreleased)
			}

		case strings.HasPrefix(line, "## "):
			held, is := parsed(line)
			if !is {
				t.Errorf("%s:%d: %q is neither %s nor a version and a date",
					changelog, at+1, line, unreleased)

				continue
			}
			versions = append(versions, held)
		}
	}

	if found != 1 {
		t.Errorf("%s holds %d %s headings, want one", changelog, found, unreleased)
	}

	// Newest first, which is the order somebody reading from the top wants and
	// the order a release adds to. A version listed twice is caught by the same
	// comparison and said differently, because "X is not above X" is not a
	// sentence anybody can act on.
	for i := 1; i < len(versions); i++ {
		above, below := versions[i-1], versions[i]

		switch {
		case above.same(below):
			t.Errorf("%s lists %q twice", changelog, below.line)
		case !above.after(below):
			t.Errorf("%s lists %q above %q, want the newest first",
				changelog, above.line, below.line)
		}
	}
}

// Every released version is a tag, and the changelog is where a reader looks
// for one — so the document says how a release is cut rather than leaving it to
// somebody's memory.
func TestTheChangelogSaysHowAReleaseIsCut(t *testing.T) {
	held, err := os.ReadFile(changelog)
	if err != nil {
		t.Fatalf("reading %s: %v", changelog, err)
	}

	if !strings.Contains(string(held), "docs/releasing.md") {
		t.Errorf("%s does not point at the release process", changelog)
	}
}
