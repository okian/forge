package emit_test

import (
	"go/ast"
	"slices"
	"testing"

	"github.com/okian/forge/internal/emit"
)

// calling builds a declaration whose body names each of the given packages to
// the left of a dot, which is the only use of an import that survives a body
// being thrown away.
func calling(named ...string) ast.Decl {
	body := &ast.BlockStmt{}
	for _, one := range named {
		body.List = append(body.List, &ast.ExprStmt{X: &ast.CallExpr{
			Fun: &ast.SelectorExpr{X: ast.NewIdent(one), Sel: ast.NewIdent("Something")},
		}})
	}

	return &ast.FuncDecl{Name: ast.NewIdent("f"), Type: &ast.FuncType{}, Body: body}
}

// A caller emitting part of what it generated keeps the imports that part still
// names, and loses the rest.
//
// It is the whole point: a template variant the options did not select takes
// its imports with it, and a file importing a package it never mentions does
// not compile.
func TestWhichImportsSurvive(t *testing.T) {
	all := []emit.Import{
		{Path: "cmp", Name: "cmp"},
		{Path: "iter", Name: "iter"},
		{Path: "slices", Name: "slices"},
	}

	got := emit.Reaching([]ast.Decl{calling("cmp")}, all)
	if want := []emit.Import{{Path: "cmp", Name: "cmp"}}; !slices.Equal(got, want) {
		t.Errorf("kept %v, want the one the declarations name", got)
	}

	if got := emit.Reaching(nil, all); len(got) != 0 {
		t.Errorf("kept %v for declarations that name nothing", got)
	}
}

// An import is matched by the name it binds rather than by its path, because
// the two need not agree.
//
// A package is free to declare a name its directory does not have, and one
// bound to a name of its own because the first was taken has neither. Matching
// on the path would drop an import from a file that goes on to name the
// package, which does not compile.
func TestAnImportMatchedByWhatItBinds(t *testing.T) {
	all := []emit.Import{
		{Path: "example.com/menagerie/zoo", Name: "critters"},
		{Path: "example.com/util/slices", Name: "slices2", Aliased: true},
	}

	got := emit.Reaching([]ast.Decl{calling("critters")}, all)
	if len(got) != 1 || got[0].Path != "example.com/menagerie/zoo" {
		t.Errorf("kept %v, want the one bound to the name the declaration uses", got)
	}
}

// Two kinds of import are kept whatever the declarations say, because nothing
// a declaration says could ever mention them.
//
// A blank import is there for what loading the package does, and a dot import
// puts its names in the file's own scope where they are written with no
// qualifier at all. Dropping either changes what the file does and reports
// nothing — where keeping an import nothing needs is a build error the first
// compile prints.
func TestTheImportsNoDeclarationCanName(t *testing.T) {
	all := []emit.Import{
		{Path: "embed", Name: "_", Aliased: true},
		{Path: "strings", Name: ".", Aliased: true},
		{Path: "iter", Name: "iter"},
		{Path: "slices", Name: "slices"},
	}

	got := emit.Reaching([]ast.Decl{calling("iter")}, all)

	want := []emit.Import{
		{Path: "embed", Name: "_", Aliased: true},
		{Path: "strings", Name: ".", Aliased: true},
		{Path: "iter", Name: "iter"},
	}
	if !slices.Equal(got, want) {
		t.Errorf("kept %v, want %v", got, want)
	}
}

// The names a declaration could be using a package by are collected wide, since
// every caller uses them to decide whether something may be dropped.
func TestWhatNamesADeclarationUses(t *testing.T) {
	got := emit.Qualifiers([]ast.Decl{calling("cmp", "iter")})

	for _, want := range []string{"cmp", "iter"} {
		if !got[want] {
			t.Errorf("%s is not among %v", want, got)
		}
	}

	// A receiver and a package read the same way without the type information
	// this deliberately does not have, so both are in — and what that costs is
	// declining to drop something, never dropping something needed.
	held := &ast.FuncDecl{
		Name: ast.NewIdent("Len"),
		Recv: &ast.FieldList{List: []*ast.Field{{Names: []*ast.Ident{ast.NewIdent("s")}}}},
		Type: &ast.FuncType{},
		Body: &ast.BlockStmt{List: []ast.Stmt{&ast.ExprStmt{X: &ast.SelectorExpr{
			X: ast.NewIdent("s"), Sel: ast.NewIdent("n"),
		}}}},
	}
	if !emit.Qualifiers([]ast.Decl{held})["s"] {
		t.Error("a receiver read like a package was not collected, so an import bound to s could be dropped")
	}
}
