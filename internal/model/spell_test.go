package model_test

import (
	"go/token"
	"go/types"
	"slices"
	"testing"

	"golang.org/x/tools/go/packages"

	"github.com/okian/forge/internal/model"
)

// declaring returns a named struct type in a package, which is what generated
// code has to write a name for.
func declaring(path, name, typeName string) *types.Named {
	pkg := types.NewPackage(path, name)
	obj := types.NewTypeName(token.NoPos, pkg, typeName, nil)

	return types.NewNamed(obj, types.NewStruct(nil, nil), nil)
}

// A type declared in the package being generated into is written bare, because
// a file does not import itself.
func TestATypeFromTheLocalPackage(t *testing.T) {
	spelled := model.Spell(declaring("example.com/model", "model", "Person"), "example.com/model", nil)

	if got, want := spelled.Text, "Person"; got != want {
		t.Errorf("spelled %q, want %q", got, want)
	}
	if len(spelled.Imports) != 0 {
		t.Errorf("it wants %v imported, and it is in the package being written", spelled.Imports)
	}
	if got := spelled.String(); got != spelled.Text {
		t.Errorf("String() = %q, want the text %q", got, spelled.Text)
	}
	if len(spelled.Names()) != 0 {
		t.Errorf("it binds %v", spelled.Names())
	}
}

// A type from elsewhere is qualified by the package's declared *name*, which is
// what Go source says — and the path comes back separately, because the text
// cannot be taken apart to recover it.
func TestATypeFromAnotherPackage(t *testing.T) {
	// A path whose last element is not the package's name, which is exactly the
	// case a caller deriving the import from the text would get wrong.
	spelled := model.Spell(declaring("example.com/domain/v2", "domain", "Person"), "example.com/model", nil)

	if got, want := spelled.Text, "domain.Person"; got != want {
		t.Errorf("spelled %q, want %q", got, want)
	}
	want := []model.Import{{Path: "example.com/domain/v2", Name: "domain"}}
	if !slices.Equal(spelled.Imports, want) {
		t.Errorf("it wants %v imported, want %v", spelled.Imports, want)
	}
	if got := spelled.Names(); !slices.Equal(got, []string{"domain"}) {
		t.Errorf("it binds %v", got)
	}
}

// An instantiation names packages its own name does not mention, so what has to
// be imported is not something a reader of the text could work out.
func TestAnInstantiationNamesMoreThanItself(t *testing.T) {
	pair := declaring("example.com/domain", "domain", "Pair")
	held := types.NewSlice(declaring("example.com/other", "other", "Key"))

	spelled := model.Spell(types.NewMap(held, pair), "example.com/model", nil)

	if got, want := spelled.Text, "map[[]other.Key]domain.Pair"; got != want {
		t.Errorf("spelled %q, want %q", got, want)
	}
	// Sorted and without repeats, so that the import block reads the same way
	// twice however the type was walked.
	want := []model.Import{
		{Path: "example.com/domain", Name: "domain"},
		{Path: "example.com/other", Name: "other"},
	}
	if !slices.Equal(spelled.Imports, want) {
		t.Errorf("it wants %v imported, want %v", spelled.Imports, want)
	}
}

// A name the file already binds cannot be used for a second package: the file
// would import two things under one name, which does not compile, in generated
// code the author cannot edit and about a collision they caused by naming a
// package slices.
func TestAPackageWhoseNameIsAlreadyTaken(t *testing.T) {
	spelled := model.Spell(
		declaring("example.com/util/slices", "slices", "Person"),
		"example.com/model", []string{"iter", "slices"})

	if got, want := spelled.Text, "slices2.Person"; got != want {
		t.Errorf("spelled %q, want %q", got, want)
	}
	want := []model.Import{{Path: "example.com/util/slices", Name: "slices2", Aliased: true}}
	if !slices.Equal(spelled.Imports, want) {
		t.Errorf("it wants %v imported, want %v", spelled.Imports, want)
	}
}

// Two packages of one name inside a single type collide with each other as well
// as with the file, so the numbering keeps counting rather than starting over.
func TestTwoPackagesOfOneName(t *testing.T) {
	first := declaring("example.com/a/domain", "domain", "Key")
	second := declaring("example.com/b/domain", "domain", "Value")

	spelled := model.Spell(types.NewMap(first, second), "example.com/model", []string{"domain"})

	if got, want := spelled.Text, "map[domain2.Key]domain3.Value"; got != want {
		t.Errorf("spelled %q, want %q", got, want)
	}
	want := []model.Import{
		{Path: "example.com/a/domain", Name: "domain2", Aliased: true},
		{Path: "example.com/b/domain", Name: "domain3", Aliased: true},
	}
	if !slices.Equal(spelled.Imports, want) {
		t.Errorf("it wants %v imported, want %v", spelled.Imports, want)
	}
}

// One package named twice in one type is one import, under one name — which is
// what keeps a map of a type to itself from claiming two.
func TestOnePackageNamedTwice(t *testing.T) {
	person := declaring("example.com/domain", "domain", "Person")

	spelled := model.Spell(types.NewMap(person, types.NewSlice(person)), "example.com/model", nil)

	if got, want := spelled.Text, "map[domain.Person][]domain.Person"; got != want {
		t.Errorf("spelled %q, want %q", got, want)
	}
	if len(spelled.Imports) != 1 {
		t.Errorf("it wants %v imported, want one", spelled.Imports)
	}
}

// A package with no name cannot be qualified by one, so the type is spelled
// bare — and nothing is imported for it, because an import the text does not
// name is a line the file cannot use.
func TestAPackageWithNoName(t *testing.T) {
	spelled := model.Spell(declaring("example.com/anonymous", "", "Person"), "example.com/model", nil)

	if got, want := spelled.Text, "Person"; got != want {
		t.Errorf("spelled %q, want %q", got, want)
	}
	if len(spelled.Imports) != 0 {
		t.Errorf("it wants %v imported, and the text names none of them", spelled.Imports)
	}
}

// A predeclared type belongs to no package, which is a qualifier being asked
// about nothing rather than about somewhere.
func TestAPredeclaredTypeImportsNothing(t *testing.T) {
	spelled := model.Spell(types.Typ[types.String], "example.com/model", nil)

	if got, want := spelled.Text, "string"; got != want {
		t.Errorf("spelled %q, want %q", got, want)
	}
	if len(spelled.Imports) != 0 {
		t.Errorf("a predeclared type wants %v imported", spelled.Imports)
	}
}

// A spelling that panicked would be no use where spellings are reached for, so
// a type that is not there renders as the question it is.
func TestATypeThatIsNotThere(t *testing.T) {
	if got, want := model.Spell(nil, "example.com/model", nil).Text, "?"; got != want {
		t.Errorf("spelled %q, want %q", got, want)
	}

	var absent *model.Model
	if got, want := absent.SubjectSpelling(nil).Text, "?"; got != want {
		t.Errorf("a declaration that is not there spells its subject %q, want %q", got, want)
	}

	empty := &model.Model{}
	if got, want := empty.SubjectSpelling(nil).Text, "?"; got != want {
		t.Errorf("a declaration with no subject spells it %q, want %q", got, want)
	}

	// A model with no package is what a caller assembling one by hand leaves
	// out, and it means only that nothing is local.
	unplaced := &model.Model{Subject: &model.Struct{Named: declaring("example.com/model", "model", "Person")}}
	if got, want := unplaced.SubjectSpelling(nil).Text, "model.Person"; got != want {
		t.Errorf("a declaration in no package spells its subject %q, want %q", got, want)
	}
}

// The declaration's own package is what decides bare from qualified, since that
// is the package the generated file lands in.
func TestTheSubjectIsSpelledForTheDeclarationsPackage(t *testing.T) {
	subject := &model.Struct{Named: declaring("example.com/domain", "domain", "Person")}

	here := &model.Model{Subject: subject, Pkg: &packages.Package{PkgPath: "example.com/domain"}}
	if got, want := here.SubjectSpelling(nil).Text, "Person"; got != want {
		t.Errorf("a subject in its own package is spelled %q, want %q", got, want)
	}

	elsewhere := &model.Model{Subject: subject, Pkg: &packages.Package{PkgPath: "example.com/model"}}
	if got, want := elsewhere.SubjectSpelling(nil).Text, "domain.Person"; got != want {
		t.Errorf("a subject from elsewhere is spelled %q, want %q", got, want)
	}
	if got, want := elsewhere.SubjectSpelling([]string{"domain"}).Text, "domain2.Person"; got != want {
		t.Errorf("a subject whose name is taken is spelled %q, want %q", got, want)
	}
}
