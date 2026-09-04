// Package tmpl holds the bodies the index layer emits that do not depend on
// the subject's fields.
//
// It is a template: ordinary generic Go, compiled by the ordinary build and
// read by the ordinary vet, written once here and rewritten into whatever a
// declaration called it. Index becomes Directory, the element becomes Person,
// and the type parameter goes away because the result is not generic.
//
// The layer's lookups are written per field — a map keyed by one, a walk of
// the elements sharing one — and no template can be: a template is generic
// over its element, and a type parameter has no fields. What can be written
// here is everything those methods do once they have the field in hand — the
// map upkeep, the two-hop walk, the swap that keeps removal from searching —
// while what is built per field is a handful of statements handing fields to
// these.
//
// The helpers are methods rather than package-level functions, so that nothing
// here takes a name in the package it lands in. They keep the names the
// template gives them: a method lives on its receiver's type, where a second
// declaration's helpers cannot reach.
//
// Three declarations below — place, placeChecked and Reset — are placeholders
// in the way the collection template's container is: they are declared here so
// the constructors and the appends compile, and every run drops them and
// builds its own, because their real bodies write into maps only a
// declaration knows the keys of.
//
// The comments below are written for the file they end up in rather than for
// this one. They are the half of generated code a person actually reads, and a
// reader of somebody else's package has no interest in why forge's template
// was arranged the way it was; that reasoning belongs in the layer's own
// documentation, which is where this sentence is.
package tmpl

import (
	"errors"
	"iter"
)

// errDup is returned by an add that found its key already held.
//
// One value rather than one per call, so that a caller can compare against it
// with errors.Is and act on the answer, which is the whole reason for refusing
// rather than replacing.
var errDup = errors.New("the key is already held")

// entryOf pairs an element with where the walk order holds it.
//
// The position is carried so that removal does not search: taking an element
// out swaps the last one into its slot, and the slot is written here rather
// than looked for. The element lives in this allocation for as long as it is
// held, which is what lets a lookup hand back a pointer that stays good while
// other elements come and go.
type entryOf[T any] struct {
	elem T
	at   int
}

// Index holds elements beside lookup maps over their declared fields.
//
// The zero value is ready to use.
type Index[T any] struct {
	order []*entryOf[T]
}

// New returns a container holding the given elements, in order.
func New[T any](elems ...T) *Index[T] {
	out := &Index[T]{}
	for _, v := range elems {
		out.place(v)
	}
	return out
}

// NewChecked returns a container holding the given elements, in order.
//
// The keys are declared unique, so two elements sharing one cannot both be
// held — and a constructor has no way to hand one of them back. It panics,
// because the elements a construction is given are the caller's own values
// rather than input from elsewhere: feed data that may collide through
// AppendSeq, which reports the collision instead.
func NewChecked[T any](elems ...T) *Index[T] {
	out := &Index[T]{}
	for _, v := range elems {
		if err := out.placeChecked(v); err != nil {
			panic("forge: two of the elements a constructor was given share one key")
		}
	}
	return out
}

// Len reports how many elements the container holds.
func (r *Index[T]) Len() int { return len(r.order) }

// All walks the elements in the order they were added, less any that removal
// has since moved.
//
// Which slots the walk covers is fixed when All is called, so adding during
// one does not extend it. Removing during one moves the last element into the
// hole, so a walk that has not reached either slot sees the moved element
// where the removed one was and nothing at the end.
func (r *Index[T]) All() iter.Seq[T] {
	held := r.order

	return func(yield func(T) bool) {
		for _, e := range held {
			if !yield(e.elem) {
				return
			}
		}
	}
}

// AppendSeq adds every element the sequence yields, in the order it yields
// them.
func (r *Index[T]) AppendSeq(seq iter.Seq[T]) {
	for v := range seq {
		r.place(v)
	}
}

// AppendSeqChecked adds every element the sequence yields, and stops at the
// first whose key is already held.
//
// What was added stays. The sequence is not read past the element that was
// refused, so a caller holding one that is expensive to produce does not pay
// to produce the rest of it.
func (r *Index[T]) AppendSeqChecked(seq iter.Seq[T]) error {
	for v := range seq {
		if err := r.placeChecked(v); err != nil {
			return err
		}
	}
	return nil
}

// place adds one element to everything that holds elements.
func (r *Index[T]) place(v T) {
	r.order = append(r.order, &entryOf[T]{elem: v, at: len(r.order)})
}

// placeChecked adds one element to everything that holds elements, unless its
// key is already held.
func (r *Index[T]) placeChecked(v T) error {
	r.place(v)
	return nil
}

// Reset empties the container, keeping the memory it has already taken.
func (r *Index[T]) Reset() {
	clear(r.order)
	r.order = r.order[:0]
}

// pick returns a pointer to the element held under a key, and whether one is.
//
// The pointer stays good while other elements are added and removed, and stops
// meaning anything once the element it names is removed. The fields the
// lookups are keyed by are not the caller's to change through it: a container
// told nothing files the element where the old values say.
func (r *Index[T]) pick[K comparable](m map[K]*entryOf[T], k K) (*T, bool) {
	e, held := m[k]
	if !held {
		return nil, false
	}
	return &e.elem, true
}

// spread walks the elements one primary bucket holds, oldest first.
func (r *Index[T]) spread(bucket []*entryOf[T]) iter.Seq[T] {
	return func(yield func(T) bool) {
		for _, e := range bucket {
			if !yield(e.elem) {
				return
			}
		}
	}
}

// found walks the elements a secondary bucket names, resolving each key
// through the primary map.
//
// Two hops rather than one, and deliberately: a secondary bucket holds keys
// rather than elements, so removing by key never has to repair more than the
// buckets the removed element was in.
func (r *Index[T]) found[K comparable](keys []K, of map[K]*entryOf[T]) iter.Seq[T] {
	return func(yield func(T) bool) {
		for _, k := range keys {
			if e, held := of[k]; held && !yield(e.elem) {
				return
			}
		}
	}
}

// noted files an element under its key, making the map on first use.
func (r *Index[T]) noted[K comparable](m map[K]*entryOf[T], k K, e *entryOf[T]) map[K]*entryOf[T] {
	if m == nil {
		m = make(map[K]*entryOf[T])
	}
	m[k] = e
	return m
}

// grouped files an element under its key beside the ones already there, making
// the map on first use.
func (r *Index[T]) grouped[K comparable](m map[K][]*entryOf[T], k K, e *entryOf[T]) map[K][]*entryOf[T] {
	if m == nil {
		m = make(map[K][]*entryOf[T])
	}
	m[k] = append(m[k], e)
	return m
}

// listed adds a primary key to a secondary bucket, making the map on first
// use.
func (r *Index[T]) listed[K comparable, P comparable](m map[K][]P, k K, p P) map[K][]P {
	if m == nil {
		m = make(map[K][]P)
	}
	m[k] = append(m[k], p)
	return m
}

// delisted takes one occurrence of a primary key out of a secondary bucket,
// and takes the bucket out of the map when that empties it.
//
// The last key moves into the hole rather than everything shuffling down, so
// the cost is the search and not the length — and the order inside a bucket
// is whatever additions and removals have made it, which the walks over one
// never promised was anything.
func (r *Index[T]) delisted[K comparable, P comparable](m map[K][]P, k K, p P) map[K][]P {
	held := m[k]
	for i, one := range held {
		if one != p {
			continue
		}

		last := len(held) - 1
		held[i] = held[last]
		held = held[:last]
		break
	}

	if len(held) == 0 {
		delete(m, k)
		return m
	}
	m[k] = held
	return m
}

// cut takes the element at one slot out of the walk order, moving the last
// element into the hole so nothing shuffles.
func (r *Index[T]) cut(at int) {
	last := len(r.order) - 1
	moved := r.order[last]

	r.order[at] = moved
	moved.at = at

	r.order[last] = nil
	r.order = r.order[:last]
}
