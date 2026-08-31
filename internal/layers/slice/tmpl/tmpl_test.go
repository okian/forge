package tmpl_test

import (
	"slices"
	"testing"

	"github.com/okian/forge/internal/layers/slice/tmpl"
)

// A template is the bodies a layer emits, so testing it here is testing what
// every declaration using this layer will run. The alternative is finding out
// through a golden file, which says the output changed and not whether it works.

// The constructor holds what it was given, in order, and holds a copy of it.
func TestWhatTheConstructorHolds(t *testing.T) {
	if got := tmpl.New[int]().Len(); got != 0 {
		t.Errorf("a container built from nothing holds %d elements", got)
	}

	given := []int{1, 2, 3}
	held := tmpl.New(given...)

	if got := slices.Collect(held.All()); !slices.Equal(got, given) {
		t.Errorf("holds %v, want %v", got, given)
	}

	// The caller's slice is the caller's. A container that kept it would change
	// under them, which is the kind of aliasing that only shows up under load.
	given[0] = 99
	if got := slices.Collect(held.All()); got[0] != 1 {
		t.Errorf("the container followed the caller's slice: %v", got)
	}
}

// The two walks are the same elements in the two orders, and Len agrees with
// both without walking anything.
func TestWalkingBothWays(t *testing.T) {
	held := tmpl.New("a", "b", "c")

	if got, want := slices.Collect(held.All()), []string{"a", "b", "c"}; !slices.Equal(got, want) {
		t.Errorf("All() yields %v, want %v", got, want)
	}
	if got, want := slices.Collect(held.Backward()), []string{"c", "b", "a"}; !slices.Equal(got, want) {
		t.Errorf("Backward() yields %v, want %v", got, want)
	}
	if got, want := held.Len(), 3; got != want {
		t.Errorf("Len() = %d, want %d", got, want)
	}
}

// A walk that stops early stops the loop behind it rather than running it out,
// which is what a range over one of these compiles to and what makes a chain
// over a large container cheap.
func TestAWalkThatStopsEarly(t *testing.T) {
	held := tmpl.New(1, 2, 3, 4)

	for _, walk := range map[string]func() int{
		"forward":  func() int { return count(held.All()) },
		"backward": func() int { return count(held.Backward()) },
	} {
		if got := walk(); got != 2 {
			t.Errorf("a walk stopped after 2 elements yielded %d", got)
		}
	}
}

// count consumes two elements of a sequence and abandons it, reporting how many
// the sequence offered before it noticed.
func count(seq func(func(int) bool)) int {
	seen := 0
	for range seq {
		seen++
		if seen == 2 {
			break
		}
	}
	return seen
}

// The sink appends, so it gathers rather than replaces — and a container
// appended to from its own walk is the one case where that has to hold without
// the walk noticing.
func TestTheSinkGathers(t *testing.T) {
	held := tmpl.New(1, 2)

	held.AppendSeq(slices.Values([]int{3, 4}))
	held.AppendSeq(slices.Values([]int{5}))

	if got, want := slices.Collect(held.All()), []int{1, 2, 3, 4, 5}; !slices.Equal(got, want) {
		t.Errorf("holds %v, want %v", got, want)
	}

	// A walk is over the container as it was when the walk began, so appending
	// what it yields terminates instead of feeding itself.
	held.AppendSeq(held.All())
	if got, want := held.Len(), 10; got != want {
		t.Errorf("appending its own walk left %d elements, want %d", got, want)
	}
}

// The zero container is usable: it walks, it reports no length, and it can be
// appended to. A constructor is a convenience here rather than a requirement,
// which is what the underlying type being a real slice buys.
func TestTheZeroContainer(t *testing.T) {
	var held tmpl.Slice[int]

	if got := held.Len(); got != 0 {
		t.Errorf("the zero container holds %d elements", got)
	}
	if got := slices.Collect(held.All()); len(got) != 0 {
		t.Errorf("the zero container walks %v", got)
	}
	if got := slices.Collect(held.Backward()); len(got) != 0 {
		t.Errorf("the zero container walks backward over %v", got)
	}

	held.AppendSeq(slices.Values([]int{1}))
	if got, want := held.Len(), 1; got != want {
		t.Errorf("Len() = %d, want %d", got, want)
	}
}
