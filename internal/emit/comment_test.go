package emit_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/okian/forge/internal/emit"
)

// A line of prose breaks at the last space that fits, so that a generated
// comment reads beside the ones a template supplied rather than running off the
// side of the file they share.
func TestWhereALineBreaks(t *testing.T) {
	cases := map[string]struct {
		text  string
		width int
		want  []string
	}{
		"nothing at all": {text: "", width: 20},
		"spaces only":    {text: " \t\n ", width: 20},
		"shorter than the width": {
			text: "one two three", width: 20, want: []string{"one two three"},
		},
		"exactly the width": {
			text: "aaaa bbbb", width: 9, want: []string{"aaaa bbbb"},
		},
		"one over the width": {
			text: "aaaa bbbb", width: 8, want: []string{"aaaa", "bbbb"},
		},
		"a word longer than the width is left long": {
			text: "a supercalifragilistic b", width: 8, want: []string{"a", "supercalifragilistic", "b"},
		},
		"a width nothing fits in": {
			text: "a b c", width: 0, want: []string{"a", "b", "c"},
		},
		"tabs and newlines are spaces": {
			text: "one\ttwo\nthree", width: 7, want: []string{"one two", "three"},
		},
	}

	for name, want := range cases {
		t.Run(name, func(t *testing.T) {
			if got := emit.Wrapped(want.text, want.width); !slices.Equal(got, want.want) {
				t.Errorf("wrapped to %q, want %q", got, want.want)
			}
		})
	}
}

// No line is ever longer than the width unless one word is, which is the
// property the whole thing exists for.
func TestNoLineIsLongerThanItHasToBe(t *testing.T) {
	held := "The two numbers this is made of are written out rather than taken from " +
		"somewhere else, so that what a value hashes to is decided here."

	for _, line := range emit.Wrapped(held, emit.CommentWidth) {
		if len(line) > emit.CommentWidth && strings.Contains(line, " ") {
			t.Errorf("a line of %d characters holds more than one word: %q", len(line), line)
		}
	}
}

// Wrapping loses nothing: the words come out in the order they went in.
func TestWrappingLosesNothing(t *testing.T) {
	held := "one two three four five six seven eight nine ten"

	if got := strings.Join(emit.Wrapped(held, 11), " "); got != held {
		t.Errorf("wrapping gave back %q, want %q", got, held)
	}
}
