// Package discover finds the type declarations that might be generation
// requests.
//
// The search is deliberately syntactic. A declaration qualifies if it is a
// defined type — not an alias — whose right-hand side instantiates something:
//
//	type Persons Collection[Ring[Json[Person]]]
//
// It has to be syntactic, because go/types does not keep the instantiation
// around. For a defined type the underlying type is all that survives, so
// Persons is recorded as []Ring[Json[Person]] and the Collection that wrote it
// is nowhere in the type graph. The only place the declaration still exists as
// the author wrote it is the syntax tree.
//
// Being syntactic also means this stage cannot tell a generation request from
// any other instantiation: type Numbers Box[int], over a generic type of the
// author's own, looks exactly the same. Sorting that out is resolution's job,
// which follows the instantiation to its origin and quietly drops the ones no
// layer claims. What this stage produces is candidates, not requests.
//
// Alongside each candidate it collects two things nothing downstream can
// recover on its own: the //forge: directives written above the declaration,
// which carry its options, and whether the file it lives in is a spec file,
// which decides whether forge adds methods to the author's type or owns the
// declaration outright.
package discover
