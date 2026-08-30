// Package goldentest holds generated output to what it claimed to be.
//
// A golden file is a recorded copy of what generation produced, checked in
// beside the test that produced it. Comparing against one turns a change in
// output into a diff in a review, which is the only way a person ever notices
// that a layer started emitting something subtly different — output that
// compiles is not output that is right, and nobody reads a generated file
// looking for surprises.
//
// A recorded copy on its own is a weak claim, though: it says the output has
// not changed, not that it was ever correct. So every golden is also compiled.
// The package it belongs to is type-checked in memory, against the fixture it
// was generated for, and then put through the analyses that catch what
// compiling does not. A golden that does not compile fails, however faithfully
// it matches what was recorded last time.
//
// Imports are resolved from source rather than from a compiler's export data,
// so the gate works on a machine that has never built anything. What may be
// imported is decided separately and before anything is read: the part of the
// standard library a package outside GOROOT can import, and nothing else.
// Generated code makes that promise — it is why there is no runtime package for
// a generated file to version-skew against — and a promise a suite does not
// check is a comment.
//
// One thing it does not check: a recorded copy of output nothing generates any
// more. Nothing compares against it again, so it sits in the tree looking like
// evidence. Noticing would mean knowing the whole of what a test produced, and
// a test may hold several packages and call [Check] for each — no one call
// knows about the others, and a check that guessed would fail a test that
// cannot be made to pass.
package goldentest
