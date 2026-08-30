// Package layer defines what a layer is and holds the ones forge knows about.
//
// A layer is the unit forge is extensible in. It claims one marker, declares
// what kind it is and what options it takes, says whether it can sit on the
// shape beneath it, reports the shape it exposes upward, and generates. Nothing
// else in the pipeline knows which layers exist: resolution finds markers,
// composition asks about shapes, and generation asks for units, so a layer
// nobody has written yet costs the rest of the code nothing.
//
// That is why this interface is worth being careful about now, while there is
// still one implementation of it. Publishing it is what freezes it, and the
// pieces most likely to be regretted are the ones a first implementation does
// not exercise: a unit returns declarations rather than methods on the declared
// type, because an element layer's receiver is the subject and not the
// container above it, and a shape carries the method surface rather than only
// capability bits, because a decorator cannot wrap what it cannot enumerate.
// Both are cheap now and expensive after the first release.
//
// Every layer forge ships is registered here as a stub: the right marker, the
// right kind, the right options, and a generator that reports it has not been
// written. That is not a placeholder for its own sake. It is what lets the
// stages that reason *about* layers — resolution, composition, explain, list —
// be built and tested before any of them can emit a line of code, and what
// makes each real layer a change to one file rather than to the pipeline.
package layer
