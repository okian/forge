package people

import "github.com/okian/forge"

// Person is somebody in a directory.
//
// An ordinary struct, written without regard for what will be generated from
// it. That is the whole of the contract a subject has: forge reads the fields
// that are there, and a subject is not required to know it is one.
//
// The validate tags are the exception that proves it: they are read by the
// generated check, and they are also the ordinary tags every validation library
// in Go reads. A field says what a valid one looks like, beside the field,
// where somebody changing it will see it.
type Person struct {
	// ID identifies the person, and is what the generated lookup is keyed by.
	ID int `validate:"min=1"`

	// Name is how the person is listed, and is the first declared sort key.
	//
	// The display tag is what asks for a String. It carries no name, so the
	// name is rendered on its own rather than labelled.
	Name string `validate:"required,max=64" display:""`

	// Email reaches them. It is projected like any other field; nothing about
	// a projection needs the field to be interesting.
	//
	// The pattern is deliberately the loose one: a check that insists on a
	// stricter shape than the world uses rejects real addresses, and the only
	// way to know an address works is to send to it.
	//
	// The redact tag keeps it out of logs, and is what asks for a LogValue at
	// all. Without one slog reaches for the fields of a Person and prints every
	// address it finds.
	Email string `validate:"required,regexp=^[^@[:space:]]+@[^@[:space:]]+$" redact:""`

	// Age is the second declared sort key, so that the example has a sorted
	// view over something that is not a string.
	//
	// Its display tag carries a name, so it is labelled where Name is not —
	// which is the difference between the two forms of the tag.
	Age int `validate:"min=0,max=150" display:"age"`

	// Aliases are the other names this person is listed under.
	//
	// It is the field that makes a copy worth having. Assigning a Person copies
	// this slice's header and not the array behind it, so two "copies" would
	// share their aliases and writing to one would change the other — which is
	// exactly the bug nothing in the assignment says anything about.
	Aliases []string
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
