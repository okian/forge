// Package model is a package whose author deleted a declaration and left the
// stubs a previous run wrote for it behind.
//
// It is the state the recovery hint exists for. The stubs name a type nothing
// declares any more, so the package stops type-checking; the type-check stops
// generation; and generation is the only thing that would have rewritten the
// stubs. Nothing in the compiler's message says any of that.
package model

// Person is the subject the author kept.
type Person struct {
	Name string
}
