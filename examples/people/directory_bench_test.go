package people_test

import (
	"slices"
	"testing"

	"github.com/okian/forge/examples/people"
)

// The keyed storage against the same work by hand, over the same thousand
// people. Four verbs are priced: finding one by key, walking a shared value,
// building the pair, and churning it. The by-hand versions are the code the
// storage replaces — a slice beside maps, kept in agreement by attention —
// written the way somebody would write them, sized ahead where a person
// naturally would.

// A generated point lookup: one map access, a pointer back.
func BenchmarkDirectoryLookup(b *testing.B) {
	held := people.NewDirectory(census(elements)...)
	b.ReportAllocs()
	for b.Loop() {
		found, _ := held.ByID(elements / 2)
		sink(found.Age)
	}
}

// The same, written by hand over a value map.
func BenchmarkDirectoryLookupByHand(b *testing.B) {
	byID := make(map[int]people.Person, elements)
	for _, p := range census(elements) {
		byID[p.ID] = p
	}
	b.ReportAllocs()
	for b.Loop() {
		found := byID[elements/2]
		sink(found.Age)
	}
}

// throng returns n people with a hundred distinct names, so a name reaches
// one percent of them — the selectivity an index is declared for. The census
// the tests share is deliberately unselective, and a benchmark over it would
// record the one case a scan wins.
func throng(n int) []people.Person {
	out := census(n)
	for i := range out {
		out[i].Name = names[i%len(names)]
	}
	return out
}

var names = func() []string {
	held := make([]string, 100)
	for i := range held {
		held[i] = "name " + string(rune('a'+i/10)) + string(rune('a'+i%10))
	}
	return held
}()

// A generated secondary lookup: the bucket names one percent of the
// directory, and the walk resolves each key through the primary map.
func BenchmarkDirectoryByName(b *testing.B) {
	held := people.NewDirectory(throng(elements)...)
	b.ReportAllocs()
	for b.Loop() {
		n := 0
		for range held.ByName(names[7]) {
			n++
		}
		sink(n)
	}
}

// The same question answered the way a slice answers it: scan everything and
// compare.
func BenchmarkDirectoryByNameByHand(b *testing.B) {
	held := throng(elements)
	b.ReportAllocs()
	for b.Loop() {
		n := 0
		for _, p := range held {
			if p.Name == names[7] {
				n++
			}
		}
		sink(n)
	}
}

// Building the directory: one entry allocation per element, maps grown as
// they fill.
func BenchmarkDirectoryBuild(b *testing.B) {
	held := census(elements)
	b.ReportAllocs()
	for b.Loop() {
		sink(people.NewDirectory(held...).Len())
	}
}

// The same pair built by hand, sized ahead — which the constructor cannot be,
// its maps being made on first use by bodies that also serve the zero value.
func BenchmarkDirectoryBuildByHand(b *testing.B) {
	held := census(elements)
	b.ReportAllocs()
	for b.Loop() {
		order := make([]people.Person, 0, len(held))
		byID := make(map[int]people.Person, len(held))
		byName := make(map[string][]int, 3)
		for _, p := range held {
			order = append(order, p)
			byID[p.ID] = p
			byName[p.Name] = append(byName[p.Name], p.ID)
		}
		sink(len(order) + len(byID) + len(byName))
	}
}

// Steady-state churn: one add and one removal per operation, the swap keeping
// removal from shuffling a thousand elements.
func BenchmarkDirectoryChurn(b *testing.B) {
	held := people.NewDirectory(census(elements)...)
	one := []people.Person{{ID: elements + 1, Name: "churn"}}
	b.ReportAllocs()
	for b.Loop() {
		if err := held.AppendSeq(slices.Values(one)); err != nil {
			b.Fatal(err)
		}
		if !held.Remove(elements + 1) {
			b.Fatal("nothing to remove")
		}
		sink(held.Len())
	}
}

// The same churn by hand: append, file, unfile, swap-remove.
func BenchmarkDirectoryChurnByHand(b *testing.B) {
	order := census(elements)
	byID := make(map[int]int, elements)
	byName := make(map[string][]int, 4)
	for i, p := range order {
		byID[p.ID] = i
		byName[p.Name] = append(byName[p.Name], p.ID)
	}
	one := people.Person{ID: elements + 1, Name: "churn"}
	b.ReportAllocs()
	for b.Loop() {
		order = append(order, one)
		byID[one.ID] = len(order) - 1
		byName[one.Name] = append(byName[one.Name], one.ID)

		at := byID[one.ID]
		delete(byID, one.ID)
		delete(byName, one.Name)
		if last := len(order) - 1; at != last {
			moved := order[last]
			order[at] = moved
			byID[moved.ID] = at
		}
		order = order[:len(order)-1]

		sink(len(order))
	}
}
