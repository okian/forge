package people_test

import (
	"testing"

	"github.com/okian/forge/examples/people"
)

// The lazy chain allocates nothing per element.
//
// Stated as an invariant rather than as a number, because a number is the wrong
// claim and would be wrong in both directions: a chain costs a few allocations
// to build and to start, which is a fixed price nobody should be held to zero
// on, and holding it to that fixed price would let a per-element allocation
// hide inside a budget somebody raised once.
//
// What is actually claimed is that walking ten elements and walking ten
// thousand cost the same, which is the only shape "nothing per element" has.
// A per-element allocation of any size fails it by a factor of a thousand.
func TestTheLazyChainAllocatesNothingPerElement(t *testing.T) {
	short := walking(10)
	long := walking(10_000)

	if short != long {
		t.Errorf("walking 10 elements allocated %.0f times and walking 10,000 allocated %.0f; "+
			"the difference is per-element, and there is meant to be none", short, long)
	}
}

// walking reports what one walk of a chain over n elements allocates.
//
// The chain is built outside the measurement because building it is what costs
// — a closure per link — and is not what this is about.
func walking(n int) float64 {
	held := crowd(n)
	chain := held.Seq().
		Filter(func(p people.Person) bool { return p.Age >= 0 }).
		Map(func(p people.Person) string { return p.Name })

	return testing.AllocsPerRun(50, func() {
		for name := range chain.All() {
			sink(len(name))
		}
	})
}

// The same claim for the container's own walk, which everything above is built
// on: walking is a fixed price however much there is to walk.
func TestWalkingTheContainerAllocatesNothingPerElement(t *testing.T) {
	short := ranging(10)
	long := ranging(10_000)

	if short != long {
		t.Errorf("walking 10 elements allocated %.0f times and walking 10,000 allocated %.0f", short, long)
	}
}

// ranging reports what one walk of the container over n elements allocates.
func ranging(n int) float64 {
	held := crowd(n)

	return testing.AllocsPerRun(50, func() {
		for p := range held.All() {
			sink(p.ID)
		}
	})
}

// Taking a few from a long sequence costs what taking a few costs.
//
// The chain stops the walk beneath it rather than reading on and discarding, so
// the price of the first ten is the same whether there are a hundred behind
// them or a hundred thousand. Measured in allocations rather than in time,
// which is the half of it that is exact.
func TestTakingAFewFromALongSequenceCostsWhatAFewCost(t *testing.T) {
	short := taking(100)
	long := taking(100_000)

	if short != long {
		t.Errorf("taking 10 from 100 allocated %.0f times and taking 10 from 100,000 allocated %.0f", short, long)
	}
}

// taking reports what taking ten elements from a sequence of n allocates.
func taking(n int) float64 {
	held := crowd(n)
	chain := held.Seq().Take(10)

	return testing.AllocsPerRun(50, func() {
		for p := range chain.All() {
			sink(p.ID)
		}
	})
}
