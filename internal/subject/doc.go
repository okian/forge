// Package subject builds the model of the type a stack is specialised to.
//
// Resolution hands over whatever was written innermost in a declaration. This
// stage turns that into the description everything downstream reads: the
// fields, their classified types, their parsed tags, and every struct reachable
// from them that will need generated code of its own.
//
// Three things about it are worth stating before the code says them.
//
// The reachable set is the point. A codec for a struct is not a codec for one
// struct: a field whose type is another struct needs one too, and so does a
// field of that one. Walking that closure once, here, is what lets generation
// emit each of them exactly once no matter how many declarations reach them —
// and what lets a layer refuse a type it cannot generate for before it has
// written half a file. The walk is the reason this stage exists at all.
//
// A type that reaches itself is not an error, and pretending otherwise would
// rule out the linked list and the tree. It is recorded, because a layer that
// walks the closure without expecting one will not come back.
//
// What it will not build is a subject out of anything but one concrete named
// type. A pointer, a predeclared type and an unnamed composite each fail for
// their own reason; so do a generic declaration and an instantiation still
// holding a type parameter, which are named types and are not concrete ones —
// their fields have no shape yet, and a model of them would describe nothing.
// Each refusal gets the fix for that shape rather than one that covers them all
// and helps with none, and each points at the declaration rather than at the
// type: the type is very often somebody else's, in somebody else's file, and
// the line the author can edit is the one that named it.
package subject
