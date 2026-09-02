// Package embedded carries a Go file this repository compiles into the package
// a layer generates into.
//
// A layer that needs a helper beside its output has two ways to write one: as a
// string, or as a file. A string is code nothing checks until somebody's
// generated file fails to build — it is not compiled here, not vetted here, and
// not exercised by any test here. A file is ordinary Go, and this is what
// carries it across: the layer embeds it, hands it over, and what lands in the
// author's package is the same code this module's own build has already been
// through.
//
// It is not a template. What comes through here depends on nothing about the
// subject, which is why it can be one file rather than one per declaration —
// and why a layer that needs the subject's name in what it writes wants the
// template engine instead.
package embedded

import (
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"

	"github.com/okian/forge/plugin"
)

// Unit returns an embedded file's declarations as a contribution to somebody
// else's package.
//
// name is what a failure is reported against, source the file's bytes, and
// binds every import the file is allowed to have together with the name it is
// bound to. The list decides rather than describes: an import the file gained
// and the list did not is refused here, where it is a failing test in this
// repository, rather than emitted into a package that then names something it
// never imported.
func Unit(name string, source []byte, binds []plugin.Import) (plugin.Unit, error) {
	fset := token.NewFileSet()

	file, err := parser.ParseFile(fset, name, source, parser.ParseComments)
	if err != nil {
		return plugin.Unit{}, fmt.Errorf("%s does not parse: %w", name, err)
	}

	decls := carried(file)
	if len(decls) == 0 {
		return plugin.Unit{}, errors.New(name + " declares nothing")
	}

	if wrong := accounted(name, file, fset, binds); wrong != "" {
		return plugin.Unit{}, errors.New(wrong)
	}

	imports := make([]plugin.Import, 0, len(binds))
	for _, one := range binds {
		imports = append(imports, plugin.Import{Path: one.Path, Name: one.Name, Aliased: one.Aliased})
	}

	return plugin.Unit{
		Decls:    decls,
		Comments: file.Comments,
		Fset:     fset,
		Imports:  plugin.Reaching(decls, imports),
	}, nil
}

// carried returns the declarations that go into somebody else's package, which
// is everything but the imports.
//
// The package clause names the package the file was written in and the imports
// are re-derived from what the declarations turn out to name, so carrying
// either across would put this repository's own names in a file that is not its.
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

// accounted reports an import of the file that the list does not mention, or
// nothing.
//
// It fails on the first run of the layer's own tests, which is where an import
// added to the file beside it is cheapest to notice — and long before a package
// somewhere else is written a file naming a package it does not import.
func accounted(name string, file *ast.File, fset *token.FileSet, binds []plugin.Import) string {
	for _, one := range file.Imports {
		path, err := strconv.Unquote(one.Path.Value)
		if err != nil {
			return name + " imports " + one.Path.Value + ", which is not a path"
		}

		known := false
		for _, held := range binds {
			known = known || held.Path == path
		}
		if !known {
			return name + " imports " + path + " at " + fset.Position(one.Pos()).String() +
				", which nothing recorded a bound name for"
		}
	}
	return ""
}
