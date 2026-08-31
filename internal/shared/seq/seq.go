package seq

import (
	_ "embed"
	"go/token"

	"github.com/okian/forge/internal/layer"
	"github.com/okian/forge/internal/model"
	"github.com/okian/forge/internal/templates"
)

// bodies is the view as it is emitted, embedded from the package beside this
// one so that what is written into somebody's repository is a file the ordinary
// build compiles and this package's own tests exercise.
//
//go:embed tmpl/tmpl.go
var bodies []byte

// Name is what the view is called in generated code.
//
// One name for every package rather than one per declaration, which is the
// whole of what makes it shared: a projection from one declaration's view
// returns the same type a projection from another's does, so a chain that
// crosses between them is a chain and not a conversion. A name that varied by
// declaration would give every one of them its own view of the same shape, and
// the chains would stop at the boundary.
//
// The cost is a name forge takes in somebody else's package. An author who
// already has a type called Seq gets a redeclaration in a file they did not
// write, which is the same hazard every generated name carries and is answered
// where all of them are: by the stage that knows what the package already
// declares. Nothing here can see that, and a name chosen to be unlikely rather
// than checked would only make the collision rarer and more confusing.
const Name = "Seq"

// Ref identifies the view inside the package it is emitted into.
//
// Qualified by that package rather than by the one this file is in, because
// that is where the type will be: the view is emitted, not imported, so two
// packages that both need it hold two types that happen to read alike. It is
// also what makes the requiring work — every declaration in one package names
// the same reference, so it is emitted once, and a declaration in another names
// a different one, so that package gets its own.
func Ref(pkg string) model.TypeRef { return model.TypeRef{Pkg: pkg, Name: Name} }

// Unit returns the declarations for the view.
//
// The position is the declaration that asked. A view is emitted because
// something required it, and if reading it goes wrong there is nothing about
// the view an author could have written differently — so what a diagnostic
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
