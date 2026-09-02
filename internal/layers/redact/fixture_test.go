package redact_test

import (
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/okian/forge/internal/emit"
	"github.com/okian/forge/internal/goldentest"
	"github.com/okian/forge/internal/layers/redact"
	"github.com/okian/forge/internal/load"
	"github.com/okian/forge/internal/subject"
	"github.com/okian/forge/plugin"
)

// modelPkg is the fixture package the subjects are declared in.
const modelPkg = "redactfixture/model"

// declaredAt is where the declaration these tests generate for was written.
var declaredAt = token.Position{Filename: "model.go", Line: 10, Column: 6}

// loadFixture loads the fixture module.
func loadFixture(t *testing.T) *load.Session {
	t.Helper()

	dir, err := filepath.Abs(filepath.Join("testdata", "secrets"))
	if err != nil {
		t.Fatalf("resolving the fixture: %v", err)
	}

	loaded, err := load.Load(load.Config{Dir: dir})
	if err != nil {
		t.Fatalf("loading the fixture: %v", err)
	}
	if !loaded.Diagnostics.Empty() {
		t.Fatalf("the fixture does not load clean:\n%s", loaded.Diagnostics.Render())
	}
	return loaded
}

// generating asks the layer for one fixture subject's log value and returns
// what it produced, and what it said.
func generating(t *testing.T, name string) (plugin.Unit, error) {
	t.Helper()
	return asking(t, modelPkg, name)
}

// asking generates for one subject of one fixture package, into the model
// package — so that a subject declared somewhere else is asked about from where
// a declaration over it would be written.
func asking(t *testing.T, from, name string) (plugin.Unit, error) {
	t.Helper()

	loaded := loadFixture(t)

	declares, ok := loaded.Package(from)
	if !ok {
		t.Fatalf("the fixture has no package %s", from)
	}

	pkg, ok := loaded.Package(modelPkg)
	if !ok {
		t.Fatalf("the fixture has no package %s", modelPkg)
	}

	obj := declares.Types.Scope().Lookup(name)
	if obj == nil {
		t.Fatalf("%s declares no %s", from, name)
	}

	held, is := types.Unalias(obj.Type()).(*types.Named)
	if !is {
		t.Fatalf("%s is a %T, want a named type", name, obj.Type())
	}

	built, problems := subject.New(subject.Config{
		Fset:      loaded.Fset,
		Owned:     loaded.Owned(),
		Docs:      loaded.FieldDocs(),
		Generated: loaded.Generated(),
	}).Build(held, subject.At(declaredAt))
	if !problems.Empty() {
		t.Fatalf("modelling %s: %s", name, problems.Render())
	}

	ctx := (&plugin.Context{
		Model: &plugin.Model{
			Name: name, Form: plugin.FormSpec, Subject: built,
			Pkg: pkg, Pos: declaredAt,
		},
	}).Binding(redact.New().Binds())

	return redact.New().Generate(ctx, plugin.Shape{})
}

// written asks for a subject's log value and fails the test if the layer
// refused.
func written(t *testing.T, name string) plugin.Unit {
	t.Helper()

	unit, err := generating(t, name)
	if err != nil {
		t.Fatalf("generating for %s: %v", name, err)
	}
	return unit
}

// refused asks for a subject's log value and fails the test if the layer wrote
// one.
func refused(t *testing.T, name string) error {
	t.Helper()

	_, err := generating(t, name)
	if err == nil {
		t.Fatalf("%s was written a log value and should have been refused", name)
	}
	return err
}

// source renders everything a subject's generation contributed, as one file.
//
// Everything rather than the one unit about the subject, because a log value is
// written for the structs it reaches as well — which is the whole of what makes
// this layer more than the one a tag earns on its own — and what a reader is
// asking about is the file the package ends up with.
func source(t *testing.T, unit plugin.Unit) string {
	t.Helper()

	file := emit.File{Package: "model"}
	for _, key := range slices.Sorted(keys(unit.Provides)) {
		held := unit.Provides[key]

		file.Sections = append(file.Sections, emit.Section{
			Decls: held.Decls, Comments: held.Comments, Fset: held.Fset,
		})
		file.Imports = append(file.Imports, held.Imports...)
	}

	out, err := file.Render()
	if err != nil {
		t.Fatalf("rendering the log value: %v", err)
	}
	return string(out)
}

// keys yields a map's keys, so that what a test renders does not depend on how
// a map iterated.
func keys[V any](held map[string]V) func(func(string) bool) {
	return func(yield func(string) bool) {
		for key := range held {
			if !yield(key) {
				return
			}
		}
	}
}

// compiles checks that what was generated is a package, alongside the subjects
// it was generated from.
//
// The fixture's own source rather than a copy written out here, so that a
// method generated for a type is compiled against that type rather than against
// a second declaration of it that has drifted.
func compiles(t *testing.T, out string) {
	t.Helper()

	goldentest.Check(t, goldentest.Package{
		Path: modelPkg,
		Files: []goldentest.Source{
			{Name: "model.go", Content: fixtureSource(t)},
			{Name: "forge.gen.go", Content: []byte(out), Generated: true},
		},
		Requires: []goldentest.Package{beside(t)},
	})
}

// beside returns the package the fixture reaches a secret in, so that what is
// generated is compiled against it rather than against a second copy of it.
func beside(t *testing.T) goldentest.Package {
	t.Helper()

	held, err := os.ReadFile(filepath.Join("testdata", "secrets", "other", "other.go"))
	if err != nil {
		t.Fatalf("reading the neighbouring fixture: %v", err)
	}

	return goldentest.Package{
		Path:  "redactfixture/other",
		Files: []goldentest.Source{{Name: "other.go", Content: held}},
	}
}

// fixtureSource returns the fixture's own source.
func fixtureSource(t *testing.T) []byte {
	t.Helper()

	held, err := os.ReadFile(filepath.Join("testdata", "secrets", "model", "model.go"))
	if err != nil {
		t.Fatalf("reading the fixture: %v", err)
	}
	return held
}
