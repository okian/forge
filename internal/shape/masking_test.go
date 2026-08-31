package shape_test

import (
	"slices"
	"testing"

	"github.com/okian/forge/internal/model"
	"github.com/okian/forge/internal/shape"
)

// storage is a shape standing in for what a container layer exposes.
func storage() shape.Shape {
	return shape.Shape{
		Caps: shape.Set(shape.Sized, shape.Ordered, shape.Indexed, shape.Streamable),
		Elem: model.TypeRef{Pkg: "example.com/domain", Name: "Person"},
		Surface: []shape.Method{
			{Name: "Len", Signature: "() int"},
			{Name: "All", Signature: "() iter.Seq[Person]"},
			{Name: "Backward", Signature: "() iter.Seq[Person]"},
			{Name: "AppendSeq", Signature: "(seq iter.Seq[Person])", Pointer: true},
		},
	}
}

// A method taken off the surface is gone from every way of reading it.
//
// This is the half of masking capabilities cannot express. A lock that hands
// out a sequence is broken whichever side of the lock the caller walks it on,
// so the layer that adds the lock takes the walk away and offers scoped access
// instead — and the layers above it must not be able to find what it withdrew.
func TestWithdrawingAMethod(t *testing.T) {
	locked := storage().Without("All", "Backward")

	if got, want := locked.Names(), []string{"Len", "AppendSeq"}; !slices.Equal(got, want) {
		t.Errorf("the surface reads %v after the walk was withdrawn, want %v", got, want)
	}
	for _, gone := range []string{"All", "Backward"} {
		if _, found := locked.Method(gone); found {
			t.Errorf("%s was withdrawn and is still answerable by name", gone)
		}
	}
}

// Withdrawing does not disturb the shape it was taken from.
//
// A shape travels up a stack by value, and a caller holding an intermediate one
// — which is what a per-step explanation does — would otherwise watch its copy
// lose methods to a decorator above it and report a stack that never existed.
func TestWithdrawingLeavesTheShapeBelowAlone(t *testing.T) {
	below := storage()
	_ = below.Without("All")

	if _, found := below.Method("All"); !found {
		t.Error("withdrawing from a shape took the method out of the one it was taken from")
	}
}

// A name that is not on the surface is not an error.
//
// A decorator withdraws what it cannot uphold, and whether the stack beneath it
// happened to have that method is a fact about the stack: one written over a
// storage with no backward walk should not have to ask before saying it does
// not offer one.
func TestWithdrawingWhatWasNeverThere(t *testing.T) {
	for _, held := range []shape.Shape{storage(), {}} {
		before := held.Names()

		got := held.Without("Snapshot", "Do")
		if !slices.Equal(got.Names(), before) {
			t.Errorf("withdrawing a name nothing has left %v, want %v", got.Names(), before)
		}

		if got := held.Without(); !slices.Equal(got.Names(), before) {
			t.Errorf("withdrawing nothing left %v, want %v", got.Names(), before)
		}
	}
}

// A layer that emits a method of a name already there replaces it rather than
// adding a second.
//
// That is what wrapping is: the method above and the method beneath have one
// name, and the one a caller reaches is the wrapper. A surface holding both
// would answer with whichever came first — the one that is no longer reachable
// — for collision detection, for a decorator above, and for anything printing
// the type's methods.
func TestWrappingAMethodReplacesIt(t *testing.T) {
	guard := model.TypeRef{Pkg: "github.com/okian/forge", Name: "Guarded"}

	locked := storage().WithMethods(
		shape.Method{Name: "Len", Signature: "() int", Owner: guard, Doc: "read under the lock"},
		shape.Method{Name: "Snapshot", Signature: "() []Person", Owner: guard},
	)

	if got, want := locked.Names(), []string{"Len", "All", "Backward", "AppendSeq", "Snapshot"}; !slices.Equal(got, want) {
		t.Errorf("the surface reads %v after Len was wrapped, want %v", got, want)
	}

	held, ok := locked.Method("Len")
	if !ok {
		t.Fatal("Len is not on the surface that wrapped it")
	}
	if held.Owner != guard {
		t.Errorf("Len is owned by %s, want the layer that wrapped it", held.Owner)
	}
	if held.Doc != "read under the lock" {
		t.Errorf("Len reads %q, want the wrapper's own summary", held.Doc)
	}
}

// Wrapping a method does not disturb the shape it was wrapped in.
//
// The replace path is the one that would write in place if the copy were
// dropped: appending a new name grows the slice and reallocates anyway, so a
// test that only added would pass over a shape that shared its surface. This
// replaces, which writes to an index that already exists, and the shape below
// is what it was.
func TestWrappingLeavesTheShapeBelowAlone(t *testing.T) {
	guard := model.TypeRef{Pkg: "github.com/okian/forge", Name: "Guarded"}

	below := storage()
	_ = below.WithMethods(shape.Method{Name: "Len", Signature: "() int", Owner: guard})

	held, ok := below.Method("Len")
	if !ok {
		t.Fatal("the shape beneath lost Len entirely")
	}
	if held.Owner == guard {
		t.Error("wrapping a method changed it in the shape it was wrapped in")
	}
}

// The two halves compose the way a decorator uses them: withdraw what cannot be
// upheld, then offer what replaces it.
func TestWithdrawingAndReplacing(t *testing.T) {
	guard := model.TypeRef{Pkg: "github.com/okian/forge", Name: "Guarded"}

	below := storage()
	above := below.
		Without("All", "Backward", "AppendSeq").
		WithMethods(
			shape.Method{Name: "Do", Signature: "(f func(v *PersonsView))", Owner: guard, Pointer: true},
			shape.Method{Name: "Snapshot", Signature: "() []Person", Owner: guard},
		)
	above.Caps = below.Caps.Without(shape.Streamable, shape.Indexed).With(shape.Concurrent)

	if got, want := above.Names(), []string{"Len", "Do", "Snapshot"}; !slices.Equal(got, want) {
		t.Errorf("the locked surface reads %v, want %v", got, want)
	}
	if above.Caps.Has(shape.Streamable) {
		t.Error("the locked shape still reports that it can be walked")
	}
	if !above.Caps.Has(shape.Concurrent) {
		t.Error("the locked shape does not report that it is safe to share")
	}

	// And the shape it wrapped is what it was, so a report of the step beneath
	// still describes the storage rather than the lock.
	if got, want := below.Names(), []string{"Len", "All", "Backward", "AppendSeq"}; !slices.Equal(got, want) {
		t.Errorf("the shape beneath reads %v after being wrapped, want %v", got, want)
	}
}
