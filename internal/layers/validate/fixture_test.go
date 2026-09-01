package validate_test

import (
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"testing"

	"github.com/okian/forge/internal/emit"
	"github.com/okian/forge/internal/layer"
	"github.com/okian/forge/internal/layers/validate"
	"github.com/okian/forge/internal/load"
	"github.com/okian/forge/internal/model"
	"github.com/okian/forge/internal/shape"
	"github.com/okian/forge/internal/subject"
)

// modelPkg is the fixture package the subjects are declared in.
const modelPkg = "validatefixture/model"

// loadFixture loads the fixture module.
func loadFixture(t *testing.T) *load.Session {
	t.Helper()

	dir, err := filepath.Abs(filepath.Join("testdata", "rules"))
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

// generating asks the layer for one fixture subject's check and returns what it
// produced, and what it said.
func generating(t *testing.T, name string) (layer.Unit, error) {
	t.Helper()

	return asking(t, modelPkg, name)
}

// refusing does the same for a subject in the package of tags that cannot
// become a check.
func refusing(t *testing.T, name string) (layer.Unit, error) {
	t.Helper()

	return asking(t, refusedPkg, name)
}

// asking generates for one subject of one fixture package.
func asking(t *testing.T, pkgPath, name string) (layer.Unit, error) {
	t.Helper()

	loaded := loadFixture(t)
	builder := subject.New(subject.Config{
		Fset:      loaded.Fset,
		Owned:     loaded.Owned(),
		Docs:      loaded.FieldDocs(),
		Generated: loaded.Generated(),
	})

	pkg, ok := loaded.Package(pkgPath)
	if !ok {
		t.Fatalf("the fixture has no package %s", pkgPath)
	}

	obj := pkg.Types.Scope().Lookup(name)
	if obj == nil {
		t.Fatalf("%s declares no %s", pkgPath, name)
	}
	held, is := types.Unalias(obj.Type()).(*types.Named)
	if !is {
		t.Fatalf("%s is a %T, want a named type", name, obj.Type())
	}

	built, problems := builder.Build(held, subject.At(token.Position{Filename: "model.go"}))
	if !problems.Empty() {
		t.Fatalf("modelling %s: %s", name, problems.Render())
	}

	return validate.New().Generate(&layer.Context{
		Model: &model.Model{
			Name: name, Form: model.FormInline, Subject: built,
			Pkg: pkg, Pos: token.Position{Filename: "model.go"},
		},
	}, shape.Shape{})
}

// written asks for a subject's check and fails the test if the layer refused.
func written(t *testing.T, name string) layer.Unit {
	t.Helper()

	unit, err := generating(t, name)
	if err != nil {
		t.Fatalf("generating for %s: %v", name, err)
	}
	return unit
}

// source renders everything a subject's generation contributed, as one file.
//
// Everything rather than the one unit about the subject, because a check is
// written for the structs it reaches as well, and what a reader is asking about
// is the file the package ends up with.
func source(t *testing.T, unit layer.Unit) string {
	t.Helper()

	file := emit.File{Package: "model"}
	for _, key := range sorted(unit.Provides) {
		held := unit.Provides[key]

		file.Sections = append(file.Sections, emit.Section{
			Decls: held.Decls, Comments: held.Comments, Fset: held.Fset,
		})
		file.Imports = append(file.Imports, held.Imports...)
	}

	out, err := file.Render()
	if err != nil {
		t.Fatalf("rendering the check: %v", err)
	}
	return string(out)
}

// sorted returns a map's keys in order, so that what a test renders does not
// depend on how a map iterated.
func sorted[V any](held map[string]V) []string {
	out := make([]string, 0, len(held))
	for key := range held {
		out = append(out, key)
	}

	for i := range out {
		for j := i + 1; j < len(out); j++ {
			if out[j] < out[i] {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out
}

// fixtureSource returns the fixture's own source, so that what is generated is
// compiled against the types it was generated from rather than against a second
// copy of them written out here.
func fixtureSource(t *testing.T) []byte {
	t.Helper()

	held, err := os.ReadFile(filepath.Join("testdata", "rules", "model", "model.go"))
	if err != nil {
		t.Fatalf("reading the fixture: %v", err)
	}
	return held
}
