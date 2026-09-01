// Package cmp holds a subject in a package named after one that some layers
// import and others do not.
//
// The sibling package named slices is the easy half of this, for the stack
// these two are used with: a collection over a slice binds the standard
// library's slices in both of its layers, so both move a subject from a package
// of that name out of the way and both move it to the same place. This one is
// the hard half. The collection imports the standard library's cmp and the
// storage beneath it does not, so what to call this package has two answers
// depending on which layer is asked — and both land in one file.
package cmp

// Person is the subject, in a package only some of a stack has heard of.
type Person struct {
	Name string
}

// Place is a second subject here, so that two declarations can name one foreign
// package between them.
//
// The file a package's stand-ins go into is written from every spec declaration
// at once, so two of them spelling this package differently meet there however
// consistent each was on its own. One subject cannot show that.
//
// And it has to be this package rather than the sibling called slices. Every
// stack binds slices somewhere — the default storage does, so every package
// forge writes for reserves it — which means two declarations would agree about
// that name whatever decided it, and a test over it would pass without
// discriminating anything.
type Place struct {
	Name string
}
