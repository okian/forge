package shape_test

import (
	"go/token"
	"go/types"
	"slices"
	"testing"

	"github.com/okian/forge/internal/model"
	"github.com/okian/forge/internal/shape"
)

// namedStruct builds a named struct type, which is what a subject is.
func namedStruct(pkgPath, name string) *types.Named {
	pkg := types.NewPackage(pkgPath, "domain")
	obj := types.NewTypeName(token.NoPos, pkg, name, nil)
	return types.NewNamed(obj, types.NewStruct(nil, nil), nil)
}

// A set holds what was put in it and answers about what was not, which is the
// whole of what a composition rule asks.
func TestCapSet(t *testing.T) {
	set := shape.Set(shape.Sized, shape.Streamable)

	if !set.Has(shape.Sized) || !set.Has(shape.Streamable) {
		t.Errorf("%s does not hold what it was built from", set)
	}
	if set.Has(shape.Ordered) {
		t.Errorf("%s holds a capability nobody added", set)
	}
	// Every one has to be there, not any one: a layer needing two things is not
	// satisfied by a stack with one of them.
	if set.Has(shape.Sized, shape.Ordered) {
		t.Errorf("%s reports holding both of two when it holds one", set)
	}
	// A layer that requires nothing is satisfied by anything, including nothing.
	if !set.Has() || !shape.CapSet(0).Has() {
		t.Error("a set does not satisfy an empty requirement")
	}
}

func TestCapSetWithAndWithout(t *testing.T) {
	below := shape.Set(shape.Sized, shape.Ordered, shape.Streamable)

	above := below.With(shape.Concurrent).Without(shape.Streamable, shape.Indexed)

	want := []shape.Cap{shape.Sized, shape.Ordered, shape.Concurrent}
	if got := above.All(); !slices.Equal(got, want) {
		t.Errorf("All() = %v, want %v", got, want)
	}
	// Taking away what was never there is not an error, because a decorator
	// masks what it would expose without knowing what is beneath it.
	if !above.Has(shape.Sized) {
		t.Errorf("%s lost a capability nobody took away", above)
	}
	// The set beneath is unchanged: a shape is passed by value up the stack, and
	// a layer that edited what it was handed would rewrite its own history.
	if !below.Has(shape.Streamable) {
		t.Errorf("the shape beneath became %s", below)
	}
}

// Capabilities render in declaration order whatever order they were added in,
// or two runs would print one stack two ways.
func TestCapSetRendersInDeclarationOrder(t *testing.T) {
	late := shape.Set(shape.Concurrent, shape.Sized)
	early := shape.Set(shape.Sized, shape.Concurrent)

	if got, want := late.String(), "Sized, Concurrent"; got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
	if late.String() != early.String() {
		t.Errorf("%q and %q are the same set rendered two ways", late, early)
	}
}

// An empty set is a thing a table has to print, not a blank cell.
func TestTheEmptySet(t *testing.T) {
	var none shape.CapSet

	if !none.Empty() {
		t.Error("the zero value is not empty")
	}
	if got, want := none.String(), "—"; got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
	if none.All() != nil {
		t.Errorf("All() = %v, want nothing", none.All())
	}
}

// A capability nobody defined still has to render, because the alternative is a
// panic in the middle of a diagnostic — or worse, a set that is not empty
// rendering as though it were, which turns "and it is Sized" into "and it is".
func TestAnUndefinedCapabilityStillRenders(t *testing.T) {
	unknown := shape.Cap(1 << 20)

	if got, want := unknown.String(), "cap(1048576)"; got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}

	only := shape.Set(unknown)
	if only.Empty() {
		t.Error("a set holding an undeclared capability reports itself empty")
	}
	if got, want := only.String(), "cap(1048576)"; got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}

	// Declared ones still come first, in their own order.
	mixed := shape.Set(shape.Ordered, unknown, shape.Sized)
	if got, want := mixed.String(), "Sized, Ordered, cap(1048576)"; got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
}

// A subject is where the capabilities no layer adds come from. A stack of
// layers over nothing is not structured however deep it is.
func TestSubjectShape(t *testing.T) {
	person := &model.Struct{
		Named:  namedStruct("example.com/domain", "Person"),
		Fields: []model.Field{{Name: "Name"}},
	}

	structured := shape.Subject(person)
	if !structured.Caps.Has(shape.Structured) {
		t.Errorf("a subject with fields exposes %s", structured.Caps)
	}
	if got, want := structured.Elem, person.Ref(); got != want {
		t.Errorf("Elem = %v, want %v", got, want)
	}

	// A named type with no fields is a subject like any other, and there is
	// nothing in it to generate a codec or a validator from.
	scalar := shape.Subject(&model.Struct{Named: namedStruct("example.com/domain", "Celsius")})
	if scalar.Caps.Has(shape.Structured) {
		t.Errorf("a subject with no fields exposes %s", scalar.Caps)
	}

	// A subject that could not be built is not a shape that panics. The model's
	// own accessors are nil-safe for the same reason, and a caller reaching
	// here after a refusal should get an empty shape rather than a crash.
	if got := shape.Subject(nil); !got.Caps.Empty() || !got.Elem.IsZero() {
		t.Errorf("Subject(nil) = %+v, want the zero shape", got)
	}
}

// A shape travels up a stack by value, and a caller holding an intermediate one
// must not watch it grow methods belonging to a layer above it.
func TestWithMethodsCopies(t *testing.T) {
	base := shape.Shape{Surface: []shape.Method{{Name: "Len"}}}

	first := base.WithMethods(shape.Method{Name: "All"})
	second := base.WithMethods(shape.Method{Name: "Push"})

	if got, want := first.Names(), []string{"Len", "All"}; !slices.Equal(got, want) {
		t.Errorf("Names() = %v, want %v", got, want)
	}
	if got, want := second.Names(), []string{"Len", "Push"}; !slices.Equal(got, want) {
		t.Errorf("Names() = %v, want %v", got, want)
	}
	if got, want := base.Names(), []string{"Len"}; !slices.Equal(got, want) {
		t.Errorf("the shape beneath became %v", got)
	}

	// Adding nothing is not a copy nobody asked for.
	if got := base.WithMethods(); !slices.Equal(got.Names(), base.Names()) {
		t.Errorf("Names() = %v, want %v", got.Names(), base.Names())
	}
}

// The surface is what a decorator wraps and what collision detection reads, so
// it has to be answerable by name and stable in order.
func TestShapeSurface(t *testing.T) {
	subject := shape.Shape{
		Elem: model.TypeRef{Pkg: "example.com/domain", Name: "Person"},
		Surface: []shape.Method{
			{Name: "Len", Signature: "() int", Doc: "how many elements it holds"},
			{Name: "All", Signature: "() iter.Seq[Person]"},
		},
	}

	all, ok := subject.Method("All")
	if !ok {
		t.Fatal("the surface has no All")
	}
	if got, want := all.String(), "All() iter.Seq[Person]"; got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
	if _, ok := subject.Method("Push"); ok {
		t.Error("the surface has a method nobody added")
	}

	if got, want := subject.Names(), []string{"Len", "All"}; !slices.Equal(got, want) {
		t.Errorf("Names() = %v, want %v", got, want)
	}
}
