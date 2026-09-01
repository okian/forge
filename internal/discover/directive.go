package discover

import (
	"go/ast"
	"go/token"
	"strings"

	"github.com/okian/forge/internal/diag"
	"github.com/okian/forge/internal/model"
)

// DirectivePrefix marks a comment as carrying options for a layer.
//
// An alias rather than a second spelling of the same string: a directive above
// a field is read by the stage that walks the subject, and a prefix defined
// twice is two answers to what forge's own marker is.
const DirectivePrefix = model.DirectivePrefix

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

// Directive is one //forge: comment attached to a declaration.
//
// An alias, so that a directive read from above a declaration and one read from
// above a field of the subject are the same value and reach a layer through one
// type. What one is, and what it says about where its options were written, is
// documented on [model.Directive].
type Directive = model.Directive

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
