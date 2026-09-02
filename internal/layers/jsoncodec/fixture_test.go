package jsoncodec_test

import (
	_ "embed"
	"go/token"
	"go/types"
	"path/filepath"
	"slices"
	"testing"

	"github.com/okian/forge/internal/layers/jsoncodec"
	"github.com/okian/forge/internal/load"
	"github.com/okian/forge/internal/subject"
	"github.com/okian/forge/plugin"
)

// agreement is the test that runs inside the generated module, comparing what
// forge wrote against what the standard library does reflectively.
//
// Embedded from a file rather than quoted, so that it is Go this repository's
// own tools read: a comparison held only as a string is one nothing checks
// until the day it fails to compile inside a temporary directory.
//
//go:embed testdata/agreement.go.txt
var agreement []byte

// loadFixture loads the fixture module once per test.
func loadFixture(t *testing.T) *load.Session {
	t.Helper()

	dir, err := filepath.Abs(filepath.Join("testdata", "codec"))
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

// named returns the named type the fixture declares under this name.
func named(t *testing.T, loaded *load.Session, name string) *types.Named {
	t.Helper()

	pkg, ok := loaded.Package(modelPkg)
	if !ok {
		t.Fatalf("the fixture has no package %s", modelPkg)
	}

	obj := pkg.Types.Scope().Lookup(name)
	if obj == nil {
		t.Fatalf("%s declares no %s", modelPkg, name)
	}

	held, ok := types.Unalias(obj.Type()).(*types.Named)
	if !ok {
		t.Fatalf("%s is a %T, want a named type", name, obj.Type())
	}
	return held
}

// structural reports whether a type is a struct, which is what a codec is
// written for. The fixture's named scalars are reached through the structs that
// hold them rather than asked for on their own.
func structural(t types.Type) bool {
	_, ok := t.Underlying().(*types.Struct)
	return ok
}

// sortedKeys returns a map's keys in order, so that what a test renders does
// not depend on how a map iterated.
func sortedKeys[V any](held map[string]V) []string {
	out := make([]string, 0, len(held))
	for key := range held {
		out = append(out, key)
	}
	slices.Sort(out)
	return out
}

// codecUnit asks the layer for one fixture subject's codec and returns it.
func codecUnit(t *testing.T, pkgPath, name string) plugin.Unit {
	t.Helper()

	loaded := loadFixture(t)
	builder := subject.New(subject.Config{
		Fset:  loaded.Fset,
		Owned: loaded.Owned(),
		Docs:  loaded.FieldDocs(),
	})

	pkg, ok := loaded.Package(pkgPath)
	if !ok {
		t.Fatalf("the fixture has no package %s", pkgPath)
	}

	obj := pkg.Types.Scope().Lookup(name)
	if obj == nil {
		t.Fatalf("%s declares no %s", pkgPath, name)
	}
	held, ok := types.Unalias(obj.Type()).(*types.Named)
	if !ok {
		t.Fatalf("%s is a %T, want a named type", name, obj.Type())
	}

	built, problems := builder.Build(held, subject.Site{})
	if !problems.Empty() {
		t.Fatalf("modelling %s: %s", name, problems.Render())
	}

	unit, err := jsoncodec.New().Generate(&plugin.Context{
		Model: &plugin.Model{
			Name: name, Form: plugin.FormInline, Subject: built,
			Pkg: pkg, Pos: token.Position{Filename: "person.go"},
		},
	}, plugin.Shape{})
	if err != nil {
		t.Fatalf("generating a codec for %s: %v", name, err)
	}

	return unit
}
