// Package tmpl holds the bodies the collection layer emits that do not depend
// on the subject's fields.
//
// The layer's surface is written per field — a projection to one, a sort by
// one, a lookup keyed by one — and no template can be: a template is generic
// over its element, and a type parameter has no fields. What can be written
// here is everything those methods do once they have the field in hand, and
// that turns out to be all of it. Each generated method hands one field to one
// of these and does nothing else, so the loops, the sorting and the map
// building are compiled by the ordinary build and read by the ordinary vet,
// while what is built per field is a single expression that cannot go wrong in
// an interesting way.
//
// Methods rather than package-level functions, so that nothing here takes a
// name in the package it lands in. The names below are on the declared type,
// where they collide with nothing an author has and where a second declaration
// gets its own.
//
// The comments below are written for the file they end up in rather than for
// this one, since they are what a reader of somebody else's package sees.
package tmpl

import (
	"cmp"
	"iter"
	"slices"
)

// Collection is the declared type. It is declared here so the bodies compile
// and is not emitted: the author wrote it, or the storage layer beneath did.
type Collection[T any] []T

// All is the storage layer's, declared here for the same reason and emitted no
// more than the type is. Everything below walks through it rather than over the
// collection directly, because what the collection is underneath is the storage
// layer's business — a ring is not a slice, and a query surface written against
// one representation would work over exactly that one.
func (c Collection[T]) All() iter.Seq[T] { return slices.Values(c) }

// project collects one value from every element, in the order the elements come
// in.
//
// The result is grown rather than sized ahead, because what is walked is not
// always something that can be counted first — a walk is all this needs of the
// collection, and needing a length as well would be needing more.
func (c Collection[T]) project[V any](of func(T) V) []V {
	var out []V
	for v := range c.All() {
		out = append(out, of(v))
	}
	return out
}

// keyed returns the elements by the key taken from each of them.
//
// Later wins where two elements share a key, which is what writing into a map
// does and is the only answer that does not need a policy. A key that has to be
// unique is a thing to check, not a thing to discover from which element came
// last.
//
// A key type that holds an interface is a key that can be built and cannot
// always be used: the map panics on a dynamic value that is not comparable,
// exactly as any other map keyed the same way does.
func (c Collection[T]) keyed[K comparable](by func(T) K) map[K]T {
	out := make(map[K]T)
	for v := range c.All() {
		out[by(v)] = v
	}
	return out
}

// ordered returns the elements sorted by the key taken from each of them,
// leaving equal ones in the order they came in.
//
// A new slice rather than a sort in place: the collection is what it was, and a
// method that quietly reordered it would make asking a question change the
// answer to the next one.
func (c Collection[T]) ordered[K cmp.Ordered](by func(T) K) []T {
	out := slices.Collect(c.All())
	slices.SortStableFunc(out, func(a, b T) int { return cmp.Compare(by(a), by(b)) })

	return out
}
