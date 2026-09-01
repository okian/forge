// Package shared holds the arithmetic every generated hash is built out of.
//
// It is emitted rather than imported: generated code depends on the standard
// library and on nothing that has to be kept in step with the binary that wrote
// it, so what a package needs is written into it once however many declarations
// asked for it.
//
// The algorithm is FNV-1a over sixty-four bits. What it has to be is stable —
// one value hashing to one number in every process, on every architecture, and
// in next year's build — and this is that with nothing to configure: no seed,
// no table, and arithmetic short enough for the compiler to put where it is
// used rather than call.
//
// It is not a cryptographic hash and nothing here claims otherwise. A hash
// whose inputs an attacker chooses wants a keyed one, and a value whose
// identity has to survive an adversary wants a signature; what this is for is
// telling values apart cheaply, in a program where the values are the
// program's own.
package shared

import "math"

// The two numbers FNV-1a is made of: what an empty stream hashes to, and the
// multiplier each byte is mixed with.
//
// Written out rather than taken from hash/fnv. That package answers through an
// interface, so a hash taken through it allocates and is called rather than
// inlined — and what is wanted here is arithmetic in the middle of a method,
// not an object with a lifetime.
const (
	fnvSeed  uint64 = 14695981039346656037
	fnvPrime uint64 = 1099511628211
)

// fnvUint64 mixes a whole number into a hash, eight bytes at a time.
//
// Eight whatever the number was declared as, so that a value held in an int
// hashes the same where an int is four bytes and where it is eight. A number
// that hashed differently depending on the machine would not be a stable
// identity, which is the only thing this is for.
func fnvUint64(h, v uint64) uint64 {
	for range 8 {
		h = (h ^ (v & 0xff)) * fnvPrime
		v >>= 8
	}
	return h
}

// fnvString mixes a string's length and then its bytes.
//
// The length as well as the bytes, because the bytes alone do not say where one
// string ended and the next began: "ab" beside "c" and "a" beside "bc" are the
// same five bytes in the same order and are not the same pair of values.
func fnvString(h uint64, s string) uint64 {
	h = fnvUint64(h, uint64(len(s)))
	for i := range len(s) {
		h = (h ^ uint64(s[i])) * fnvPrime
	}
	return h
}

// fnvBool mixes the one byte a boolean is worth.
func fnvBool(h uint64, b bool) uint64 {
	if b {
		return (h ^ 1) * fnvPrime
	}
	return h * fnvPrime
}

// fnvFloat mixes a floating-point number by its bits, with the two values
// whose bits are not a function of the number written down first.
//
// Zero is one number with two spellings, since the sign bit is free, and a hash
// taken straight off the bits would give positive and negative zero two answers
// though they compare equal. Not-a-number is the opposite case: it is many bit
// patterns, no one of which equals any other or even itself, so no answer
// agrees with comparison and the least surprising one is that they all hash
// alike.
func fnvFloat(h uint64, f float64) uint64 {
	switch {
	case math.IsNaN(f):
		return fnvUint64(h, fnvNaN)
	case f == 0:
		return fnvUint64(h, 0)
	default:
		return fnvUint64(h, math.Float64bits(f))
	}
}

// fnvNaN is what every not-a-number hashes as. A constant of this file rather
// than the bits of math.NaN, so that what a value hashes to is decided here and
// cannot move under a library release.
const fnvNaN uint64 = 0x7ff8000000000001
