package people

import "github.com/okian/forge"

// Person is somebody in a directory.
//
// An ordinary struct, written without regard for what will be generated from
// it. That is the whole of the contract a subject has: forge reads the fields
// that are there, and a subject is not required to know it is one.
type Person struct {
	// ID identifies the person, and is what the generated lookup is keyed by.
	ID int

	// Name is how the person is listed, and is the first declared sort key.
	Name string

	// Email reaches them. It is projected like any other field; nothing about
	// a projection needs the field to be interesting.
	Email string

	// Age is the second declared sort key, so that the example has a sorted
	// view over something that is not a string.
	Age int
}

// Persons is a directory of people.
//
// The declaration names one layer over one subject. sort= asks for a sorted
// view per key and index= for a lookup map per key, and both are spelled with
// the field's own name in the generated method — SortedByName, ByID — because
// the generator knows the fields rather than taking them as arguments.
//
//forge:collection sort=Name,Age index=ID
type Persons forge.Collection[Person]
