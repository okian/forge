// Package tmpl is a template: real Go, compiled by the ordinary build, holding
// the bodies a layer would emit.
//
// It exists to be rewritten. Every name in it that stands for something the
// declaration decides — the element, the container, the helpers — is written
// once here and replaced once there, so that what a layer emits is checked by
// the compiler before anybody asks it to emit anything.
package tmpl

import (
	"iter"
	"slices"
)

// Collection is the container the declaration becomes.
type Collection[T any] []T

// counted is a helper type the template needs and the declaration does not
// name, so it takes a prefix on the way out.
//
// This line mentions counted below the first, which is where a rewrite that
// only reworded the opening line would leave the old name behind.
type counted struct {
	seen  int
	first bool
}

// Len reports how many elements the collection holds.
func (c Collection[T]) Len() int { return len(c) }

// All walks the collection in the order it was built.
func (c Collection[T]) All() iter.Seq[T] { return slices.Values(c) }

// Backward walks the collection from the end.
func (c Collection[T]) Backward() iter.Seq[T] {
	return func(yield func(T) bool) {
		for i := len(c) - 1; i >= 0; i-- {
			if !yield(c[i]) {
				return
			}
		}
	}
}

// Append adds elements to the collection and returns it.
//
// A method returning the container is where a rewrite most easily goes wrong:
// the receiver, the result and the conversion are three separate mentions of a
// type that is generic here and is not generic there.
func (c Collection[T]) Append(more ...T) Collection[T] {
	return Collection[T](append(c, more...))
}

// Count reports how many elements a sequence yields, through a helper type, so
// that a template's own helpers are exercised as well as its methods.
func Count[T any](seq iter.Seq[T]) int {
	tally := counted{first: true}
	for range seq {
		tally.seen++
	}
	return tally.seen
}

// Empty reports whether a collection holds nothing.
//
// It calls a package-level function of the template, which is the case that
// tells a rewrite by name from one that understands what it is renaming.
func (c Collection[T]) Empty() bool { return Count(c.All()) == 0 }
