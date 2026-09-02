// Package tally holds a marker of somebody else's and nothing forge ships.
//
// It is the third party's half of the arrangement: a marker declared in their
// own package, over which their own layer generates. Nothing here imports
// forge, which is the point — a marker is a phantom generic type and needs
// nothing from the tool that will claim it.
package tally

// Tally counts the elements of a container by one of the subject's fields.
type Tally[T any] struct{ _ [0]T }
