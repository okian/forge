// Package contenthash generates a number that stands for what a value is.
//
// What it writes is Hash() uint64 on the subject, built from everything the
// subject holds: two values that are the same all the way down hash to the same
// number, in this process and in every other, on any machine and in next year's
// build. That is the whole claim, and it is what a value needs before it can be
// a set member or a map key without being comparable in the language's sense.
//
// Go's own equality is not enough for that. A struct holding a slice or a map
// cannot be compared with == at all, and a struct holding a pointer compares by
// where the pointer goes rather than by what is there — so the two obvious ways
// to give a value an identity either do not compile or answer the wrong
// question. This answers the other one: what the value contains.
//
// # A hash, not an identity
//
// Sixty-four bits cannot tell every value apart and this one does not try. Two
// different values may hash alike, and a caller who cannot afford that compares
// as well as hashes — which is what a hash is for in a lookup structure: it
// finds the small number of candidates that a comparison then decides between.
//
// It is not a cryptographic hash either. Nothing here resists somebody choosing
// inputs to collide; a program whose inputs an attacker writes wants a keyed
// hash, and one that has to prove a value has not changed wants a signature.
//
// # What is hashed, and how
//
// Fields in the order they are declared, so that a field added in the middle of
// a struct changes the generated hash in the middle of the method rather than
// the whole of it. Numbers are mixed as sixty-four bits whatever width they
// were declared at, so an int hashes the same on a machine where one is four
// bytes and on a machine where it is eight. Strings carry their length, so that
// two of them running together are told apart from two others that run together
// the same way.
//
// Three shapes carry more than what they hold. A pointer says whether there is
// anything there, because nil and a pointer to the zero value are different
// values. A slice says whether it is there and how long it is, and then its
// elements in order. A map says whether it is there and how many entries it
// has, and then a total of its entries — added up rather than chained, because
// ranging over a map is deliberately unordered and a chained hash would give
// one map as many answers as it has orders to be walked in.
//
// Two floating-point values are written down rather than read off their bits.
// Positive and negative zero are one number with two spellings and hash alike;
// every not-a-number hashes alike, because no two of them are equal — including
// each to itself — and no answer agrees with comparison.
//
// A type that declares Hash of its own is asked rather than written for, which
// is how a hand-written identity stays authoritative and how a type whose
// invariants forge cannot see is hashed properly.
//
// # What is refused
//
// An interface, a channel and a function have no content to hash: what an
// interface holds is decided at run time, and the other two are references
// whose identity is where something is rather than what it is. unsafe.Pointer
// is refused for the same reason, and so is a struct declared in another
// package whose unexported fields generated code here cannot read — a hash of
// the half of a value that happens to be visible would call two different
// values the same, which is worse than not having one.
//
// Each is a diagnostic naming the field, and the way out is to say what was
// meant:
//
//	//forge:hash ignore
//
// above the field, which leaves it out of the hash and says so where a reader
// would ask. That is the right answer more often than it sounds: a cached
// total, the time a record was last read, a mutex — none of them is part of
// what the value is.
//
// # A value that contains itself
//
// The generator terminates whatever the types do, because a struct is hashed by
// calling its own method rather than by inlining what it holds: a type that
// reaches itself produces a method that calls itself, which is a finite amount
// of code. A chain of pointers, slices or maps that closes on itself with no
// struct in between has no such stopping point and is refused instead.
//
// The value is another matter. A hash follows what is there, so a list, a tree
// and a graph with no cycle in it are all hashed in full — and a value that
// really does contain itself would be followed for ever. Tracking what had
// already been seen would cost an allocation on every hash of every value, to
// bound a case most programs do not have; the way to bound it where it does
// arise is //forge:hash ignore on the field that closes the loop.
package contenthash
