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

	// Imports holds the paths the declarations need, deduplicated, in the order
	// they were first asked for.
	Imports []string

	// Assertions holds the compile-time claims to emit, deduplicated.
	Assertions []layer.Assertion

	// Requires names the helper types the declarations call into and do not
	// themselves declare, deduplicated. Two layers reaching one helper name it
	// twice and it is emitted once, which is the whole point of naming it
	// rather than emitting it.
	Requires []model.TypeRef
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
		// Cloned rather than aliased: a layer hands its declarations over and
		// is done with them, but sharing the backing array makes that a
		// convention rather than a fact, and a layer that reused its slice
		// would be editing what was merged.
		section := emit.Section{
			Decls:    slices.Clone(unit.Decls),
			Comments: slices.Clone(unit.Comments),
			Fset:     unit.Fset,
		}
		if !section.Empty() {
			out.Sections = append(out.Sections, section)
		}

		for _, path := range unit.Imports {
			if path != "" && !slices.Contains(out.Imports, path) {
				out.Imports = append(out.Imports, path)
			}
		}

		for _, assertion := range unit.Assertions {
			if !slices.Contains(out.Assertions, assertion) {
				out.Assertions = append(out.Assertions, assertion)
			}
		}

		for _, required := range unit.Requires {
			if !required.IsZero() && !slices.Contains(out.Requires, required) {
				out.Requires = append(out.Requires, required)
			}
		}
	}

	return out
}

// ImportSpecs returns the unit's imports in the shape a file takes them.
//
// A layer names an import by path and nothing else, because a layer has no way
// to know what else the file will import and so no business choosing a name to
// avoid it. Choosing names belongs to the stage that can see the whole file.
func (u Unit) ImportSpecs() []emit.Import {
	out := make([]emit.Import, 0, len(u.Imports))
	for _, path := range u.Imports {
		out = append(out, emit.Import{Path: path})
	}
	return out
}
