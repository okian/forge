package explain_test

import (
	"bytes"
	"slices"
	"strings"
	"testing"

	"github.com/okian/forge/internal/explain"
	"github.com/okian/forge/internal/layer"
	"github.com/okian/forge/internal/layers"
	"github.com/okian/forge/internal/model"
	"github.com/okian/forge/internal/shape"
)

// emitting is a layer that contributes methods, which none of the ones forge
// ships does yet.
//
// Written here rather than waited for: the report's answer to "what will this
// emit" is the half a reader is most interested in, and the code that renders
// it would otherwise go untried until the first generator lands, carrying
// whatever is wrong with it into that work.
type emitting struct {
	name       string
	kind       model.Kind
	methods    []shape.Method
	adds       []shape.Cap
	withdraws  []string
	collapsing bool
}

func (e emitting) Origin() model.TypeRef {
	return model.TypeRef{Pkg: model.MarkerPkg, Name: e.name}
}

func (e emitting) Kind() model.Kind                { return e.kind }
func (e emitting) OptionSchema() []layer.OptionDef { return nil }
func (e emitting) Accepts(shape.Shape) error       { return nil }

func (e emitting) Shape(below shape.Shape) shape.Shape {
	if e.collapsing {
		panic("this layer cannot say what it exposes")
	}

	out := shape.Shape{Caps: below.Caps.With(e.adds...), Elem: below.Elem}
	for _, method := range below.Surface {
		if !slices.Contains(e.withdraws, method.Name) {
			out.Surface = append(out.Surface, method)
		}
	}
	return out.WithMethods(e.methods...)
}

func (e emitting) Generate(*layer.Context, shape.Shape) (layer.Unit, error) {
	return layer.Unit{}, nil
}

// describing is a layer that answers the questions a layer may answer about
// itself, including badly.
type describing struct {
	emitting
	stage layer.Stage
	doc   string
}

func (d describing) Stage() layer.Stage { return d.stage }
func (d describing) Doc() string        { return d.doc }

// catalog builds a registry holding exactly these layers.
func catalog(t *testing.T, layers ...layer.Layer) *layer.Registry {
	t.Helper()

	registry := layer.New()
	for _, one := range layers {
		if err := registry.Register(one); err != nil {
			t.Fatalf("registering %s: %v", one.Origin(), err)
		}
	}
	return registry
}

// What a step will emit is what a reader most wants from this, and a step that
// emits nothing has to be told from one whose methods are simply not written
// yet.
func TestWhatEachStepWillEmit(t *testing.T) {
	registry := catalog(t,
		emitting{
			name: "Slice", kind: model.KindStorage, adds: []shape.Cap{shape.Sized},
			methods: []shape.Method{{Name: "Len", Signature: "() int"}, {Name: "All", Signature: "() iter.Seq[Person]"}},
		},
		emitting{
			name: "Collection", kind: model.KindRefining,
			methods: []shape.Method{{Name: "Seq", Signature: "() PersonsSeq"}},
		},
	)

	decl := documented()
	decl.Stack = stack("Collection", "Slice")
	decl.Layout.Text = "Collection[Slice[Person]]"

	got := explain.Of(decl, registry)

	emitted := map[string][]string{}
	for _, step := range got.Steps {
		emitted[step.Name] = step.Methods
	}

	if want := "Len, All"; strings.Join(emitted["Slice"], ", ") != want {
		t.Errorf("Slice emits %v, want %s", emitted["Slice"], want)
	}
	// Only what this step adds. A layer inherits the surface beneath it, and
	// reporting the whole of it against every step would say each one emits
	// what the one below it did.
	if want := "Seq"; strings.Join(emitted["Collection"], ", ") != want {
		t.Errorf("Collection emits %v, want %s", emitted["Collection"], want)
	}
	if len(emitted["Person"]) != 0 {
		t.Errorf("the subject emits %v", emitted["Person"])
	}

	var out bytes.Buffer
	if err := got.Text(&out); err != nil {
		t.Fatalf("rendering: %v", err)
	}
	if !strings.Contains(out.String(), "Emits") {
		t.Errorf("the report has no methods in it:\n%s", out.String())
	}
	if strings.Contains(out.String(), "no step of this stack emits") {
		t.Errorf("a stack that emits was reported as one that does not:\n%s", out.String())
	}
}

// A layer may replace a method with one of the same name — masking is how a
// decorator withdraws what it cannot uphold — and counting rather than naming
// would report that as nothing having happened.
func TestAStepThatReplacesAMethod(t *testing.T) {
	registry := catalog(t,
		emitting{
			name: "Slice", kind: model.KindStorage,
			methods: []shape.Method{{Name: "All", Signature: "() iter.Seq[Person]"}},
		},
		emitting{
			name: "Guarded", kind: model.KindDecorator,
			methods: []shape.Method{{Name: "All", Signature: "() iter.Seq[Person]"}, {Name: "Do", Signature: "(func())"}},
		},
	)

	decl := documented()
	decl.Stack = stack("Guarded", "Slice")
	decl.Layout.Text = "Guarded[Slice[Person]]"

	got := explain.Of(decl, registry)

	last := got.Steps[len(got.Steps)-1]
	if want := "Do"; strings.Join(last.Methods, ", ") != want {
		t.Errorf("Guarded emits %v, want %s", last.Methods, want)
	}
}

// A layer written outside forge has no answer to a question about forge's
// roadmap, and inventing one for it would be forge speaking on its behalf.
func TestALayerThatSaysNothingAboutItself(t *testing.T) {
	registry := catalog(t, emitting{name: "Collection", kind: model.KindRefining})

	decl := documented()
	decl.Stack = stack("Collection")
	decl.Layout.Text = "Collection[Person]"

	got := explain.Of(decl, registry)

	last := got.Steps[len(got.Steps)-1]
	if last.Staged {
		t.Error("a layer that said nothing was reported as one this release does not ship")
	}
	// Taken to be written, not merely unstaged. Both answers are guesses; this
	// is the one that does not tell a reader to wait for a release that will
	// never mention the layer.
	if last.Pending {
		t.Error("a layer that said nothing was reported as one whose generator is unwritten")
	}
	if last.Effect == "" {
		t.Error("a layer that said nothing was reported with nothing at all")
	}
}

// A subject with one field reads as one field rather than as one fields.
func TestASubjectWithOneOfEverything(t *testing.T) {
	decl := documented()
	decl.Subject = &model.Struct{Fields: person.Fields[:1]}
	decl.Stack = nil
	decl.Layout.Text = "Person"

	got := explain.Of(decl, layers.Builtins())

	if want := "struct model: 1 field, 1 tag"; got.Steps[0].Effect != want {
		t.Errorf("the subject reads %q, want %q", got.Steps[0].Effect, want)
	}
}

// A subject with fields and no tags is a subject a codec has nothing to work
// from, which is worth saying rather than leaving to be inferred.
func TestASubjectWithNoTags(t *testing.T) {
	decl := documented()
	decl.Subject = &model.Struct{Fields: []model.Field{{Name: "ID"}, {Name: "Name"}}}
	decl.Stack = nil
	decl.Layout.Text = "Person"

	got := explain.Of(decl, layers.Builtins())

	if want := "struct model: 2 fields, 0 tags"; got.Steps[0].Effect != want {
		t.Errorf("the subject reads %q, want %q", got.Steps[0].Effect, want)
	}
}

// A layer that describes itself with nothing said is a layer with no summary,
// and a blank cell reads as a layer that does nothing rather than as one that
// did not say.
func TestALayerThatDescribesItselfWithNothing(t *testing.T) {
	registry := catalog(t, describing{
		emitting: emitting{name: "Collection", kind: model.KindRefining},
		stage:    layer.StageStub,
	})

	decl := documented()
	decl.Stack = stack("Collection")
	decl.Layout.Text = "Collection[Person]"

	got := explain.Of(decl, registry)

	last := got.Steps[len(got.Steps)-1]
	if last.Effect == "" {
		t.Error("a layer that said nothing was reported with nothing at all")
	}
	if last.Staged {
		t.Error("a layer this release ships was reported as one it does not")
	}
}

// A layer whose marker forge ships and whose generator it does not says so in
// the table as well as in the document — the reader of the table is the one
// deciding whether to wait for it.
func TestTheTableSaysWhichLayersAreNotShipped(t *testing.T) {
	registry := catalog(t, describing{
		emitting: emitting{name: "Sorted", kind: model.KindRefining},
		stage:    layer.StageStaged,
		doc:      "a sorted view",
	})

	decl := documented()
	decl.Stack = stack("Sorted")
	decl.Layout.Text = "Sorted[Person]"

	var out bytes.Buffer
	if err := explain.Of(decl, registry).Text(&out); err != nil {
		t.Fatalf("rendering: %v", err)
	}

	if !strings.Contains(out.String(), "staged") {
		t.Errorf("the table does not say the layer is not shipped:\n%s", out.String())
	}
}

// A decorator that cannot uphold a method over what it wraps takes it away, and
// a report that only ever added would describe a surface nobody can call.
func TestAStepThatTakesAMethodAway(t *testing.T) {
	registry := catalog(t,
		emitting{
			name: "Slice", kind: model.KindStorage,
			methods: []shape.Method{{Name: "All", Signature: "() iter.Seq[Person]"}, {Name: "Len", Signature: "() int"}},
		},
		emitting{
			name: "Guarded", kind: model.KindDecorator,
			withdraws: []string{"All"},
			methods:   []shape.Method{{Name: "Do", Signature: "(func())"}},
		},
	)

	decl := documented()
	decl.Stack = stack("Guarded", "Slice")
	decl.Layout.Text = "Guarded[Slice[Person]]"

	got := explain.Of(decl, registry)

	last := got.Steps[len(got.Steps)-1]
	if want := "Do"; strings.Join(last.Methods, ", ") != want {
		t.Errorf("Guarded emits %v, want %s", last.Methods, want)
	}
	if want := "All"; strings.Join(last.Withdraws, ", ") != want {
		t.Errorf("Guarded withdraws %v, want %s", last.Withdraws, want)
	}

	var out bytes.Buffer
	if err := got.Text(&out); err != nil {
		t.Fatalf("rendering: %v", err)
	}
	if !strings.Contains(out.String(), "Withdraws") {
		t.Errorf("the report does not say what was taken away:\n%s", out.String())
	}
}

// A report that fails with a stack trace is worse than one that says a layer
// misbehaved: the reader learns nothing about the other layers, and the one
// thing they do learn is in a form only forge's authors can read.
func TestALayerThatCannotSayWhatItExposes(t *testing.T) {
	registry := catalog(t,
		emitting{name: "Collection", kind: model.KindRefining, collapsing: true},
		emitting{name: "Slice", kind: model.KindStorage, adds: []shape.Cap{shape.Sized}},
	)

	decl := documented()
	decl.Stack = stack("Collection", "Slice")
	decl.Layout.Text = "Collection[Slice[Person]]"

	got := explain.Of(decl, registry)

	if len(got.Steps) != 3 {
		t.Fatalf("walked %d steps, want 3", len(got.Steps))
	}

	last := got.Steps[len(got.Steps)-1]
	if !strings.Contains(last.Effect, "could not say") {
		t.Errorf("nothing says the layer misbehaved: %q", last.Effect)
	}
	// The layer below it is still described, which is most of what the question
	// was about.
	if !strings.Contains(strings.Join(got.Steps[1].Shape, ","), "Sized") {
		t.Errorf("the layers below were lost: %v", got.Steps[1].Shape)
	}
}

// A walk with no catalog can still report the stack that was written, which is
// more than half of what was asked.
func TestAWalkWithNoCatalogAtAll(t *testing.T) {
	got := explain.Of(documented(), nil)

	if len(got.Steps) != 4 {
		t.Fatalf("walked %d steps, want 4", len(got.Steps))
	}
	for _, step := range got.Steps[1:] {
		if !strings.Contains(step.Effect, "no layer") {
			t.Errorf("%s reports %q", step.Name, step.Effect)
		}
	}
}

// A layer's summary is its own text, and a third-party layer may write anything
// in it. A tab would open a column nobody declared and shift every row after
// it; a newline would end the row halfway through.
func TestALayerWhoseSummaryWouldBreakTheTable(t *testing.T) {
	registry := catalog(t, describing{
		emitting: emitting{name: "Collection", kind: model.KindRefining},
		stage:    layer.StageStub,
		doc:      "a summary\nwith a line break\tand a tab",
	})

	decl := documented()
	decl.Stack = stack("Collection")
	decl.Layout.Text = "Collection[Person]"

	var out bytes.Buffer
	if err := explain.Of(decl, registry).Text(&out); err != nil {
		t.Fatalf("rendering: %v", err)
	}

	for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		if strings.ContainsAny(line, "\t") {
			t.Errorf("a cell opened a column of its own:\n%s", out.String())
		}
	}
	if !strings.Contains(out.String(), "a summary with a line break and a tab") {
		t.Errorf("the summary was not folded onto one line:\n%s", out.String())
	}
}
