// Package plain holds the same missing name and asks forge for nothing.
//
// It is the control. The declarations here are the ones next door with the
// directive taken off them — the same names, reached the same two ways,
// producing the same two errors — so a hint about generation arriving here
// would be a hint arriving on the strength of the error alone, which is a
// suggestion to generate offered to somebody who misspelt a name.
package plain

// Person is the subject nothing is declared over.
type Person struct {
	Name string
}

// Persons is an ordinary slice, with no directive above it.
type Persons []Person

// Chain names a type nobody declares, in a signature.
func Chain(p Persons) PersonsSeq { return PersonsSeq{} }

// Count calls a method nobody declares.
var Count = Persons(nil).Len()
