// Package clone generates a copy of a value that shares nothing with it.
//
// What it writes is Clone() on the subject, built from the fields the subject
// declares: a copy that a caller can change without changing what they copied
// it from. Everything reachable is copied — the elements of a slice, the values
// of a map, what a pointer points at, and so on down — so the two values have
// no memory in common and nothing done to one is visible in the other.
//
// A copy is where a whole class of bug lives, and the bug is always the same
// shape: the copy was shallower than somebody thought. A struct assigned to a
// variable is copied, and its slice header is copied with it — pointing at the
// same array. So `b := a` and then `b.Tags[0] = "x"` changes a.Tags too, and
// nothing in either line says so. That is what this exists to write out.
//
// # What is copied and what is shared
//
// Everything the type can be seen through is copied: a pointer is allocated
// again and what it points at is copied, a slice and a map are built again and
// what they hold is copied, an array's elements are copied one at a time. A
// number, a string and a boolean are copied by being assigned, which is what
// assigning them does.
//
// A type that declares Clone of its own is asked to copy itself, which is how a
// hand-written copy stays authoritative — and how a type whose invariants
// forge cannot see is copied properly rather than field by field.
//
// A struct from another module is copied as a value and no further. Generated
// code cannot read its unexported fields, so it cannot copy what they hold; a
// time.Time copied this way shares the location it points at, which is correct
// because a location is immutable, and a type for which it is not correct can
// say so by declaring Clone itself.
//
// # aliasing
//
//	//forge:clone aliasing=share
//
// The one option, and it is a decision rather than a knob. Written on the
// declaration it makes every pointer, slice and map be carried across rather
// than copied — a shallow copy, which is what a caller wants when what they
// hold is read-only and copying it would be waste. Written above a field it
// says the same of that field alone.
//
// The default is to copy. A copy that quietly shared would be worse than no
// copy at all, because it would look like the thing it is not.
//
// # What cannot be copied without being told
//
// An interface, a channel and a function are refused rather than shared
// silently. Nothing can copy what it cannot see the type of: an interface holds
// something decided at run time, and a channel and a function are references to
// things a copy has no meaning for. Each is a diagnostic naming the field, and
// the way out is to say what was meant —
//
//	//forge:clone aliasing=share
//
// above the field, which carries it across as it is and says so where a reader
// would ask.
//
// # A value that contains itself
//
// The generator terminates whatever the types do, because a struct is copied by
// calling its own method rather than by inlining what it holds: a type that
// reaches itself produces a method that calls itself, which is a finite amount
// of code.
//
// The *value* is another matter. A copy follows what is there, so a list, a
// tree and a graph with no cycle in it are all copied in full — and a value
// that really does contain itself would be followed for ever. Tracking what had
// already been seen would cost an allocation on every copy of every value, to
// bound a case most programs do not have; the way to bound it where it does
// arise is aliasing=share on the field that closes the loop, which is also the
// only place anybody can say which of the two values the copy should point at.
package clone
