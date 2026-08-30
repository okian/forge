package diag

import (
	"errors"
	"fmt"
	"go/token"
	"strings"
)

// indent is the margin the lines beneath a diagnostic's first line are printed
// at, so that a multi-line diagnostic reads as one block.
const indent = "  "

// Diagnostic is one problem with one declaration.
//
// It renders as up to four lines: the position, code and message; the
// declaration's stack; a caret underlining the part of that stack the message
// is about; and a hint. Only the first is required, and a diagnostic that has
// nothing useful to underline simply omits the middle two.
//
//	model/spec.go:12:6: FRG1003: two storage layers in stack (Ring, Heap)
//	  Collection[Ring[Heap[Person]]]
//	                  ^^^^
//	  hint: at most one Storage layer; mark Heap as Refining or drop Ring
type Diagnostic struct {
	// Code identifies the failure and never changes once published.
	Code Code

	// Pos is the position of the declaration at fault. It is never the position
	// of a generated file, because a generated file is not what the author
	// edits.
	Pos token.Position

	// Message states what is wrong, naming the specific types, layers or
	// options involved. It begins in lower case and carries no terminating
	// punctuation.
	Message string

	// Stack is the declaration's layer stack rendered on one line. It is empty
	// for a diagnostic about something other than a stack.
	Stack string

	// Caret is a line of spaces and carets that underlines the part of Stack
	// the message is about when printed directly beneath it. It is ignored when
	// Stack is empty.
	Caret string

	// Hint is one line naming the fix. Every diagnostic that can suggest one
	// should: the identifier says what broke, and the hint says what to do.
	Hint string
}

// New returns a diagnostic for code at pos, with the message built from format
// and args.
//
// It panics if the code falls outside every reserved range, for the same
// reason [Register] does: a code that places itself nowhere is a programming
// error, and one that reaches a user has already failed at its only job.
func New(code Code, pos token.Position, format string, args ...any) Diagnostic {
	if code.Category() == CategoryInvalid {
		panic(fmt.Sprintf("diag: code %d is outside the reserved ranges %d-%d", int(code), int(minCode), int(maxCode)))
	}
	return Diagnostic{
		Code:    code,
		Pos:     pos,
		Message: fmt.Sprintf(format, args...),
	}
}

// WithStack returns a copy of the diagnostic that prints the declaration's
// stack beneath the message, with caret underlining the offending part of it.
// Pass an empty caret to print the stack without underlining anything.
func (d Diagnostic) WithStack(stack, caret string) Diagnostic {
	d.Stack = stack
	d.Caret = caret
	return d
}

// WithHint returns a copy of the diagnostic carrying a one-line suggestion,
// built from format and args.
func (d Diagnostic) WithHint(format string, args ...any) Diagnostic {
	d.Hint = fmt.Sprintf(format, args...)
	return d
}

// Error returns the diagnostic's first line, which is the form to use where a
// single line is wanted: wrapped in another error, or logged.
//
// Implementing error is what lets a diagnostic travel through the ordinary Go
// error returns that layers use, to be recovered later with [From].
func (d Diagnostic) Error() string {
	return d.Pos.String() + ": " + d.Code.String() + ": " + d.Message
}

// Render returns the diagnostic in full, without a trailing newline.
func (d Diagnostic) Render() string {
	var b strings.Builder
	b.WriteString(d.Error())

	if d.Stack != "" {
		b.WriteString("\n")
		b.WriteString(indent)
		b.WriteString(d.Stack)

		if d.Caret != "" {
			b.WriteString("\n")
			b.WriteString(indent)
			b.WriteString(d.Caret)
		}
	}

	if d.Hint != "" {
		b.WriteString("\n")
		b.WriteString(indent)
		b.WriteString("hint: ")
		b.WriteString(d.Hint)
	}

	return b.String()
}

// From returns the diagnostic err carries, and whether it carries one. Layers
// report failures as errors so that they compose with ordinary Go error
// handling; this is how the pipeline gets the diagnostic back out.
//
// Diagnostics are meant to travel by value, but [Diagnostic.Error] has a value
// receiver, so a pointer to one satisfies error just as well. Both are
// accepted: dropping a diagnostic because of how it was returned would be a
// silent failure of the thing this package exists to prevent.
func From(err error) (Diagnostic, bool) {
	if d, ok := errors.AsType[Diagnostic](err); ok {
		return d, true
	}
	if d, ok := errors.AsType[*Diagnostic](err); ok && d != nil {
		return *d, true
	}
	return Diagnostic{}, false
}
