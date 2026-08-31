// Package layer defines what a layer is.
//
// A layer is the unit forge is extensible in. It claims one marker, declares
// what kind it is and what options it takes, says whether it can sit on the
// shape beneath it, reports the shape it exposes upward, and generates. Nothing
// else in the pipeline knows which layers exist: resolution finds markers,
// composition asks about shapes, and generation asks for units, so a layer
// nobody has written yet costs the rest of the code nothing.
//
// That is why this interface is worth being careful about now, while there are
// still few implementations of it. Publishing it is what freezes it, and the
// pieces most likely to be regretted are the ones a first implementation does
// not exercise: a unit returns declarations rather than methods on the declared
// type, because an element layer's receiver is the subject and not the
// container above it; a shape carries the method surface rather than only
// capability bits, because a decorator cannot wrap what it cannot enumerate;
// and a unit's imports carry the names they are bound to, because the bodies
// call a package by the name the file binds and nothing downstream can work
// that name out again. All three are cheap now and expensive after a release.
//
// The last of those is the one with a cost worth naming: it brings the
// emitter's own import type into the surface a layer is written against,
// alongside the syntax trees a unit already carries. That is a wider vocabulary
// than "a list of paths" — and the narrower one cannot express a file that
// imports two packages of one name, which is not a corner case but what happens
// the first time somebody's subject lives in a package called slices.
//
// Which layers forge itself ships is deliberately not answered here. This
// package is the vocabulary a layer is written in, including one written
// outside forge, and a catalog of forge's own would make it depend on them —
// so the catalog and the layers live beside it rather than in it.
package layer
