package discover

import (
	"go/ast"
	"go/token"
	"strings"

	"github.com/okian/forge/internal/diag"
)

// DirectivePrefix marks a comment as carrying options for a layer.
//
// There is no space after the slashes, which is the Go convention for a
// directive rather than prose. It is what makes gofmt leave the line alone and
// godoc keep it out of rendered documentation.
const DirectivePrefix = "//forge:"

// Diagnostics this package reports. Both are about a directive that will never
// be read, which is worth saying out loud: an option that is silently dropped
// leaves a declaration quietly misconfigured, which is worse than one that
// fails.
var (
	codeDirectiveNotAttached = diag.Register(3001, "directive applies to no declaration")
	codeDirectiveMalformed   = diag.Register(3002, "comment resembles a directive but is not one")
)

// directiveHint says where a directive has to go, by showing one.
const directiveHint = `write it immediately above a declaration, as in //forge:collection sort=Age above "type Persons Collection[Person]"`

// Directive is one //forge: comment attached to a declaration, collected but
// not interpreted.
//
// Whether the layer exists, whether its options are spelled correctly and
// whether their values name real fields are all questions for the stage that
// validates options against a layer's schema. This stage records what was
// written, and where.
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

// directives extracts the forge directives from a comment group.
//
// Anything that is not a directive — the prose of a doc comment, a //go: line
// meant for another tool — is passed over without comment.
func directives(fset *token.FileSet, group *ast.CommentGroup) []Directive {
	if group == nil {
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

// reportStrays records every directive in the file that no candidate claimed.
//
// A directive that lands nowhere is the failure this stage exists to catch.
// Nothing downstream will ever see it, so the declaration it was meant for is
// generated with its options quietly defaulted — a wrong result rather than a
// missing one, and the kind an author would not think to look for.
func reportStrays(fset *token.FileSet, file *ast.File, claimed map[int]bool, diags *diag.Set) {
	for _, group := range file.Comments {
		for _, comment := range group.List {
			pos := fset.Position(comment.Pos())

			if strings.HasPrefix(comment.Text, DirectivePrefix) {
				if !claimed[pos.Offset] {
					diags.Add(
						diag.New(codeDirectiveNotAttached, pos, "directive %s applies to no declaration", comment.Text).
							WithHint("%s", directiveHint),
					)
				}
				continue
			}

			if resemblesDirective(comment.Text) {
				diags.Add(
					diag.New(codeDirectiveMalformed, pos, "comment %s resembles a forge directive but is not one", comment.Text).
						WithHint("a directive is a line comment with no space after the marker: %scollection", DirectivePrefix),
				)
			}
		}
	}
}

// resemblesDirective reports whether a comment was probably meant as a
// directive even though it is not one.
//
// The two ways to get it wrong are a space after the comment marker, which
// gofmt will not fix because it cannot know the intent, and a block comment.
// Both are caught by looking for a layer name where one would be. Requiring
// that name to look like a layer keeps ordinary prose out of it: a sentence
// beginning "forge: " has a space where the layer would be and is left alone.
func resemblesDirective(text string) bool {
	var body string
	switch {
	case strings.HasPrefix(text, "//"):
		body = text[len("//"):]
	case strings.HasPrefix(text, "/*"):
		body = strings.TrimSuffix(text[len("/*"):], "*/")
	default:
		return false
	}

	body = strings.TrimLeft(body, " \t")
	if !strings.HasPrefix(body, "forge:") {
		return false
	}

	layer := body[len("forge:"):]
	if i := strings.IndexAny(layer, " \t"); i >= 0 {
		layer = layer[:i]
	}
	return isLayerName(layer)
}

// isLayerName reports whether s is spelled the way a layer's directive name is:
// one or more lower-case letters or digits.
func isLayerName(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') {
			return false
		}
	}
	return true
}
