// Package other holds the structs a subject reaches in a package of its own.
//
// Two rules of the language meet here. A method belongs to the package that
// declares its type, so nothing generated beside the subject can put one on
// these; and an unexported field is unreadable outside the package that
// declares it, so a hash written elsewhere could not cover the whole of a value
// that has one.
package other

// Place is reached from a subject declared somewhere else, and every field of
// it can be read from there.
type Place struct {
	City  string
	Lines []string
}

// Sealed keeps part of itself to itself, so that nothing written elsewhere
// could hash the whole of one.
type Sealed struct {
	Label  string
	hidden int
}

// Sealing returns a Sealed holding the given unreachable value, so that a test
// has one to hold.
func Sealing(label string, hidden int) Sealed { return Sealed{Label: label, hidden: hidden} }

// Holder keeps nothing to itself at its own level and holds a struct written in
// place that does, so that what decides is what is beneath rather than what is
// on top.
type Holder struct {
	At struct {
		Line   int
		hidden int
	}
}
