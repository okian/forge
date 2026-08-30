// Package markers stands in for the real marker package, so that this fixture
// depends on nothing outside itself.
package markers

// Collection refines the storage beneath it.
type Collection[T any] []T

// Ring stores elements in a fixed-capacity circular buffer.
type Ring[T any] []T

// Json attaches a codec to the subject.
type Json[T any] struct{ _ [0]T }
