package emit

import (
	"strings"
	"unicode/utf8"
)

// CommentWidth is how wide a generated comment's text may be before the two
// slashes and the space in front of it, so that a wrapped line comes to eighty
// columns.
const CommentWidth = 77

// Wrapped breaks a line of prose at the last space that fits.
//
// A generated file also holds comments a template supplied, and those are
// wrapped because somebody wrote them that way. A generated method whose
// one-line summary ran to ninety columns beside them would look like the
// machine-written half of a file that is meant to read as one thing.
//
// A word longer than the width is left long rather than broken: a type name is
// one word, and hyphenating it would produce something that is not the name.
//
// Here rather than in each layer that assembles a comment, because the width is
// a property of the file rather than of whoever wrote a line into it — and four
// copies of one rule is four places for it to stop being one rule.
//
// Counted in characters rather than in bytes, because a column is a character.
// An em-dash is one column and three bytes, so a line measured by its length
// breaks early wherever the prose uses one — and the prose here uses one
// wherever a sentence turns, so a comment with a dash in it wraps raggedly
// beside the comments a template supplied.
func Wrapped(text string, width int) []string {
	var out []string

	line := ""
	held := 0

	for word := range strings.FieldsSeq(text) {
		wide := utf8.RuneCountInString(word)

		switch {
		case line == "":
			line, held = word, wide
		case held+1+wide <= width:
			line, held = line+" "+word, held+1+wide
		default:
			out = append(out, line)
			line, held = word, wide
		}
	}

	if line != "" {
		out = append(out, line)
	}
	return out
}
