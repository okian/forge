package generate

import (
	"go/ast"
	"go/token"
	"testing"

	"github.com/okian/forge/internal/emit"
	"github.com/okian/forge/internal/merge"
)

// declaring builds a type declaration of the given names.
func declaring(names ...string) *ast.GenDecl {
	specs := make([]ast.Spec, len(names))
	for i, name := range names {
		specs[i] = &ast.TypeSpec{Name: ast.NewIdent(name), Type: ast.NewIdent("int")}
	}
	return &ast.GenDecl{Tok: token.TYPE, Specs: specs}
}

// What each kind of declaration becomes in the file standing in for a
// package's output.
//
// The rule is about what the tag leaves the package without. A method is
// missing and is written again with nothing in it; a helper type is missing and
// is written as it was; the declaration's own type is not missing, because the
// author's file is what supplies it under the tag, and writing it would put the
// name in scope twice.
func TestWhatEachDeclarationBecomesInTheStandInFile(t *testing.T) {
	body := &ast.BlockStmt{Lbrace: token.Pos(10), Rbrace: token.Pos(40)}

	cases := map[string]struct {
		decl ast.Decl
		kept bool
	}{
		"a method": {
			decl: &ast.FuncDecl{Name: ast.NewIdent("Len"), Type: &ast.FuncType{}, Body: body},
			kept: true,
		},
		"a helper type":              {decl: declaring("PersonsSeq"), kept: true},
		"the declaration's own type": {decl: declaring("Persons"), kept: false},

		// A function with no body is one the language already treats as
		// declared elsewhere — implemented in assembly, most often. There is
		// nothing to replace and nothing missing under the tag.
		"a function implemented elsewhere": {
			decl: &ast.FuncDecl{Name: ast.NewIdent("add"), Type: &ast.FuncType{}},
			kept: false,
		},

		// Imports are named through the file rather than written by a section,
		// and the set this file needs is not the set the real one needed.
		//
		// Holding a specification, so that what drops it is the kind of
		// declaration it is. An empty one would be dropped for being empty and
		// would say nothing about imports.
		"an import": {decl: &ast.GenDecl{Tok: token.IMPORT, Specs: []ast.Spec{
			&ast.ImportSpec{Path: &ast.BasicLit{Kind: token.STRING, Value: `"iter"`}},
		}}, kept: false},

		// A gap a layer left, and a declaration of a kind that has no meaning
		// at the top level. Both reach the printer if they are passed on, and
		// neither is something to stand in for.
		"a gap":            {decl: nil, kept: false},
		"nothing sensible": {decl: &ast.BadDecl{}, kept: false},
	}

	for name, want := range cases {
		t.Run(name, func(t *testing.T) {
			got, kept := stubbed(want.decl, []string{"Persons"})

			if kept != want.kept {
				t.Fatalf("kept=%v, wanted %v", kept, want.kept)
			}
			if !kept {
				return
			}
			if got == want.decl {
				t.Error("the declaration was passed on rather than copied, so the real file holds the stub")
			}
		})
	}
}

// A grouped declaration keeps the helpers written beside the type forge does
// not repeat.
//
// Dropping the group would take the helpers with it, and the signatures below
// name them.
func TestAGroupHoldingTheTypeAndItsHelpers(t *testing.T) {
	got, kept := stubbed(declaring("Persons", "PersonsSeq"), []string{"Persons"})
	if !kept {
		t.Fatal("a group holding a helper beside the type was dropped whole")
	}

	group, ok := got.(*ast.GenDecl)
	if !ok {
		t.Fatalf("a type declaration became %T", got)
	}
	if len(group.Specs) != 1 {
		t.Fatalf("the group holds %d declarations, wanting only the helper", len(group.Specs))
	}
	if held := group.Specs[0].(*ast.TypeSpec).Name.Name; held != "PersonsSeq" {
		t.Errorf("the group kept %s", held)
	}
}

// A section every declaration of which is left out contributes nothing, rather
// than an empty section.
//
// An empty one would print as a blank stretch, so a layer that only declared
// the type would cost the file a gap where it used to be.
func TestASectionWithNothingToStandInFor(t *testing.T) {
	unit := merge.Unit{Sections: []emit.Section{{Decls: []ast.Decl{declaring("Persons")}}}}

	if got := stubs([]string{"Persons"}, unit); len(got) != 0 {
		t.Errorf("a section holding only the declaration's own type contributed %d sections", len(got))
	}
}

// An import is kept by the name it is written under, which is the one it was
// given where it was given one.
//
// A layer binding a package to a name of its own is a layer whose signatures
// say that name and not the last element of the path, and an import matched by
// the path would be dropped from a file that uses it.
func TestAnImportKeptByTheNameItWasGiven(t *testing.T) {
	sections := []emit.Section{{Decls: []ast.Decl{
		&ast.FuncDecl{
			Name: ast.NewIdent("At"),
			Type: &ast.FuncType{Results: &ast.FieldList{List: []*ast.Field{{
				Type: &ast.SelectorExpr{X: ast.NewIdent("clock"), Sel: ast.NewIdent("Time")},
			}}}},
		},
	}}}

	held := []emit.Import{
		{Path: "example.com/chronology", Name: "clock"},
		{Path: "example.com/unused", Name: "spare"},
		{Path: "time"},
	}

	got := reaching(sections, held)

	if len(got) != 1 || got[0].Name != "clock" {
		t.Errorf("the imports kept were %v, wanting only the one the signature names", got)
	}
}

// An import is kept by the name it binds, which is not always the last element
// of its path.
//
// A package is free to declare a name its directory does not have — a directory
// named for what it holds rather than for its clause, or a path ending in a
// version. Deriving the name from the path drops the import from a file that
// goes on to name the package, which does not compile; and the file is written
// under a tag, so the build that breaks is not the one the author just ran.
func TestAnImportKeptByTheNameItsPackageDeclares(t *testing.T) {
	sections := []emit.Section{{Decls: []ast.Decl{
		&ast.FuncDecl{
			Name: ast.NewIdent("At"),
			Type: &ast.FuncType{Results: &ast.FieldList{List: []*ast.Field{{
				Type: &ast.SelectorExpr{X: ast.NewIdent("critters"), Sel: ast.NewIdent("Beast")},
			}}}},
		},
	}}}

	held := []emit.Import{
		{Path: "example.com/menagerie/zoo", Name: "critters"},
		{Path: "example.com/menagerie/unused", Name: "spare"},
	}

	got := reaching(sections, held)

	if len(got) != 1 || got[0].Path != "example.com/menagerie/zoo" {
		t.Errorf("the imports kept were %v, wanting only the one the signature names", got)
	}
}
