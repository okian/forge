package model

import (
	"go/ast"
	"go/token"
	"strings"
)

// DirectivePrefix marks a comment as carrying options for a layer.
//
// The Go convention for a directive is a line comment with no space after the
// slashes, which is what keeps it out of the rendered documentation: gofmt
// preserves it verbatim rather than reflowing it into the prose above.
const DirectivePrefix = "//forge:"

// Directive is one //forge: comment, collected but not interpreted.
//
// Whether the layer exists, whether its options are spelled correctly and
// whether their values name real fields are all questions for the stage that
// validates options against a layer's schema. What is recorded here is what was
// written, and where.
//
// It lives in this package rather than beside the scan that first read one
// because two stages read directives from two places: one above a declaration,
// one above a field of the subject. Two types would be one grammar described
// twice, and the second description is the one that would fall behind.
type Directive struct {
	// Layer is the text between the prefix and the first space or tab: the
	// "collection" of //forge:collection sort=Age. It is empty for a directive
	// written with nothing after the prefix, which is left to be reported
	// rather than dropped here.
	Layer string

	// Args is the rest of the line, with surrounding space removed.
	Args string

	// Text is the comment exactly as written, including the prefix.
	Text string

	// ArgsOffset is the byte offset of Args within Text, so that a diagnostic
	// about one option can point at the option rather than at the line. For a
	// directive with no arguments it is the length of Text.
	ArgsOffset int

	// Pos is the position of the comment's first character.
	Pos token.Position
}

// String returns the directive as it was written.
func (d Directive) String() string { return d.Text }

// ArgsPos returns the position of the first argument, which is where a
// diagnostic about the directive's options starts counting from.
func (d Directive) ArgsPos() token.Position {
	pos := d.Pos
	pos.Column += d.ArgsOffset
	pos.Offset += d.ArgsOffset
	return pos
}

// Directives extracts the forge directives from a comment group.
//
// Anything that is not a directive — the prose of a doc comment, a //go: line
// meant for another tool — is passed over without comment. A comment that
// resembles a directive and is not one is a mistake worth reporting, but not
// here: this is called both where a stray directive is an error and where the
// same comment is somebody else's, and the caller is what knows which.
func Directives(fset *token.FileSet, group *ast.CommentGroup) []Directive {
	if fset == nil || group == nil {
		return nil
	}

	var found []Directive
	for _, comment := range group.List {
		text := comment.Text
		if !strings.HasPrefix(text, DirectivePrefix) {
			continue
		}

		body := text[len(DirectivePrefix):]

		// The layer name runs to the first space or tab; everything after it is
		// the layer's own business.
		layer, args := body, ""
		if i := strings.IndexAny(body, " \t"); i >= 0 {
			layer, args = body[:i], body[i:]
		}

		offset := len(DirectivePrefix) + len(layer)
		trimmed := strings.TrimLeft(args, " \t")
		offset += len(args) - len(trimmed)

		found = append(found, Directive{
			Layer:      layer,
			Args:       strings.TrimRight(trimmed, " \t"),
			Text:       text,
			ArgsOffset: offset,
			Pos:        fset.Position(comment.Pos()),
		})
	}

	return found
}

// Written returns the directives naming one layer, in the order they appear.
//
// A field carrying two directives for one layer is not rejected here. What two
// of them mean is the layer's question — one may repeat an option and one may
// add to it — and a stage that dropped the second would be answering it.
func Written(held []Directive, layer string) []Directive {
	var found []Directive
	for _, one := range held {
		if one.Layer == layer {
			found = append(found, one)
		}
	}
	return found
}
