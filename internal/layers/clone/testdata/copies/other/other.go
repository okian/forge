// Package other holds a struct a subject reaches in a package of its own.
//
// It exists for one rule of the language: a method belongs to the package that
// declares its type, so nothing generated beside the subject can put one here.
// A copy for this struct has to be a function in the package being generated
// into, and everything holding one has to call that function.
package other

// Place is reached from a subject declared somewhere else, and holds a
// reference an assignment would have shared rather than copied.
type Place struct {
	City  string
	Lines []string

	// unread is what generated code outside this package cannot name, so a copy
	// written there can do no more with it than the assignment already did —
	// which is what makes the copy for this type shallower than a copy usually
	// is, and what its comment has to say.
	unread []string
}
