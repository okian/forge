package contenthash

import (
	"go/token"
	"go/types"
	"testing"

	"github.com/okian/forge/plugin"
)

// What a composite written in place is made of, and nothing for what is not one.
//
// The walk that decides whether a package can name everything it is asked to
// hash descends through these and no others. A map contributes both halves,
// because either may be a struct written in place; a channel and a function
// contribute nothing, which is not an oversight — neither is a value a hash can
// be taken of, and both are refused at the point they are used rather than
// walked into here.
func TestWhatACompositeWrittenInPlaceHolds(t *testing.T) {
	elem := types.Typ[types.Int]
	key := types.Typ[types.String]

	cases := map[string]struct {
		under types.Type
		want  int
	}{
		"a pointer": {types.NewPointer(elem), 1},
		"a slice":   {types.NewSlice(elem), 1},
		"an array":  {types.NewArray(elem, 3), 1},
		"a map":     {types.NewMap(key, elem), 2},
		"a channel": {types.NewChan(types.SendRecv, elem), 0},
	}

	for name, one := range cases {
		t.Run(name, func(t *testing.T) {
			if got := holds(one.under); len(got) != one.want {
				t.Errorf("holds(%s) returned %d types, want %d", name, len(got), one.want)
			}
		})
	}
}

// The walk stops at what it cannot learn anything more from.
//
// Three stops, each for its own reason. A type that is not there is a package
// that did not type-check, and there is nothing to read. A type already seen is
// how a type that reaches itself terminates — without it the walk recurses
// until the stack runs out. And a named type stops because being named is the
// whole question: the package can spell it, so whatever it holds is that type's
// own business rather than this walk's.
func TestWhereTheWalkStops(t *testing.T) {
	pkg := types.NewPackage("example.com/other", "other")
	obj := types.NewTypeName(token.NoPos, pkg, "Roster", nil)
	named := types.NewNamed(obj, types.NewStruct(nil, nil), nil)
	loop := types.NewSlice(types.Typ[types.Int])

	cases := map[string]struct {
		held types.Type
		seen map[types.Type]bool
	}{
		"nothing to read":    {nil, map[types.Type]bool{}},
		"already seen":       {loop, map[types.Type]bool{loop: true}},
		"a type with a name": {named, map[types.Type]bool{}},
	}

	for name, one := range cases {
		t.Run(name, func(t *testing.T) {
			if unnameable(one.held, "example.com/mine", one.seen) {
				t.Errorf("unnameable(%s) = true, want false", name)
			}
		})
	}
}

// A struct written in place keeps back a field another package declared
// unexported.
//
// This is the case the whole walk exists for. The struct has no name, so the
// package being generated into would have to write it out in full to hash it —
// and it cannot write a field it may not refer to. Reported as unnameable so
// that the refusal names the type the author wrote, rather than emitting a
// composite literal the compiler rejects in a file nobody edited.
func TestAStructKeepingBackAnotherPackagesField(t *testing.T) {
	elsewhere := types.NewPackage("example.com/other", "other")
	hidden := types.NewField(token.NoPos, elsewhere, "hidden", types.Typ[types.Int], false)
	under := types.NewStruct([]*types.Var{hidden}, nil)

	if !keepsBack(under, "example.com/mine", map[types.Type]bool{}) {
		t.Error("a struct holding another package's unexported field: want it kept back, got readable")
	}
	if keepsBack(under, "example.com/other", map[types.Type]bool{}) {
		t.Error("the declaring package can read its own unexported field, want it readable")
	}
}

// A struct that is not one is not remembered.
//
// The same guard the check layer carries, for the same reason: what the planner
// walks is built from a load that may have failed, and reserving a plan for a
// struct with no type of its own would leave an entry every later stage has to
// special-case.
func TestAStructThatIsNotOneIsNotRemembered(t *testing.T) {
	cases := map[string]*plugin.Struct{
		"nothing":               nil,
		"a struct with no type": {},
	}

	for name, held := range cases {
		t.Run(name, func(t *testing.T) {
			var p planner
			p.remember(held)

			if len(p.order) != 0 {
				t.Errorf("remembering %s left %v in the order, want nothing", name, p.order)
			}
		})
	}
}
