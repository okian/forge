package subject_test

import (
	"go/token"
	"go/types"
	"slices"
	"testing"

	"github.com/okian/forge/internal/model"
)

// A layer branches on the class before it looks at anything finer, so every
// shape an author can write a field in has to have one.
func TestFieldsAreClassified(t *testing.T) {
	composite := build(t, "Composite")

	cases := map[string]model.Class{
		"Basic":   model.ClassBasic,
		"Named":   model.ClassNamed,
		"Pointer": model.ClassPointer,
		"Slice":   model.ClassSlice,
		"Array":   model.ClassArray,
		"Map":     model.ClassMap,
		"Struct":  model.ClassStruct,
		// error is a named type whose underlying type is an interface, and the
		// name is the interesting part: it is what a diagnostic prints.
		"Iface": model.ClassNamed,
		// any is an alias for the empty interface, and an alias is a spelling
		// rather than a type.
		"Anything": model.ClassInterface,
		"Chan":     model.ClassChan,
		"Func":     model.ClassFunc,
	}

	for name, want := range cases {
		t.Run(name, func(t *testing.T) {
			field, ok := composite.Field(name)
			if !ok {
				t.Fatalf("Composite has no field %s", name)
			}
			if got := field.Type.Class; got != want {
				t.Errorf("%s is classified %s, want %s", name, got, want)
			}
			if !field.Type.Class.Valid() {
				t.Errorf("%s is classified %s, which is not a class", name, field.Type.Class)
			}
		})
	}
}

// An unnamed composite nests, so what it is made of is taken apart once here
// rather than by every layer that needs it.
func TestUnnamedCompositesAreTakenApart(t *testing.T) {
	composite := build(t, "Composite")
	unit := model.TypeRef{Pkg: domainPkg, Name: "Unit"}

	pointer, _ := composite.Field("Pointer")
	if got := pointer.Type.Elem; got == nil || got.Ref != unit {
		t.Errorf("Pointer points at %v, want %v", got, unit)
	}

	array, _ := composite.Field("Array")
	if got, want := array.Type.Len, int64(3); got != want {
		t.Errorf("Array is %d long, want %d", got, want)
	}

	mapped, _ := composite.Field("Map")
	if got := mapped.Type.Key; got == nil || got.Class != model.ClassBasic {
		t.Errorf("Map is keyed by %v, want a basic type", got)
	}
	if got := mapped.Type.Elem; got == nil || got.Ref != unit {
		t.Errorf("Map holds %v, want %v", got, unit)
	}

	// A named type is where the description stops: the name is what a method
	// attaches to, and what is underneath it is reached through the type.
	named, _ := composite.Field("Named")
	if named.Type.Elem != nil {
		t.Errorf("a named type was taken apart into %v", named.Type.Elem)
	}
	// A field's type is rendered with its package's name, unlike a stack, which
	// is rendered as the declaration spells it. A reader of "field Since of
	// type Time" is worse off than one told time.Time; a reader of
	// "Collection[Ring[Person]]" is not helped by import paths.
	if got, want := named.Type.String(), "domain.Unit"; got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
}

// A statically generated codec cannot see through an interface, and the class
// alone does not say which fields those are: a named interface is ClassNamed
// like every other name. A layer refusing what it cannot generate for has to
// refuse both spellings.
func TestInterfacesAreRecognisedBehindANameToo(t *testing.T) {
	composite := build(t, "Composite")

	for _, name := range []string{"Iface", "Anything"} {
		field, _ := composite.Field(name)
		if !field.Type.IsInterface() {
			t.Errorf("%s is classified %s and is not recognised as an interface", name, field.Type.Class)
		}
	}

	for _, name := range []string{"Basic", "Named", "Slice", "Func"} {
		field, _ := composite.Field(name)
		if field.Type.IsInterface() {
			t.Errorf("%s is recognised as an interface", name)
		}
	}

	if (model.Classified{}).IsInterface() {
		t.Error("an unclassified type is recognised as an interface")
	}
}

// Every composite a field can be written out of leads to the types inside it,
// and the two that lead nowhere generated code could follow lead nowhere.
func TestTheWalkFollowsEveryComposite(t *testing.T) {
	composite := build(t, "Composite")

	// Unit is reached through a name, a pointer, a slice, an array, a map, a
	// struct written in place and a channel — and reached once.
	if got, want := closureNames(composite), []string{"Unit"}; !slices.Equal(got, want) {
		t.Fatalf("closure = %v, want %v", got, want)
	}
	if composite.Cyclic {
		t.Error("Composite reaches itself")
	}
}

// The subject is the one thing a stack cannot be generated without, so the
// shapes that cannot be one are refused by name, each with the fix for that
// shape rather than a hint that covers all of them and helps with none.
func TestSubjectsNothingCanBeBuiltFrom(t *testing.T) {
	loaded := session(t)
	person := named(t, loaded, "Person")

	cases := map[string]struct {
		subject types.Type
		code    string
		hint    string
	}{
		"a pointer":            {types.NewPointer(person), "FRG2002", "value type"},
		"a predeclared type":   {types.Typ[types.Int], "FRG2001", "type Celsius int"},
		"an unnamed composite": {types.NewSlice(person), "FRG2001", "declare a type"},
		"a type parameter":     {named(t, loaded, "Pair").TypeParams().At(0), "FRG2003", "instantiation"},
		"nothing at all":       {nil, "FRG2005", "report this declaration"},
	}

	where := token.Position{Filename: "domain/spec.go", Line: 12, Column: 6}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			built, diags := builder(t, loaded).Build(tc.subject, where)

			if built != nil {
				t.Errorf("built %s from it", built)
			}
			if diags.Len() != 1 {
				t.Fatalf("reported %d diagnostics, want 1:\n%s", diags.Len(), diags.Render())
			}

			reported := diags.All()[0]
			if got := reported.Code.String(); got != tc.code {
				t.Errorf("code is %s, want %s", got, tc.code)
			}
			if !contains(reported.Hint, tc.hint) {
				t.Errorf("hint %q does not mention %q", reported.Hint, tc.hint)
			}
			// The diagnostic points at the declaration that named the subject,
			// not at the subject: the subject is very often somebody else's
			// type in somebody else's file.
			if reported.Pos != where {
				t.Errorf("position is %s, want the declaration's %s", reported.Pos, where)
			}
		})
	}
}
