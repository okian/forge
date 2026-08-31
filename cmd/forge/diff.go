package main

import (
	"fmt"
	"strings"
)

// budget is the largest pair of runs this will line up properly.
//
// Lining two runs up costs a table of one cell per pair, which is nothing for
// the tens of lines a generated file usually differs by and is hundreds of
// megabytes for two files that share nothing. Past the budget the answer
// becomes the blunt one — everything old, then everything new — which is
// correct, is what a reader of a file rewritten end to end would want anyway,
// and cannot exhaust anybody's memory.
const budget = 2000

// changes renders the difference between two files as the lines that differ.
//
// Lined up rather than paired off by position. A generated file gains a method
// in the middle far more often than it changes throughout, and a diff that
// compared the first line to the first line would report every line after an
// insertion as changed — which is the report that makes a reader stop reading
// diffs.
func changes(was, now []string) string {
	var b strings.Builder

	// The ends first, which is nearly all of it: two generations of one
	// declaration share a header, a package clause, and everything before and
	// after whatever moved.
	head := 0
	for head < len(was) && head < len(now) && was[head] == now[head] {
		head++
	}

	tail := 0
	for tail < len(was)-head && tail < len(now)-head &&
		was[len(was)-1-tail] == now[len(now)-1-tail] {
		tail++
	}

	was, now = was[head:len(was)-tail], now[head:len(now)-tail]
	if len(was) == 0 && len(now) == 0 {
		// The ends met, so the two files are the same and there is nothing to
		// say about how much of that was passed over.
		return ""
	}

	middle := aligned(was, now)

	// The ends are counted along with everything else that was passed over, so
	// that what is reported adds up to the file. Trimming them first is how the
	// alignment stays cheap; leaving them out of the count is how a reader ends
	// up thinking a four-line difference is a four-line file.
	for _, one := range trimmed(middle, head, tail) {
		fmt.Fprintf(&b, "%s%s\n", one.mark, one.text)
	}
	return b.String()
}

// context is how many unchanged lines are kept on each side of a change.
//
// Enough to see what a change is next to, and not enough to have to scroll. A
// generated file's changes are usually two — a fingerprint at the top and a
// method somewhere in the middle — and everything between them is unchanged; a
// diff that printed it would be a report whose useful part a reader has to
// hunt for.
const context = 3

// trimmed keeps the changed lines and their surroundings, and says how much it
// left out.
//
// What it leaves out is named rather than elided silently, because a reader
// counting methods in a diff has to know the count is not the file's.
func trimmed(all []line, before, after int) []line {
	keep := make([]bool, len(all))
	for i, one := range all {
		if one.mark == " " {
			continue
		}
		for j := max(0, i-context); j <= min(len(all)-1, i+context); j++ {
			keep[j] = true
		}
	}

	var out []line

	// What was trimmed off the front before anything was lined up is the first
	// thing passed over, and what was trimmed off the back is the last.
	skipped := before

	flush := func() {
		if skipped > 0 {
			out = append(out, line{mark: "@", text: gap(skipped)})
			skipped = 0
		}
	}

	for i, one := range all {
		if !keep[i] {
			skipped++
			continue
		}
		flush()
		out = append(out, one)
	}

	skipped += after
	flush()

	return out
}

// gap says how much of a file a difference passed over.
func gap(lines int) string {
	if lines == 1 {
		return " 1 unchanged line"
	}
	return fmt.Sprintf(" %d unchanged lines", lines)
}

// line is one line of a rendered difference: what it says and whether it is
// going or arriving.
type line struct {
	mark string
	text string
}

// aligned pairs two runs of lines by their longest common subsequence, so that
// what did not move is not reported as having moved.
func aligned(was, now []string) []line {
	if len(was)*len(now) > budget*budget {
		return blunt(was, now)
	}

	// common[i][j] is how many lines the tails starting at i and j share.
	common := make([][]int, len(was)+1)
	for i := range common {
		common[i] = make([]int, len(now)+1)
	}
	for i := len(was) - 1; i >= 0; i-- {
		for j := len(now) - 1; j >= 0; j-- {
			if was[i] == now[j] {
				common[i][j] = common[i+1][j+1] + 1
				continue
			}
			common[i][j] = max(common[i+1][j], common[i][j+1])
		}
	}

	var out []line

	i, j := 0, 0
	for i < len(was) && j < len(now) {
		switch {
		case was[i] == now[j]:
			// Shared, and inside the part that differs, so it is context: a
			// closing brace between two changed methods reads as nothing at
			// all without it.
			out = append(out, line{mark: " ", text: was[i]})
			i, j = i+1, j+1
		case common[i+1][j] >= common[i][j+1]:
			out = append(out, line{mark: "-", text: was[i]})
			i++
		default:
			out = append(out, line{mark: "+", text: now[j]})
			j++
		}
	}

	for ; i < len(was); i++ {
		out = append(out, line{mark: "-", text: was[i]})
	}
	for ; j < len(now); j++ {
		out = append(out, line{mark: "+", text: now[j]})
	}

	return out
}

// blunt reports two runs as one replacing the other, which is the answer for a
// pair too large to line up and the right one for a file rewritten throughout.
func blunt(was, now []string) []line {
	out := make([]line, 0, len(was)+len(now))

	for _, text := range was {
		out = append(out, line{mark: "-", text: text})
	}
	for _, text := range now {
		out = append(out, line{mark: "+", text: text})
	}
	return out
}
