// Package tmpl holds the bodies the slice layer emits.
//
// It is a template: ordinary generic Go, compiled by the ordinary build and
// read by the ordinary vet, written once here and rewritten into whatever a
// declaration called it. Slice becomes Persons, the element becomes Person, and
// the type parameter goes away because the result is not generic.
//
// Nothing here reads the element. A storage layer stores what it is given and
// hands it back, so every body in this package treats the element as opaque —
// which is exactly the condition under which a template can be written at all.
//
// The comments below are written for the file they end up in rather than for
// this one. They are the half of generated code a person actually reads, and a
// reader of somebody else's package has no interest in why forge's template was
// arranged the way it was; that reasoning belongs in the layer's own
// documentation, which is where this sentence is.
package tmpl

import (
	"iter"
	"slices"
)

// Slice holds elements in the order they were appended.
//
// The underlying type is a real slice, so everything the language does to one
// can be done to this: index it, range over it, take a part of it, pass it
// where a slice is wanted. The methods add to that rather than stand in for it.
//
// Reading a container does not need a pointer to one, so those methods take it
// by value: a value of this type satisfies an interface asking for them, and a
// container returned by a function can be read without being stored in a
// variable first. Only the method that changes the container takes a pointer,
// because appending may move it.
type Slice[T any] []T

// New returns a container holding the given elements, in order.
//
// The elements are copied, so the container does not follow the slice they were
// passed in.
func New[T any](elems ...T) Slice[T] { return slices.Clone(elems) }

// Len reports how many elements the container holds.
func (s Slice[T]) Len() int { return len(s) }

// All walks the container from the first element to the last.
//
// The walk is fixed when All is called, so appending during one does not extend
// it.
func (s Slice[T]) All() iter.Seq[T] { return slices.Values(s) }

// Backward walks the container from the last element to the first. Like All,
// the walk is fixed when it is called.
func (s Slice[T]) Backward() iter.Seq[T] {
	return func(yield func(T) bool) {
		for _, elem := range slices.Backward(s) {
			if !yield(elem) {
				return
			}
		}
	}
}

// AppendSeq adds every element the sequence yields, in the order it yields
// them.
//
// It appends rather than replaces, so several sequences can be gathered into
// one container by calling it more than once.
func (s *Slice[T]) AppendSeq(seq iter.Seq[T]) { *s = slices.AppendSeq(*s, seq) }
