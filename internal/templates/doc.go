// Package templates specialises a layer's method bodies to one subject.
//
// A layer's output is written as real Go, in a generic package that sits beside
// it and compiles:
//
//	type Collection[T any] []T
//
//	func (c Collection[T]) Len() int { return len(c) }
//
// and is rewritten into the declaration it was asked for — Collection becomes
// Persons, T becomes Person, and the type parameters go away because the result
// is not generic.
//
// Writing it as Go rather than as text is the whole point. A template package
// is compiled by the ordinary build, vetted by the ordinary vet, and read by an
// editor that can jump to its definitions; a mistake in one is a build failure
// where it was written rather than a syntax error in somebody's generated file.
// Twenty layers of text templates would be twenty bodies of Go that nothing
// checks until they are pasted together, which is the arrangement this project
// exists to avoid for its users and should not adopt for itself.
//
// What it cannot do is as important as what it can. A template is generic over
// its element, so it can hold anything that treats the element as opaque —
// storing it, counting it, handing it back. It cannot read the element's
// fields, because no type parameter can, so a layer that encodes a subject
// field by field builds its declarations rather than rewriting a template. The
// two ways of producing a unit sit side by side on purpose: this one is cheaper
// to write and to read, and is available whenever the layer's work does not
// depend on what the subject is made of.
package templates
