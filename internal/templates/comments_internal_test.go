package templates

import (
	"go/ast"
	"testing"
)

// A comment is rewritten by whole words, so that a template's own names are
// replaced and prose that merely contains them is not. Getting this wrong is
// quiet: the output still compiles, and only a reader notices that a sentence
// now says something else.
func TestWhichWordsACommentLoses(t *testing.T) {
	words := map[string]string{"Collection": "Persons", "counted": "personsCounted"}

	cases := map[string]string{
		// The whole word, wherever it stands.
		"// Collection is a thing":     "// Persons is a thing",
		"// see Collection":            "// see Persons",
		"// (Collection) and more":     "// (Persons) and more",
		"// Collection, counted, more": "// Persons, personsCounted, more",

		// Part of a longer name, which is somebody else's word.
		"// Collections are plural": "// Collections are plural",
		"// myCollection is mine":   "// myCollection is mine",
		"// Collection2 is second":  "// Collection2 is second",
		"// counted_up is separate": "// counted_up is separate",

		// The type parameter is left alone: a letter standing alone in a
		// sentence is more often a word than a reference, and replacing it
		// corrupts prose to fix a mention a reader would have understood.
		"// The T is the element": "// The T is the element",
		"// a T-shaped hole":      "// a T-shaped hole",
		"// That is THAT":         "// That is THAT",

		// Nothing to replace.
		"// nothing here": "// nothing here",
		"//":              "//",
	}

	for from, want := range cases {
		if got := replace(from, words); got != want {
			t.Errorf("%q became %q, want %q", from, got, want)
		}
	}
}

// A group is rewritten line by line, since a comment written as a block is one
// group and every line of it may name something.
func TestEveryLineOfACommentIsRewritten(t *testing.T) {
	group := &ast.CommentGroup{List: []*ast.Comment{
		{Text: "// Collection holds elements."},
		{Text: "//"},
		{Text: "// Each is a T."},
	}}

	reword([]*ast.CommentGroup{group}, map[string]string{"Collection": "Persons"})

	want := []string{"// Persons holds elements.", "//", "// Each is a T."}
	for i, line := range group.List {
		if line.Text != want[i] {
			t.Errorf("line %d is %q, want %q", i+1, line.Text, want[i])
		}
	}
}

// A name rewritten into another name is not then rewritten again, or a
// container renamed to something a helper is also called would be renamed
// twice and land somewhere nobody asked for.
func TestARewrittenNameIsNotRewrittenAgain(t *testing.T) {
	words := map[string]string{"Collection": "Persons", "Persons": "Nowhere"}

	if got := replace("// Collection", words); got != "// Persons" {
		t.Errorf("a name was rewritten twice: %q", got)
	}
}

// A prefix joins to what the template wrote, so a name is capitalised where it
// opens in lower case and left alone otherwise — including when there is no
// name at all.
func TestHowANameTakesAPrefix(t *testing.T) {
	cases := map[string]string{
		"counted": "Counted",
		"Holder":  "Holder",
		"_thing":  "_thing",
		"":        "",
		"x":       "X",
		"ünicode": "ünicode",
	}

	for from, want := range cases {
		if got := upper(from); got != want {
			t.Errorf("upper(%q) is %q, want %q", from, got, want)
		}
	}
}
