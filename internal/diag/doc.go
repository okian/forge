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
//
// Within the composition range the number says one thing more: a code is
// numbered after the composition rule it enforces, so the rule that a stack
// holds at most one storage layer reports as FRG1003 and the rule that a layer
// takes exactly one type argument reports as FRG1007. The rules are numbered
// and finite, so their codes are spoken for before anything claims them, and a
// stage that enforces one rule can allocate its code without knowing which
// stage will enforce the rest. A composition failure that is not one of the
// numbered rules — a stack written in a form its layers cannot support — takes
// a number above them, so the correspondence stays exact where it holds.
package diag
