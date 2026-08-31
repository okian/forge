package templates

import (
	"go/ast"
	"maps"
	"strings"
)

// reword rewrites the names a template's comments mention.
//
// A doc comment opens with the name of the thing it documents, so a template's
// comments carried through unchanged would document types that are not there:
// "Collection is the container" above a declaration called Persons. The
// comments are the half of generated code a person actually reads, and one that
// names the wrong type is worse than none at all.
//
// Whole words only. A name is replaced where it stands alone, so that Collection
// becomes Persons and Collections, myCollection and Collection2 are left as
// somebody's prose. That is a smaller rule than it looks: what is renamed here
// is what the template declares, and a template's comments talk about what the
// template declares.
//
// The type parameter is not among them. It is one or two letters, and a letter
// standing alone in a sentence is far more often a word than a reference —
// "the T switch", "a T-shaped hole" — so replacing it corrupts prose to fix a
// mention that a reader would have understood either way.
func reword(groups []*ast.CommentGroup, renamed map[string]string) {
	words := make(map[string]string, len(renamed))
	maps.Copy(words, renamed)

	for _, group := range groups {
		for _, line := range group.List {
			line.Text = replace(line.Text, words)
		}
	}
}

// replace rewrites whole words in a comment.
//
// One pass over the text rather than one pass per name, so that a name rewritten
// into another name is not then rewritten again — Collection becoming Persons
// must not go on to become whatever Persons maps to.
func replace(text string, words map[string]string) string {
	var b strings.Builder

	for at := 0; at < len(text); {
		width := word(text[at:])
		if width == 0 {
			b.WriteByte(text[at])
			at++
			continue
		}

		found := text[at : at+width]
		if to, is := words[found]; is {
			b.WriteString(to)
		} else {
			b.WriteString(found)
		}
		at += width
	}

	return b.String()
}

// word returns the length of the identifier at the start of text, or zero when
// it does not start with one.
//
// Identifier rather than word, because that is what is being replaced: a name
// touching a digit or an underscore is part of a longer name and is not the one
// the template declared.
func word(text string) int {
	if text == "" || !letter(text[0]) {
		return 0
	}

	for i := 1; i < len(text); i++ {
		if !letter(text[i]) && !digit(text[i]) {
			return i
		}
	}
	return len(text)
}

// letter reports whether a byte can open a Go identifier.
//
// ASCII only. A name outside it is legal Go and is not a name forge's own
// templates use, so the cost of leaving one alone is a comment that keeps a
// name it should have lost — visible, and in forge's own source.
func letter(b byte) bool {
	return b == '_' || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}

// digit reports whether a byte can continue a Go identifier.
func digit(b byte) bool { return b >= '0' && b <= '9' }
