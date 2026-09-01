package model

import (
	"go/token"
	"go/types"
)

// Unnameable returns the first type beneath t that a file in the package given
// cannot write down, and whether there is one.
//
// A generated file names types: in a signature, in a field, in a conversion. A
// name that is unexported belongs to the package that declared it and cannot be
// written anywhere else, so a layer that spelled one out would produce a file
// that does not compile — and the spelling would look perfectly reasonable,
// because how a type is written and whether it may be written are different
// questions and only the second one is about where the file is.
//
// It stops at a name it can write. What is inside such a type is that type's
// business: a field of it is reached through the name rather than by spelling
// out what it holds, so an unexported type inside an exported one is nobody
// else's problem. What is walked is everything a type is written out of —
// pointers, slices, arrays, maps, channels, functions and a struct written in
// place — because each of those is spelled by spelling what it holds.
func Unnameable(t types.Type, pkg string) (string, bool) {
	return unnameable(t, pkg, make(map[types.Type]bool))
}

// unnameable is Unnameable with the types already visited, so that a type
// reaching itself ends.
func unnameable(t types.Type, pkg string, seen map[types.Type]bool) (string, bool) {
	if t == nil || seen[t] {
		return "", false
	}
	seen[t] = true

	if named, is := types.Unalias(t).(*types.Named); is {
		return namedUnnameable(named, pkg, seen)
	}

	switch held := t.Underlying().(type) {
	case *types.Pointer:
		return unnameable(held.Elem(), pkg, seen)

	case *types.Slice:
		return unnameable(held.Elem(), pkg, seen)

	case *types.Array:
		return unnameable(held.Elem(), pkg, seen)

	case *types.Chan:
		return unnameable(held.Elem(), pkg, seen)

	case *types.Map:
		if what, found := unnameable(held.Key(), pkg, seen); found {
			return what, true
		}
		return unnameable(held.Elem(), pkg, seen)

	case *types.Struct:
		return structUnnameable(held, pkg, seen)

	case *types.Signature:
		return signatureUnnameable(held, pkg, seen)

	default:
		return "", false
	}
}

// namedUnnameable answers for a defined type: the name itself, and then the
// arguments it was instantiated with, which are written out beside it.
func namedUnnameable(named *types.Named, pkg string, seen map[types.Type]bool) (string, bool) {
	obj := named.Obj()
	if obj != nil && !obj.Exported() && (obj.Pkg() == nil || obj.Pkg().Path() != pkg) {
		return TypeString(named), true
	}

	for one := range named.TypeArgs().Types() {
		if what, found := unnameable(one, pkg, seen); found {
			return what, true
		}
	}
	return "", false
}

// structUnnameable answers for a struct written in place, whose members are
// written out where it is.
//
// The member's own name as well as its type. A field written in another
// package's struct literal type is named by that package, so a file elsewhere
// cannot declare one — which is the same wall from the other side.
func structUnnameable(held *types.Struct, pkg string, seen map[types.Type]bool) (string, bool) {
	for field := range held.Fields() {
		if !field.Exported() && (field.Pkg() == nil || field.Pkg().Path() != pkg) {
			return field.Name(), true
		}
		if what, found := unnameable(field.Type(), pkg, seen); found {
			return what, true
		}
	}
	return "", false
}

// signatureUnnameable answers for a function type, whose parameters and results
// are written out where it is.
func signatureUnnameable(held *types.Signature, pkg string, seen map[types.Type]bool) (string, bool) {
	for _, list := range []*types.Tuple{held.Params(), held.Results()} {
		for one := range list.Variables() {
			if what, found := unnameable(one.Type(), pkg, seen); found {
				return what, true
			}
		}
	}
	return "", false
}

// locker is the interface a value that must not be copied satisfies.
//
// Built rather than imported, because what is wanted is the shape and not the
// package: sync.Mutex is the usual one, and a type of somebody's own with the
// same two methods is as unsafe to copy and is what the vet everybody runs
// will say so about.
var locker = types.NewInterfaceType([]*types.Func{
	types.NewFunc(token.NoPos, nil, "Lock", types.NewSignatureType(nil, nil, nil, nil, nil, false)),
	types.NewFunc(token.NoPos, nil, "Unlock", types.NewSignatureType(nil, nil, nil, nil, nil, false)),
}, nil).Complete()

// Uncopyable returns the first type beneath t that must not be copied, and
// whether there is one.
//
// A lock is the case, and it is not a matter of taste: a copied mutex is a
// second mutex protecting nothing, and `go vet` reports the assignment that
// made it. Generated code that a caller's own vet complains about is generated
// code they cannot fix, so a layer that would write such an assignment says so
// instead.
//
// Through the fields of a struct and the elements of an array, which are copied
// with the value, and not through a pointer, a slice or a map, which are not: a
// struct holding a *sync.Mutex is copied without copying the lock, which is
// exactly why holding one that way is the usual advice.
func Uncopyable(t types.Type) (string, bool) {
	return uncopyable(t, make(map[types.Type]bool))
}

// uncopyable is Uncopyable with the types already visited.
func uncopyable(t types.Type, seen map[types.Type]bool) (string, bool) {
	if t == nil || seen[t] {
		return "", false
	}
	seen[t] = true

	// Asked of a pointer to it, because that is where the methods are: a mutex
	// locks through a pointer receiver, and the value's own method set has
	// neither of them.
	if types.Implements(types.NewPointer(t), locker) {
		return TypeString(t), true
	}

	switch held := t.Underlying().(type) {
	case *types.Struct:
		for field := range held.Fields() {
			if what, found := uncopyable(field.Type(), seen); found {
				return what, true
			}
		}
		return "", false

	case *types.Array:
		return uncopyable(held.Elem(), seen)

	default:
		return "", false
	}
}
