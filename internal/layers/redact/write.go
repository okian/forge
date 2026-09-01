package redact

import (
	"fmt"
	"strings"

	"github.com/okian/forge/internal/emit"
	"github.com/okian/forge/internal/scalars"
)

// writer assembles one type's log value as source.
//
// Text rather than syntax, for the reason the other layers' writers give: what
// is assembled is a method with a line per field, which is many times its own
// size as a tree. The cost is the possibility of writing something that is not
// Go, and it is paid where the layer can still be stopped — the source is
// parsed before it leaves the package.
type writer struct{ out strings.Builder }

// String returns what has been written.
func (w *writer) String() string { return w.out.String() }

// line writes one line. Indentation is left to gofmt, which the emitter runs
// over everything anyway.
func (w *writer) line(format string, args ...any) {
	if len(args) == 0 {
		w.out.WriteString(format)
	} else {
		fmt.Fprintf(&w.out, format, args...)
	}
	w.out.WriteByte('\n')
}

// wrapped writes a sentence over however many comment lines it takes, so that a
// long one does not run off the side of a file the rest of which is wrapped.
func (w *writer) wrapped(text string) {
	for _, held := range emit.Wrapped(text, emit.CommentWidth) {
		w.line("// %s", held)
	}
}

// value writes the whole of one type's log value.
func (w *writer) value(held *plan) {
	w.doc(held)

	w.line("func (v %s) %s() slog.Value {", held.spelled.Text, method)

	for _, field := range held.fields {
		if field.guarded {
			w.guard(field)
		}
	}

	w.line("\treturn slog.GroupValue(")

	for _, field := range held.fields {
		w.line("\t\t%s,", attr(field))
	}

	w.line("\t)")
	w.line("}")
}

// guard works out a pointer field's value before the attribute list, so that
// one which is not there is logged as nothing rather than as a stack trace.
//
// slog resolves a pointer by calling the method on it, and the method is
// written with a value receiver — it has to be, or a plain value of the type
// would not implement the interface and slog would print its fields. Reached
// through nothing, that call panics; slog recovers it and logs the trace where
// the field should have been.
//
// A statement rather than an expression because there is no way to write this
// as one, and a local rather than a helper beside the type because a local is
// read where it is used and cannot collide with anything the package declares.
func (w *writer) guard(field logged) {
	w.line("\t%s := slog.AnyValue(nil)", local(field))
	w.line("\tif v.%s != nil {", field.field.Name)
	w.line("\t\t%s = v.%s.%s()", local(field), field.field.Name, method)
	w.line("\t}")
	w.line("")
}

// local names the variable one field's value is worked out into.
//
// The field's own name, which is unique among the fields of a struct and so
// among the locals here, with a suffix that keeps it from being the receiver or
// a package a spelling bound. Lower-cased because it is a local, and because an
// exported name here would read as something a caller could reach.
func local(field logged) string {
	held := field.field.Name
	return strings.ToLower(held[:1]) + held[1:] + "Logged"
}

// attr writes what one field contributes.
//
// The masking and the rendering both come from the emitters that write the log
// value a tag earns on its own. A field is redacted here exactly as it is
// redacted there — the same fixed string — and one that is not is rendered
// through the same table, so a package holding both writes one kind of
// attribute rather than two that differ by which of them got there first.
func attr(field logged) string {
	if field.masked {
		return scalars.Masked(field.field.Name)
	}
	if field.guarded {
		// Already a slog.Value, so it goes in as one. Through the ordinary
		// table it would reach slog.Any, which takes an interface and boxes it
		// for AnyValue to unwrap — an allocation per guarded field per record,
		// on a path a log line runs every time.
		return scalars.Held(field.field.Name, local(field))
	}
	return scalars.Attr(field.field.Name, "v."+field.field.Name, field.field.Type)
}

// doc writes the comment the method carries.
//
// It says what was done and why rather than that a generator did it, because
// the reader who needs it is somebody deciding whether a value is safe to log —
// and a comment saying "generated" tells them nothing the file name does not.
func (w *writer) doc(held *plan) {
	w.wrapped(method + " returns " + held.spelled.Text + " as it may be logged.")
	w.line("//")

	if n := masked(held); n > 0 {
		w.wrapped("Every exported field, with " + fields(n) + " replaced by a fixed " +
			"string. Fixed rather than shortened or starred: a length is something, " +
			"a prefix is more, and two records holding one secret are told apart by " +
			"any hash of it.")
		w.line("//")
	}

	w.wrapped("Implementing this is what keeps a field out of a log. slog reaches " +
		"for a value's fields when the value does not say otherwise, so a type with " +
		"a secret in it and no " + method + " prints the secret. A handler can be " +
		"given a hook that rewrites attributes, but that is a property of the " +
		"handler rather than of the type, and every place the value is logged from " +
		"has to have installed it.")
}

// masked counts the fields this value replaces.
func masked(held *plan) int {
	n := 0
	for _, field := range held.fields {
		if field.masked {
			n++
		}
	}
	return n
}

// fields names the masked half of the value, in the number there is.
//
// Two wordings rather than one with a count in it, because the singular is what
// almost every subject has and "1 field" in a doc comment reads as something
// nobody wrote.
func fields(n int) string {
	if n == 1 {
		return "the one tagged " + tag
	}
	return "the ones tagged " + tag
}
