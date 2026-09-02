package merge

import (
	"slices"

	"github.com/okian/forge/internal/emit"
	"github.com/okian/forge/internal/layer"
	"github.com/okian/forge/internal/model"
)

// Unit is what a stack generated, gathered into one place.
//
// It holds the same four things a layer produces, because merging several is
// not a different kind of thing from producing one — but declarations arrive as
// sections rather than as one run. Each layer's declarations only make sense
// beside the comments and the file set they were parsed with, and a stack's
// layers do not share those, so what one layer generated stays one section.
type Unit struct {
	// Sections holds what each layer generated, in stack order.
	Sections []emit.Section

	// Imports holds the imports the declarations need, deduplicated, in the
	// order they were first asked for.
	//
	// Deduplicated by path *and* bound name, so two layers importing one path
	// under one name import it once and two that disagree about the name are
	// both kept — which is not a fix, it is what makes the disagreement visible
	// to the stage that writes the file rather than silently resolving it here
	// in favour of whichever layer generated first.
	Imports []emit.Import

	// Assertions holds the compile-time claims to emit, deduplicated.
	Assertions []layer.Assertion

	// Requires names the helper types the declarations call into and do not
	// themselves declare, deduplicated. Two layers reaching one helper name it
	// twice and it is emitted once, which is the whole point of naming it
	// rather than emitting it.
	Requires []model.TypeRef

	// Provides holds what the layers contributed to something other than the
	// declaration, keyed by what it is about and merged so that one key holds
	// one answer.
	//
	// Carried rather than folded into the sections, because where it goes is
	// not this stage's to decide: two declarations of one package can both
	// provide it, and only the stage that sees the whole package knows that.
	Provides map[string]layer.Unit
}

// Empty reports whether the unit would write nothing.
func (u Unit) Empty() bool {
	for _, section := range u.Sections {
		if !section.Empty() {
			return false
		}
	}
	return true
}

// Units gathers what several layers generated into one unit, in the order the
// layers were given.
//
// That order is the stack's, outermost first, and it is what makes a generated
// file a function of the declaration that asked for it. Nothing is sorted:
// within a layer the order is the layer's own, and a layer that emits a type
// and then the constructor for it means them to stay together.
func Units(units ...layer.Unit) Unit {
	var out Unit

	for _, unit := range units {
		take(&out, unit)
	}

	return out
}

// take folds one layer's unit into the merge.
//
// Everything but the declarations is deduplicated, because everything but the
// declarations is a name rather than a thing: two layers importing one package
// import it once, and two requiring one helper get one. The declarations are
// kept whole and in order, since two layers writing methods of the same name is
// a collision to report rather than a duplicate to drop.
func take(u *Unit, unit layer.Unit) {
	// Cloned rather than aliased: a layer hands its declarations over and is
	// done with them, but sharing the backing array makes that a convention
	// rather than a fact, and a layer that reused its slice would be editing
	// what was merged.
	section := emit.Section{
		Decls:    slices.Clone(unit.Decls),
		Comments: slices.Clone(unit.Comments),
		Fset:     unit.Fset,
	}
	if !section.Empty() {
		u.Sections = append(u.Sections, section)
	}

	for _, one := range unit.Imports {
		if one.Path != "" && !slices.Contains(u.Imports, one) {
			u.Imports = append(u.Imports, one)
		}
	}

	for _, assertion := range unit.Assertions {
		if !slices.Contains(u.Assertions, assertion) {
			u.Assertions = append(u.Assertions, assertion)
		}
	}

	for _, required := range unit.Requires {
		if !required.IsZero() && !slices.Contains(u.Requires, required) {
			u.Requires = append(u.Requires, required)
		}
	}

	provided(u, unit.Provides)
}

// provided keeps what a unit contributed to something other than the
// declaration, once per thing it is about.
//
// The first answer under a key wins and the rest are the same answer: what a
// key means is that two contributions are about the same thing, and two layers
// contributing differently about one thing is a disagreement this cannot
// resolve and the package will not compile with either way of resolving it.
func provided(u *Unit, held map[string]layer.Unit) {
	for about, one := range held {
		if _, taken := u.Provides[about]; taken {
			continue
		}
		if u.Provides == nil {
			u.Provides = make(map[string]layer.Unit)
		}
		u.Provides[about] = one
	}
}

// Join folds several merged units into one, in the order they are given.
//
// What [Units] does for the layers of one stack, this does for the stacks of
// one package: a package is written as a single file, so everything its
// declarations produced has to become one unit before it can be rendered.
//
// The declarations stay whole and in order, because two of them writing one
// name is a collision to report rather than a duplicate to drop — and because a
// reader of the file gets the declarations in the order their declarations were
// written. Everything else is deduplicated for the reason [Units] deduplicates
// it: an import, a helper and an assertion are names rather than things, and two
// stacks asking for one of them ask for one.
func Join(units ...Unit) Unit {
	var out Unit

	for _, unit := range units {
		joined(&out, unit)
	}
	return out
}

// joined folds one merged unit into another.
func joined(out *Unit, unit Unit) {
	for _, section := range unit.Sections {
		if !section.Empty() {
			out.Sections = append(out.Sections, section)
		}
	}

	out.Imports = once(out.Imports, unit.Imports, func(one emit.Import) bool { return one.Path != "" })
	out.Assertions = once(out.Assertions, unit.Assertions, func(layer.Assertion) bool { return true })
	out.Requires = once(out.Requires, unit.Requires, func(one model.TypeRef) bool { return !one.IsZero() })

	provided(out, unit.Provides)
}

// once appends what is worth keeping and is not already held.
func once[T comparable](held, adding []T, worth func(T) bool) []T {
	for _, one := range adding {
		if worth(one) && !slices.Contains(held, one) {
			held = append(held, one)
		}
	}
	return held
}
