package jsoncodec

import (
	"fmt"
	"strings"
)

// writer assembles the source of a codec.
//
// Text rather than syntax. What is written here is a function with loops and
// branches in it, and a tree for one is many times its own size — the sentences
// this builds are what a person would write, and reading them back is how they
// are checked. What that costs is the possibility of assembling something that
// is not Go, which is why the assembled source is parsed before it leaves this
// package and a failure to parse is reported against the declaration rather
// than discovered in a file on disk.
type writer struct {
	out strings.Builder

	// refused lists the members whose omission could not be written, which is a
	// diagnostic rather than a silence: an author who asked for a member to be
	// left out and got it written anyway has a wire format that differs from
	// the one they described, and nothing in their own code says so.
	refused []member

	// asking holds the types whose emptiness is being decided, so that one
	// reached through itself is answered rather than expanded. A condition is a
	// finite piece of text and a type that contains itself has no finite one:
	// the value terminates at run time because a pointer is eventually nil, and
	// nothing about the type says where.
	//
	// Emptiness only. The zero question descends through members rather than
	// through pointers — a pointer is zero when it is nil, which is asked
	// without looking inside — so it has nothing to reach itself through.
	asking map[*form]bool

	// names are the identifiers the bodies bind, allocated out of the way of
	// the types they spell. See [locals].
	names locals
}

// newWriter returns a writer ready to assemble one codec.
func newWriter(names locals) *writer {
	return &writer{asking: make(map[*form]bool), names: names}
}

// line writes one line of the body. Indentation is left to gofmt, which the
// emitter runs over everything anyway, so that the assembly here reads as the
// sentences it is rather than as a column of tabs.
func (w *writer) line(format string, args ...any) {
	if len(args) == 0 {
		w.out.WriteString(format)
	} else {
		fmt.Fprintf(&w.out, format, args...)
	}
	w.out.WriteByte('\n')
}

// blank separates two declarations.
func (w *writer) blank() { w.out.WriteByte('\n') }

// checked writes a call whose error stops the function.
//
// Every write and every read is one of these, which is what makes the generated
// code's error handling uniform: the first failure is returned and nothing after
// it is attempted, because a JSON stream that has gone wrong cannot be written
// further into.
func (w *writer) checked(format string, args ...any) {
	w.line("if err := "+format+"; err != nil {", args...)
	w.line("return err")
	w.line("}")
}

// String returns the assembled source.
func (w *writer) String() string { return w.out.String() }
