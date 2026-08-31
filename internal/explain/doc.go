// Package explain says what a stack is: the one somebody wrote, or the whole
// catalogue of what they could have written.
//
// Two questions with one answer between them. A declaration that will not
// compose sends its author to both — what did this resolve to, and what else
// was there — and the second is only useful in the terms of the first, so both
// are answered from the layers themselves and rendered the same two ways.
//
// # What a declaration resolves to
//
// A stack is written the way a reader thinks about it — outermost first, the
// subject buried innermost — and resolved the other way, because every layer's
// shape is decided by the one beneath it. So the first thing anybody asks of a
// nested declaration is which layer is doing what, and the answer is a walk
// nobody can do in their head past two levels.
//
// What this package produces is that walk, in the order it happens: the subject
// first, then each layer over it, with the capabilities each one adds and the
// methods each one will emit. Every answer comes from the layer itself — its
// declared kind, its own summary, the shape it returns for the shape beneath it
// — so a report and a generated file disagree only if a layer disagrees with
// itself.
//
// It is not yet the whole of what generation will do. Nothing here checks that
// each layer accepts the shape below it, and nothing inserts the storage a
// refining layer implies when a declaration names none, so a stack this reports
// on cleanly may still be one generation refuses. Both belong to the stage that
// validates a composition, which does not exist yet; until it does, this
// describes a stack rather than approving one.
//
// # What could have been written
//
// The catalogue is the same thing asked of a build rather than of a
// declaration: every registered layer, where in a stack it may appear, what it
// needs and contributes, and what may be written about it. Nothing there is
// read from a table kept beside the layers — it is asked of each layer, so a
// listing and a refusal cannot disagree.
//
// # Two renderings, deliberately
//
// A table for a person and a document for a program. Neither is the other's
// serialisation — a table that a script parses becomes a format nobody can
// improve, and a document that a person reads is one they have to decode.
package explain
