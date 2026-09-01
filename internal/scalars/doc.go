// Package scalars writes what a subject earns from its own shape and tags,
// rather than from a marker anybody named.
//
// Everything else forge generates is asked for: a declaration names a stack and
// the layers in it produce methods. What is here is not named by anyone. A
// display tag says how a value should read, and the method that reads it that
// way is fmt.Stringer's; a struct wrapping one scalar is a type whose text form
// is the scalar's, and there is nothing to decide; a redact tag says a field is
// not for logs, and the only place that can be honoured is slog's.
//
// Each of them is a signal an author wrote for a purpose the standard library
// already has an interface for, so the interface is where it is answered. What
// this package does not do is claim those interfaces: it writes the methods,
// and whatever reads the declarations afterwards decides what they add up to.
//
// The receiver is the subject in every case, so what is written here is shared
// by every declaration over that subject and belongs to the package rather than
// to any one of them. That is why the contributions come back keyed: two
// declarations over one Person ask for the same String and the package needs
// one.
package scalars
