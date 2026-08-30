// Package load runs the one go/packages session everything downstream reads
// from.
//
// The load is the dominant cost of a run, so it happens once and every later
// stage works from its result. Two things about how it is done are load-bearing
// rather than incidental.
//
// # Function bodies are stripped
//
// Every function body is discarded at parse time, keeping signatures,
// positions and comments. A body-less function declaration is ordinary Go — it
// is how a function implemented in assembly is written — so the type-checker
// accepts it rather than reporting the missing return an emptied body would
// produce.
//
// This is what lets the generator bootstrap. Code that calls a generated
// method does so from inside a body, so a package whose generated file does
// not exist yet still loads and still yields complete type information for
// everything that matters: declarations, signatures and struct fields. Without
// it, forge could not read a package until forge had already run on it.
//
// The one error stripping introduces is an import that only the bodies used,
// which the type-checker then reports as unused. Those are dropped rather than
// reported, because they are an artefact of how forge reads the package and
// not a problem with it — and the compiler tells the author about a genuinely
// unused import every time they build anyway.
//
// # Spec files are in scope
//
// The session sets the forgespec build tag, so files guarded by it are part of
// the package and the declarations they carry are visible. The complementary
// //go:build !forgespec files, which is where forge writes the real
// declarations, are excluded — so exactly one declaration of a generated type
// is ever in scope, whether or not generation has run yet.
package load
