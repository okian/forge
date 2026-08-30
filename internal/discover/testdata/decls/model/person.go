// Package model holds the declarations discovery has to sort out.
package model

import "declsfixture/markers"

// Person is the subject.
type Person struct {
	Name string
	Age  int
}

// Box is a generic type of the author's own, unrelated to forge.
type Box[T any] []T

// People is an inline declaration: a candidate.
//
//forge:collection sort=Age index=Name
type People markers.Collection[Person]

// Aliased is an alias, so it keeps the methods it already has and asks for
// nothing. Not a candidate.
type Aliased = markers.Collection[Person]

// Plain is not an instantiation at all. Not a candidate.
type Plain []Person

// registry instantiates a generic type outside any type declaration, which
// discovery never looks at.
var registry = map[string]Box[int]{}

// Lookup has an instantiation in its signature, which is also not a
// declaration.
func Lookup(boxes Box[Person]) Box[Person] {
	return boxes
}
