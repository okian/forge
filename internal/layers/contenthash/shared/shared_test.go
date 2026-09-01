package shared

import (
	"hash/fnv"
	"math"
	"testing"
)

// This package is emitted into somebody else's module, where nothing this
// repository runs will ever exercise it again. So it is tested here, as the
// ordinary Go it is — which is the whole reason it is a file rather than a
// string.

// The arithmetic is FNV-1a, and the way to be sure of that is to ask the
// standard library for the same answer.
//
// Byte for byte rather than in spirit: what is claimed of this hash is that a
// value hashes to the same number everywhere, and the only way to hold that to
// anything is to compare it against an implementation nobody here wrote.
func TestTheArithmeticIsTheOneItClaims(t *testing.T) {
	for _, bytes := range [][]byte{nil, {0}, {1, 2, 3}, []byte("a longer run of bytes")} {
		want := fnv.New64a()
		if _, err := want.Write(bytes); err != nil {
			t.Fatalf("writing to the standard library's hash: %v", err)
		}

		got := fnvSeed
		for _, b := range bytes {
			got = (got ^ uint64(b)) * fnvPrime
		}

		if got != want.Sum64() {
			t.Errorf("hashing %v gives %d, want %d", bytes, got, want.Sum64())
		}
	}
}

// A number hashes the same however wide the type it came out of was, which is
// what lets a value hash the same on two machines.
func TestAWholeNumberIsEightBytesWide(t *testing.T) {
	if fnvUint64(fnvSeed, uint64(uint8(7))) != fnvUint64(fnvSeed, uint64(uint32(7))) {
		t.Error("one number hashed two ways depending on the type it came from")
	}
	if fnvUint64(fnvSeed, 7) == fnvUint64(fnvSeed, 8) {
		t.Error("two numbers hash alike")
	}
}

// A string's length is hashed as well as its bytes, so that two strings running
// together the same way are still told apart.
func TestWhereOneStringEndsAndTheNextBegins(t *testing.T) {
	first := fnvString(fnvString(fnvSeed, "ab"), "c")
	second := fnvString(fnvString(fnvSeed, "a"), "bc")

	if first == second {
		t.Error(`"ab","c" and "a","bc" hash alike`)
	}
	if fnvString(fnvSeed, "") == fnvSeed {
		t.Error("an empty string is hashed as nothing at all")
	}
}

// A boolean is one byte and its two values are two hashes.
func TestABooleanIsTwoAnswers(t *testing.T) {
	if fnvBool(fnvSeed, true) == fnvBool(fnvSeed, false) {
		t.Error("true and false hash alike")
	}
	// Through a variable, because constant arithmetic in Go does not wrap and
	// the product of these two does not fit.
	zero := fnvSeed
	if fnvBool(fnvSeed, false) != zero*fnvPrime {
		t.Error("false is not the zero byte")
	}
}

// The two floating-point values whose bits are not a function of the number are
// each answered for.
func TestTheTwoNumbersBitsCannotAnswerFor(t *testing.T) {
	if fnvFloat(fnvSeed, 0) != fnvFloat(fnvSeed, math.Copysign(0, -1)) {
		t.Error("positive and negative zero hash differently though they compare equal")
	}

	// Two not-a-numbers with different bits, which is what makes the case worth
	// writing: reading the bits would give them two hashes.
	other := math.Float64frombits(math.Float64bits(math.NaN()) | 1<<30)
	if !math.IsNaN(other) {
		t.Fatal("the fixture is a number")
	}
	if fnvFloat(fnvSeed, math.NaN()) != fnvFloat(fnvSeed, other) {
		t.Error("two not-a-numbers hash differently")
	}

	if fnvFloat(fnvSeed, 1.5) == fnvFloat(fnvSeed, 2.5) {
		t.Error("two numbers hash alike")
	}
	if fnvFloat(fnvSeed, 1.5) != fnvUint64(fnvSeed, math.Float64bits(1.5)) {
		t.Error("an ordinary number is not hashed by its bits")
	}
}

// Hashing costs no memory, because it is arithmetic. A hash that allocated
// would be one a set could not afford to take per lookup.
func TestHashingAllocatesNothing(t *testing.T) {
	if got := testing.AllocsPerRun(100, func() {
		h := fnvSeed
		h = fnvUint64(h, 42)
		h = fnvString(h, "a name")
		h = fnvBool(h, true)
		h = fnvFloat(h, 1.5)
		_ = h
	}); got != 0 {
		t.Errorf("hashing allocates %v times per run", got)
	}
}
