package slice

import (
	"go/ast"
	"go/token"
	"go/types"
	"slices"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"

	"github.com/okian/forge/internal/emit"
	"github.com/okian/forge/internal/layer"
	"github.com/okian/forge/internal/model"
	"github.com/okian/forge/internal/shape"
)

// What the template imports is written down, and what is written down is what
// the subject is spelled against — so a template that grows an import nobody
// recorded is a subject that was not moved out of its way. The check runs on
// every generate rather than in a test of its own, so the first run of anything
// in this package catches it.
//
// The list cannot be derived from the paths, which is the reason it exists: a
// path does not say what it binds. encoding/json/v2 binds json and math/rand/v2
// binds rand, and the last element is v2 for both.
func TestATemplateThatGrewAnImport(t *testing.T) {
	if wrong := accounted([]emit.Import{{Path: "iter"}, {Path: "slices"}}); wrong != "" {
		t.Errorf("the template's own imports were refused: %s", wrong)
	}

	wrong := accounted([]emit.Import{{Path: "iter"}, {Path: "encoding/json/v2"}})
	if wrong == "" {
		t.Fatal("an import nothing recorded a name for was accepted")
	}
	if !strings.Contains(wrong, "encoding/json/v2") {
		t.Errorf("the complaint %q does not name the import", wrong)
	}
}

// The names the spelling keeps clear of are the template's, in an order a map
// did not decide.
func TestWhatTheTemplateBinds(t *testing.T) {
	if want := []string{"iter", "slices"}; !slices.Equal(taken(), want) {
		t.Errorf("the template binds %v, want %v", taken(), want)
	}
}

// The recorded names are asked of the packages themselves, because they are the
// half of the list nothing else can check.
//
// Generation refuses an import nobody wrote down, which catches a template that
// grew one. It cannot catch a name written down wrongly: a subject in a package
// called slices, under a list that says the template's slices binds v2, is
// spelled slices.Person beside an import of the real slices — two packages
// under one name, in generated code, silently. Nothing derivable from the paths
// answers it, so the only thing that can is the build.
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

// Only a type declaration under the declared name is the author's own. A
// constant, a variable, a function or a type of another name is this layer's
// and is emitted whatever the form.
func TestWhatCountsAsTheDeclarationItself(t *testing.T) {
	cases := map[string]struct {
		decl ast.Decl
		is   bool
	}{
		"the declaration":     {typed("Persons"), true},
		"another type":        {typed("personsSeq"), false},
		"a constant":          {&ast.GenDecl{Tok: token.CONST}, false},
		"a function":          {&ast.FuncDecl{Name: ast.NewIdent("Persons")}, false},
		"a spec of no type":   {&ast.GenDecl{Tok: token.TYPE, Specs: []ast.Spec{&ast.ValueSpec{}}}, false},
		"a gap where one was": {nil, false},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := declares(tc.decl, "Persons"); got != tc.is {
				t.Errorf("declares() = %v, want %v", got, tc.is)
			}
		})
	}
}

// typed builds the declaration of a type under a name, with nothing on the
// right of it.
func typed(name string) ast.Decl {
	return &ast.GenDecl{
		Tok:   token.TYPE,
		Specs: []ast.Spec{&ast.TypeSpec{Name: ast.NewIdent(name), Type: ast.NewIdent("int")}},
	}
}

// Leaving a declaration out is only safe under two conditions, and this is the
// file every later storage layer copies — so both are checked rather than left
// to whoever writes the next template to remember.
//
// Neither is reachable through the template this layer ships, which is the
// point: the check is what keeps it that way.
func TestWhatCannotBeLeftOut(t *testing.T) {
	grouped := &ast.GenDecl{
		Tok: token.TYPE,
		Specs: []ast.Spec{
			&ast.TypeSpec{Name: ast.NewIdent("Persons"), Type: ast.NewIdent("int")},
			&ast.TypeSpec{Name: ast.NewIdent("personsSeq"), Type: ast.NewIdent("int")},
		},
	}

	// A type declared beside the container would be taken out with it — the
	// helper and its documentation both, out of a file whose other
	// declarations still call it.
	if _, err := owned([]ast.Decl{grouped}, model.FormInline, "Persons"); err == nil {
		t.Error("a helper declared beside the container was dropped with it")
	} else if !strings.Contains(err.Error(), "2 types in one group") {
		t.Errorf("the error %q does not say what is wrong", err)
	}

	// A package the dropped declaration is the last mention of leaves an import
	// nothing uses, which is a file that does not compile.
	naming := &ast.GenDecl{
		Tok: token.TYPE,
		Specs: []ast.Spec{&ast.TypeSpec{
			Name: ast.NewIdent("Persons"),
			Type: &ast.SelectorExpr{X: ast.NewIdent("sync"), Sel: ast.NewIdent("Map")},
		}},
	}

	if _, err := owned([]ast.Decl{naming}, model.FormInline, "Persons"); err == nil {
		t.Error("a declaration naming a package was dropped, and its import left behind")
	} else if !strings.Contains(err.Error(), "sync") {
		t.Errorf("the error %q does not name the package", err)
	}

	// A package something staying also names is one the import block still
	// needs, so dropping the declaration costs nothing. This is the ordinary
	// case for a subject from another package: the type declaration names it,
	// and so does every method signature.
	using := &ast.FuncDecl{
		Name: ast.NewIdent("Held"),
		Recv: &ast.FieldList{List: []*ast.Field{{Type: ast.NewIdent("Persons")}}},
		Type: &ast.FuncType{
			Params:  &ast.FieldList{},
			Results: &ast.FieldList{List: []*ast.Field{{Type: &ast.SelectorExpr{X: ast.NewIdent("sync"), Sel: ast.NewIdent("Map")}}}},
		},
	}

	out, err := owned([]ast.Decl{naming, using}, model.FormInline, "Persons")
	if err != nil {
		t.Errorf("a declaration whose package is still named was kept: %v", err)
	}
	if len(out) != 1 {
		t.Errorf("owned() kept %d declarations, want the one that is not the author's", len(out))
	}

	// Something that is not a type declaration at all cannot be the container.
	if wrong := droppable(&ast.FuncDecl{Name: ast.NewIdent("Persons")}, nil); wrong == "" {
		t.Error("a function was accepted as the declaration of a type")
	}
}

// A name with nothing in it has no first letter to change, which is the one
// case the case changes have to survive rather than index into.
func TestRecasingANameWithNothingInIt(t *testing.T) {
	if got := lower(""); got != "" {
		t.Errorf("lower() = %q", got)
	}
	if got := upper(""); got != "" {
		t.Errorf("upper() = %q", got)
	}
}

// Both refusals reach an author through Generate or not at all, so both are
// asked of it rather than only of the function that decides them. Which means
// standing in a template of their own, since the one this layer ships is
// written not to provoke either — which is the whole point of them.
func TestWhatGenerateRefusesATemplateFor(t *testing.T) {
	cases := map[string]struct {
		source string
		says   string
	}{
		"an import nobody recorded a name for": {
			source: "package tmpl\n\nimport \"encoding/json/v2\"\n\n" +
				"type Slice[T any] []T\n\n" +
				"func New[T any](elems ...T) Slice[T] { return elems }\n\n" +
				"func (s Slice[T]) Encoded() (json.Value, error) { return nil, nil }\n",
			says: "encoding/json/v2",
		},
		"a helper declared beside the container": {
			source: "package tmpl\n\ntype (\n\tSlice[T any] []T\n\tholder[T any] struct{ v T }\n)\n\n" +
				"func New[T any](elems ...T) Slice[T] { return elems }\n",
			says: "2 types in one group",
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			defer stood(t, tc.source)()

			_, err := New().Generate(declaring(), shape.Shape{})
			if err == nil {
				t.Fatal("the template was generated from")
			}
			if !strings.Contains(err.Error(), tc.says) {
				t.Errorf("the error %q does not say what is wrong", err)
			}
		})
	}
}

// stood puts a template in place of the one this layer ships, and returns what
// puts the real one back.
//
// The template is a package-level value, so the swap is visible to everything
// in the process: nothing in either of this layer's test packages may run in
// parallel while this exists. The restore is deferred rather than written at
// the end of the test, so a test that gives up part way through still leaves
// the real template behind it.
func stood(t *testing.T, source string) func() {
	t.Helper()

	was := bodies
	bodies = []byte(source)

	return func() { bodies = was }
}

// declaring builds what a layer is asked to generate against: an inline
// declaration over a subject in its own package, which is the ordinary case and
// the one that provokes nothing on its own.
func declaring() *layer.Context {
	pkg := types.NewPackage("example.com/model", "model")
	obj := types.NewTypeName(token.NoPos, pkg, "Person", nil)

	return &layer.Context{
		Model: &model.Model{
			Name:    "Persons",
			Form:    model.FormInline,
			Subject: &model.Struct{Named: types.NewNamed(obj, types.NewStruct(nil, nil), nil)},
			Pkg:     &packages.Package{PkgPath: "example.com/model"},
			Pos:     token.Position{Filename: "model/person.go", Line: 10, Column: 6},
		},
	}
}
