package templates

import (
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"strings"
)

// settle prints a rewritten tree and reads it back, so that every position in
// it describes the text it now holds.
//
// Rewriting is done on the tree, and a tree's positions are byte offsets into
// the source it was parsed from. Renaming Collection to Persons makes every
// node three bytes shorter than its position says; renaming counted to
// personsCounted makes one seven bytes longer. Nothing notices, because a
// printer takes positions as hints for nodes — except for comments, which it
// places by position alone.
//
// The gap between a doc comment and what it documents is one byte. A comment
// whose last line grows by two has an end past the declaration beneath it, and
// is then either refused as documenting something above it or, for a comment
// inside a body, quietly dropped from the output. Both are produced by an
// ordinary template: a doc comment ending in the name of the type parameter is
// enough.
//
// Printing and reparsing costs one pass over a file that is a few hundred lines
// at most, and it is the only way to be sure: any arithmetic that shifted
// positions by hand would have to know every node the rewrite touched, which is
// the thing the rewrite does not track.
func settle(decls []ast.Decl, comments []*ast.CommentGroup, from *token.FileSet) ([]ast.Decl, []*ast.CommentGroup, *token.FileSet, error) {
	var b strings.Builder

	// The file carries its own package clause, which the printer writes; the
	// name is nobody's, since what comes back out is a list of declarations and
	// the clause is read and dropped.
	config := printer.Config{Mode: printer.TabIndent, Tabwidth: 8}
	source := &ast.File{
		Name:     ast.NewIdent("settled"),
		Decls:    decls,
		Comments: comments,
	}
	if err := config.Fprint(&b, from, source); err != nil {
		return nil, nil, nil, err
	}

	fset := token.NewFileSet()
	read, err := parser.ParseFile(fset, "settled.go", b.String(), parser.ParseComments|parser.SkipObjectResolution)
	if err != nil {
		return nil, nil, nil, err
	}

	return read.Decls, read.Comments, fset, nil
}
