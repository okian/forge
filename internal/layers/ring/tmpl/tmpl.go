// Package tmpl holds the bodies the ring layer emits.
//
// It is a template: ordinary generic Go, compiled by the ordinary build and
// read by the ordinary vet, written once here and rewritten into whatever a
// declaration called it. Ring becomes Persons, the element becomes Person, and
// the type parameter goes away because the result is not generic.
//
// Nothing here reads the element. A storage layer stores what it is given and
// hands it back, so every body treats the element as opaque — which is exactly
// the condition under which a template can be written at all.
//
// More is declared here than any one declaration gets. Two options decide the
// shape of two methods each — where the capacity comes from, and what a push
// does when the buffer is full — and writing every answer as real Go compiled
// by the ordinary build is what keeps the ones a given run does not choose from
// rotting. The layer keeps one of each pair and renames it; the rest never
// reach a file.
//
// The comments below are written for the file they end up in rather than for
// this one. They are the half of generated code a person actually reads, and a
// reader of somebody else's package has no interest in why forge's template was
// arranged the way it was; that reasoning belongs in the layer's own
// documentation, which is where this sentence is.
package tmpl

import (
	"errors"
	"iter"
)

// fixedCap is how many elements the buffer holds.
//
// It is a constant rather than a field, so the size is part of the type rather
// than of a value: every container of this type holds the same number, the
// compiler knows it, and no caller can be handed one sized differently from the
// one it expected.
const fixedCap = 8

// errFull is returned by a push that would have overwritten an element that is
// still there.
//
// One value rather than one per call, so that a caller can compare against it
// with errors.Is and act on the answer, which is the whole reason for refusing
// rather than overwriting.
var errFull = errors.New("the buffer is full")

// Ring holds a fixed number of the most recent elements.
//
// The buffer is allocated once and never grows, so a producer that outruns its
// consumer costs a bounded amount of memory rather than an increasing one. That
// is the whole of what this type is for, and it is why every method that adds
// an element has an answer for a buffer that is already full.
//
// The elements live in a slice that is treated as a circle: head is where the
// oldest one is, and n is how many there are. Reading walks from head, wrapping
// at the end, so the order elements come back in is the order they went in
// — the oldest first — regardless of where in the slice they happen to sit.
//
// The zero value has no buffer at all, which is not the same as an empty
// container and is not something any method can make sense of. Adding to one
// says so rather than carrying on. Use the constructor.
type Ring[T any] struct {
	buf  []T
	head int
	n    int
}

// New returns an empty container that holds at most the given number of
// elements.
//
// The buffer is allocated once, here, at the size asked for. A capacity that is
// not positive is a container that could never hold anything, which is a
// mistake worth hearing about at the call that made it rather than at the push
// that silently did nothing.
func New[T any](size int) *Ring[T] {
	if size <= 0 {
		panic("forge: a container's capacity must be positive")
	}
	return &Ring[T]{buf: make([]T, size)}
}

// NewFixed returns an empty container, whose capacity is the one the
// declaration fixed.
func NewFixed[T any]() *Ring[T] {
	return &Ring[T]{buf: make([]T, fixedCap)}
}

// Cap reports how many elements the container can hold, which does not change.
func (r *Ring[T]) Cap() int { return len(r.buf) }

// Len reports how many elements the container holds, which is never more than
// its capacity.
func (r *Ring[T]) Len() int { return r.n }

// Push adds an element, dropping the oldest one if the buffer is full.
//
// Dropping rather than growing is the point of the type: the newest elements
// are the ones kept, and how much memory that costs was decided when the
// container was made.
func (r *Ring[T]) Push(v T) {
	r.built()

	if r.n == len(r.buf) {
		r.buf[r.head] = v
		r.head = indexOf(r.head, 1, len(r.buf))
		return
	}

	r.buf[indexOf(r.head, r.n, len(r.buf))] = v
	r.n++
}

// PushChecked adds an element, and reports that it did not when the buffer is
// full.
//
// The element already in the buffer stays. A caller that would rather lose the
// oldest than the newest wants the other policy, which is chosen where the
// container is declared rather than where it is pushed to.
func (r *Ring[T]) PushChecked(v T) error {
	r.built()

	if r.n == len(r.buf) {
		return errFull
	}

	r.buf[indexOf(r.head, r.n, len(r.buf))] = v
	r.n++

	return nil
}

// All walks the container from the oldest element to the newest.
//
// Which slots the walk covers is fixed when All is called, so pushing during
// one neither lengthens it nor makes it come back round to a slot it has been
// to. What is in a slot is read when the walk reaches it: pushing to a full
// container overwrites the oldest slot, and a walk that has not yet reached
// that slot yields what was written there rather than what it displaced.
//
// Fixing that too would mean copying the elements, which is the one thing a
// container that exists to bound its memory should not do behind a caller's
// back. Walk it, or push to it, or take a copy and do both.
func (r *Ring[T]) All() iter.Seq[T] {
	held, from, size := r.n, r.head, len(r.buf)

	return func(yield func(T) bool) {
		for i := range held {
			if !yield(r.buf[indexOf(from, i, size)]) {
				return
			}
		}
	}
}

// Backward walks the container from the newest element to the oldest. Like All,
// which slots it covers is fixed when it is called.
func (r *Ring[T]) Backward() iter.Seq[T] {
	held, from, size := r.n, r.head, len(r.buf)

	return func(yield func(T) bool) {
		for i := held - 1; i >= 0; i-- {
			if !yield(r.buf[indexOf(from, i, size)]) {
				return
			}
		}
	}
}

// AppendSeq adds every element the sequence yields, in the order it yields
// them, dropping older ones as it fills.
//
// A sequence longer than the container leaves the last capacity elements of it,
// which is what pushing them one at a time would leave.
func (r *Ring[T]) AppendSeq(seq iter.Seq[T]) {
	for v := range seq {
		r.Push(v)
	}
}

// AppendSeqChecked adds every element the sequence yields, and stops at the
// first one that does not fit.
//
// What was added stays. The sequence is not read past the element that was
// refused, so a caller holding one that is expensive to produce does not pay to
// produce the rest of it.
func (r *Ring[T]) AppendSeqChecked(seq iter.Seq[T]) error {
	for v := range seq {
		if err := r.PushChecked(v); err != nil {
			return err
		}
	}
	return nil
}

// built stops a container that was never constructed from being added to.
//
// A zero value has no buffer, so it can hold nothing and never will. Under the
// policy that overwrites, adding to one indexes an empty slice and panics on
// its own; under the policy that refuses, it would report the container full —
// which is true and useless, since a caller checking for a full container reads
// that as back-pressure and retries for ever. Saying the same thing both ways
// makes the mistake one answer rather than two.
func (r *Ring[T]) built() {
	if len(r.buf) == 0 {
		panic("forge: this container was never constructed; use the constructor")
	}
}

// indexOf returns where the element i places after the one at from is kept.
//
// Given the buffer's own head and size it answers for the container as it is;
// given a head a walk took when it started, it answers for the container as it
// was, which is what keeps a walk on the positions it set out to visit.
//
// One subtraction rather than a remainder: i is never more than the number of
// elements held, which is never more than the size, so the sum passes the end
// of the buffer at most once.
func indexOf(from, i, size int) int {
	if slot := from + i; slot < size {
		return slot
	}
	return from + i - size
}
