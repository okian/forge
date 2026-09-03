// Package jsonwire is the shared JSON wire runtime: the half of a generated
// codec that does not know what it is encoding, written once and emitted once
// per package.
//
// It is not a layer. Nothing declares it, no marker claims it, and no
// declaration is generated for it — the codec layer names it among what it
// requires, and the stage that assembles a package's output emits it once for
// however many subjects asked. That is what the requiring is for: twenty
// subjects over one package share one escaper rather than carrying twenty.
//
// The division it sits on is between what writing a value needs to know about
// the value and what it does not. Escaping a string, choosing the notation a
// float goes on the wire in, holding a number to JSON's grammar rather than
// Go's, finding where a string ends without being fooled by an escape inside
// it, and stepping over a member nobody reads: none of that depends on which
// subject asked. Which members exist, what they are called, what order they
// come in and which rule each is held to cannot be written without the
// subject, and are generated against it.
//
// The bodies live in the package beside this one, as compiling Go, for the same
// reason a layer's do: a mistake in them is a build failure where they were
// written rather than a syntax error in somebody's generated file. They are
// emitted unchanged, since nothing in them varies by declaration.
//
// What the tests beside those bodies do is the reason to trust them. Every
// helper that writes bytes is held to what encoding/json/v2 writes and every
// helper that reads them to what it accepts, by differential fuzzing rather
// than by a table of what somebody believed: the escaper cleared twenty-two
// million executions against jsontext.AppendQuote, the float writer
// twenty-five million against jsontext.AppendFloat, and the string reader
// twenty-six million against json.Unmarshal, with no disagreement in any of
// them. The emitted code imports neither of those packages — a codec should not
// carry a coder it never uses — so the tests are where that dependency is
// allowed to live, and they are what keeps the reimplementation honest.
package jsonwire
