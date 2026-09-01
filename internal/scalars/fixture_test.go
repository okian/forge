package scalars_test

import (
	"go/printer"
	"go/token"
	"go/types"
	"path/filepath"
	"strings"
	"testing"

	"github.com/okian/forge/internal/diag"
	"github.com/okian/forge/internal/layer"
	"github.com/okian/forge/internal/load"
	"github.com/okian/forge/internal/model"
	"github.com/okian/forge/internal/scalars"
	"github.com/okian/forge/internal/subject"
)

// modelPkg is the fixture package the subjects are declared in.
const modelPkg = "scalarfixture/model"

// fixture holds the load, which is read and never written.
var fixture *load.Session

// loadFixture loads the fixture module once for the whole package.
func loadFixture(t *testing.T) *load.Session {
	t.Helper()

	if fixture != nil {
		return fixture
	}

	dir, err := filepath.Abs(filepath.Join("testdata", "tagged"))
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

	fixture = loaded
	return loaded
}

// modelled builds the model of one fixture subject.
func modelled(t *testing.T, name string) *model.Struct {
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
	}).Build(held, subject.At(token.Position{Filename: "model.go"}))
	if !problems.Empty() {
		t.Fatalf("modelling %s: %s", name, problems.Render())
	}

	return built
}

// written returns what the emitters wrote for a subject, keyed by verb, and
// fails the test where anything was reported.
func written(t *testing.T, name string) map[string]string {
	t.Helper()

	out, diags := writing(t, name)
	if !diags.Empty() {
		t.Fatalf("writing for %s was refused:\n%s", name, diags.Render())
	}
	return out
}

// writing is the same without the expectation, for a test about what is
// reported.
func writing(t *testing.T, name string) (map[string]string, diag.Set) {
	t.Helper()

	var diags diag.Set

	held, err := scalars.For(asked(t, name), &diags)
	if err != nil {
		t.Fatalf("writing for %s: %v", name, err)
	}

	out := make(map[string]string, len(held))
	for about, unit := range held {
		verb, _, _ := strings.Cut(about, ":")
		out[verb] = source(t, unit)
	}
	return out, diags
}

// asked builds what the emitters are handed for one fixture subject.
//
// Every declarable type in the fixture counts as a subject of the run, which is
// what a package generating for all of them would hand over. A test that named
// only the subject under it would answer differently about a field whose type
// is another of them, and that difference is one of the things being tested.
func asked(t *testing.T, name string) scalars.Asked {
	t.Helper()

	return scalars.Asked{
		Subject: modelled(t, name),
		Local:   modelPkg,
		At:      token.Position{Filename: "model.go", Line: 1, Column: 1},
		Earning: reading(t),
	}
}

// read holds the answer, which is the same for every test in the package.
var read map[string]bool

// reading returns which of the fixture's types a run over all of them would
// give a String to.
func reading(t *testing.T) map[string]bool {
	t.Helper()

	if read != nil {
		return read
	}

	loaded := loadFixture(t)

	pkg, ok := loaded.Package(modelPkg)
	if !ok {
		t.Fatalf("the fixture has no package %s", modelPkg)
	}

	built := subject.New(subject.Config{
		Fset:      loaded.Fset,
		Owned:     loaded.Owned(),
		Docs:      loaded.FieldDocs(),
		Generated: loaded.Generated(),
	})

	out := make(map[string]bool)

	for _, name := range pkg.Types.Scope().Names() {
		held, is := types.Unalias(pkg.Types.Scope().Lookup(name).Type()).(*types.Named)
		if !is {
			continue
		}

		one, problems := built.Build(held, subject.At(token.Position{Filename: "model.go"}))
		if !problems.Empty() || !scalars.Earns(one) {
			continue
		}

		out[model.TypeIdentity(one.Type())] = true
	}

	read = out
	return out
}

// source renders a contribution back as Go, so that a test can read what an
// author would read.
func source(t *testing.T, unit layer.Unit) string {
	t.Helper()

	var b strings.Builder

	fset := unit.Fset
	if fset == nil {
		fset = token.NewFileSet()
	}

	for _, decl := range unit.Decls {
		if err := printer.Fprint(&b, fset, decl); err != nil {
			t.Fatalf("printing what was written: %v", err)
		}
		b.WriteString("\n\n")
	}

	return b.String()
}
