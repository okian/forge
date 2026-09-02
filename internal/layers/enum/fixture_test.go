package enum_test

import (
	"go/token"
	"go/types"
	"os"
	"path"
	"path/filepath"
	"slices"
	"testing"

	"golang.org/x/tools/go/packages"

	"github.com/okian/forge/internal/emit"
	"github.com/okian/forge/internal/goldentest"
	"github.com/okian/forge/internal/layers/enum"
	"github.com/okian/forge/internal/load"
	"github.com/okian/forge/internal/subject"
	"github.com/okian/forge/plugin"
)

// modelPkg is the fixture package the closed sets are declared in.
const modelPkg = "enumfixture/model"

// declaredAt is where the declaration these tests generate for was written.
var declaredAt = token.Position{Filename: "model.go", Line: 10, Column: 6}

// loadFixture loads the fixture module.
func loadFixture(t *testing.T) *load.Session {
	t.Helper()

	dir, err := filepath.Abs(filepath.Join("testdata", "closed"))
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

// generating asks the layer for one fixture subject's set and returns what it
// produced, and what it said.
func generating(t *testing.T, name string) (plugin.Unit, error) {
	t.Helper()
	return asking(t, name, modelPkg)
}

// asking generates for one fixture subject into a package of the caller's
// choosing, so that a declaration written somewhere other than where the type
// is declared can be asked about.
func asking(t *testing.T, name, into string) (plugin.Unit, error) {
	t.Helper()

	loaded := loadFixture(t)

	pkg, ok := loaded.Package(modelPkg)
	if !ok {
		t.Fatalf("the fixture has no package %s", modelPkg)
	}

	obj := pkg.Types.Scope().Lookup(name)
	if obj == nil {
		t.Fatalf("%s declares no %s", modelPkg, name)
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

	written := pkg
	if into != modelPkg {
		written = &packages.Package{PkgPath: into, Name: path.Base(into), Fset: loaded.Fset}
	}

	ctx := (&plugin.Context{
		Model: &plugin.Model{
			Name: name, Form: plugin.FormSpec, Subject: built,
			Pkg: written, Pos: declaredAt,
		},
	}).Binding(enum.New().Binds())

	return enum.New().Generate(ctx, plugin.Shape{})
}

// written asks for a subject's set and fails the test if the layer refused.
func written(t *testing.T, name string) plugin.Unit {
	t.Helper()

	unit, err := generating(t, name)
	if err != nil {
		t.Fatalf("generating for %s: %v", name, err)
	}
	return unit
}

// refused asks for a subject's set and fails the test if the layer wrote one.
func refused(t *testing.T, name string) error {
	t.Helper()

	_, err := generating(t, name)
	if err == nil {
		t.Fatalf("%s was written a closed set and should have been refused", name)
	}
	return err
}

// source renders what a subject's generation contributed, as one file.
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
		t.Fatalf("rendering the set: %v", err)
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

// compiles checks that what was generated is a package, alongside the types it
// was generated from.
func compiles(t *testing.T, out string) {
	t.Helper()

	goldentest.Check(t, goldentest.Package{
		Path:  modelPkg,
		Files: append(fixture(t), goldentest.Source{Name: "forge.gen.go", Content: []byte(out), Generated: true}),
	})
}

// fixture returns every file of the fixture package.
//
// Every one rather than the ones a given set needs, because a set declared in
// one file and constant-ed in another is the case worth compiling and a list
// written by hand would be the list somebody forgot to add a file to. Found on
// disk for the same reason: a file added to the fixture is compiled without
// anybody remembering to say so.
func fixture(t *testing.T) []goldentest.Source {
	t.Helper()

	found, err := filepath.Glob(filepath.Join("testdata", "closed", "model", "*.go"))
	if err != nil {
		t.Fatalf("looking for the fixture's files: %v", err)
	}
	slices.Sort(found)

	out := make([]goldentest.Source, 0, len(found))
	for _, path := range found {
		held, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("reading the fixture: %v", err)
		}
		out = append(out, goldentest.Source{Name: filepath.Base(path), Content: held})
	}

	return out
}
