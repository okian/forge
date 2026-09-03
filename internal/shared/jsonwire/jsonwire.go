package jsonwire

import (
	_ "embed"
	"go/token"

	"github.com/okian/forge/internal/layer"
	"github.com/okian/forge/internal/model"
	"github.com/okian/forge/internal/templates"
)

// bodies is the runtime as it is emitted, embedded from the package beside
// this one so that what is written into somebody's repository is a file the
// ordinary build compiles and this package's own tests exercise.
//
//go:embed tmpl/tmpl.go
var bodies []byte

// Name is what the runtime is called in generated code, by way of the one type
// in it a codec names.
//
// A file of functions has no single name, and the requiring wants one: what it
// records is whether a package has already been given this, so any name that
// identifies the file will do. The type is the honest choice among them
// because it is the one a generated decoder writes down — a decoder declares a
// jsonNames to catch a member written twice — so the reference names something
// generated code actually refers to rather than a label invented for the
// registry.
//
// The cost is the same one every shared name carries: an author who already
// has a type called jsonNames gets a redeclaration in a file they did not
// write. It is unexported and prefixed, which makes it unlikely rather than
// impossible, and impossible is not on offer from here — nothing in this
// package can see what the target package already declares.
const Name = "jsonNames"

// Ref identifies the runtime inside the package it is emitted into.
//
// Qualified by that package rather than by the one this file is in, because
// that is where the code will be: the runtime is emitted, not imported, so two
// packages that both need it hold two copies that happen to read alike. It is
// also what makes the requiring work — every subject in one package names the
// same reference, so it is emitted once, and a subject in another names a
// different one, so that package gets its own.
func Ref(pkg string) model.TypeRef { return model.TypeRef{Pkg: pkg, Name: Name} }

// Unit returns the declarations for the runtime.
//
// The position is the declaration that asked. The runtime is emitted because
// something required it, and if reading it goes wrong there is nothing about
// the runtime an author could have written differently — so what a diagnostic
// points at is the declaration they were working on.
func Unit(at token.Position) (layer.Unit, error) {
	out, diags := templates.Verbatim(templates.Template{Name: Name, Source: bodies}, at)
	if err := diags.Err(); err != nil {
		return layer.Unit{}, err
	}

	return layer.Unit{
		Decls:    out.Decls,
		Comments: out.Comments,
		Fset:     out.Fset,
		Imports:  out.Imports,
	}, nil
}
