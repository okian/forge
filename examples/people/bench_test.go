package people_test

import (
	"cmp"
	"slices"
	"strconv"
	"testing"

	"github.com/okian/forge/examples/people"
)

// elements is how many people every benchmark below works over.
//
// Large enough that per-element cost dominates the cost of starting a walk,
// which is what these are measuring, and small enough that the whole thing
// stays in cache — a benchmark that spends its time missing cache measures the
// machine rather than the code.
const elements = 1000

// crowd builds a directory of n people with distinct keys.
func crowd(n int) people.Persons {
	held := make([]people.Person, 0, n)
	for i := range n {
		held = append(held, people.Person{
			ID:    i,
			Name:  "person-" + strconv.Itoa(i),
			Email: "person-" + strconv.Itoa(i) + "@example.com",

			// Spread over a small range so that the sorted views do real work
			// on a key with many ties, which is the case a stable sort has to
			// get right and the one that costs the most.
			Age: i % 97,
		})
	}
	return people.NewPersons(held...)
}

// The lazy chain, walked. Built once and walked many times, because what it
// costs to build is a fixed price per chain and what it costs to walk is the
// price this is about.
func BenchmarkLazyChain(b *testing.B) {
	held := crowd(elements)
	chain := held.Seq().
		Filter(func(p people.Person) bool { return p.Age > 40 }).
		Map(func(p people.Person) string { return p.Name }).
		Take(100)

	b.ReportAllocs()

	for b.Loop() {
		for name := range chain.All() {
			sink(len(name))
		}
	}
}

// The same work written as a loop, which is what the chain is worth measuring
// against: it reads the same elements, tests the same predicate, and stops at
// the same point.
func BenchmarkLazyChainByHand(b *testing.B) {
	held := crowd(elements)

	b.ReportAllocs()

	for b.Loop() {
		taken := 0
		for _, p := range held {
			if p.Age <= 40 {
				continue
			}
			sink(len(p.Name))
			taken++
			if taken == 100 {
				break
			}
		}
	}
}

// A generated projection, which is the method the declaration bought.
func BenchmarkProjection(b *testing.B) {
	held := crowd(elements)

	b.ReportAllocs()

	for b.Loop() {
		sink(len(held.Names()))
	}
}

// The same projection written by hand, sized ahead the way somebody writing it
// once for one field would size it.
//
// This is the honest comparison and the one the generated method loses on
// allocation: it knows the length and can ask for it, where the generated
// method walks a sequence and grows. What the generated method buys instead is
// that it exists for every field, spelled with the field's name, without
// anybody writing this loop four times.
//
// The loss is not inherent. The helper underneath requires only that the stack
// beneath it can be walked, which is the right contract; a stack that can also
// report its length says so, and a variant that sized its result ahead when the
// length is free is available and not written. This is what it would be
// measured against.
func BenchmarkProjectionByHand(b *testing.B) {
	held := crowd(elements)

	b.ReportAllocs()

	for b.Loop() {
		names := make([]string, 0, len(held))
		for _, p := range held {
			names = append(names, p.Name)
		}
		sink(len(names))
	}
}

// A generated sorted view, over a key with many ties — which is both the case a
// stable sort costs the most on and the one it is worth having.
func BenchmarkSortedView(b *testing.B) {
	held := crowd(elements)

	b.ReportAllocs()

	for b.Loop() {
		sink(len(held.SortedByAge()))
	}
}

// The same, written by hand.
func BenchmarkSortedViewByHand(b *testing.B) {
	held := crowd(elements)

	b.ReportAllocs()

	for b.Loop() {
		sorted := slices.Clone(held)
		slices.SortStableFunc(sorted, func(a, b people.Person) int { return cmp.Compare(a.Age, b.Age) })
		sink(len(sorted))
	}
}

// A generated lookup map.
func BenchmarkIndex(b *testing.B) {
	held := crowd(elements)

	b.ReportAllocs()

	for b.Loop() {
		sink(len(held.ByID()))
	}
}

// The same, written by hand and sized ahead.
func BenchmarkIndexByHand(b *testing.B) {
	held := crowd(elements)

	b.ReportAllocs()

	for b.Loop() {
		byID := make(map[int]people.Person, len(held))
		for _, p := range held {
			byID[p.ID] = p
		}
		sink(len(byID))
	}
}

// sink keeps a benchmark's result reachable, so that the work producing it
// cannot be dropped as dead.
//
//go:noinline
func sink(n int) { kept = n }

// kept holds what sink was last given.
var kept int
