package seq_test

import (
	"bytes"
	"go/token"
	"testing"

	"github.com/okian/forge/internal/emit"
	"github.com/okian/forge/internal/goldentest"
	"github.com/okian/forge/internal/layer"
	"github.com/okian/forge/internal/merge"
	"github.com/okian/forge/internal/model"
	"github.com/okian/forge/internal/shared/seq"
)

// declaredAt stands in for the declaration that required the view, which is
// what a fault in reading it is reported against.
var declaredAt = token.Position{Filename: "model/spec.go", Line: 8, Column: 6}

// rendered writes the view as the file it will be written to, through the same
// merge and emit steps generation uses.
func rendered(t *testing.T) []byte {
	t.Helper()

	unit, err := seq.Unit(declaredAt)
	if err != nil {
		t.Fatalf("the view could not be read: %v", err)
	}

	merged := merge.Units(unit)

	out, err := emit.File{
		Package:  "model",
		Decl:     seq.Name,
		Pos:      declaredAt,
		Imports:  merged.Imports,
		Sections: merged.Sections,
	}.Render()
	if err != nil {
		t.Fatalf("rendering the file: %v", err)
	}
	return out
}

// The view is emitted unchanged, and what it is emitted as is Go that compiles
// on its own — it names no subject, so unlike a layer's output there is no
// fixture it needs beside it.
func TestTheViewIsEmittedAsItIsWritten(t *testing.T) {
	goldentest.Check(t, goldentest.Package{
		Path: "model",
		Files: []goldentest.Source{
			{Name: "zz_forge_shared.go", Content: rendered(t), Generated: true},
		},
	})
}

// The package comment describes the template and not what the template becomes,
// so it is left behind — otherwise every package that gains the view gains a
// paragraph about forge's own arrangements at the top of a file.
func TestWhatTheTemplateKeepsToItself(t *testing.T) {
	out := rendered(t)

	for _, gone := range []string{
		"Package tmpl",
		"emitted once per package",
		"written for the file they end up in",
	} {
		if bytes.Contains(out, []byte(gone)) {
			t.Errorf("the output carries %q, which describes the template:\n%s", gone, out)
		}
	}

	// And what documents the view itself survives, since that is what a reader
	// of the generated file is there for.
	for _, want := range []string{
		"// Seq is a lazy view over a sequence of elements.",
		"// Filter returns a view of the elements the predicate keeps.",
	} {
		if !bytes.Contains(out, []byte(want)) {
			t.Errorf("the output does not carry %q:\n%s", want, out)
		}
	}
}

// A unit names what it needs imported, by path and by the name the bodies call
// it — which for the view is the two the standard library gives it.
func TestWhatTheViewNeedsImported(t *testing.T) {
	unit, err := seq.Unit(declaredAt)
	if err != nil {
		t.Fatalf("the view could not be read: %v", err)
	}

	want := []emit.Import{{Path: "iter", Name: "iter"}, {Path: "slices", Name: "slices"}}
	if len(unit.Imports) != len(want) {
		t.Fatalf("it imports %v, want %v", unit.Imports, want)
	}
	for i, one := range want {
		if unit.Imports[i] != one {
			t.Errorf("import %d is %+v, want %+v", i, unit.Imports[i], one)
		}
	}

	// Nothing else: the view declares no helper for anything to require, and
	// claims no interface for anything to assert.
	if len(unit.Requires) != 0 || len(unit.Assertions) != 0 {
		t.Errorf("the view requires %v and asserts %v", unit.Requires, unit.Assertions)
	}
}

// The view is emitted into the package that needs it rather than imported from
// one, so its identity is that package's — which is what makes two declarations
// there share one copy and a declaration elsewhere get its own.
func TestWhichPackageTheViewBelongsTo(t *testing.T) {
	here := seq.Ref("example.com/model")

	if want := (model.TypeRef{Pkg: "example.com/model", Name: "Seq"}); here != want {
		t.Errorf("Ref() = %+v, want %+v", here, want)
	}
	if here == seq.Ref("example.com/store") {
		t.Error("the view in one package is the same reference as the view in another")
	}

	// Two declarations in one package name one reference, so merging what they
	// required leaves one.
	merged := merge.Units(
		layerRequiring(here),
		layerRequiring(seq.Ref("example.com/model")),
	)
	if len(merged.Requires) != 1 {
		t.Errorf("two declarations requiring the view required %v", merged.Requires)
	}
}

// The same declaration read twice is the same bytes, which is what lets
// generation skip a write when nothing has changed.
func TestReadingTheViewTwiceIsTheSameBytes(t *testing.T) {
	if first, second := rendered(t), rendered(t); !bytes.Equal(first, second) {
		t.Errorf("two reads differ:\n%s", first)
	}
}

// layerRequiring is a unit that names the view and generates nothing, which is
// what a layer calling into it contributes.
func layerRequiring(ref model.TypeRef) layer.Unit {
	return layer.Unit{Requires: []model.TypeRef{ref}}
}
