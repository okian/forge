// Package other holds a struct a subject reaches in a package of its own.
//
// It exists for one rule of the language: a method belongs to the package that
// declares its type, so nothing generated beside the subject can put one here.
// A check for this struct has to be a function in the package being generated
// into, and everything holding one has to call that function.
package other

import "errors"

// Place is reached from a subject declared somewhere else, and carries a rule
// of its own so that there is a check to write for it.
type Place struct {
	City string `validate:"required"`

	// unread is what generated code outside this package cannot name, so a
	// rule written on it is one that has to be enforced here.
	unread string `validate:"required"`
}

// Guarded checks itself, so that a struct in another package can be the one
// that decides what a valid one is.
type Guarded struct {
	Label string `validate:"required"`

	// made records that the value came from the constructor below, which is an
	// invariant nothing outside this package can see and nothing outside this
	// package could check.
	made bool
}

// Validate is the author's own, and enforces what the tags cannot reach.
func (g Guarded) Validate() error {
	if !g.made {
		return errUnmade
	}
	return nil
}

// errUnmade is what a Guarded that was never made through Guarding reports.
var errUnmade = errors.New("a guarded value has to be made here")

// Guarding returns a Guarded that satisfies its own check.
func Guarding(label string) Guarded { return Guarded{Label: label, made: true} }
