package templates_test

import (
	"strings"
	"testing"

	"github.com/okian/forge/internal/templates"
)

// A comment's end is its start plus its length, and the gap between a doc
// comment and what it documents is one byte. Rewriting a name in place makes
// the text longer than the position says, so a comment whose last line grows
// ends up past the declaration beneath it — where it is either refused as
// documenting something above, or, inside a body, dropped from the output with
// nothing said.
//
// The names that grow are the ones a real template has: a helper takes a prefix,
// and a prefix is longer than nothing.
func TestACommentThatGrowsPastWhatItDocuments(t *testing.T) {
	const source = "package tmpl\n\n" +
		"type Collection[T any] []T\n\n" +
		"// counted counts, and this line ends with the name counted\n" +
		"type counted struct{ n int }\n\n" +
		"// Len counts, and this line ends with the name counted\n" +
		"func (c Collection[T]) Len() int { return len(c) }\n"

	out, diags := templates.Apply(templates.Template{Name: "toy", Source: []byte(source)}, ordinary, where)
	if !diags.Empty() {
		t.Fatalf("the template was refused:\n%s", diags.Render())
	}

	text := rendered(t, out)
	for _, want := range []string{
		"// personsCounted counts, and this line ends with the name personsCounted\ntype personsCounted struct",
		"// Len counts, and this line ends with the name personsCounted\nfunc (c Persons) Len() int",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("a doc comment did not stay with what it documents:\n%s", text)
		}
	}
}

// And a comment inside a body has nothing beneath it to be refused against, so
// one that outgrows its place is simply not printed — which is the same fault
// with nothing to notice it by.
func TestACommentInsideABodyThatGrows(t *testing.T) {
	const source = "package tmpl\n\n" +
		"type Collection[T any] []T\n\n" +
		"type counted struct{ n int }\n\n" +
		"func (c Collection[T]) Len() int {\n" +
		"\t// counted counted counted counted counted\n" +
		"\tn := len(c)\n" +
		"\t// and a second note, after the first\n" +
		"\treturn n\n" +
		"}\n"

	out, diags := templates.Apply(templates.Template{Name: "toy", Source: []byte(source)}, ordinary, where)
	if !diags.Empty() {
		t.Fatalf("the template was refused:\n%s", diags.Render())
	}

	text := rendered(t, out)
	for _, want := range []string{
		"// personsCounted personsCounted personsCounted personsCounted personsCounted",
		"// and a second note, after the first",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("a comment inside a body was lost:\n%s", text)
		}
	}
}

// Whatever the rewrite did to the lengths of things, what comes back describes
// the text it now holds — so a position read off it lands where it says.
func TestPositionsDescribeTheTextTheyPointAt(t *testing.T) {
	out := persons(t)
	text := rendered(t, out)

	for _, group := range out.Comments {
		for _, line := range group.List {
			at := out.Fset.Position(line.Pos())
			end := out.Fset.Position(line.End())

			if end.Offset-at.Offset != len(line.Text) {
				t.Errorf("a comment says it is %d bytes and holds %d: %q",
					end.Offset-at.Offset, len(line.Text), line.Text)
			}
		}
	}

	// And nothing was dropped on the way through.
	for _, want := range []string{"// Len reports", "// All walks", "// Backward walks"} {
		if !strings.Contains(text, want) {
			t.Errorf("the output does not hold %q:\n%s", want, text)
		}
	}
}
