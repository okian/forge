package subject_test

import (
	"go/token"
	"go/types"
	"strings"
	"testing"

	"github.com/okian/forge/internal/model"
	"github.com/okian/forge/internal/subject"
)

// declaring names a stack the way a declaration does, outermost first.
func declaring(markers ...string) []model.LayerRef {
	out := make([]model.LayerRef, len(markers))
	for i, name := range markers {
		out[i] = model.LayerRef{Origin: model.TypeRef{Pkg: model.MarkerPkg, Name: name}}
	}
	return out
}

// where is a position standing in for the declaration a subject was named in.
var where = token.Position{Filename: "model/spec.go", Line: 8, Column: 6}

// A subject that cannot be modelled is refused by name, and a name is not
// enough: "subject *Person is a pointer" leaves the reader to find which of
// four nested layers holds it. The picture is what turns that into a glance.
func TestARefusedSubjectIsUnderlined(t *testing.T) {
	loaded := session(t)
	person := named(t, loaded, "Person")

	cases := map[string]struct {
		subject types.Type
		stack   []model.LayerRef
		drawn   string
		under   string
	}{
		"a pointer, two layers down": {
			subject: types.NewPointer(person),
			stack:   declaring("Collection", "Ring"),
			drawn:   "Collection[Ring[*Person]]",
			under:   "                ^^^^^^^",
		},
		"a predeclared type, one layer down": {
			subject: types.Typ[types.Int],
			stack:   declaring("Collection"),
			drawn:   "Collection[int]",
			under:   "           ^^^",
		},
		"a pointer with no layers over it at all": {
			subject: types.NewPointer(person),
			drawn:   "*Person",
			under:   "^^^^^^^",
		},
		// A span is measured in bytes, because that is what slicing the text
		// needs, and drawn in characters, because that is what lines up on
		// screen. An identifier outside ASCII is where the two part company,
		// and Go accepts one.
		"a subject named outside ASCII": {
			subject: types.NewPointer(named(t, loaded, "Åéîõü")),
			stack:   declaring("Collection"),
			drawn:   "Collection[*Åéîõü]",
			under:   "           ^^^^^^",
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, diags := subject.New(subject.Config{Fset: loaded.Fset}).Build(tc.subject, subject.Site{
				Pos:    where,
				Layout: model.LayoutOf(tc.stack, tc.subject),
			})

			if diags.Empty() {
				t.Fatal("a subject no model can be built from was accepted")
			}

			rendered := diags.Render()
			// The caret line directly beneath the declaration, so that what is
			// underlined is decided by looking rather than by counting.
			if want := tc.drawn + "\n  " + tc.under; !strings.Contains(rendered, want) {
				t.Errorf("the refusal does not draw\n%s\ngot:\n%s", want, rendered)
			}
		})
	}
}

// The declaration a refusal draws is the one that named the subject, and the
// position it reports is where that declaration was written — not where the
// subject was, which is very often somebody else's file.
func TestARefusalPointsAtTheDeclaration(t *testing.T) {
	loaded := session(t)

	_, diags := subject.New(subject.Config{Fset: loaded.Fset}).
		Build(types.NewPointer(named(t, loaded, "Person")), subject.Site{
			Pos:    where,
			Layout: model.LayoutOf(declaring("Collection"), types.NewPointer(named(t, loaded, "Person"))),
		})

	if !strings.Contains(diags.Render(), "model/spec.go:8:6") {
		t.Errorf("the refusal does not point at the declaration:\n%s", diags.Render())
	}
}

// And one refused with nothing to draw carries a position and no picture,
// rather than a picture of nothing.
func TestARefusalWithNothingToDraw(t *testing.T) {
	loaded := session(t)

	_, diags := subject.New(subject.Config{Fset: loaded.Fset}).
		Build(types.NewPointer(named(t, loaded, "Person")), subject.At(where))

	if diags.Empty() {
		t.Fatal("a pointer subject was accepted")
	}

	rendered := diags.Render()
	if strings.Contains(rendered, "^") {
		t.Errorf("a caret was drawn under nothing:\n%s", rendered)
	}
	if !strings.Contains(rendered, "model/spec.go:8:6") {
		t.Errorf("the refusal lost its position:\n%s", rendered)
	}
}

// What is wrong with a field is not what is wrong with the subject, and
// underlining the subject for it would point at the one part of the declaration
// that is right.
func TestAFieldIsNotUnderlinedAsTheSubject(t *testing.T) {
	loaded := session(t)
	tagged := named(t, loaded, "Tagged")

	_, diags := subject.New(subject.Config{Fset: loaded.Fset}).Build(tagged, subject.Site{
		Pos:    where,
		Layout: model.LayoutOf(declaring("Collection"), tagged),
	})

	if diags.Empty() {
		t.Fatal("a malformed tag was accepted")
	}
	if strings.Contains(diags.Render(), "Collection[Tagged]") {
		t.Errorf("a field's problem was drawn as the subject's:\n%s", diags.Render())
	}
}

// Two declarations over one subject share whatever is wrong with it. The model
// is built once and shared, which is what lets generation emit one codec for a
// type — but a fault memoised away with it would make the second declaration
// look clean, and which one that is depends on the order they were written in.
func TestASharedSubjectIsBrokenForBoth(t *testing.T) {
	loaded := session(t)
	tagged := named(t, loaded, "Tagged")

	builder := subject.New(subject.Config{Fset: loaded.Fset})

	first := token.Position{Filename: "model/first.go", Line: 3, Column: 6}
	second := token.Position{Filename: "model/second.go", Line: 9, Column: 6}

	_, before := builder.Build(tagged, subject.At(first))
	built, after := builder.Build(tagged, subject.At(second))

	if before.Empty() {
		t.Fatal("a malformed tag was accepted")
	}
	if after.Len() != before.Len() {
		t.Errorf("the second declaration was told %d things and the first %d:\n%s",
			after.Len(), before.Len(), after.Render())
	}
	if built == nil {
		t.Error("the second declaration got no model at all")
	}

	// Word for word, positions included. A malformed tag is in one place
	// whoever named the type that carries it, and pointing the second author at
	// their own declaration would send them to a line holding nothing but a
	// name.
	if after.Render() != before.Render() {
		t.Errorf("the two declarations were told different things:\n%s\n---\n%s",
			before.Render(), after.Render())
	}
}

// And the order they are built in changes nothing, since the fault is in the
// subject rather than in either of them.
func TestASharedSubjectDoesNotDependOnOrder(t *testing.T) {
	loaded := session(t)
	tagged := named(t, loaded, "Tagged")

	alone := subject.New(subject.Config{Fset: loaded.Fset})
	_, first := alone.Build(tagged, subject.At(where))

	shared := subject.New(subject.Config{Fset: loaded.Fset})
	_, _ = shared.Build(tagged, subject.At(token.Position{Filename: "model/other.go", Line: 1, Column: 1}))
	_, second := shared.Build(tagged, subject.At(where))

	if first.Render() != second.Render() {
		t.Errorf("a subject reported differently for having been built before:\n%s\n---\n%s",
			first.Render(), second.Render())
	}
}
