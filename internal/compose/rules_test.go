package compose_test

import (
	"strings"
	"testing"

	"github.com/okian/forge/internal/compose"
	"github.com/okian/forge/internal/layer"
	"github.com/okian/forge/internal/layers"
	"github.com/okian/forge/internal/model"
	"github.com/okian/forge/internal/shape"
)

// Every rule about the shape of a stack, with a stack that keeps it and one
// that does not.
//
// Written as one table because the rules are one thing: what arrangements of
// kinds are a stack. A rule with only a failing case would pass over a check
// that refused everything, and one with only a passing case would pass over a
// check that refused nothing — and the second is the way a rule actually dies,
// since a check nobody can trip is a check nobody notices has stopped working.
//
// The stacks are written in real markers from forge's own catalog, so that what
// each case says about the rule is also true of the tool. The kinds come from
// the catalog rather than from the test, which is what makes these stacks the
// ones an author could type.
func TestTheShapeOfAStack(t *testing.T) {
	cases := map[string]struct {
		stack []string
		form  model.Form
		code  string
		says  string

		// under is the layer the caret is drawn beneath. Which one a complaint
		// points at is the half of it a reader acts on, and every one of these
		// has a layer that is the one to move: a run that underlined the
		// outermost every time would read as correct and send them to the wrong
		// bracket.
		under string
	}{
		// R3: at most one storage layer. A second container that means to add
		// to the first is a refining layer, which is the kind that says so.
		"one storage layer":  {stack: []string{"Collection", "Ring"}, form: model.FormSpec},
		"two storage layers": {stack: []string{"Collection", "Ring", "Heap"}, form: model.FormSpec, code: "FRG1003", says: "Ring, Heap", under: "Heap"},

		// R4: a decorator wraps a container, so there has to be one to wrap.
		"a decorator over a container": {stack: []string{"Guarded", "Collection", "Ring"}, form: model.FormSpec},
		"a decorator on the storage":   {stack: []string{"Guarded", "Ring"}, form: model.FormSpec},
		"a decorator over a subject":   {stack: []string{"Guarded"}, form: model.FormSpec, code: "FRG1004", says: "Guarded", under: "Guarded"},
		"a decorator under a storage":  {stack: []string{"Collection", "Ring", "Guarded"}, form: model.FormSpec, code: "FRG1004", says: "Guarded", under: "Guarded"},

		// R2: element layers attach to the subject, so they belong together at
		// the bottom of the stack.
		"elements together at the bottom": {stack: []string{"Collection", "Ring", "Json", "Validate"}, form: model.FormSpec},
		"an element above a container":    {stack: []string{"Json", "Ring"}, form: model.FormSpec, code: "FRG1002", says: "Json", under: "Json"},

		// R8: a transport terminates a stack, so nothing is written over it and
		// there is only one.
		"a transport outermost": {stack: []string{"Csv", "Collection", "Ring"}, form: model.FormSpec},
		"a transport wrapped":   {stack: []string{"Collection", "Csv", "Ring"}, form: model.FormSpec, code: "FRG1008", says: "Csv", under: "Csv"},
		"two transports":        {stack: []string{"Csv", "Csv"}, form: model.FormSpec, code: "FRG1008"},
		// Outermost by being the only layer, so the shape rules have nothing to
		// say — and then refused for what it needs, which is the other half of
		// composition doing its own job.
		"a transport over a subject": {stack: []string{"Csv"}, form: model.FormSpec, code: "FRG1006", says: "Streamable"},

		// A directive names a layer, so a stack naming one twice has directives
		// that could belong to either.
		"each layer once": {stack: []string{"Guarded", "Collection", "Ring"}, form: model.FormSpec},
		"one layer twice": {stack: []string{"Guarded", "Guarded", "Collection", "Ring"}, form: model.FormSpec, code: "FRG1020", says: "Guarded", under: "Guarded"},

		// D1: an inline declaration is the author's own type, and every layer in
		// it has to uphold what it promises over that raw underlying form.
		"an inline stack of transparent layers": {stack: []string{"Collection"}, form: model.FormInline},
		"an inline stack over a ring":           {stack: []string{"Collection", "Ring"}, form: model.FormInline, code: "FRG1021", says: "Ring", under: "Ring"},
		"an inline stack with an element":       {stack: []string{"Collection", "Json"}, form: model.FormInline, code: "FRG1021", says: "Json", under: "Json"},

		// A container marker is a defined slice type, so one of them over a
		// subject has the underlying type it appears to have. Two of them is a
		// slice of a slice, and the methods generated for the elements the
		// author named do not compile against it.
		"an inline stack of one layer":  {stack: []string{"Collection"}, form: model.FormInline},
		"an inline stack of two layers": {stack: []string{"Collection", "Slice"}, form: model.FormInline, code: "FRG1022", says: "Slice", under: "Slice"},
		"the same stack in a spec file": {stack: []string{"Collection", "Slice"}, form: model.FormSpec},

		// The same stacks are legal where forge owns the type.
		"a ring in a spec file":     {stack: []string{"Collection", "Ring"}, form: model.FormSpec},
		"an element in a spec file": {stack: []string{"Collection", "Json"}, form: model.FormSpec},
	}

	for name, held := range cases {
		t.Run(name, func(t *testing.T) {
			decl := written(held.form, held.stack...)

			_, diags := compose.Compose(decl, catalog())

			if held.code == "" {
				if !diags.Empty() {
					t.Fatalf("a stack that keeps the rule was refused:\n%s", diags.Render())
				}
				return
			}

			rendered := diags.Render()
			if !strings.Contains(rendered, held.code) {
				t.Fatalf("the stack was not refused with %s:\n%s", held.code, rendered)
			}
			if held.says != "" && !strings.Contains(said(rendered), held.says) {
				t.Errorf("the complaint does not say %q:\n%s", held.says, rendered)
			}

			if held.under != "" && !underlines(rendered, held.under) {
				t.Errorf("the caret is not under %s:\n%s", held.under, rendered)
			}
		})
	}
}

// said returns the sentence a diagnostic leads with, without the stack it
// prints beneath it.
//
// The distinction is the whole of what makes an assertion about the message
// worth making. The stack line holds every marker name in the declaration, so a
// message that named none of them would still be found in the rendering — and a
// complaint that does not say which layer is wrong is one a reader cannot act
// on, which is the same reason the caret is checked rather than counted.
func said(rendered string) string {
	line, _, _ := strings.Cut(rendered, "\n")
	return line
}

// underlines reports whether the rendered diagnostic draws its caret beneath
// this layer's name.
//
// Read off the two lines rather than trusted: the stack is printed and the
// carets on the line under it, so where the carets start is a column into the
// stack, and what is at that column is what the reader will look at.
func underlines(rendered, name string) bool {
	lines := strings.Split(rendered, "\n")

	for i, line := range lines {
		mark := strings.TrimRight(line, " ")
		if i == 0 || !strings.HasSuffix(mark, "^") || strings.TrimLeft(mark, " ^") != "" {
			continue
		}

		// The stack is the line above, indented the same way.
		stack := lines[i-1]
		at := strings.Index(mark, "^")
		if at >= len(stack) {
			return false
		}
		return strings.HasPrefix(stack[at:], name)
	}
	return false
}

// written builds a declaration of these layers, in a file of that form.
func written(form model.Form, names ...string) compose.Declaration {
	decl := declaration(names...)
	decl.Model = &model.Model{
		Name: "Persons", Form: form, Subject: decl.Subject, Stack: decl.Stack, Pos: declaredAt,
	}
	return decl
}

// A stack broken in two ways is reported twice.
//
// The rules are independent, and an author who is told about one of two
// mistakes fixes it, runs again, and is told about the other. Reporting both
// costs nothing and saves the second run.
func TestEveryBrokenRuleIsReported(t *testing.T) {
	decl := written(model.FormSpec, "Collection", "Ring", "Heap", "Guarded")

	_, diags := compose.Compose(decl, catalog())

	rendered := diags.Render()
	for _, want := range []string{"FRG1003", "FRG1004"} {
		if !strings.Contains(rendered, want) {
			t.Errorf("the report does not hold %s:\n%s", want, rendered)
		}
	}
}

// One code, two reasons, and the message says which.
//
// A layer that cannot be the underlying type of a declaration the author wrote
// is refused for one of two reasons that share nothing but the remedy. An
// element marker is not a container at all — the language leaves it nowhere to
// stand but a phantom struct — and a storage layer is a container whose
// invariants an ordinary declaration lets the author reach past. A reader told
// only that the layer "cannot be its underlying type" has to know which of the
// two forge means before the sentence is any use.
func TestWhyALayerCannotBeAnUnderlyingType(t *testing.T) {
	cases := map[string]struct {
		stack []string
		says  string
	}{
		"an element marker": {stack: []string{"Collection", "Json"}, says: "attaches to the subject"},
		"a storage layer":   {stack: []string{"Collection", "Ring"}, says: "keeps invariants"},
	}

	for name, held := range cases {
		t.Run(name, func(t *testing.T) {
			_, diags := compose.Compose(written(model.FormInline, held.stack...), catalog())

			if got := said(diags.Render()); !strings.Contains(got, held.says) {
				t.Errorf("the complaint reads %q, want it to say %q", got, held.says)
			}
		})
	}
}

// One edit, one complaint.
//
// A nested inline stack that also names an opaque layer breaks both halves of
// one rule, and the two would print the same position, the same caret and the
// same hint. Moving the declaration answers both, so hearing it twice is
// hearing one sentence twice — which is the standard every other rule here is
// held to from the other direction.
func TestANestedInlineStackIsToldToMoveOnce(t *testing.T) {
	_, diags := compose.Compose(written(model.FormInline, "Collection", "Ring"), catalog())

	rendered := diags.Render()
	if strings.Contains(rendered, "FRG1022") {
		t.Errorf("a declaration was told to move twice:\n%s", rendered)
	}
	if !strings.Contains(rendered, "FRG1021") {
		t.Errorf("a declaration that has to move was not told to:\n%s", rendered)
	}
}

// A rule broken twice is reported twice.
//
// Each of these walks the whole stack rather than stopping at the first thing
// it finds, for the same reason the rules do not stop at the first rule: an
// author told about one of two identical mistakes fixes it, runs again, and
// meets the other. The cost of the second report is a line.
func TestEveryOccurrenceOfOneRuleIsReported(t *testing.T) {
	cases := map[string]struct {
		stack []string
		code  string
		form  model.Form
	}{
		"three storage layers":   {stack: []string{"Collection", "Ring", "Heap", "LRU"}, code: "FRG1003", form: model.FormSpec},
		"two elements above one": {stack: []string{"Json", "Validate", "Ring"}, code: "FRG1002", form: model.FormSpec},
		"two decorators beneath": {stack: []string{"Collection", "Ring", "Guarded", "Atomic"}, code: "FRG1004", form: model.FormSpec},
		"two opaque layers":      {stack: []string{"Collection", "Json", "Validate"}, code: "FRG1021", form: model.FormInline},
	}

	for name, held := range cases {
		t.Run(name, func(t *testing.T) {
			_, diags := compose.Compose(written(held.form, held.stack...), catalog())

			if got := strings.Count(diags.Render(), held.code); got < 2 {
				t.Errorf("a rule broken twice was reported %d times:\n%s", got, diags.Render())
			}
		})
	}
}

// A layer forge filled in is not counted against the rule about naming one
// twice.
//
// It cannot collide with anything an author wrote — one is inserted only where
// there is no storage, and what is inserted is a storage — so the skip changes
// nothing today. What it is for is a build whose default storage is misnamed,
// where the inserted entry could duplicate a layer in the stack: no edit to the
// author's file answers that, so it is not their complaint to receive.
func TestTheStorageForgeFillsInIsNotCountedTwice(t *testing.T) {
	registry := layers.Builtins()

	decl := written(model.FormInline, "Collection")

	// A default storage that is already in the stack, which forge's own catalog
	// cannot produce and a build assembling its own registry can.
	_, diags := compose.Compose(decl, compose.Catalog{
		Registry:       registry,
		DefaultStorage: model.TypeRef{Pkg: model.MarkerPkg, Name: "Collection"},
	})

	if strings.Contains(diags.Render(), "FRG1020") {
		t.Errorf("an entry nobody wrote was reported as a layer named twice:\n%s", diags.Render())
	}
}

// A layer forge filled in is not blamed for being opaque either.
//
// The default storage this build ships is transparent, which is what lets it be
// filled in at all — so the skip in the rule about transparency only shows on a
// build that named an opaque one. There it is the difference between an author
// being told to move a declaration they can fix and being told about a layer
// that is not in their file, under a caret of no width.
func TestAnOpaqueStorageForgeFillsInIsNotBlamed(t *testing.T) {
	decl := written(model.FormInline, "Collection")

	_, diags := compose.Compose(decl, compose.Catalog{
		Registry:       layers.Builtins(),
		DefaultStorage: model.TypeRef{Pkg: model.MarkerPkg, Name: "Ring"},
	})

	if strings.Contains(diags.Render(), "FRG1021") {
		t.Errorf("an entry nobody wrote was reported as one to move:\n%s", diags.Render())
	}
}

// A stack whose shape is wrong is not then asked what each layer needs.
//
// Every such question has an answer, and over a stack arranged wrongly the
// answer is a consequence: a decorator written beneath the storage refuses
// because it was handed a subject, which is true and is not what the author
// did. Reporting it beside the real fault makes one mistake read as two, and
// the invented one reads like the cause.
func TestTheShapeIsCheckedBeforeTheLayersAreAsked(t *testing.T) {
	// Two storage layers, the inner of which needs something the subject does
	// not have. Asked, it refuses — truthfully, and about a stack the author is
	// already being told not to write.
	decl := written(model.FormSpec, "Collection", "Ring", "Heap")

	_, diags := compose.Compose(decl, catalog())

	rendered := diags.Render()
	if !strings.Contains(rendered, "FRG1003") {
		t.Fatalf("the stack was not refused for its shape:\n%s", rendered)
	}
	if strings.Contains(rendered, "FRG1006") {
		t.Errorf("a stack refused for its shape was also asked what its layers need:\n%s", rendered)
	}

	// And nothing was worked out, since the shape is what decides what each
	// layer is handed.
	held, _ := compose.Compose(decl, catalog())
	if len(held.Steps) != 0 {
		t.Errorf("a stack that is not one composed %d steps", len(held.Steps))
	}
}

// An inline declaration whose storage forge filled in is not blamed for it.
//
// The default storage is transparent, which is what lets it be filled in at
// all; a rule that reported an entry nobody wrote would be reporting forge's
// own decision as the author's mistake, and no edit to their file would answer
// it.
func TestTheStorageForgeFillsInIsNotBlamed(t *testing.T) {
	decl := written(model.FormInline, "Collection")

	held, diags := compose.Compose(decl, catalog())

	if !diags.Empty() {
		t.Fatalf("an inline collection was refused:\n%s", diags.Render())
	}
	if got, want := len(held.Stack()), 2; got != want {
		t.Fatalf("the composed stack has %d entries, want %d", got, want)
	}
	if !held.Stack()[1].Implicit {
		t.Error("the storage was not marked as one forge filled in")
	}
}

// misclassified is a layer that says it is the subject.
//
// Nothing forge ships says so, and nothing sensibly could — but the rule is not
// about forge's catalog. A layer is what a third party writes, and a kind is a
// thing a layer declares about itself, so the one answer that cannot work is one
// a registry will accept and hand over.
type misclassified struct{}

func (misclassified) Binds() []model.Import { return nil }
func (misclassified) Writes() []string      { return nil }
func (misclassified) Origin() model.TypeRef {
	return model.TypeRef{Pkg: model.MarkerPkg, Name: "Itself"}
}

func (misclassified) Kind() model.Kind                { return model.KindSubject }
func (misclassified) OptionSchema() []layer.OptionDef { return nil }
func (misclassified) Accepts(shape.Shape) error       { return nil }

func (misclassified) Shape(_ *layer.Context, below shape.Shape) shape.Shape { return below }

func (misclassified) Generate(*layer.Context, shape.Shape) (layer.Unit, error) {
	return layer.Unit{}, nil
}

// A layer that classifies itself as the subject is refused wherever it is
// written.
//
// The subject is the type at the bottom of a stack and is carried apart from
// the layers from the moment a declaration resolves: nothing is generated for
// it and nothing is asked of it. A marker claiming to be one is built on a
// misunderstanding of what a stack is, and there is no arrangement that repairs
// it — so the diagnostic names the layer rather than suggesting a move.
func TestALayerThatSaysItIsTheSubject(t *testing.T) {
	registry := layer.New()
	if err := registry.Register(misclassified{}); err != nil {
		t.Fatalf("registering: %v", err)
	}

	decl := written(model.FormSpec, "Itself")

	_, diags := compose.Compose(decl, compose.Catalog{Registry: registry})

	rendered := diags.Render()
	if !strings.Contains(rendered, "FRG1001") {
		t.Fatalf("a layer claiming to be the subject was not refused:\n%s", rendered)
	}
	// In the sentence rather than anywhere in the rendering: the stack is
	// printed beneath it and holds the name whatever the message says.
	if !strings.Contains(said(rendered), "Itself") {
		t.Errorf("the complaint does not name the layer:\n%s", rendered)
	}
}
