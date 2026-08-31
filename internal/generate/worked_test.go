package generate_test

import (
	"bytes"
	"testing"

	"github.com/okian/forge/internal/generate"
	"github.com/okian/forge/internal/goldentest"
	"github.com/okian/forge/internal/model"
)

// A refining layer over storage that is not a slice generates, and what it
// generates compiles.
//
// It is the first stack of two written layers, and the first where one of them
// is not representation-transparent — so the declaration is one forge owns the
// type of, and the file it lands in is constrained against the tag. Everything
// the two layers decide separately has to agree here: the ring says what the
// underlying type is and the collection writes methods over the walk it
// exposes, and neither of them has seen the other.
func TestARefiningLayerOverAContainerThatIsNotASlice(t *testing.T) {
	asked := request("Persons", "//forge:collection sort=Name index=ID", "//forge:ring cap=1024")
	asked.Model.Form = model.FormSpec
	asked.Model.Stack = []model.LayerRef{
		{Origin: model.TypeRef{Pkg: model.MarkerPkg, Name: "Collection"}, Kind: model.KindRefining},
		{Origin: model.TypeRef{Pkg: model.MarkerPkg, Name: "Ring"}, Kind: model.KindStorage},
	}

	files, diags := generate.Package(local, "model", []generate.Request{asked}, config())
	if !diags.Empty() {
		t.Fatalf("generating was refused:\n%s", diags.Render())
	}

	held := written(t, files, generate.Named("Persons"))

	for _, want := range []string{
		// The ring's, including the capacity the directive fixed.
		"personsFixedCap = 1024",
		"func NewPersons() *Persons {",
		"func (r *Persons) Push(v Person) {",

		// And the collection's, written over the walk the ring exposes rather
		// than over a slice it cannot assume is there. The receiver is a value,
		// which reads a container the ring's own methods take a pointer to —
		// legal, since a parameter is addressable, and cheap, since what is
		// copied is a slice header and two integers rather than the elements.
		"func (c Persons) SortedByName() []Person {",
		"func (c Persons) ByID() map[int]Person {",
	} {
		if !bytes.Contains(held, []byte(want)) {
			t.Errorf("the output does not hold %q:\n%s", want, held)
		}
	}

	// Compiled under both tag settings, because a spec declaration is only
	// half-written until the build that excludes it holds the other half.
	//
	// The subject alone, rather than the source the inline cases compile
	// against: that one declares Persons, because there the author wrote it.
	// Here they wrote it under the tag, which is the file below.
	spec := goldentest.Source{Name: "spec.go", Content: []byte(
		"//go:build forgespec\n\npackage model\n\ntype Persons struct{}\n")}

	for _, tags := range [][]string{nil, {"forgespec"}} {
		sources := []goldentest.Source{subject, spec}
		for _, file := range files {
			sources = append(sources, goldentest.Source{Name: file.Name, Content: file.Content, Generated: true})
		}

		if err := goldentest.Compiles(goldentest.Package{Path: "model", Tags: tags, Files: sources}); err != nil {
			t.Errorf("the package does not compile with tags %v: %v", tags, err)
		}
	}
}
