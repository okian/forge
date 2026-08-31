// Package people is a worked example of a generated container.
//
// It holds one subject, [Person], and one declaration over it, [Persons]. That
// declaration is a single line, and every method on the type below is generated
// from it and the directive above it. The hand-written half of the package is
// this file, person.go and the tests beside them; everything else carries
// forge's header and is nobody's to edit.
//
// The point of reading it is to see what a declaration costs and what it buys.
// What it costs is one line and a commit of generated code. What it buys is a
// container that walks, projects, sorts and indexes its elements with the
// subject's own field names in the method names — [Persons.Names],
// [Persons.SortedByAge], [Persons.ByID] — rather than a package of helpers
// taking a func(Person) string at every call site.
//
// The generated files are committed, so building this package needs no tool
// installed. That is the arrangement forge is for: generation happens when the
// declaration changes, not when the code is built.
package people
