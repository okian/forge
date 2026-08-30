// Package markers stands in for a marker package of someone else's, so that a
// rule no marker forge ships can break — every one of them takes a single type
// argument — still has something to break it.
package markers

// Pipeline takes two type arguments, which no layer may.
type Pipeline[S, O any] []S

// Collection takes one, so a stack written against this package resolves like
// any other.
type Collection[T any] []T

// Opaque takes none. Nothing is applied to anything, so a declaration
// specialised to it names a subject that happens to live in the marker package
// rather than a second layer.
type Opaque struct {
	ID string
}
