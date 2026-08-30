// Package emit turns declarations into the bytes of a generated file.
//
// Generated files are committed, which decides almost everything about how
// they are written. They are read in review, walked in an editor, and diffed
// against their predecessors far more often than they are produced, so the file
// has to be gofmt-clean, has to say plainly that it is generated, and — the
// part that is easy to get wrong — has to come out byte-identical when nothing
// it was made from has changed. A generator that rewrites a file on every run
// turns every unrelated commit into a diff nobody reads, and a diff nobody
// reads is where a wrong line hides.
//
// Determinism here is structural rather than promised. Nothing in this package
// iterates a map, imports are sorted, and declarations are written in the order
// they arrive — which the stage that merges them has already made deterministic.
//
// The header is a record, not a decoration. It carries a digest of everything
// the file was made from, so that asking whether a file is stale is a
// comparison rather than a regeneration, and it names the versions of both the
// tool and the markers it read, so that a file produced by one and read by
// another can be told apart from one that merely looks odd.
package emit
