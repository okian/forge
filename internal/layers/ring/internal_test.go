package ring

import (
	"go/ast"
	"go/token"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"

	"github.com/okian/forge/internal/emit"
	"github.com/okian/forge/internal/layer"
	"github.com/okian/forge/internal/model"
	"github.com/okian/forge/internal/shape"
)

// The pieces below are reached through Generate in the tests beside this file.
// What is here is the answers they give to inputs a run cannot produce — a
// template that has changed under this package, a name nothing declares — which
// are the paths that exist so that a mistake in forge is reported as one rather
// than written into somebody's package.

// A run with no declaration to generate for is forge calling itself wrongly,
// and says so as itself rather than as a diagnostic about anybody's source.
func TestGeneratingWithNothingToGenerateFor(t *testing.T) {
	cases := map[string]*layer.Context{
		"no context":     nil,
		"no declaration": {},
		"no subject":     {Model: &model.Model{Name: "Persons"}},
	}

	for name, ctx := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := New().Generate(ctx, shape.Shape{})

			if err == nil {
				t.Fatal("generating was allowed")
			}
			if said := err.Error(); !strings.Contains(said, "ring:") {
				t.Errorf("the refusal does not say who refused: %s", said)
			}
		})
	}
}

// A capacity that is not a number reaches the layer only if the option's own
// validation did not run, and is refused rather than assumed.
func TestACapacityThatIsNotANumber(t *testing.T) {
	ctx := &layer.Context{
		Model:   &model.Model{Name: "Persons", Form: model.FormSpec, Subject: &model.Struct{}},
		Options: model.Options{Entries: []model.Option{{Key: optionCap, Value: "lots"}}},
	}

	_, err := declaredCapacity(ctx)
	if err == nil {
		t.Fatal("a capacity of \"lots\" was accepted")
	}
	if said := err.Error(); !strings.Contains(said, "lots") {
		t.Errorf("the refusal does not say what was written: %s", said)
	}
}

// What a declaration introduces is read off it, and a declaration introducing
// none or more than one introduces nothing this can act on.
//
// It is what decides which half of each pair is dropped, so a shape it does not
// recognise has to come back as nothing rather than as a name that happens to
// match: dropping the wrong declaration is a file missing a method, and the
// build that finds out is the author's.
func TestWhatADeclarationIntroduces(t *testing.T) {
	one := func(name string) []ast.Spec {
		return []ast.Spec{&ast.TypeSpec{Name: ast.NewIdent(name), Type: ast.NewIdent("int")}}
	}

	cases := map[string]struct {
		decl ast.Decl
		want string
	}{
		"a function":     {decl: &ast.FuncDecl{Name: ast.NewIdent("NewPersons")}, want: "NewPersons"},
		"a method":       {decl: &ast.FuncDecl{Name: ast.NewIdent("Push"), Recv: &ast.FieldList{}}, want: "Push"},
		"a type":         {decl: &ast.GenDecl{Tok: token.TYPE, Specs: one("Persons")}, want: "Persons"},
		"a constant":     {decl: &ast.GenDecl{Tok: token.CONST, Specs: []ast.Spec{&ast.ValueSpec{Names: []*ast.Ident{ast.NewIdent("personsFixedCap")}}}}, want: "personsFixedCap"},
		"a group of two": {decl: &ast.GenDecl{Tok: token.TYPE, Specs: append(one("A"), one("B")...)}, want: ""},
		"two names at once": {
			decl: &ast.GenDecl{Tok: token.VAR, Specs: []ast.Spec{
				&ast.ValueSpec{Names: []*ast.Ident{ast.NewIdent("a"), ast.NewIdent("b")}},
			}},
			want: "",
		},
		"an import":         {decl: &ast.GenDecl{Tok: token.IMPORT, Specs: []ast.Spec{&ast.ImportSpec{}}}, want: ""},
		"a nameless func":   {decl: &ast.FuncDecl{}, want: ""},
		"nothing sensible":  {decl: &ast.BadDecl{}, want: ""},
		"a gap":             {decl: nil, want: ""},
		"a nil declaration": {decl: (*ast.GenDecl)(nil), want: ""},
	}

	for name, want := range cases {
		t.Run(name, func(t *testing.T) {
			if got := declaredAs(want.decl); got != want.want {
				t.Errorf("introduces %q, want %q", got, want.want)
			}
		})
	}
}

// A doc comment is rewritten only where it opens with the name it documents,
// which is the convention the rewrite exists to preserve.
func TestRewritingADocComment(t *testing.T) {
	cases := map[string]struct {
		doc  *ast.CommentGroup
		want string
	}{
		"the opening word": {
			doc:  &ast.CommentGroup{List: []*ast.Comment{{Text: "// PushChecked adds an element."}}},
			want: "// Push adds an element.",
		},
		"the whole of it": {
			doc:  &ast.CommentGroup{List: []*ast.Comment{{Text: "// PushChecked"}}},
			want: "// Push",
		},
		"a comment about something else": {
			doc:  &ast.CommentGroup{List: []*ast.Comment{{Text: "// Adding an element."}}},
			want: "// Adding an element.",
		},
		"a name that merely starts the same": {
			doc:  &ast.CommentGroup{List: []*ast.Comment{{Text: "// PushCheckedTwice is not this."}}},
			want: "// PushCheckedTwice is not this.",
		},
		"nothing at all": {doc: nil},
		"an empty group": {doc: &ast.CommentGroup{}},
	}

	for name, want := range cases {
		t.Run(name, func(t *testing.T) {
			redocument(want.doc, pushRefusing, pushOverwriting)

			if want.doc == nil || len(want.doc.List) == 0 {
				return
			}
			if got := want.doc.List[0].Text; got != want.want {
				t.Errorf("became %q, want %q", got, want.want)
			}
		})
	}
}

// The capacity goes into the constant that carries it, and a template with no
// such constant is reported rather than emitted with the number missing.
func TestWritingTheCapacityIn(t *testing.T) {
	held := &ast.GenDecl{Tok: token.CONST, Specs: []ast.Spec{&ast.ValueSpec{
		Names:  []*ast.Ident{ast.NewIdent("personsFixedCap")},
		Values: []ast.Expr{&ast.BasicLit{Kind: token.INT, Value: "8"}},
	}}}

	if err := size([]ast.Decl{held}, "personsFixedCap", 1024); err != nil {
		t.Fatalf("writing a capacity in: %v", err)
	}
	if got := held.Specs[0].(*ast.ValueSpec).Values[0].(*ast.BasicLit).Value; got != "1024" {
		t.Errorf("the constant is %s, want 1024", got)
	}

	if err := size(nil, "personsFixedCap", 1024); err == nil {
		t.Error("a template with no constant to write into was accepted")
	}

	// A constant declared with no value is not one a number can be written
	// into, and the template having changed that way is worth saying rather
	// than working around.
	valueless := &ast.GenDecl{Tok: token.CONST, Specs: []ast.Spec{&ast.ValueSpec{
		Names: []*ast.Ident{ast.NewIdent("personsFixedCap")},
	}}}
	if err := size([]ast.Decl{valueless}, "personsFixedCap", 1024); err == nil {
		t.Error("a constant equal to nothing was accepted")
	}
}

// An import the template grew that nothing wrote a bound name for is reported,
// because it is a name the subject was not moved out of the way of.
func TestAnImportNothingRecorded(t *testing.T) {
	if wrong := accounted([]emit.Import{{Path: "iter", Name: "iter"}}); wrong != "" {
		t.Errorf("a recorded import was reported: %s", wrong)
	}

	wrong := accounted([]emit.Import{{Path: "encoding/json/v2", Name: "json"}})
	if wrong == "" {
		t.Error("an import nothing recorded was passed over")
	}
	if !strings.Contains(wrong, "encoding/json/v2") {
		t.Errorf("the report does not name the import: %s", wrong)
	}
}

// A spelling's imports are carried whole, so that a file knows what each one
// binds and whether it has to write the name.
func TestCarryingASpellingsImports(t *testing.T) {
	got := imported(model.Spelling{Imports: []model.Import{
		{Path: "example.com/domain", Name: "domain"},
		{Path: "example.com/util/iter", Name: "iter2", Aliased: true},
	}})

	want := []emit.Import{
		{Path: "example.com/domain", Name: "domain"},
		{Path: "example.com/util/iter", Name: "iter2", Aliased: true},
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("import %d is %+v, want %+v", i, got[i], want[i])
		}
	}
}

// The names built around a declaration take its visibility, because a
// constructor or an error reachable from outside a package the type is
// unexported in is a name nobody can use for anything.
func TestTheNamesBuiltAroundADeclaration(t *testing.T) {
	cases := map[string]struct{ constructor, refusal string }{
		"Persons": {constructor: "NewPersons", refusal: "ErrPersonsFull"},
		"persons": {constructor: "newPersons", refusal: "errPersonsFull"},
	}

	for declared, want := range cases {
		t.Run("of "+declared, func(t *testing.T) {
			if got := constructorFor(declared); got != want.constructor {
				t.Errorf("the constructor is %s, want %s", got, want.constructor)
			}
			if got := errorFor(declared); got != want.refusal {
				t.Errorf("the refusal is %s, want %s", got, want.refusal)
			}
		})
	}
}

// A rename that finds something other than a function is the template having
// changed under this package, and is reported rather than acted on.
func TestRenamingSomethingThatIsNotAFunction(t *testing.T) {
	held := plan{
		names:  map[string]string{},
		drop:   map[string]bool{"dropped": true},
		rename: map[string]string{"Push": "Pushed"},
	}

	decls := []ast.Decl{
		&ast.GenDecl{Tok: token.VAR, Specs: []ast.Spec{&ast.ValueSpec{Names: []*ast.Ident{ast.NewIdent("dropped")}}}},
		&ast.GenDecl{Tok: token.TYPE, Specs: []ast.Spec{
			&ast.TypeSpec{Name: ast.NewIdent("Push"), Type: ast.NewIdent("int")},
		}},
	}

	_, err := chosen(decls, held, &layer.Context{Model: &model.Model{Name: "Persons"}}, 0)
	if err == nil {
		t.Fatal("a type carrying a method's name was renamed as though it were one")
	}
	if said := err.Error(); !strings.Contains(said, "Push") {
		t.Errorf("the refusal does not name what it found: %s", said)
	}
}

// A template that no longer declares the pairs this package chooses between is
// one nothing was dropped from, which is worth saying: the alternative is a
// file holding both answers to an option.
func TestATemplateWithNothingToChooseBetween(t *testing.T) {
	held := plan{names: map[string]string{}, drop: map[string]bool{}, rename: map[string]string{}}

	decls := []ast.Decl{&ast.FuncDecl{Name: ast.NewIdent("Unrelated")}}

	_, err := chosen(decls, held, &layer.Context{Model: &model.Model{Name: "Persons"}}, 0)
	if err == nil {
		t.Fatal("a template nothing was dropped from was emitted whole")
	}
	if said := err.Error(); !strings.Contains(said, "Persons") {
		t.Errorf("the refusal does not name the declaration: %s", said)
	}
}

// Renaming a method renames what calls it, which is not the same set of places.
func TestRenamingWhatCallsAMethod(t *testing.T) {
	body := &ast.BlockStmt{List: []ast.Stmt{&ast.ExprStmt{X: &ast.CallExpr{
		Fun: &ast.SelectorExpr{X: ast.NewIdent("r"), Sel: ast.NewIdent("PushChecked")},
	}}}}
	bare := &ast.BlockStmt{List: []ast.Stmt{&ast.ExprStmt{X: ast.NewIdent("PushChecked")}}}

	decls := []ast.Decl{
		&ast.FuncDecl{Name: ast.NewIdent("AppendSeq"), Type: &ast.FuncType{}, Body: body},
		&ast.FuncDecl{Name: ast.NewIdent("Elsewhere"), Type: &ast.FuncType{}, Body: bare},
	}

	calls(decls, map[string]string{pushRefusing: pushOverwriting})

	if got := body.List[0].(*ast.ExprStmt).X.(*ast.CallExpr).Fun.(*ast.SelectorExpr).Sel.Name; got != pushOverwriting {
		t.Errorf("the call still names %s", got)
	}

	// A bare identifier of the same name is a different thing, and renaming it
	// would be this reaching past what it is about.
	if got := bare.List[0].(*ast.ExprStmt).X.(*ast.Ident).Name; got != pushRefusing {
		t.Errorf("something that is not a method call was renamed to %s", got)
	}

	// And nothing at all to rename is not a walk worth taking.
	calls(decls, nil)
}

// What the template imports is written down, and what is written down is what
// every layer of the stack spells against — so a template that grows an import
// nobody recorded, or records one it no longer has, is a name the subject is
// moved out of the way of wrongly in files this layer does not write.
//
// It cannot be caught by reading the paths. A subject in a package called
// errors, under a list that had lost the template's errors, is spelled
// errors.Person beside an import of the real one — two packages under one name,
// in generated code, silently. Only the build answers it.
//
// A load rather than a parse. What an import binds is what the package it
// resolves to declares itself to be, which is a question about that package and
// not about the line naming it.
func TestTheTemplateBindsWhatItSaysItDoes(t *testing.T) {
	loaded, err := packages.Load(&packages.Config{
		Mode: packages.NeedName | packages.NeedImports,
	}, "./tmpl")
	if err != nil {
		t.Fatalf("loading the template package: %v", err)
	}
	if len(loaded) != 1 {
		t.Fatalf("loaded %d packages, want the template", len(loaded))
	}

	found := make(map[string]string, len(loaded[0].Imports))
	for path, imported := range loaded[0].Imports {
		found[path] = imported.Name

		switch recorded, is := templateImports[path]; {
		case !is:
			t.Errorf("the template imports %s and nothing recorded a name for it", path)
		case recorded != imported.Name:
			t.Errorf("%s binds %q, and it is recorded as binding %q", path, imported.Name, recorded)
		}
	}

	// And nothing recorded that the template no longer imports, which is a name
	// the subject is moved out of the way of for no reason.
	for path := range templateImports {
		if _, imported := found[path]; !imported {
			t.Errorf("%s is recorded and the template does not import it", path)
		}
	}
}
