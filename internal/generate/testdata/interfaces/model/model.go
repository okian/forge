// Package model holds the subjects the interface pack generates over.
//
// Between them they earn every interface this build knows how to claim, which
// is what the pack is for: one place where the whole of that list is written
// down as output somebody can read, rather than a claim per test asserted in
// isolation.
//
// Two subjects rather than one, because the rows divide that way. A container's
// interfaces are about many values and are claimed on the declared type; a
// scalar wrapper's are about one value and are claimed on the subject, and a
// type cannot be both.
package model

// Person is what the container holds.
//
// It carries both tags these emitters read, so the pack exercises the two
// subject-level rows without needing a third subject for the second.
type Person struct {
	// ID is the declared sort key, which is what earns the container an order
	// of its own rather than only a sorted view.
	ID int

	// Name is displayed, unlabelled, so the rendering has a bare value in it.
	Name string `display:""`

	// Age is displayed with a label, so the rendering has one of those too.
	Age int `display:"age"`

	// Secret is kept out of logs, which is what earns the log value at all.
	Secret string `redact:""`
}

// Code is a scalar wrapper, which is the shape a text codec is written for.
//
// The tag is what asks for it. Without one the type reads and encodes as the
// struct it is, which is the right default: a codec written unasked would
// change how it appears in every JSON document it is part of.
type Code struct {
	Value string `display:""`
}
