package subject_test

import (
	"go/token"
	"go/types"
	"slices"
	"strings"
	"testing"

	"github.com/okian/forge/internal/model"
	"github.com/okian/forge/internal/subject"
)

// contains reports whether text holds the fragment.
func contains(text, fragment string) bool { return strings.Contains(text, fragment) }

// stringerLike returns an interface with one String() string method, under
// whatever identity the caller wants to see recorded.
//
// It is built rather than loaded because the check is structural: go/types
// answers it from method sets, so an interface with the right shape is the
// right interface, and building one keeps the test from depending on which
// packages the fixture happens to import.
func stringerLike(ref model.TypeRef) subject.Interface {
	result := types.NewTuple(types.NewVar(token.NoPos, nil, "", types.Typ[types.String]))
	signature := types.NewSignatureType(nil, nil, nil, nil, result, false)

	return subject.Interface{
		Ref:  ref,
		Type: types.NewInterfaceType([]*types.Func{types.NewFunc(token.NoPos, nil, "String", signature)}, nil).Complete(),
	}
}

// A layer that would generate a method the type already has has to emit nothing
// instead, because two implementations of one method do not compile — and
// because a hand-written one is the author overriding forge on purpose.
func TestInterfacesAlreadyImplementedAreRecorded(t *testing.T) {
	loaded := session(t)

	// Two identities over one shape, passed in the order that is not sorted, so
	// that the sort is doing something.
	late := stringerLike(model.TypeRef{Pkg: "zoo", Name: "Speaker"})
	early := stringerLike(model.TypeRef{Pkg: "fmt", Name: "Stringer"})
	// An interface with nothing behind it is skipped rather than crashed on.
	empty := subject.Interface{Ref: model.TypeRef{Pkg: "none", Name: "Missing"}}

	built, diags := builder(t, loaded, late, empty, early).Build(named(t, loaded, "Holder"), subject.At(token.Position{}))
	if !diags.Empty() {
		t.Fatalf("Holder does not model clean:\n%s", diags.Render())
	}

	want := []model.TypeRef{early.Ref, late.Ref}

	one, _ := built.Field("One")
	if !slices.Equal(one.Implements, want) {
		t.Errorf("One implements %v, want %v", one.Implements, want)
	}
	if !one.Satisfies(early.Ref) {
		t.Errorf("One does not satisfy %v", early.Ref)
	}

	// The method is on the pointer, which is not in the value's method set and
	// is still a method the author wrote.
	two, _ := built.Field("Two")
	if !slices.Equal(two.Implements, want) {
		t.Errorf("Two implements %v, want %v", two.Implements, want)
	}

	// And the struct records what it satisfies for itself, not only its fields.
	named := built.Closure[0]
	if !named.Satisfies(early.Ref) {
		t.Errorf("%s implements %v, want %v", named, named.Implements, want)
	}
	if built.Satisfies(early.Ref) {
		t.Errorf("Holder claims to implement %v", early.Ref)
	}
}

// Nothing is recorded when nothing is asked for, which is honest: no interface
// is claimed rather than every interface denied.
func TestNoInterfacesAreRecordedWhenNoneAreGiven(t *testing.T) {
	holder := build(t, "Holder")

	if len(holder.Implements) != 0 {
		t.Errorf("Holder implements %v, want nothing recorded", holder.Implements)
	}
	if one, _ := holder.Field("One"); len(one.Implements) != 0 {
		t.Errorf("One implements %v, want nothing recorded", one.Implements)
	}
}

// A builder given no module has no boundary to place a type on either side of,
// so it places none of them outside.
func TestWithoutAModuleNothingIsExternal(t *testing.T) {
	loaded := session(t)

	built, diags := subject.New(subject.Config{Fset: loaded.Fset}).
		Build(named(t, loaded, "External"), subject.At(token.Position{}))
	if !diags.Empty() {
		t.Fatalf("External does not model clean:\n%s", diags.Render())
	}

	when, _ := built.Field("When")
	if when.External {
		t.Error("time.Time is external to a builder that was given no module")
	}
	// Which means its contents are followed, since nothing marked it as a
	// boundary — the flag is the only thing that stops the walk.
	if len(built.Closure[0].Fields) == 0 {
		t.Error("time.Time was recorded without being opened")
	}
}

// A predeclared type belongs to no package at all, and nothing can attach a
// method to one from anywhere.
func TestPredeclaredTypesAreExternal(t *testing.T) {
	composite := build(t, "Composite")

	iface, _ := composite.Field("Iface")
	if !iface.External {
		t.Error("error is not marked external")
	}
	if got, want := iface.Type.Ref, (model.TypeRef{Name: "error"}); got != want {
		t.Errorf("Ref = %v, want %v", got, want)
	}
}

// Positions are what a field-level diagnostic points at, and a builder given no
// file set to resolve them against reports none rather than wrong ones.
func TestWithoutAFileSetPositionsAreEmpty(t *testing.T) {
	loaded := session(t)

	built, _ := subject.New(subject.Config{Owned: loaded.Owned()}).
		Build(named(t, loaded, "Person"), subject.At(token.Position{}))

	if built.Pos != (token.Position{}) {
		t.Errorf("Pos = %s, want the zero position", built.Pos)
	}
	if field, _ := built.Field("Name"); field.Pos != (token.Position{}) {
		t.Errorf("Name is at %s, want the zero position", field.Pos)
	}
}

// The positions a builder does resolve are the declarations' own, which is
// where an author reads about a field.
func TestPositionsPointAtTheDeclaration(t *testing.T) {
	person := build(t, "Person")

	if person.Pos.Line == 0 || !contains(person.Pos.Filename, "domain.go") {
		t.Errorf("Person is at %s, want a position in the fixture", person.Pos)
	}

	name, _ := person.Field("Name")
	if name.Pos.Line <= person.Pos.Line {
		t.Errorf("Name is at %s and Person at %s, want the field below the type", name.Pos, person.Pos)
	}
}
