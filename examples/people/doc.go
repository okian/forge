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
// Two things in this package come out other than a reader would guess, and both
// are left as they come out rather than arranged around.
//
// [Persons.Aliases] is a projection of a field whose name was already plural,
// so the method and the field are spelled alike. It used to be Aliaseses, from
// three suffix rules that could not tell Aliases from Address; forge now knows
// the difference, and what it costs is that the column and the field read the
// same. They are on different types — the field is a [Person]'s and the method
// is a [Persons]' — so nothing is ambiguous to the compiler, and something is
// to a reader.
//
// It is also what makes two fields able to reach one projection. A subject with
// both Alias and Aliases would derive Aliases twice, and there is no spelling
// that separates them: doubling the inflection is the Aliaseses this change
// exists to stop writing. So the field spelled like the name keeps it, the
// other is projected as AliasValues, and forge reports the pair with both names
// and what each of them got. This package has only the one field, so the report
// is not in it; the case is written up here because it is the shape of edge a
// dictionary trades three wrong names for.
//
// [Persons] is the second, and it is the author's own doing rather than the
// tool's. A subject called Person now pluralises to People wherever forge
// derives a name, so a reader might expect the declaration to be called People
// too. It is not, because this package is called people and people.People is
// the stutter every naming guide in Go warns about — the declaration's name is
// one thing forge never derives, and this is why.
//
// An example is worth reading for what a tool really does, and a package shaped
// to avoid its own edges would be an example of a tool that does not exist.
//
// The generated files are committed, so building this package needs no tool
// installed. That is the arrangement forge is for: generation happens when the
// declaration changes, not when the code is built.
//
// There are two of them and there is one of them, depending on how you count.
// [Persons] is written inline and the other four in spec form, so the whole of
// what forge wrote for this package goes in forge.gen.go under //go:build
// !forgespec, and forge_stubs.gen.go stands in for it under the tag the spec
// file is written behind. Exactly one is in any build.
package people
