package merge_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"slices"
	"strings"
	"testing"

	"github.com/okian/forge/internal/emit"
	"github.com/okian/forge/internal/layer"
	"github.com/okian/forge/internal/merge"
	"github.com/okian/forge/internal/model"
	"github.com/okian/forge/internal/shape"
)

// generated parses a fragment of Go into the shape a layer hands over.
func generated(t *testing.T, source string) layer.Unit {
	t.Helper()

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "layer.go", "package tmpl\n\n"+source,
		parser.ParseComments|parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	return layer.Unit{Decls: file.Decls, Comments: file.Comments, Fset: fset}
}

// rendered writes what was merged, which is the only way to see the order the
// declarations ended up in.
func rendered(t *testing.T, merged merge.Unit) string {
	t.Helper()

	out, err := emit.File{
		Package:  "model",
		Imports:  merged.ImportSpecs(),
		Sections: merged.Sections,
	}.Render()
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	return string(out)
}

// marker builds a reference to one of the markers a stack is written against.
func marker(name string) model.TypeRef {
	return model.TypeRef{Pkg: model.MarkerPkg, Name: name}
}

// Declarations keep the order the layers were given in, which is the stack's
// own, so the file a declaration produces is a function of the declaration.
func TestUnitsKeepStackOrder(t *testing.T) {
	merged := merge.Units(
		generated(t, "type Persons struct{}\n\ntype PersonsSeq struct{}"),
		generated(t, "type personsRing struct{}"),
	)

	out := rendered(t, merged)
	at := 0
	for _, want := range []string{"type Persons struct", "type PersonsSeq struct", "type personsRing struct"} {
		found := strings.Index(out[at:], want)
		if found < 0 {
			t.Fatalf("%q is missing or out of order in:\n%s", want, out)
		}
		at += found
	}
}

// What one layer generated stays one section, because its declarations only
// make sense beside the comments and the file set they were parsed with.
func TestEachLayerIsItsOwnSection(t *testing.T) {
	merged := merge.Units(
		generated(t, "// Persons holds people.\ntype Persons struct{}"),
		generated(t, "// Push adds one.\nfunc (p *Persons) Push(v Person) {\n\t// grow\n}"),
	)

	if got, want := len(merged.Sections), 2; got != want {
		t.Fatalf("merged into %d sections, want %d", got, want)
	}

	out := rendered(t, merged)
	for _, want := range []string{"// Persons holds people.", "// Push adds one.", "// grow"} {
		if !strings.Contains(out, want) {
			t.Errorf("%q did not survive merging:\n%s", want, out)
		}
	}
}

// Two layers needing one import need it once, and the order they are kept in is
// the order they were first asked for — not one a map decided.
func TestUnitsDeduplicateImports(t *testing.T) {
	merged := merge.Units(
		layer.Unit{Imports: []string{"iter", "encoding/json/jsontext"}},
		layer.Unit{Imports: []string{"iter", "", "slices"}},
	)

	want := []string{"iter", "encoding/json/jsontext", "slices"}
	if !slices.Equal(merged.Imports, want) {
		t.Errorf("imports = %v, want %v", merged.Imports, want)
	}

	specs := merged.ImportSpecs()
	if len(specs) != len(want) {
		t.Fatalf("ImportSpecs() gave %d, want %d", len(specs), len(want))
	}
	for i, path := range want {
		if specs[i] != (emit.Import{Path: path}) {
			t.Errorf("ImportSpecs()[%d] = %+v, want the path alone", i, specs[i])
		}
	}
}

// A helper named twice is emitted once, which is the whole reason a layer names
// what it needs instead of emitting it.
func TestUnitsDeduplicateRequirements(t *testing.T) {
	seq, view := marker("Seq"), marker("View")

	merged := merge.Units(
		layer.Unit{Requires: []model.TypeRef{seq}},
		layer.Unit{Requires: []model.TypeRef{seq, view, {}}},
	)

	if want := []model.TypeRef{seq, view}; !slices.Equal(merged.Requires, want) {
		t.Errorf("requires = %v, want %v", merged.Requires, want)
	}
}

// The same claim made by two layers is one claim.
func TestUnitsDeduplicateAssertions(t *testing.T) {
	writerTo := layer.Assertion{Interface: model.TypeRef{Pkg: "io", Name: "WriterTo"}}
	all := layer.Assertion{
		Interface: model.TypeRef{Pkg: "iter", Name: "Seq"},
		Method:    "All",
		Signature: "func(*Persons) iter.Seq[Person]",
	}

	merged := merge.Units(
		layer.Unit{Assertions: []layer.Assertion{writerTo}},
		layer.Unit{Assertions: []layer.Assertion{all, writerTo}},
	)

	if want := []layer.Assertion{writerTo, all}; !slices.Equal(merged.Assertions, want) {
		t.Errorf("assertions = %v, want %v", merged.Assertions, want)
	}
}

// A layer that generated nothing contributes no section, so a stack of stubs
// merges to a unit that writes nothing rather than to a run of blank lines.
func TestLayersThatGeneratedNothingContributeNoSection(t *testing.T) {
	merged := merge.Units(
		layer.Unit{},
		layer.Unit{Decls: []ast.Decl{nil}},
		generated(t, "type Persons struct{}"),
	)

	if got, want := len(merged.Sections), 1; got != want {
		t.Fatalf("merged into %d sections, want %d", got, want)
	}
	if merged.Empty() {
		t.Error("a unit with a declaration in it reports itself empty")
	}
	if !merge.Units(layer.Unit{}, layer.Unit{}).Empty() {
		t.Error("a unit with nothing in it does not report itself empty")
	}
}

// A layer handing over a declaration that is nothing is a bug in that layer,
// and merging happens two stages before anything can report one. It has to
// carry the declaration through to where there is somewhere to report to.
func TestALayerHandingOverNothingDoesNotStopTheMerge(t *testing.T) {
	var missing *ast.GenDecl

	merged := merge.Units(layer.Unit{Decls: []ast.Decl{missing}})

	if merged.Empty() {
		t.Error("a unit holding a declaration that is nothing merged to nothing")
	}
	if _, err := (emit.File{Package: "model", Sections: merged.Sections}).Render(); err == nil {
		t.Error("the declaration was written without complaint")
	}
}

// Merging nothing is a unit with nothing in it, not a panic: a stack every
// layer of which generated nothing is what the whole catalog does today.
func TestMergingNothing(t *testing.T) {
	merged := merge.Units()

	if !merged.Empty() || len(merged.Imports) != 0 ||
		len(merged.Assertions) != 0 || len(merged.Requires) != 0 {
		t.Errorf("merging nothing gave %+v", merged)
	}
}

// The same units merged twice are the same file. Everything downstream rests on
// it, and nothing here may reach for a map to get it.
func TestMergingIsDeterministic(t *testing.T) {
	units := func() []layer.Unit {
		first := generated(t, "// Persons holds people.\ntype Persons struct{}\n\nfunc NewPersons() *Persons { return nil }")
		first.Imports = []string{"iter", "slices"}
		first.Requires = []model.TypeRef{marker("Seq")}
		first.Assertions = []layer.Assertion{{Interface: model.TypeRef{Pkg: "io", Name: "WriterTo"}}}

		second := generated(t, "func (p *Persons) Len() int {\n\t// count\n\treturn 0\n}")
		second.Imports = []string{"slices", "encoding/json/jsontext"}
		second.Requires = []model.TypeRef{marker("Seq"), marker("View")}

		return []layer.Unit{first, second}
	}

	first := rendered(t, merge.Units(units()...))
	for range 8 {
		if again := rendered(t, merge.Units(units()...)); again != first {
			t.Fatalf("two merges rendered differently:\n%s\n---\n%s", first, again)
		}
	}
}

// What a stub generates is nothing at all, and merging what the whole catalog
// generates today has to be that rather than a crash.
func TestMergingWhatTheCatalogGeneratesToday(t *testing.T) {
	registry := layer.Builtins()

	var units []layer.Unit
	for _, found := range registry.All() {
		unit, err := found.Generate(&layer.Context{Model: &model.Model{Name: "Persons"}}, shape.Shape{})
		if err == nil {
			t.Errorf("%s generated without complaint", found.Origin().Name)
		}
		units = append(units, unit)
	}

	if merged := merge.Units(units...); !merged.Empty() {
		t.Errorf("merging %d empty units gave %d sections", len(units), len(merged.Sections))
	}
}
