// Package diag defines the diagnostics forge reports and how they are
// rendered.
//
// Errors are the product. A generator that fails with a stack trace is worse
// than no generator, so every failure here is a value with four properties: a
// stable identifier that survives rewording, the position of the declaration
// the author can actually fix, a message naming the specific thing that is
// wrong, and a one-line hint at the fix.
//
// A rendered diagnostic looks like this:
//
//	model/spec.go:12:6: FRG1003: two storage layers in stack (Ring, Heap)
//	  Collection[Ring[Heap[Person]]]
//	                  ^^^^
//	  hint: at most one Storage layer; mark Heap as Refining or drop Ring
//
// The position is always the declaration's, never a generated file's, because
// a generated file is not what the author edits.
//
// Identifiers are grouped into reserved ranges by the stage that reports them,
// so a code alone says roughly where to look: FRG1xxx composition, FRG2xxx the
// subject and its type model, FRG3xxx options, FRG4xxx emission and name
// collisions, FRG5xxx input, output and toolchain. Codes are registered at
// initialisation, and registering one twice panics rather than letting two
// failures answer to the same name.
package diag
