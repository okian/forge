package generate

import (
	"go/ast"
	"go/token"
	"testing"
)

// A position with nothing to resolve it against resolves to none.
//
// Both halves matter and neither happens on the ordinary path. A file set is
// absent only where a package failed to load far enough to have one, and an
// invalid position comes from an object the type checker synthesised rather
// than read. What the guard buys is that a diagnostic about a collision still
// carries its message: token.Position's zero value renders as nothing at all,
// where resolving against a nil file set would panic and take the run with it.
func TestAPositionWithNothingToResolveAgainstIsNone(t *testing.T) {
	fset := token.NewFileSet()

	cases := map[string]struct {
		fset *token.FileSet
		at   token.Pos
	}{
		"no file set": {nil, token.Pos(1)},
		"no position": {fset, token.NoPos},
		"neither":     {nil, token.NoPos},
	}

	for name, one := range cases {
		t.Run(name, func(t *testing.T) {
			if got := position(one.fset, one.at); got != (token.Position{}) {
				t.Errorf("position(%s) = %v, want the zero position", name, got)
			}
		})
	}
}

// A method declared on something that is not a plain name is not a method this
// can attribute to a type.
//
// The receiver is unwrapped through a pointer and through an instantiation, and
// what is left has to be an identifier. A qualified name is not: a method may
// not be declared on another package's type, so source spelling one is source
// that does not compile, and reading a type name out of it would be inventing a
// collision against a type that does not exist here.
func TestAMethodOnAQualifiedNameIsNotAttributed(t *testing.T) {
	decl := &ast.FuncDecl{
		Name: ast.NewIdent("Len"),
		Recv: &ast.FieldList{List: []*ast.Field{{
			Type: &ast.SelectorExpr{X: ast.NewIdent("other"), Sel: ast.NewIdent("Roster")},
		}}},
	}

	if _, on, is := methodOn(decl); is {
		t.Errorf("methodOn attributed a method to %q, want it left alone", on)
	}
}

// What introduces no package-level name introduces none.
//
// An import declaration is the case with a reason behind it rather than a
// shape that happens not to occur: an import binds a name in the file rather
// than in the package, so two files importing the same package under the same
// name are not a collision and counting them as one would report every
// generated file against every other.
func TestWhatIntroducesNoPackageLevelName(t *testing.T) {
	cases := map[string]ast.Decl{
		"an import":                        &ast.GenDecl{Tok: token.IMPORT},
		"a declaration that did not parse": &ast.BadDecl{},
	}

	for name, decl := range cases {
		t.Run(name, func(t *testing.T) {
			if got := declares(decl); len(got) != 0 {
				t.Errorf("declares(%s) = %v, want none", name, got)
			}
		})
	}
}

// A specification naming nothing names nothing.
//
// A type specification with no name comes from source that did not parse, and
// an import specification reaches here only from a declaration a caller has
// already decided is not an import. Neither can contribute a name, and the
// alternative to returning none is a nil dereference on the first and an
// invented one on the second.
func TestASpecificationNamingNothing(t *testing.T) {
	cases := map[string]ast.Spec{
		"a type with no name": &ast.TypeSpec{},
		"an import":           &ast.ImportSpec{},
	}

	for name, spec := range cases {
		t.Run(name, func(t *testing.T) {
			if got := specifies(spec); len(got) != 0 {
				t.Errorf("specifies(%s) = %v, want none", name, got)
			}
		})
	}
}
