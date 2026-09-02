package cli

import (
	"fmt"
	"strings"
	"testing"
)

// A generated file gains a method in the middle far more often than it changes
// throughout, and a difference that paired lines off by position would report
// every line after an insertion as changed — which is the report that makes a
// reader stop reading them.
func TestWhatAnInsertionLooksLike(t *testing.T) {
	was := lines([]byte("a\nb\nc\nd\ne\n"))
	now := lines([]byte("a\nb\nX\nc\nd\ne\n"))

	got := changes(was, now)

	if strings.Count(got, "+") != 1 || !strings.Contains(got, "+X\n") {
		t.Errorf("one inserted line was reported as:\n%s", got)
	}
	if strings.Contains(got, "-") {
		t.Errorf("an insertion removed something:\n%s", got)
	}
}

// The ends are trimmed before anything is lined up, which is nearly all of a
// generated file: two generations of one declaration share a header, a package
// clause, and everything before and after whatever moved.
func TestTheEndsAreNotReported(t *testing.T) {
	was := lines([]byte("head\nx\ntail\n"))
	now := lines([]byte("head\ny\ntail\n"))

	got := changes(was, now)

	if strings.Contains(got, "head") || strings.Contains(got, "tail") {
		t.Errorf("what did not change was reported:\n%s", got)
	}
	if !strings.Contains(got, "-x\n+y\n") {
		t.Errorf("the difference is\n%s", got)
	}

	// And what was passed over is counted rather than left out, so that what is
	// reported adds up to the file: one line at each end.
	if got != "@ 1 unchanged line\n-x\n+y\n@ 1 unchanged line\n" {
		t.Errorf("the ends are not accounted for:\n%s", got)
	}
}

// Two files that are the same differ in nothing.
func TestNoDifferenceAtAll(t *testing.T) {
	held := lines([]byte("a\nb\n"))

	if got := changes(held, held); got != "" {
		t.Errorf("two identical files differ by\n%s", got)
	}
}

// What is left out between two changes is named rather than elided silently,
// because a reader counting methods in a difference has to know the count is
// not the file's.
func TestWhatIsLeftOutIsSaid(t *testing.T) {
	var b strings.Builder
	b.WriteString("first\n")
	for range 20 {
		b.WriteString("same\n")
	}
	b.WriteString("last\n")

	was := lines([]byte(b.String()))
	now := lines([]byte("changed\n" + strings.Repeat("same\n", 20) + "also changed\n"))

	got := changes(was, now)

	if !strings.Contains(got, "unchanged lines") {
		t.Errorf("nothing says what was passed over:\n%s", got)
	}
	if strings.Count(got, "same") > 2*context {
		t.Errorf("more than the surroundings were kept:\n%s", got)
	}

	// Everything the file holds is either reported or counted, which is what
	// lets a reader take the report for the whole of the change. Counted by
	// what each line opens with, since a mark on the first line has no newline
	// in front of it and a count that missed it would come out right only by
	// being wrong twice.
	kept := 0
	for _, line := range strings.Split(strings.TrimSuffix(got, "\n"), "\n") {
		if strings.HasPrefix(line, " ") || strings.HasPrefix(line, "-") {
			kept++
		}
	}

	if reported := kept + passed(got); reported != len(was) {
		t.Errorf("%d lines are accounted for and the file has %d:\n%s", reported, len(was), got)
	}

	// One line passed over is one line, not "1 unchanged lines".
	if got := gap(1); !strings.Contains(got, "1 unchanged line") || strings.Contains(got, "lines") {
		t.Errorf("one line reads %q", got)
	}
}

// A pair too large to line up is reported as one replacing the other, which is
// correct, is what a reader of a file rewritten end to end wants anyway, and
// cannot exhaust anybody's memory.
func TestAPairTooLargeToLineUp(t *testing.T) {
	was := make([]string, budget+1)
	now := make([]string, budget+1)
	for i := range was {
		was[i], now[i] = "old", "new"
	}

	held := aligned(was, now)
	if len(held) != len(was)+len(now) {
		t.Fatalf("lined up %d lines, want everything twice", len(held))
	}
	if held[0].mark != "-" || held[len(held)-1].mark != "+" {
		t.Error("the two runs were not reported one after the other")
	}
}

// A file that was not there and one that is emptied are the two ends of the
// same operation.
func TestAFileArrivingAndLeaving(t *testing.T) {
	if got := changes(nil, lines([]byte("a\nb\n"))); got != "+a\n+b\n" {
		t.Errorf("a file arriving reads\n%s", got)
	}
	if got := changes(lines([]byte("a\nb\n")), nil); got != "-a\n-b\n" {
		t.Errorf("a file emptied reads\n%s", got)
	}
}

// An empty file is no lines rather than one empty one, which is what splitting
// on the trailing newline would leave.
func TestWhatAFileIsMadeOf(t *testing.T) {
	if got := lines(nil); got != nil {
		t.Errorf("nothing is made of %v", got)
	}
	if got := lines([]byte("only\n")); len(got) != 1 || got[0] != "only" {
		t.Errorf("one line is %v", got)
	}
	if got := lines([]byte("no newline")); len(got) != 1 {
		t.Errorf("a file with no final newline is %v", got)
	}
}

// passed adds up the lines a difference says it left out.
func passed(text string) int {
	total := 0

	for _, held := range strings.Split(text, "\n") {
		if !strings.HasPrefix(held, "@ ") {
			continue
		}

		count := 0
		if _, err := fmt.Sscanf(held, "@ %d unchanged", &count); err == nil {
			total += count
		}
	}
	return total
}
