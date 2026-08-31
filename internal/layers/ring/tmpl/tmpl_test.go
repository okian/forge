package tmpl_test

import (
	"errors"
	"slices"
	"testing"

	"github.com/okian/forge/internal/layers/ring/tmpl"
)

// A template is the bodies a layer emits, so testing it here is testing what
// every declaration using this layer will run. The alternative is finding out
// through a golden file, which says the output changed and not whether it
// works — and wrapping is the kind of thing a golden file cannot notice going
// wrong, because the output that computes it wrongly looks exactly the same.

// A new container holds nothing, and holds room for what it was asked for.
func TestWhatAConstructorReturns(t *testing.T) {
	held := tmpl.New[int](3)

	if got := held.Len(); got != 0 {
		t.Errorf("a new container holds %d elements", got)
	}
	if got := held.Cap(); got != 3 {
		t.Errorf("a container asked for 3 has room for %d", got)
	}
	if got := slices.Collect(held.All()); len(got) != 0 {
		t.Errorf("a new container walks %v", got)
	}
}

// A capacity that could never hold anything is a mistake at the call that made
// it, rather than a container that silently keeps nothing.
func TestAConstructorGivenNoRoom(t *testing.T) {
	for _, capacity := range []int{0, -1} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("a container asked for %d was built anyway", capacity)
				}
			}()
			_ = tmpl.New[int](capacity)
		}()
	}
}

// The fixed constructor takes its capacity from the declaration rather than
// from its caller.
func TestTheFixedConstructor(t *testing.T) {
	if got := tmpl.NewFixed[int]().Cap(); got <= 0 {
		t.Errorf("a container built to a declared size has room for %d", got)
	}
}

// Elements come back oldest first, whatever they have been through.
//
// The walk is the whole of what a layer above this one sees, and it is where
// wrapping either works or quietly does not: a container that has been pushed
// to more times than it holds has its oldest element somewhere in the middle of
// its buffer, and every position it reports is that offset plus an index that
// has to wrap exactly once.
func TestWhatTheWalkYields(t *testing.T) {
	cases := map[string]struct {
		pushed []int
		want   []int
	}{
		"fewer than it holds":    {pushed: []int{1, 2}, want: []int{1, 2}},
		"exactly what it holds":  {pushed: []int{1, 2, 3}, want: []int{1, 2, 3}},
		"one more than it holds": {pushed: []int{1, 2, 3, 4}, want: []int{2, 3, 4}},

		// Round and round: more pushes than the buffer has slots, twice over,
		// so head has wrapped more than once and no arithmetic that happens to
		// work for the first lap survives by accident.
		"several times over": {
			pushed: []int{1, 2, 3, 4, 5, 6, 7, 8},
			want:   []int{6, 7, 8},
		},
	}

	for name, want := range cases {
		t.Run(name, func(t *testing.T) {
			held := tmpl.New[int](3)
			for _, v := range want.pushed {
				held.Push(v)
			}

			if got := held.Len(); got != len(want.want) {
				t.Errorf("holds %d elements, want %d", got, len(want.want))
			}
			if got := slices.Collect(held.All()); !slices.Equal(got, want.want) {
				t.Errorf("walks %v, want %v", got, want.want)
			}

			backward := slices.Clone(want.want)
			slices.Reverse(backward)
			if got := slices.Collect(held.Backward()); !slices.Equal(got, backward) {
				t.Errorf("walks backward %v, want %v", got, backward)
			}
		})
	}
}

// A walk stopped early stops, and stops where it was told to.
func TestAWalkTheCallerStops(t *testing.T) {
	held := tmpl.New[int](3)
	for _, v := range []int{1, 2, 3, 4} {
		held.Push(v)
	}

	var seen []int
	for v := range held.All() {
		seen = append(seen, v)
		if len(seen) == 2 {
			break
		}
	}
	if want := []int{2, 3}; !slices.Equal(seen, want) {
		t.Errorf("a walk stopped after two yielded %v, want %v", seen, want)
	}

	seen = nil
	for v := range held.Backward() {
		seen = append(seen, v)
		break
	}
	if want := []int{4}; !slices.Equal(seen, want) {
		t.Errorf("a backward walk stopped after one yielded %v, want %v", seen, want)
	}
}

// A walk is fixed when it is asked for, so pushing during one neither extends
// it nor makes it revisit a slot it has already been to.
func TestPushingDuringAWalk(t *testing.T) {
	held := tmpl.New[int](3)
	for _, v := range []int{1, 2, 3} {
		held.Push(v)
	}

	var seen []int
	for v := range held.All() {
		seen = append(seen, v)
		held.Push(len(seen) * 100)
	}

	if want := []int{1, 2, 3}; !slices.Equal(seen, want) {
		t.Errorf("a walk pushed to during it yielded %v, want %v", seen, want)
	}
}

// The checked push refuses rather than overwriting, and says so in a way a
// caller can act on.
func TestTheCheckedPush(t *testing.T) {
	held := tmpl.New[int](2)

	for _, v := range []int{1, 2} {
		if err := held.PushChecked(v); err != nil {
			t.Fatalf("pushing %d into a container with room: %v", v, err)
		}
	}

	err := held.PushChecked(3)
	if err == nil {
		t.Fatal("a full container took another element")
	}

	// One value rather than one per call, so a caller can match it. A refusal
	// that could only be printed would leave the caller comparing strings, and
	// acting on the answer is the whole reason for refusing.
	if again := held.PushChecked(4); !errors.Is(again, err) {
		t.Errorf("two refusals are two different errors: %v and %v", err, again)
	}
	if got := slices.Collect(held.All()); !slices.Equal(got, []int{1, 2}) {
		t.Errorf("a refused push changed the container to %v", got)
	}
}

// Appending a sequence is pushing each of its elements, and follows whichever
// policy the pushes do.
func TestAppendingASequence(t *testing.T) {
	held := tmpl.New[int](3)
	held.AppendSeq(slices.Values([]int{1, 2, 3, 4, 5}))

	if got := slices.Collect(held.All()); !slices.Equal(got, []int{3, 4, 5}) {
		t.Errorf("appending five to a container of three left %v", got)
	}

	checked := tmpl.New[int](3)
	if err := checked.AppendSeqChecked(slices.Values([]int{1, 2})); err != nil {
		t.Fatalf("appending two to a container of three: %v", err)
	}

	err := checked.AppendSeqChecked(slices.Values([]int{3, 4, 5}))
	if err == nil {
		t.Fatal("appending past the end of a container was allowed")
	}

	// What fitted stays. Stopping is not undoing, and a caller that wanted all
	// or nothing has to ask before appending rather than after.
	if got := slices.Collect(checked.All()); !slices.Equal(got, []int{1, 2, 3}) {
		t.Errorf("a refused append left %v", got)
	}
}

// An empty sequence is not a refusal.
func TestAppendingNothing(t *testing.T) {
	held := tmpl.New[int](1)

	if err := held.AppendSeqChecked(slices.Values([]int(nil))); err != nil {
		t.Errorf("appending nothing was refused: %v", err)
	}
	if got := held.Len(); got != 0 {
		t.Errorf("appending nothing left %d elements", got)
	}
}

// A container that was never constructed says so, whichever policy it has.
//
// Forgetting the constructor is the likeliest mistake with this type, and the
// two policies would otherwise answer it differently: one indexes an empty
// slice and panics on its own, and the other reports itself full — which a
// caller reading back-pressure retries for ever.
func TestAContainerThatWasNeverConstructed(t *testing.T) {
	for name, push := range map[string]func(*tmpl.Ring[int]){
		"overwriting": func(r *tmpl.Ring[int]) { r.Push(1) },
		"refusing":    func(r *tmpl.Ring[int]) { _ = r.PushChecked(1) },
	} {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Error("adding to a container that has no buffer was allowed")
				}
			}()
			push(&tmpl.Ring[int]{})
		})
	}
}

// Reading one is not an error, and neither is asking what it holds. Only adding
// to it is, because only adding has nowhere to put anything.
func TestReadingAContainerThatWasNeverConstructed(t *testing.T) {
	var held tmpl.Ring[int]

	if got := held.Len(); got != 0 {
		t.Errorf("it holds %d elements", got)
	}
	if got := held.Cap(); got != 0 {
		t.Errorf("it has room for %d", got)
	}
	if got := slices.Collect(held.All()); len(got) != 0 {
		t.Errorf("it walks %v", got)
	}
	if got := slices.Collect(held.Backward()); len(got) != 0 {
		t.Errorf("it walks backward %v", got)
	}
}
