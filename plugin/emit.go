package plugin

import (
	"go/ast"

	"github.com/okian/forge/internal/emit"
)

// CommentWidth is the column a generated doc comment wraps at.
//
// One width for the whole file, so that a comment a layer wrote and a comment
// forge wrote wrap the same way. A generated file is read in review far more
// often than it is produced, and two wrapping widths in one file read as two
// authors.
const CommentWidth = emit.CommentWidth

// Wrapped breaks a doc comment into lines no wider than a column.
//
// The text goes in as prose, with no comment markers, and comes back as the
// lines to write. Counting runes rather than bytes, so a sentence with an em
// dash in it wraps where it looks like it should.
func Wrapped(text string, width int) []string { return emit.Wrapped(text, width) }

// Qualifiers returns the package names these declarations refer to, as the set
// of identifiers written before a dot.
//
// What it is for is working out which imports a body still needs after the
// bodies have been built — a layer that reserved an import and then wrote
// nothing naming it would leave a file importing a package it does not use,
// which does not compile.
func Qualifiers(decls []ast.Decl) map[string]bool { return emit.Qualifiers(decls) }

// Reaching returns the imports these declarations still need, out of the ones
// offered.
//
// A layer answers Layer.Binds wide, before it knows what the declaration
// turns out to hold, so the set it reserved is usually larger than the set its
// output names. This is how the difference is dropped: what is left is what the
// file has to bind.
func Reaching(decls []ast.Decl, imports []Import) []Import {
	return emit.Reaching(decls, imports)
}
