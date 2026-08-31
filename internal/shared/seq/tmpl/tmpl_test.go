package tmpl_test

import (
	"slices"
	"strconv"
	"testing"

	"github.com/okian/forge/internal/shared/seq/tmpl"
)

// over returns a view of the numbers from one to n, which is what the tests and
// the benchmarks below all walk.
func over(n int) tmpl.Seq[int] {
	return func(yield func(int) bool) {
		for i := 1; i <= n; i++ {
			if !yield(i) {
				return
			}
		}
	}
}

// The chain in the documentation, end to end: what it selects and in what
// order.
func TestTheChainAsItIsWritten(t *testing.T) {
	got := over(20).
		Filter(func(v int) bool { return v%2 == 0 }).
		Map(strconv.Itoa).
		Take(3).
		Collect()

	if want := []string{"2", "4", "6"}; !slices.Equal(got, want) {
		t.Errorf("the chain yields %v, want %v", got, want)
	}
}

// Every operation walks nothing until a terminal asks. Counting what the source
// yielded is the only way to see it: a chain that walked eagerly would produce
// the same answer and read the whole sequence to get it.
func TestNothingIsWalkedUntilItIsAskedFor(t *testing.T) {
	read := 0
	counted := tmpl.Seq[int](func(yield func(int) bool) {
		for i := 1; ; i++ {
			read++
			if !yield(i) {
				return
			}
		}
	})

	chain := counted.Filter(func(v int) bool { return v%3 == 0 }).Take(2)
	if read != 0 {
		t.Errorf("building the chain read %d elements", read)
	}

	if got, want := chain.Collect(), []int{3, 6}; !slices.Equal(got, want) {
		t.Errorf("the chain yields %v, want %v", got, want)
	}
	// Six, and not one more: taking two stops the walk beneath it rather than
	// reading on and discarding, which is what makes a chain over an endless
	// sequence terminate at all.
	if read != 6 {
		t.Errorf("it read %d elements to find two, want 6", read)
	}
}

// Each operation on its own, over the cases where an off-by-one lives.
func TestWhatEachOperationYields(t *testing.T) {
	cases := map[string]struct {
		view tmpl.Seq[int]
		want []int
	}{
		"filter keeping some":       {over(5).Filter(func(v int) bool { return v > 3 }), []int{4, 5}},
		"filter keeping none":       {over(5).Filter(func(int) bool { return false }), nil},
		"take fewer than there are": {over(5).Take(2), []int{1, 2}},
		"take more than there are":  {over(2).Take(5), []int{1, 2}},
		"take none":                 {over(5).Take(0), nil},
		"take a negative number":    {over(5).Take(-1), nil},
		"skip some":                 {over(5).Skip(3), []int{4, 5}},
		"skip everything":           {over(5).Skip(5), nil},
		"skip more than there are":  {over(5).Skip(9), nil},
		"skip none":                 {over(3).Skip(0), []int{1, 2, 3}},
		"skip a negative number":    {over(3).Skip(-1), []int{1, 2, 3}},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := tc.view.Collect(); !slices.Equal(got, tc.want) {
				t.Errorf("yields %v, want %v", got, tc.want)
			}
		})
	}
}

// Runs of equal elements collapse to their first, and elements equal to one
// that is not adjacent do not — which is the whole of what an operation that
// remembers one element can do.
func TestDedupCollapsesRuns(t *testing.T) {
	same := func(a, b int) bool { return a == b }

	held := tmpl.Seq[int](slices.Values([]int{1, 1, 2, 2, 2, 3, 1}))
	if got, want := held.Dedup(same).Collect(), []int{1, 2, 3, 1}; !slices.Equal(got, want) {
		t.Errorf("yields %v, want %v", got, want)
	}

	// A run at the very start is the case a first-element flag gets wrong.
	first := tmpl.Seq[int](slices.Values([]int{5, 5}))
	if got, want := first.Dedup(same).Collect(), []int{5}; !slices.Equal(got, want) {
		t.Errorf("yields %v, want %v", got, want)
	}
}

// A short last chunk is kept rather than dropped, and each chunk is a slice of
// its own — so a caller holding one still holds what it held after the next has
// been built.
func TestChunkingKeepsTheShortLastOne(t *testing.T) {
	var batches [][]int
	for batch := range over(5).Chunk(2) {
		batches = append(batches, batch)
	}

	want := [][]int{{1, 2}, {3, 4}, {5}}
	if len(batches) != len(want) {
		t.Fatalf("chunked into %v, want %v", batches, want)
	}
	for i := range want {
		if !slices.Equal(batches[i], want[i]) {
			t.Errorf("chunk %d is %v, want %v", i, batches[i], want[i])
		}
	}

	// A sequence that divides evenly yields nothing extra at the end.
	even := 0
	for range over(4).Chunk(2) {
		even++
	}
	if even != 2 {
		t.Errorf("four elements in twos made %d chunks, want 2", even)
	}
}

// Chunks are a plain sequence because a view of them cannot be written, and one
// conversion is the whole of the difference.
func TestChunksChainAfterAConversion(t *testing.T) {
	sizes := tmpl.Seq[[]int](over(5).Chunk(2)).
		Map(func(batch []int) int { return len(batch) }).
		Collect()

	if want := []int{2, 2, 1}; !slices.Equal(sizes, want) {
		t.Errorf("the chunk sizes are %v, want %v", sizes, want)
	}
}

// A chunk of nothing has no answer, so it says so where the caller is rather
// than yielding an emptiness they will read as "no data".
func TestChunkingByNothing(t *testing.T) {
	for _, n := range []int{0, -1} {
		t.Run(strconv.Itoa(n), func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Errorf("chunking by %d returned", n)
				}
			}()

			over(3).Chunk(n)
		})
	}
}

// The terminals: what each of them ends a chain with.
func TestHowAChainEnds(t *testing.T) {
	if got := over(3).Collect(); !slices.Equal(got, []int{1, 2, 3}) {
		t.Errorf("Collect() = %v", got)
	}

	// Into is Collect for a caller who owns the memory, so it appends rather
	// than replaces and hands back the slice it grew.
	dst := make([]int, 0, 8)
	if got := over(3).Into(append(dst, 0)); !slices.Equal(got, []int{0, 1, 2, 3}) {
		t.Errorf("Into() = %v", got)
	}

	if got, ok := over(3).First(); !ok || got != 1 {
		t.Errorf("First() = %d, %v", got, ok)
	}
	if got, ok := over(0).First(); ok || got != 0 {
		t.Errorf("First() over nothing = %d, %v", got, ok)
	}

	// Reduce builds something that is not an element, which is what being
	// generic in the accumulator is for.
	if got := over(4).Reduce(0, func(sum, v int) int { return sum + v }); got != 10 {
		t.Errorf("Reduce() = %d, want 10", got)
	}
	if got := over(3).Reduce("", func(acc string, v int) string { return acc + strconv.Itoa(v) }); got != "123" {
		t.Errorf("Reduce() = %q, want %q", got, "123")
	}
}

// The zero view is usable rather than a panic waiting to happen, so a view that
// was never given a sequence behaves like an empty one everywhere.
func TestTheZeroView(t *testing.T) {
	var none tmpl.Seq[int]

	if got := none.Collect(); len(got) != 0 {
		t.Errorf("the zero view yields %v", got)
	}
	if _, ok := none.First(); ok {
		t.Error("the zero view has a first element")
	}
	if got := none.Filter(func(int) bool { return true }).Take(3).Collect(); len(got) != 0 {
		t.Errorf("a chain over the zero view yields %v", got)
	}
	if got := none.Reduce(7, func(sum, v int) int { return sum + v }); got != 7 {
		t.Errorf("reducing the zero view gave %d, want the value it started with", got)
	}

	count := 0
	for range none.Chunk(2) {
		count++
	}
	if count != 0 {
		t.Errorf("the zero view chunked into %d batches", count)
	}
}

// Stopping early has to be honoured by the operations that count as well as by
// the ones that pass elements through — a link keeping a tally of its own is
// where a yield's answer is easiest to drop on the floor.
func TestStoppingEarlyPartWayThroughACount(t *testing.T) {
	seen := 0
	for range over(10).Take(5).All() {
		seen++
		break
	}
	if seen != 1 {
		t.Errorf("stopping after one element of a take of five read %d", seen)
	}

	// And part way through a chunk, where what is abandoned is a batch being
	// built rather than an element being passed on.
	batches := 0
	for range over(10).Chunk(3) {
		batches++
		break
	}
	if batches != 1 {
		t.Errorf("stopping after one chunk of ten elements read %d", batches)
	}
}

// A caller who stops reading stops the whole chain, at every link, which is
// what "lazy" has to mean for a chain rather than for one operation.
func TestStoppingEarlyStopsEveryLink(t *testing.T) {
	read := 0
	counted := tmpl.Seq[int](func(yield func(int) bool) {
		for i := 1; i <= 100; i++ {
			read++
			if !yield(i) {
				return
			}
		}
	})

	chain := counted.
		Filter(func(int) bool { return true }).
		Map(func(v int) int { return v }).
		Skip(1).
		Dedup(func(a, b int) bool { return a == b })

	seen := 0
	for range chain.All() {
		seen++
		if seen == 2 {
			break
		}
	}

	// Three: one skipped and two yielded. An inequality would pass a chain that
	// read one element too many, which is the whole of what could go wrong.
	if read != 3 {
		t.Errorf("reading two elements through four links read %d of them, want 3", read)
	}
}

// The allocation claim, measured rather than asserted: a chain costs the same
// whether it walks a hundred elements or a hundred thousand.
//
// Two sizes rather than a budget, because the number that matters is the slope
// and not the intercept. Building a chain allocates once per link and no test
// should be pinned to how many links a future combinator adds — what must stay
// true is that walking one more element adds nothing.
func TestAChainCostsNothingPerElement(t *testing.T) {
	// Reduce rather than Collect, because a terminal that builds a slice
	// allocates for the slice — legitimately, and in proportion to the answer
	// rather than to the walk. What is being measured is the walk.
	drain := func(n int) func() {
		return func() {
			over(n).
				Filter(func(v int) bool { return v%2 == 0 }).
				Map(func(v int) int { return v * 2 }).
				Take(n).
				Reduce(0, func(sum, v int) int { return sum + v })
		}
	}

	// The other links, and the terminal that fills a slice a caller owns. The
	// documented chain is the one the target names, but a combinator that
	// started buffering would land in whichever of them nothing measured.
	gathering := func(n int) func() {
		held := make([]int, 0, n)
		return func() {
			over(n).
				Skip(1).
				Dedup(func(a, b int) bool { return a == b }).
				Into(held[:0])
		}
	}

	for name, chain := range map[string]func(int) func(){
		"filtering and taking":       drain,
		"skipping and deduplicating": gathering,
	} {
		small := testing.AllocsPerRun(100, chain(100))
		large := testing.AllocsPerRun(100, chain(100_000))

		if small != large {
			t.Errorf("%s over 100 elements allocates %v and over 100,000 allocates %v; "+
				"the difference between them is what each element costs", name, small, large)
		}
		t.Logf("%s costs %v allocations, whatever it walks", name, small)
	}
}

// BenchmarkChain is the documented chain over a thousand elements, which is
// what a regression in the per-element cost shows up in first.
func BenchmarkChain(b *testing.B) {
	b.ReportAllocs()

	for b.Loop() {
		over(1000).
			Filter(func(v int) bool { return v%2 == 0 }).
			Map(func(v int) int { return v * 2 }).
			Take(100).
			Reduce(0, func(sum, v int) int { return sum + v })
	}
}

// BenchmarkChainIntoOwnedMemory is the same chain ending in a slice the caller
// keeps, which is the shape a caller reaching for Into over Collect is after.
func BenchmarkChainIntoOwnedMemory(b *testing.B) {
	b.ReportAllocs()

	held := make([]int, 0, 1024)
	for b.Loop() {
		held = over(1000).
			Filter(func(v int) bool { return v%2 == 0 }).
			Map(func(v int) int { return v * 2 }).
			Into(held[:0])
	}
}

// BenchmarkRange is the same walk written by hand, which is what the chain's
// cost has to be read against.
func BenchmarkRange(b *testing.B) {
	b.ReportAllocs()

	for b.Loop() {
		sum, taken := 0, 0
		for v := range over(1000).All() {
			if v%2 != 0 {
				continue
			}
			sum += v * 2
			taken++
			if taken == 100 {
				break
			}
		}
	}
}
