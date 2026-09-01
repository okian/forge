package validate

import (
	_ "embed"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"

	"github.com/okian/forge/internal/emit"
	"github.com/okian/forge/internal/layer"
	"github.com/okian/forge/internal/model"
)

// sharedKey is what the error types are contributed under.
//
// One key for the package rather than one per subject, because there is one
// copy of them however many declarations asked: a package holding two
// ValidationError types does not compile, and the key is what says the two
// contributions are the same thing.
const sharedKey = "validate: what a check reports"

// failures is the source of the types every check reports through, embedded
// from the package beside this one.
//
// Embedded rather than quoted, so that what is emitted is Go this repository's
// own build compiles, its own vet reads and its own tests exercise. Code that
// is only ever a string is code nothing checks until somebody's generated file
// fails to build.
//
// It is not a template: nothing in it depends on the subject, so there is
// nothing to rewrite and no reason for it to be anything but a file.
//
//go:embed shared/shared.go
var failures []byte

// sharedImports names what the shared file imports, and what each binds.
//
// Written down rather than read off the file, so that an import added to it
// is a change somebody makes here as well — and so that what a run narrows
// against is a list rather than a parse of the same bytes twice.
var sharedImports = []model.Import{
	{Path: "errors", Name: "errors"},
	{Path: "strconv", Name: "strconv"},
	{Path: "strings", Name: "strings"},
}

// shared returns the error types as a contribution the package holds once.
func shared() (layer.Unit, error) {
	fset := token.NewFileSet()

	file, err := parser.ParseFile(fset, "shared.go", failures, parser.ParseComments)
	if err != nil {
		return layer.Unit{}, fmt.Errorf("validate: the shared failures do not parse: %w", err)
	}

	imports := make([]emit.Import, 0, len(sharedImports))
	for _, one := range sharedImports {
		imports = append(imports, emit.Import{Path: one.Path, Name: one.Name})
	}

	decls := carried(file)
	if len(decls) == 0 {
		return layer.Unit{}, errors.New("validate: the shared failures declare nothing")
	}

	if wrong := accounted(file, fset); wrong != "" {
		return layer.Unit{}, fmt.Errorf("validate: %s", wrong)
	}

	return layer.Unit{
		Decls:    decls,
		Comments: file.Comments,
		Fset:     fset,
		Imports:  emit.Reaching(decls, imports),
	}, nil
}

// carried returns the declarations that go into somebody else's package, which
// is everything but the imports.
//
// The package clause names this package and the imports are re-derived from
// what the declarations turn out to name, so carrying either across would put
// this package's name in a file that is not its.
func carried(file *ast.File) []ast.Decl {
	out := make([]ast.Decl, 0, len(file.Decls))
	for _, decl := range file.Decls {
		if gen, is := decl.(*ast.GenDecl); is && gen.Tok == token.IMPORT {
			continue
		}
		out = append(out, decl)
	}
	return out
}

// accounted reports an import of the shared file that the list above does not
// mention, or nothing.
//
// The list decides what a generated file may bind, so an import missing from it
// is a package the output names and does not import. It fails on the first run
// of this package's tests, which is where an import added to the file beside
// this one is cheapest to notice.
func accounted(file *ast.File, fset *token.FileSet) string {
	for _, one := range file.Imports {
		path, err := strconv.Unquote(one.Path.Value)
		if err != nil {
			return "the shared failures import " + one.Path.Value + ", which is not a path"
		}

		known := false
		for _, held := range sharedImports {
			known = known || held.Path == path
		}
		if !known {
			return "the shared failures import " + path + " at " +
				fset.Position(one.Pos()).String() + ", which nothing recorded a bound name for"
		}
	}
	return ""
}
