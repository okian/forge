// Package people is a worked example of a generated container.
//
// It holds three subjects — [Person], [Status] and [Credential] — and five
// declarations over them, each a single line. Every method on the declared
// types is generated from those lines and the directives above them. The
// hand-written half of the package is this file, person.go, credential.go,
// spec.go and the tests beside them; everything else carries forge's header and
// is nobody's to edit.
//
// The point of reading it is to see what a declaration costs and what it buys.
// What it costs is one line and a commit of generated code. What it buys is a
// container that walks, projects, sorts and indexes its elements with the
// subject's own field names in the method names — [Persons.Names],
// [Persons.SortedByAge], [Persons.ByID] — rather than a package of helpers
// taking a func(Person) string at every call site.
//
// # The declarations
//
// [Persons] is one layer written in an ordinary file, which is the common case
// and the one to read first. Its underlying type really is []Person, so a
// caller can index it, range over it, and pass it where a slice is wanted.
//
// [Recent] is eight layers written in a build-tagged spec file, which is what a
// stack needs: its underlying type is forge's rather than the author's, so the
// declaration lives under one tag and the generated type under its complement.
// It is the composition the design exists for — a bounded ring of subjects that
// encodes in one pass over its elements and decodes straight back into the
// ring, with the document never existing as a slice in between.
//
// Six of those layers are about the subject rather than about the container,
// and none of them knows about the others: [Person] carries a codec, a check of
// the rules its tags declare, a copy that shares nothing with what it was
// copied from, a content hash, a builder and a patch. That is what an element
// layer is, and why several declarations over one subject get one of each
// rather than one apiece.
//
// [Roster] is a smaller ring behind a mutex, which is the decorator kind. It
// takes away more than it adds, and what it adds is the safe way to reach what
// it took: the ring is reachable only inside [Roster.Do] and [Roster.RDo], so
// the six methods that reached it directly — All and Backward and Push among
// them — are withdrawn rather than wrapped, and [Roster.Snapshot] is what a
// caller gets instead. A decorator that wrapped them would be a lock
// somebody holds across a loop body they did not write.
//
// [Statuses] and [Credentials] are declarations over the other two subjects,
// and they are here because two of the layers have nothing to demonstrate over
// a Person. A closed set needs a named scalar to be closed over, and redaction
// is only worth a layer when the secret is a struct down from the value being
// logged. Read them after the three above.
//
// # What is not smooth
//
// Three things in this package come out other than a reader would guess, and
// all three are left as they come out rather than arranged around.
//
// The first is a name. [Persons.Aliaseses] is a projection named by pluralising
// a field name that was already plural.
//
// The second and third are one composition and its consequence. Forge writes
// its own codec for [Credential] and does not reach for the text codec on
// [Status] when it does, so [Credential.State] goes over the wire as a number
// and a number no member stands for is read back without complaint. What
// follows from that is the third: such a credential logs an error where its
// state should be, in every line, because the log value asks the closed set to
// write a member it has no name for.
//
// Each is written up where it happens, on [Persons] and on [Statuses], and each
// is held down by a test that fails if it stops being true. An example is worth
// reading for what a tool really does, and a package shaped to avoid its own
// edges would be an example of a tool that does not exist.
//
// The generated files are committed, so building this package needs no tool
// installed. That is the arrangement forge is for: generation happens when the
// declaration changes, not when the code is built.
package people
