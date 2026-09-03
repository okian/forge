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

	held := written(t, files, generate.Name())

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
	bothWays(t, files)
}

// The whole worked example — a refining layer over bounded storage over a
// subject with a codec — generates the surface it is documented to, and that
// surface compiles.
//
// It is the first stack of three layers of three different kinds, and the first
// where a layer writes for a type it does not sit next to: the codec is an
// element layer at the bottom of the stack, and what it writes for the
// container is written over methods the two layers above it decided on
// afterwards. Nothing else in the suite puts a layer in that position.
func TestTheWorkedExampleStreamsThroughEveryLayer(t *testing.T) {
	asked := request("Persons", "//forge:collection sort=Name index=ID", "//forge:ring cap=1024")
	asked.Model.Form = model.FormSpec
	asked.Model.Stack = []model.LayerRef{
		{Origin: model.TypeRef{Pkg: model.MarkerPkg, Name: "Collection"}, Kind: model.KindRefining},
		{Origin: model.TypeRef{Pkg: model.MarkerPkg, Name: "Ring"}, Kind: model.KindStorage},
		{Origin: model.TypeRef{Pkg: model.MarkerPkg, Name: "Json"}, Kind: model.KindElement},
	}

	files, diags := generate.Package(local, "model", []generate.Request{asked}, config())
	if !diags.Empty() {
		t.Fatalf("generating was refused:\n%s", diags.Render())
	}

	held := written(t, files, generate.Name())

	for _, want := range []string{
		// The ring's representation and the collection's queries, as the
		// example without a codec already produces.
		"personsFixedCap = 1024",
		"func NewPersons() *Persons {",
		"func (r *Persons) Push(v Person) {",
		"func (c Persons) SortedByName() []Person {",

		// And the codec for the whole stack, written over the walk the ring
		// exposes and calling the codec written for the subject. The receiver
		// is a pointer because the ring's walk takes one.
		"func (c *Persons) AppendJSON(dst []byte) ([]byte, error) {",
		"func (c *Persons) UnmarshalJSON(data []byte) error {",
		"func (c *Persons) WriteTo(w io.Writer) (int64, error) {",
		"func (c *Persons) ReadFrom(r io.Reader) (int64, error) {",
		"for v := range c.All() {",
		"c.AppendSeq(func(yield func(Person) bool) {",
	} {
		if !bytes.Contains(held, []byte(want)) {
			t.Errorf("the output does not hold %q:\n%s", want, held)
		}
	}

	// The subject's own codec goes in the file the package shares, since two
	// declarations over one subject want one copy of it.
	shared := written(t, files, generate.Name())
	for _, want := range []string{
		"func (v Person) AppendJSON(dst []byte) ([]byte, error) {",
		"func (v *Person) UnmarshalJSON(data []byte) error {",
		"func appendModelPersonJSON(dst []byte, v Person) ([]byte, error) {",
	} {
		if !bytes.Contains(shared, []byte(want)) {
			t.Errorf("the shared file does not hold %q:\n%s", want, shared)
		}
	}

	bothWays(t, files)
}

// bothWays compiles what a spec declaration generated, under both build
// configurations.
//
// Both, because a spec declaration is only half-written until the build that
// excludes its file holds the other half. The author's own declaration stands
// in under the tag, which is the arrangement the two constraints exist to
// produce; the subject is compiled either way.
func bothWays(t *testing.T, files []generate.File) {
	t.Helper()

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

// A container that reports a refusal rather than dropping its oldest element
// gets a codec that reads the refusal, and that codec compiles.
//
// It is the one shape of generated codec nothing else reaches. The matrix
// builds every stack and writes no options, so it never sees a ring asked to
// refuse — and what the option changes here is the shape of a statement rather
// than the value of a flag: the sink answers, so the call has to bind what it
// answers with and act on it.
func TestACodecOverAContainerThatRefusesElements(t *testing.T) {
	asked := request("Persons", "//forge:ring cap=4 overflow=error")
	asked.Model.Form = model.FormSpec
	asked.Model.Stack = []model.LayerRef{
		{Origin: model.TypeRef{Pkg: model.MarkerPkg, Name: "Ring"}, Kind: model.KindStorage},
		{Origin: model.TypeRef{Pkg: model.MarkerPkg, Name: "Json"}, Kind: model.KindElement},
	}

	files, diags := generate.Package(local, "model", []generate.Request{asked}, config())
	if !diags.Empty() {
		t.Fatalf("generating was refused:\n%s", diags.Render())
	}

	held := written(t, files, generate.Name())
	for _, want := range []string{
		"refused := c.AppendSeq(func(yield func(Person) bool) {",
		"if refused != nil {",
	} {
		if !bytes.Contains(held, []byte(want)) {
			t.Errorf("the codec does not hold %q:\n%s", want, held)
		}
	}

	bothWays(t, files)
}
