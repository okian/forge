package explain_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/okian/forge/internal/explain"
	"github.com/okian/forge/internal/goldentest"
	"github.com/okian/forge/internal/layers"
	"github.com/okian/forge/internal/model"
	"github.com/okian/forge/internal/tags"
)

// marker names a layer the way a declaration does.
func marker(name string) model.LayerRef {
	return model.LayerRef{Origin: model.TypeRef{Pkg: model.MarkerPkg, Name: name}}
}

// stack builds a declaration's layers from their names, outermost first, the
// way an author writes them.
func stack(names ...string) []model.LayerRef {
	out := make([]model.LayerRef, len(names))
	for i, name := range names {
		out[i] = marker(name)
	}
	return out
}

// person is the subject the documented example is specialised to: a struct with
// four fields, every one of them tagged.
//
// Built rather than loaded. What a resolution says about a subject is its field
// count and its tags, and a fixture module would put a go/packages session and
// a type-checker between this package and two numbers.
var person = &model.Struct{
	Fields: []model.Field{
		{Name: "ID", Exported: true, Tags: []tags.Tag{{Key: "json", Name: "id"}}},
		{Name: "Name", Exported: true, Tags: []tags.Tag{{Key: "json", Name: "name"}}},
		{Name: "Age", Exported: true, Tags: []tags.Tag{{Key: "json", Name: "age"}}},
		{Name: "Email", Exported: true, Tags: []tags.Tag{{Key: "json", Name: "email"}}},
	},
}

// documented is the declaration the specification works through.
func documented() explain.Declaration {
	return explain.Declaration{
		Name:        "Persons",
		Package:     "example.com/model",
		Position:    "model/spec.go:8:6",
		Form:        model.FormSpec,
		Stack:       stack("Collection", "Ring", "Json"),
		Subject:     person,
		SubjectName: "Person",
		Layout:      model.Layout{Text: "Collection[Ring[Json[Person]]]"},
	}
}

// The worked example is the one resolution this project has written down, and
// a stack that explains differently from the way it is documented is either a
// wrong explanation or documentation nobody updated.
func TestTheDocumentedResolution(t *testing.T) {
	var out bytes.Buffer
	if err := explain.Of(documented(), layers.Builtins()).Text(&out); err != nil {
		t.Fatalf("rendering: %v", err)
	}

	goldentest.Compare(t, "resolution.txt", out.Bytes())
}

// And the same resolution as a document, since the two are read by different
// people and neither is the other's serialisation.
func TestTheDocumentedResolutionAsADocument(t *testing.T) {
	var out bytes.Buffer
	if err := explain.Of(documented(), layers.Builtins()).JSON(&out); err != nil {
		t.Fatalf("rendering: %v", err)
	}

	goldentest.Compare(t, "resolution.json", out.Bytes())
}

// The walk runs subject outward, which is the direction resolution runs and the
// reverse of the direction the declaration reads. A resolution reported the
// other way would tell somebody that the outermost layer decides what the
// innermost one is offered.
func TestTheWalkRunsSubjectOutward(t *testing.T) {
	got := explain.Of(documented(), layers.Builtins())

	want := []string{"Person", "Json", "Ring", "Collection"}
	if len(got.Steps) != len(want) {
		t.Fatalf("walked %d steps, want %d", len(got.Steps), len(want))
	}
	for i, name := range want {
		if got.Steps[i].Name != name {
			t.Errorf("step %d is %s, want %s", i+1, got.Steps[i].Name, name)
		}
		if got.Steps[i].Number != i+1 {
			t.Errorf("step %s is numbered %d, want %d", name, got.Steps[i].Number, i+1)
		}
	}
}

// Every layer's shape is decided by the shape beneath it, so what one step
// exposes is what the next is offered. A walk that lost that would report a
// stack nothing could be validated against.
func TestEachStepIsOfferedWhatTheOneBelowExposes(t *testing.T) {
	got := explain.Of(documented(), layers.Builtins())

	// Json requires a structured subject and adds encodability; Ring adds the
	// capabilities a container has; Collection requires what Ring added and
	// adds nothing of its own.
	want := map[string][]string{
		"Person":     {"Structured"},
		"Json":       {"Encodable"},
		"Ring":       {"Sized", "Ordered", "Streamable", "Bounded"},
		"Collection": nil,
	}

	for _, step := range got.Steps {
		if strings.Join(step.Adds, ",") != strings.Join(want[step.Name], ",") {
			t.Errorf("%s adds %v, want %v", step.Name, step.Adds, want[step.Name])
		}
	}

	// And what the last step exposes holds everything the stack accumulated.
	last := got.Steps[len(got.Steps)-1]
	for _, cap := range []string{"Structured", "Encodable", "Sized", "Ordered", "Streamable", "Bounded"} {
		if !strings.Contains(strings.Join(last.Shape, ","), cap) {
			t.Errorf("the stack does not expose %s: %v", cap, last.Shape)
		}
	}
}

// A decorator may withdraw what it cannot uphold, and a report that only ever
// added would describe a stack offering iteration that iterating would break.
func TestADecoratorThatWithdraws(t *testing.T) {
	decl := documented()
	decl.Stack = stack("Guarded", "Collection", "Ring", "Json")
	decl.Layout.Text = "Guarded[Collection[Ring[Json[Person]]]]"

	got := explain.Of(decl, layers.Builtins())

	last := got.Steps[len(got.Steps)-1]
	if last.Name != "Guarded" {
		t.Fatalf("the outermost step is %s", last.Name)
	}
	if strings.Join(last.Masks, ",") != "Streamable" {
		t.Errorf("Guarded masks %v, want Streamable", last.Masks)
	}
	if strings.Contains(strings.Join(last.Shape, ","), "Streamable") {
		t.Errorf("a masked capability is still exposed: %v", last.Shape)
	}
}

// A subject that could not be modelled still resolves. The stack above it is
// what the author wrote and is worth reading, and it is the case somebody
// explaining a declaration is most likely to be in.
func TestAStackOverASubjectThatWasRefused(t *testing.T) {
	decl := documented()
	decl.Subject = nil
	decl.SubjectName = "*Person"
	decl.Layout.Text = "Collection[Ring[Json[*Person]]]"

	got := explain.Of(decl, layers.Builtins())

	if len(got.Steps) != 4 {
		t.Fatalf("walked %d steps, want 4", len(got.Steps))
	}
	if !strings.Contains(got.Steps[0].Effect, "refused") {
		t.Errorf("the subject step does not say what happened: %q", got.Steps[0].Effect)
	}
	// The layers still report their kinds, which is most of what the question
	// was about.
	for _, step := range got.Steps[1:] {
		if step.Kind == model.KindInvalid {
			t.Errorf("%s reports no kind", step.Name)
		}
	}
}

// A marker nothing claims is not a layer. Reporting it as one contributing
// nothing would be a stack that explains cleanly and cannot generate.
func TestAMarkerNoLayerClaims(t *testing.T) {
	decl := documented()
	decl.Stack = stack("Nonesuch")
	decl.Layout.Text = "Nonesuch[Person]"

	got := explain.Of(decl, layers.Builtins())

	last := got.Steps[len(got.Steps)-1]
	if last.Kind != model.KindInvalid {
		t.Errorf("an unclaimed marker reports kind %s", last.Kind)
	}
	if !strings.Contains(last.Effect, "no layer") {
		t.Errorf("nothing says the marker is unclaimed: %q", last.Effect)
	}
}

// A layer whose marker forge ships and whose generator it does not is worth
// waiting for; a layer that emits nothing is not. Told apart, they read the
// same.
func TestALayerThisReleaseDoesNotShip(t *testing.T) {
	decl := documented()
	decl.Stack = stack("Sorted", "Json")
	decl.Layout.Text = "Sorted[Json[Person]]"

	got := explain.Of(decl, layers.Builtins())

	last := got.Steps[len(got.Steps)-1]
	if last.Name != "Sorted" {
		t.Fatalf("the outermost step is %s", last.Name)
	}
	if !last.Staged {
		t.Error("a layer this release does not ship is reported as one it does")
	}
	if !strings.Contains(last.Effect, "not in this release") {
		t.Errorf("the effect does not say so: %q", last.Effect)
	}
}

// A declaration with no layers at all is a subject and nothing else, and
// reporting it is better than reporting nothing.
func TestADeclarationWithNoLayers(t *testing.T) {
	decl := documented()
	decl.Stack = nil
	decl.Layout.Text = "Person"

	got := explain.Of(decl, layers.Builtins())

	if len(got.Steps) != 1 {
		t.Fatalf("walked %d steps, want 1", len(got.Steps))
	}
	if got.Steps[0].Kind != model.KindSubject {
		t.Errorf("the only step is %s, want subject", got.Steps[0].Kind)
	}
}

// A layer that actually generates reaches this report through the same three
// methods a stub does, and its surface is the first thing in a resolution that
// a stub cannot stand in for. The catalog it comes from is forge's own, so the
// names below are what an author reading the report will see.
func TestWhatARealLayerSaysItWillEmit(t *testing.T) {
	decl := documented()
	decl.Form = model.FormInline
	decl.Stack = stack("Slice")
	decl.Layout.Text = "Slice[Person]"

	got := explain.Of(decl, layers.Builtins())

	if len(got.Steps) != 2 {
		t.Fatalf("walked %d steps, want the subject and one layer", len(got.Steps))
	}

	storage := got.Steps[1]
	if storage.Pending {
		t.Error("a layer that generates is reported as one whose generator is not written")
	}
	if want := "Len, All, Backward, AppendSeq, Reset"; strings.Join(storage.Methods, ", ") != want {
		t.Errorf("it emits %v, want %s", storage.Methods, want)
	}

	var out bytes.Buffer
	if err := got.Text(&out); err != nil {
		t.Fatalf("rendering: %v", err)
	}
	if strings.Contains(out.String(), "pending") {
		t.Errorf("a stack of layers that all generate reports work as pending:\n%s", out.String())
	}
}
