// Package broken declares an instantiation cycle, which the compiler rejects
// and forge still has to read.
//
// A package is loaded before anything has been generated into it, so a package
// that does not build is the ordinary case here rather than the exceptional
// one, and every guarantee the walk makes has to hold on one.
package broken

// Recur instantiates itself with a different argument at every level, so it has
// no fixed point and no end.
type Recur[T any] struct {
	Next *Recur[[]T]
}

// Start reaches it.
type Start struct {
	Head Recur[int]
}
