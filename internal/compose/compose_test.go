package compose_test

import (
	"go/token"
	"go/types"
	"strings"
	"testing"

	"github.com/okian/forge/internal/compose"
	"github.com/okian/forge/internal/layer"
	"github.com/okian/forge/internal/layers"
	"github.com/okian/forge/internal/model"
	"github.com/okian/forge/internal/shape"
)

// declaredAt is where the declarations these tests compose were written.
var declaredAt = token.Position{Filename: "model/spec.go", Line: 8, Column: 6}

// person is the subject every stack below is over.
func person() *model.Struct {
	pkg := types.NewPackage("example.com/model", "model")
	obj := types.NewTypeName(token.NoPos, pkg, "Person", nil)

	return &model.Struct{
		Named:  types.NewNamed(obj, types.NewStruct(nil, nil), nil),
		Fields: []model.Field{{Name: "Name", Exported: true, Type: model.Classified{Type: types.Typ[types.String]}}},
	}
}

// declaration builds a stack over that subject, as resolution hands one over:
// origins and nothing else, with no kind decided.
func declaration(names ...string) compose.Declaration {
	stack := make([]model.LayerRef, len(names))
	for i, name := range names {
		stack[i] = model.LayerRef{Origin: model.TypeRef{Pkg: model.MarkerPkg, Name: name}}
	}

	return compose.Declaration{Stack: stack, Subject: person(), Pos: declaredAt}
}

// catalog is the layers forge ships, which is what a run composes with.
func catalog() compose.Catalog {
	return compose.Catalog{Registry: layers.Builtins(), DefaultStorage: layers.DefaultStorage()}
}

// named returns the marker names of a composed stack, outermost first.
func named(held compose.Composed) []string {
	out := make([]string, 0, len(held.Steps))
	for _, ref := range held.Stack() {
		name := ref.Origin.Name
		if ref.Implicit {
			name += "(implicit)"
		}
		out = append(out, name)
	}
	return out
}

// A refining layer written over no storage is over the ordinary one. That is
// what makes an inline declaration's underlying type a real slice rather than a
// special case, and it is the whole reason Collection[Person] is a thing anyone
// can write.
func TestTheStorageADeclarationMeansAndDoesNotSay(t *testing.T) {
	held, diags := compose.Compose(declaration("Collection"), catalog())
	if !diags.Empty() {
		t.Fatalf("a collection over a subject was refused:\n%s", diags.Render())
	}

	if got, want := strings.Join(named(held), " "), "Collection Slice(implicit)"; got != want {
		t.Errorf("composed to %s, want %s", got, want)
	}

	// And it is marked, so that nothing draws a caret under a layer nobody
	// wrote.
	for _, ref := range held.Stack() {
		if ref.Origin.Name == "Slice" && !ref.Implicit {
			t.Error("the storage that was filled in is reported as one the author wrote")
		}
	}
}

// The kinds come from the layers, because resolution produces origins and
// nothing else — and every rule here is written in terms of kinds.
func TestTheKindsAreFilledIn(t *testing.T) {
	held, _ := compose.Compose(declaration("Collection"), catalog())

	kinds := map[string]model.Kind{"Collection": model.KindRefining, "Slice": model.KindStorage}
	for _, ref := range held.Stack() {
		if got := ref.Kind; got != kinds[ref.Origin.Name] {
			t.Errorf("%s is a %s, want a %s", ref.Origin.Name, got, kinds[ref.Origin.Name])
		}
	}
}

// A declaration that names its own storage keeps it. Filling one in as well
// would be two containers where the author wrote one.
func TestAStorageTheDeclarationNames(t *testing.T) {
	held, diags := compose.Compose(declaration("Collection", "Slice"), catalog())
	if !diags.Empty() {
		t.Fatalf("a collection over a slice was refused:\n%s", diags.Render())
	}

	if got, want := strings.Join(named(held), " "), "Collection Slice"; got != want {
		t.Errorf("composed to %s, want %s", got, want)
	}
}

// The filled-in storage goes above the element layers, which sit around the
// subject: a storage layer holds elements, and what an element layer attached
// to the subject is still the subject.
func TestWhereTheFilledInStorageGoes(t *testing.T) {
	held, _ := compose.Compose(declaration("Collection", "Json"), catalog())

	if got, want := strings.Join(named(held), " "), "Collection Slice(implicit) Json"; got != want {
		t.Errorf("composed to %s, want %s", got, want)
	}
}

// A stack with no refining layer in it has nothing to fill in for: a storage
// layer is what a refining layer needs, and a declaration that named neither
// asked for neither.
func TestNothingIsFilledInForAStackThatWantsNone(t *testing.T) {
	held, _ := compose.Compose(declaration("Json"), catalog())

	if got, want := strings.Join(named(held), " "), "Json"; got != want {
		t.Errorf("composed to %s, want %s", got, want)
	}
}

// Every layer is handed what the ones beneath it built up, which is what it was
// asked to accept and what it generates against.
func TestWhatEachLayerIsHanded(t *testing.T) {
	held, _ := compose.Compose(declaration("Collection"), catalog())

	if len(held.Steps) != 2 {
		t.Fatalf("composed %d steps, want the storage and the refining layer", len(held.Steps))
	}

	// Innermost first: the storage is handed the subject, which is structured
	// and nothing else.
	if got := held.Steps[0].Below.Caps; !got.Has(shape.Structured) || got.Has(shape.Streamable) {
		t.Errorf("the storage was handed %s", got)
	}

	// And the refining layer is handed what the storage left.
	if got := held.Steps[1].Below.Caps; !got.Has(shape.Streamable, shape.Structured) {
		t.Errorf("the refining layer was handed %s", got)
	}

	// What the whole stack offers is what the outermost layer left.
	if got := held.Exposed.Caps; !got.Has(shape.Streamable, shape.Sized, shape.Ordered) {
		t.Errorf("the stack exposes %s", got)
	}
}

// A layer that cannot sit on what is beneath it is a declaration that will not
// generate, reported at the layer that refused with the stack drawn under it.
func TestALayerThatCannotSitOnWhatIsBeneathIt(t *testing.T) {
	// Collection over an element layer and nothing else: the default storage is
	// filled in only for a refining layer, and Json leaves nothing to walk.
	decl := declaration("Collection", "Json")

	// Composed without a default to fill in, which is the state a build whose
	// catalog names none is in.
	held, diags := compose.Compose(decl, compose.Catalog{Registry: layers.Builtins()})

	reported := diags.Render()
	if reported == "" {
		t.Fatal("a refining layer over nothing to walk was accepted")
	}
	if !strings.Contains(reported, "FRG1006") || !strings.Contains(reported, "Streamable") {
		t.Errorf("the report does not say what was missing:\n%s", reported)
	}
	if !strings.Contains(reported, "^^^^^^^^^^") {
		t.Errorf("nothing is underlined:\n%s", reported)
	}

	// What was composed below the refusal is still worth having.
	if len(held.Steps) == 0 {
		t.Error("nothing was composed at all")
	}
}

// A marker nothing claims is not a layer, and there is nothing to ask it.
func TestAMarkerNothingClaims(t *testing.T) {
	decl := declaration("Collection")
	decl.Stack = append(decl.Stack, model.LayerRef{
		Origin: model.TypeRef{Pkg: model.MarkerPkg, Name: "Nonesuch"},
	})

	_, diags := compose.Compose(decl, catalog())

	reported := diags.Render()
	if !strings.Contains(reported, "FRG1900") || !strings.Contains(reported, "Nonesuch") {
		t.Errorf("the report does not name the marker nothing claims:\n%s", reported)
	}
}

// All of them are looked up before any of them is checked, so an author with
// two misspellings learns about two.
func TestEveryMarkerNothingClaims(t *testing.T) {
	decl := declaration("Nonesuch", "Neither")
	_, diags := compose.Compose(decl, catalog())

	if got := diags.Len(); got != 2 {
		t.Errorf("two markers nobody claims were reported %d times:\n%s", got, diags.Render())
	}
}

// A caller with no catalog at all composes nothing rather than crashing, since
// a registry is a thing a caller can fail to build.
func TestComposingWithNoCatalog(t *testing.T) {
	_, diags := compose.Compose(declaration("Collection"), compose.Catalog{})

	if diags.Empty() {
		t.Error("a stack composed against no layers at all was accepted")
	}
}

// The caret marks the layer that refused, and it has to be drawn against the
// stack as composed rather than as written: a storage layer filled in shifts
// every entry beneath it, so a caret measured against the other rendering marks
// the wrong layer or falls off the end.
func TestWhichLayerTheCaretMarks(t *testing.T) {
	// Collection over an element layer, which composes to three entries where
	// two were written. Refused because nothing was named to fill the storage
	// in from, so the element layer beneath is what Collection cannot sit on.
	decl := declaration("Collection", "Json")

	_, diags := compose.Compose(decl, compose.Catalog{Registry: layers.Builtins()})

	reported := diags.Render()
	if !strings.Contains(reported, "FRG1006") {
		t.Fatalf("the stack was accepted:\n%s", reported)
	}

	// Collection[Json[Person]], with the caret under Collection: ten carets at
	// the start of the line and nothing else.
	stack, caret := drawn(t, reported)
	if !strings.HasPrefix(stack, "Collection[Json[") {
		t.Fatalf("the stack reads %q", stack)
	}
	if caret != strings.Repeat("^", len("Collection")) {
		t.Errorf("the caret is %q, want it under Collection", caret)
	}
}

// The layer that refuses is not always the outermost, and the caret follows it
// past an entry that was filled in — which is the case that a caret measured
// against the stack as written gets wrong, since everything beneath the
// insertion has moved by one.
func TestTheCaretPastAnEntryThatWasFilledIn(t *testing.T) {
	// An element layer beneath the storage that was filled in, over a subject
	// with no fields: Json needs something structured and a struct with nothing
	// in it is not.
	decl := declaration("Collection", "Json")
	decl.Subject = &model.Struct{Named: person().Named}

	_, diags := compose.Compose(decl, catalog())

	reported := diags.Render()
	if !strings.Contains(reported, "FRG1006") {
		t.Fatalf("an element layer over nothing to read was accepted:\n%s", reported)
	}

	stack, caret := drawn(t, reported)
	if at := strings.Index(stack, "Json"); at < 0 || caret != strings.Repeat(" ", at)+"^^^^" {
		t.Errorf("the caret %q does not mark Json in %q", caret, stack)
	}
}

// drawn returns the stack a report printed and the caret it drew under it.
func drawn(t *testing.T, reported string) (stack, caret string) {
	t.Helper()

	held := strings.Split(reported, "\n")
	if len(held) < 3 {
		t.Fatalf("the report has no stack in it:\n%s", reported)
	}

	// The two lines carry the margin a diagnostic prints its block at, and only
	// that: a caret's own leading spaces are what say which layer it marks, so
	// trimming them would be trimming the answer.
	const margin = "  "

	return strings.TrimPrefix(held[1], margin), strings.TrimPrefix(held[2], margin)
}

// A layer is the part of this a third party writes, so the one that answers a
// question with a panic is the one forge did not — and a run that ended in a
// stack trace would tell an author their generator is broken in a form only its
// authors can read.
func TestALayerThatPanicsWhileComposing(t *testing.T) {
	for name, one := range map[string]layer.Layer{
		"asked what it sits on": panicking{at: "accepts"},
		"asked what it exposes": panicking{at: "shape"},
	} {
		t.Run(name, func(t *testing.T) {
			registry := layers.Builtins()
			registry.MustRegister(one)

			decl := declaration("Broken")

			_, diags := compose.Compose(decl, compose.Catalog{Registry: registry})

			reported := diags.Render()
			if !strings.Contains(reported, "FRG1901") {
				t.Fatalf("a layer that panicked was not reported:\n%s", reported)
			}
			if !strings.Contains(reported, "Broken") {
				t.Errorf("the report does not name the layer:\n%s", reported)
			}
		})
	}
}

// panicking is a layer from outside that answers a question by failing.
type panicking struct{ at string }

func (p panicking) Origin() model.TypeRef {
	return model.TypeRef{Pkg: model.MarkerPkg, Name: "Broken"}
}

func (p panicking) Kind() model.Kind                { return model.KindRefining }
func (p panicking) OptionSchema() []layer.OptionDef { return nil }

func (p panicking) Accepts(shape.Shape) error {
	if p.at == "accepts" {
		panic("this layer has no idea")
	}
	return nil
}

func (p panicking) Shape(_ *layer.Context, below shape.Shape) shape.Shape {
	if p.at == "shape" {
		panic("nor this one")
	}
	return below
}

func (p panicking) Generate(*layer.Context, shape.Shape) (layer.Unit, error) {
	return layer.Unit{}, nil
}
