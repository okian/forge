// Package asked holds a declaration forge writes for, and references to names
// only the file forge writes declares.
//
// It is the state a package is in the first time it is generated: the author
// wrote the declaration and the call sites together, and the call sites name
// things that do not exist yet. The load has to type-check before generation
// can run, so this is the run that refuses — and the reason it refused is the
// one thing the compiler's own message cannot say.
package asked

import "strings"

// Person is the subject.
type Person struct {
	Name string
}

// Persons is the declaration. Its underlying type stands in for the marker, so
// that this fixture is a module with no dependencies: what is being tested is
// what the loader does with a directive and a missing name, and neither of
// those needs the markers to be resolvable.
//
//forge:collection sort=Name
type Persons []Person

// Chain names a type only the generated file declares, in a signature.
//
// The case worth worrying about: PersonsSeq is forge's own name for the view
// over this collection, and returning one is the natural thing to write.
func Chain(p Persons) PersonsSeq { return PersonsSeq{} }

// Count calls a method only the generated file declares.
var Count = Persons(nil).Len()

// Typo misspells something in another package, which is not a name forge would
// ever write here and must not be explained as one.
//
// It is the case a suggestion offered on the strength of the error alone gets
// wrong: the message is the same shape as the two above, and the only thing
// that separates them is that this one names a package forge writes nothing
// for.
var Typo = strings.ToUppr
