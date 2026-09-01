// nested.go holds the one thing a generated check needs beyond the failures
// every layer reports through.
//
// Beside them rather than among them, because it is the check's alone: a
// package that has a builder and no check would otherwise be written a function
// nothing in it calls. What is emitted is chosen a file at a time.

package shared

import "errors"

// nestedValidation folds what a field's own check reported into the failures of
// the value that holds it, under the path that reaches the field.
//
// The path is what makes a nested check worth having. A City that is too short
// is reported by Address as "City", and the value holding the address has to
// say "Address.City" or a caller has no way to know which of two addresses it
// was.
//
// An error that is not a list of failures is carried whole, under the field's
// own path. That is what a check the author wrote returns, and what a type from
// somewhere else returns, and neither is this package's to take apart.
//
// Found through the chain rather than in the hand, so that failures wrapped by
// a check of somebody's own still reach the caller as failures with paths. A
// wrapper that meant to hide them can say so by not wrapping them.
func nestedValidation(into ValidationErrors, at string, err error) ValidationErrors {
	var held ValidationErrors
	if !errors.As(err, &held) {
		return append(into, ValidationError{Path: at, Cause: err})
	}

	for _, one := range held {
		one.Path = at + "." + one.Path
		into = append(into, one)
	}
	return into
}
