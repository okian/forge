// Package patch generates the shape a partial update takes.
//
// What it writes is a companion type — a PersonPatch for a Person — with one
// field per exported field of the subject, each of them a pointer, and an Apply
// that writes the ones that are there over a value the caller already has:
//
//	var held Person
//	changes.Apply(&held)
//
// It exists for the request that means "change this and leave the rest alone".
// A handler given a whole Person cannot tell a field somebody cleared from a
// field they never mentioned, because both arrive as the zero value — so it
// either overwrites what it was not asked to or asks the caller to send
// everything back, and neither is what a partial update is.
//
// # Why a pointer per field
//
// A pointer is how Go says "there is something here" about a value that has a
// zero. A patch whose Name is nil was not asked to change the name; one whose
// Name points at the empty string was asked to clear it, and those are
// different instructions.
//
// A field that is already a pointer becomes a pointer to a pointer, which is as
// ugly as it sounds and is the honest spelling: the outer one says whether the
// patch mentions the field and the inner one is the value, which may itself be
// absent. There is no third state to collapse them into.
//
// # What Apply does, and what it does not
//
// It writes the fields the patch sets over the value it is given, one at a
// time, and leaves the rest as they were. It does not merge: a patch holding a
// slice replaces the slice rather than appending to it, and one holding a
// struct replaces the struct rather than patching inside it. A partial update
// of a nested value is a patch for that value, which is a declaration of its
// own.
//
// It does not check either. Whether what a patch holds is a value the subject's
// rules would accept is what a generated check says, asked of the value after
// the patch has been applied — which is the only point at which the whole value
// exists to be checked.
//
// And it does not copy. A field holding a slice, a map or a pointer leaves the
// value sharing it with whatever filled the patch in, which is what assignment
// does and what a caller who has read this far already expects; where it
// matters, the copy is the caller's to take.
//
// # IsZero
//
// A patch that sets nothing is one nobody asked for anything by, and IsZero is
// how a caller tells. It is also the name a codec looks for: a member tagged
// omitzero is left out when its value says it is zero, so a patch held as a
// field of something larger goes over the wire as nothing.
//
// A patch that is the whole document is another matter — there is no member for
// the tag to be on, so it is written out in full, absent members and all. That
// is the shape a request body usually has, so IsZero is more often a thing a
// handler asks than a thing a codec acts on.
//
// # What a patch can set
//
// The exported fields, and those alone. A patch is filled in from outside the
// package that declares the subject — that is what makes it a patch rather than
// an assignment — and an unexported field is not reachable from there, so one
// is left out and the generated type says so.
//
// # Reading a patch off the wire
//
// A patch is an ordinary struct of pointers, so encoding/json decodes into one
// and leaves an absent member nil.
//
// Its members are named as the subject's are. Each field carries the subject's
// own struct tag, so a document a caller received describing a value is a
// document they can send back describing a change to it — without that, a
// request written with the names the reply used would name nothing the patch
// recognised, decode into a patch that sets nothing, and change nothing at all
// without reporting anything.
//
// What it cannot yet tell is a member that arrived as null from one that did
// not arrive at all: both are nil afterwards. Saying which would need the codec
// and this layer to be written against each other, and they are not; a caller
// who needs the distinction reads the document itself.
package patch
