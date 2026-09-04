package index

import (
	"go/ast"
	"go/token"
	"strings"
	"testing"

	"github.com/okian/forge/plugin"
)

// A layer asked to generate with no declaration reports forge's own misuse
// rather than a diagnostic: a diagnostic points at a declaration, and the
// declaration is what is missing.
func TestGeneratingWithNothingToGenerateFor(t *testing.T) {
	for name, ctx := range map[string]*plugin.Context{
		"no context": nil,
		"no model":   {},
		"no subject": {Model: &plugin.Model{Name: "Directory"}},
	} {
		if _, err := New().Generate(ctx, plugin.Shape{}); err == nil {
			t.Errorf("%s was generated for", name)
		}
	}
}

// The names built around a declaration take its visibility, because a
// constructor or an error reachable from outside a package the type is
// unexported in is a name nobody can use for anything. The entry type is
// representation and stays unexported whatever the declaration is.
func TestTheNamesBuiltAroundADeclaration(t *testing.T) {
	cases := map[string]struct{ constructor, refusal, entry string }{
		"Directory": {constructor: "NewDirectory", refusal: "ErrDirectoryDuplicate", entry: "directoryEntry"},
		"directory": {constructor: "newDirectory", refusal: "errDirectoryDuplicate", entry: "directoryEntry"},
	}

	for declared, want := range cases {
		t.Run("of "+declared, func(t *testing.T) {
			if got := constructorFor(declared); got != want.constructor {
				t.Errorf("the constructor is %s, want %s", got, want.constructor)
			}
			if got := errorFor(declared); got != want.refusal {
				t.Errorf("the refusal is %s, want %s", got, want.refusal)
			}
			if got := entryFor(declared); got != want.entry {
				t.Errorf("the entry type is %s, want %s", got, want.entry)
			}
		})
	}
}

// An import the template grew that nothing recorded a bound name for is
// refused, which is what keeps the list from being a comment about a file
// that has since changed.
func TestAnImportNothingRecorded(t *testing.T) {
	wrong := accounted([]plugin.Import{{Path: "sync", Name: "sync"}})
	if wrong == "" {
		t.Fatal("an unrecorded import was accounted for")
	}
	if !strings.Contains(wrong, "sync") {
		t.Errorf("the report does not name the import: %s", wrong)
	}
}

// Each arrangement keeps one answer per choice and drops the rest, and what
// it keeps carries the contract's name.
func TestWhatEachArrangementKeeps(t *testing.T) {
	base := plan{
		declared: "Directory", key: column{field: "ID"},
		dup: errorFor("Directory"), entry: entryFor("Directory"),
	}

	t.Run("refusing", func(t *testing.T) {
		p := base
		p.unique, p.refusing = true, true
		held := chosen(p)

		for _, dropped := range []string{"Directory", "directoryNew", appendPlain, placePlain, placeRefusing, resetMethod, "spread", "grouped"} {
			if !held.drop[dropped] {
				t.Errorf("%s is not dropped", dropped)
			}
		}
		if held.rename[appendRefusing] != appendPlain {
			t.Errorf("the checked append is renamed to %q", held.rename[appendRefusing])
		}
		if held.names[constructorRefusing] != "NewDirectory" {
			t.Errorf("the kept constructor is %q", held.names[constructorRefusing])
		}
		if held.drop["ErrDirectoryDuplicate"] {
			t.Error("the refusal's own sentinel is dropped")
		}
	})

	t.Run("replacing", func(t *testing.T) {
		p := base
		p.unique = true
		held := chosen(p)

		for _, dropped := range []string{"directoryNewChecked", appendRefusing, "ErrDirectoryDuplicate"} {
			if !held.drop[dropped] {
				t.Errorf("%s is not dropped", dropped)
			}
		}
		if held.names[constructorPlain] != "NewDirectory" {
			t.Errorf("the kept constructor is %q", held.names[constructorPlain])
		}
	})

	t.Run("held many times", func(t *testing.T) {
		held := chosen(base)

		for _, dropped := range []string{"pick", "noted"} {
			if !held.drop[dropped] {
				t.Errorf("%s is not dropped", dropped)
			}
		}
		for _, kept := range []string{"spread", "grouped", "cut"} {
			if held.drop[kept] {
				t.Errorf("%s is dropped", kept)
			}
		}
	})
}

// A method the template declares that this package has never heard of is
// refused rather than silently carried or dropped: what either produces is a
// file missing something it calls, or holding something nothing does.
func TestATemplateMethodNothingKnows(t *testing.T) {
	held := chosen(plan{declared: "Directory", key: column{field: "ID"}})

	mystery := &ast.FuncDecl{
		Recv: &ast.FieldList{List: []*ast.Field{{Type: ast.NewIdent("Directory")}}},
		Name: ast.NewIdent("mystery"),
	}

	if _, err := held.applied([]ast.Decl{mystery}, "Directory"); err == nil {
		t.Fatal("a method nothing here knows was carried without a word")
	}
}

// A rename that finds something other than a function is the template having
// changed under this package, and is reported rather than acted on.
func TestRenamingSomethingThatIsNotAFunction(t *testing.T) {
	held := choice{
		names:   map[string]string{},
		drop:    map[string]bool{"dropped": true},
		rename:  map[string]string{appendRefusing: appendPlain},
		methods: map[string]bool{},
	}

	imposter := &ast.GenDecl{Tok: token.VAR, Specs: []ast.Spec{
		&ast.ValueSpec{Names: []*ast.Ident{ast.NewIdent(appendRefusing)}},
	}}
	gone := &ast.GenDecl{Tok: token.VAR, Specs: []ast.Spec{
		&ast.ValueSpec{Names: []*ast.Ident{ast.NewIdent("dropped")}},
	}}

	if _, err := held.applied([]ast.Decl{imposter, gone}, "Directory"); err == nil {
		t.Fatal("a rename of something that is not a function was acted on")
	}
}
