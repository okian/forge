// Package slices holds a subject whose package name a layer has already taken.
//
// The collection layer imports the standard library's slices, so a subject from
// here has to be written under some other name in the file generated for it —
// which is the case where the way a claim is written and the way a method is
// read can come apart.
package slices

// Person is the subject, in a package named after one the standard library has.
type Person struct {
	Name string
}
