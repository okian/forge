package model

import "strings"

// MarkerPkg is the import path of the package that declares the markers a
// declaration is written against.
//
// It lives here because more than one stage needs it and none of them owns it:
// resolution recognises a marker by it, and the layer registry is keyed by
// references built from it. Putting it in either would make the other import a
// stage it has no business knowing about.
const MarkerPkg = "github.com/okian/forge"

// TypeRef identifies a named type by the package that declares it.
//
// It exists because a *types.Named is tied to the load that produced it, while
// a layer registry, a diagnostic and a golden file all need a stable, ordered,
// comparable name for the same type. TypeRef is comparable, so it serves as a
// map key, and it sorts, so output built from it is deterministic.
type TypeRef struct {
	// Pkg is the import path of the declaring package, empty for a predeclared
	// type such as string.
	Pkg string

	// Name is the type's identifier, unqualified and unparameterised.
	Name string

	// Args is the rendered type argument list of an instantiation, brackets
	// included and arguments spelled by import path, and is empty for
	// everything else.
	//
	// It is what keeps Pair[string, int] and Pair[string, bool] apart. Without
	// it two instantiations of one generic type would share an identity, and a
	// set of reachable types would silently lose one of them. A rendered string
	// rather than a nested slice keeps the whole reference comparable.
	Args string
}

// Origin returns the reference with any instantiation dropped, which is the
// form a layer registry is keyed by: the generic Collection rather than the
// instantiation Collection[Person].
func (r TypeRef) Origin() TypeRef {
	r.Args = ""
	return r
}

// String returns the fully qualified name, "path/to/pkg.Name", with the type
// argument list appended for an instantiation. A predeclared type renders as
// its bare name.
func (r TypeRef) String() string {
	var b strings.Builder
	if r.Pkg != "" {
		b.WriteString(r.Pkg)
		b.WriteByte('.')
	}
	b.WriteString(r.Name)
	b.WriteString(r.Args)
	return b.String()
}

// IsZero reports whether the reference names nothing.
func (r TypeRef) IsZero() bool { return r == TypeRef{} }

// Compare orders two references by package path, then name, then type
// arguments, reporting a negative number, zero, or a positive one — which is
// the shape [slices.SortFunc] wants, so that sorting a set of them takes no
// comparator of its own.
func (r TypeRef) Compare(other TypeRef) int {
	if c := strings.Compare(r.Pkg, other.Pkg); c != 0 {
		return c
	}
	if c := strings.Compare(r.Name, other.Name); c != 0 {
		return c
	}
	return strings.Compare(r.Args, other.Args)
}

// Less reports whether the reference sorts before another. [TypeRef.Compare]
// is the primary and is what a sort should use; this is for the places where a
// boolean reads better than a sign.
func (r TypeRef) Less(other TypeRef) bool { return r.Compare(other) < 0 }

// LayerRef is one resolved entry in a stack: the marker the declaration named,
// the kind the registered layer reports for it, and whether the entry was
// written or inferred.
type LayerRef struct {
	// Origin identifies the marker's generic origin — Collection, not
	// Collection[Person] — which is what the layer registry is keyed by, so it
	// never carries type arguments.
	Origin TypeRef

	// Kind is the kind the registered layer reports. It stays KindInvalid for a
	// marker no layer claims, which is a diagnostic rather than a panic.
	Kind Kind

	// Implicit records that resolution inserted this entry rather than the
	// author writing it, as happens for the default storage beneath a refining
	// layer that was written on its own. Nothing points a diagnostic caret at
	// an entry nobody wrote.
	Implicit bool
}

// Directive returns the name a //forge: comment uses to address this layer:
// the marker's name, lower-cased. Collection answers to //forge:collection and
// LRU to //forge:lru.
//
// Directive names are a flat namespace. Two markers with the same name in
// different packages would answer to one directive, so the layer registry has
// to reject the second rather than let a declaration address either.
func (r LayerRef) Directive() string { return strings.ToLower(r.Origin.Name) }

// String returns the marker's name and kind, marking an inferred entry, in the
// form "Collection:refining" or "Slice:storage(implicit)".
func (r LayerRef) String() string {
	var b strings.Builder
	b.WriteString(r.Origin.Name)
	b.WriteByte(':')
	b.WriteString(r.Kind.String())
	if r.Implicit {
		b.WriteString("(implicit)")
	}
	return b.String()
}
