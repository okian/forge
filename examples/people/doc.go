// Package people is a worked example of a generated container.
//
// It holds one subject, [Person], and two declarations over it, each a single
// line. Every method on both types is generated from those lines and the
// directives above them. The hand-written half of the package is this file,
// person.go, spec.go and the tests beside them; everything else carries forge's
// header and is nobody's to edit.
//
// The point of reading it is to see what a declaration costs and what it buys.
// What it costs is one line and a commit of generated code. What it buys is a
// container that walks, projects, sorts and indexes its elements with the
// subject's own field names in the method names — [Persons.Names],
// [Persons.SortedByAge], [Persons.ByID] — rather than a package of helpers
// taking a func(Person) string at every call site.
//
// # The two declarations
//
// [Persons] is one layer written in an ordinary file, which is the common case
// and the one to read first. Its underlying type really is []Person, so a
// caller can index it, range over it, and pass it where a slice is wanted.
//
// [Recent] is five layers written in a build-tagged spec file, which is what a
// stack needs: its underlying type is forge's rather than the author's, so the
// declaration lives under one tag and the generated type under its complement.
// It is the composition the design exists for — a bounded ring of subjects that
// encodes in one pass over its elements and decodes straight back into the
// ring, with the document never existing as a slice in between.
//
// Three of those layers are about the subject rather than about the container,
// and none of them knows about the others: [Person] carries a codec, a check of
// the rules its tags declare, and a copy that shares nothing with what it was
// copied from. That is what an element layer is, and why two declarations over
// one subject get one of each rather than two.
//
// The generated files are committed, so building this package needs no tool
// installed. That is the arrangement forge is for: generation happens when the
// declaration changes, not when the code is built.
package people
