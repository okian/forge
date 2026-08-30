package subject

import (
	"go/types"

	"github.com/okian/forge/internal/model"
)

// classify describes a type the way a layer branches on it: the coarse shape
// first, and the parts an unnamed composite is made of alongside it.
//
// A named type is where the description stops. Its name is the interesting
// thing about it — it is what a generated method attaches to and what the
// closure walk follows — and a layer that needs the structure underneath goes
// through [model.Classified.Type] to get it. Unnamed composites have no name to
// stop at, so they are taken apart here rather than by every layer separately.
func classify(t types.Type) model.Classified {
	// Aliases are spellings, not types. Nothing downstream should have to know
	// whether the author wrote the name or one of its aliases.
	t = types.Unalias(t)

	out := model.Classified{Type: t}

	switch typ := t.(type) {
	case *types.Basic:
		out.Class = model.ClassBasic

	case *types.Named:
		out.Class = model.ClassNamed
		out.Ref = model.RefOf(typ)

	case *types.Pointer:
		out.Class = model.ClassPointer
		out.Elem = elem(typ.Elem())

	case *types.Slice:
		out.Class = model.ClassSlice
		out.Elem = elem(typ.Elem())

	case *types.Array:
		out.Class = model.ClassArray
		out.Elem = elem(typ.Elem())
		out.Len = typ.Len()

	case *types.Map:
		out.Class = model.ClassMap
		out.Key = elem(typ.Key())
		out.Elem = elem(typ.Elem())

	case *types.Struct:
		out.Class = model.ClassStruct

	case *types.Interface:
		out.Class = model.ClassInterface

	case *types.Chan:
		out.Class = model.ClassChan
		out.Elem = elem(typ.Elem())

	case *types.Signature:
		out.Class = model.ClassFunc
	}

	// Anything else keeps ClassInvalid and its type. A tuple or a type
	// parameter cannot be the type of a field in a subject forge accepts, so
	// the case is unreachable through a declaration — but a class of "I do not
	// know" is a thing a layer can refuse, and a panic is not.
	return out
}

// elem classifies a type nested inside an unnamed composite.
func elem(t types.Type) *model.Classified {
	classified := classify(t)
	return &classified
}
