// Package shared holds the failures every generated check reports through.
//
// It is emitted rather than imported: generated code depends on the standard
// library and on nothing that has to be kept in step with the binary that wrote
// it, so what a package needs is written into it once however many declarations
// asked for it.
//
// So it is ordinary Go, compiled by the ordinary build and read by the ordinary
// vet, rather than a string this repository would only find out was wrong from
// somebody else's failed build. Nothing here is specialised to a subject, which
// is why it is a file rather than a template.
//
// The comments below are written for the file these end up in rather than for
// this one. They are the half of generated code a person actually reads, and a
// reader of somebody else's package has no interest in why forge keeps this
// here; that reasoning belongs in the layer's own documentation, which is where
// this sentence is.
package shared

import (
	"errors"
	"strconv"
	"strings"
)

// ValidationError is one rule a value did not satisfy.
//
// It carries where rather than only what. A form with a bad postcode is not a
// form that is invalid; it is a form whose Address.Postcode is too short, and
// the difference is whether the caller can put the message beside the field it
// is about.
type ValidationError struct {
	// Path reaches the field from the value that was checked: "Email", or
	// "Address.City" for a field inside another struct.
	Path string

	// Rule is the rule that was not met, written as it was written in the tag,
	// so that the failure and the source read alike. It is empty for a failure
	// that came from a check the author wrote themselves.
	Rule string

	// Want says what the rule wanted, in words, so that a message can be shown
	// to somebody who has never seen the tag.
	Want string

	// Cause is what a check the author wrote returned. It is nil for a failure
	// of a rule, which has nothing underneath it.
	Cause error
}

// Error describes the failure, naming the field first.
func (e ValidationError) Error() string {
	switch {
	case e.Cause != nil:
		return e.Path + ": " + e.Cause.Error()
	case e.Want != "":
		return e.Path + ": " + e.Rule + " wants " + e.Want
	default:
		return e.Path + ": " + e.Rule
	}
}

// Unwrap returns what a check the author wrote returned, so that errors.Is and
// errors.As reach it.
func (e ValidationError) Unwrap() error { return e.Cause }

// ValidationErrors is every rule a value did not satisfy, in the order the
// fields are declared and the rules are written.
//
// Every failure rather than the first, because a caller showing a form to
// somebody wants to show them everything that is wrong with it at once.
type ValidationErrors []ValidationError

// Error describes every failure, one per line where there is more than one.
//
// A line each rather than a sentence joined by commas: the failures are about
// different fields, and a reader scanning for their own field finds it faster
// down the left edge than in the middle of a paragraph.
func (e ValidationErrors) Error() string {
	switch len(e) {
	case 0:
		// Nothing failed, which is not a state a caller reaches through this:
		// a check that found nothing wrong returns no error at all rather than
		// an empty list of them.
		return "nothing failed"
	case 1:
		return e[0].Error()
	}

	var out strings.Builder
	out.WriteString(strconv.Itoa(len(e)))
	out.WriteString(" failures:")

	for _, one := range e {
		out.WriteString("\n\t")
		out.WriteString(one.Error())
	}
	return out.String()
}

// Unwrap returns the failures as errors, so that errors.Is and errors.As reach
// each of them.
func (e ValidationErrors) Unwrap() []error {
	out := make([]error, len(e))
	for i, one := range e {
		out[i] = one
	}
	return out
}

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
