// Package slice is the storage layer that keeps elements in an append-ordered
// backing array.
//
// It is the storage a declaration gets when it names none: a refining layer
// written over a subject alone resolves as though this had been written beneath
// it, so Collection[Person] is Collection[Slice[Person]] and the underlying
// type of the declaration really is []Person.
//
// That last part is the whole of why this layer is transparent. A ring's head
// index, a set's deduplication and a lock's exclusion are invariants a raw
// write through the underlying type would break, so a declaration containing
// one belongs in a spec file where the representation is opaque. A slice has no
// such invariant: any []Person is a valid one, and there is nothing an author
// can do to the underlying type that this layer would have to repair. So a
// declaration over this storage may be written in an ordinary file, which is
// the form the documentation leads with and the one most declarations use.
//
// The surface is the source and sink half of the streaming contract — All,
// Backward, Len and AppendSeq — plus a constructor. Everything above it in a
// stack is written against those four, which is what lets a refining layer be
// written once rather than once per storage.
package slice
