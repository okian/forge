package people_test

import (
	"sync"
	"testing"

	"github.com/okian/forge/examples/people"
)

// stocked returns a roster with every slot taken, since a bounded container that
// has not filled up yet is not the state it spends its life in.
func stocked() *people.Roster {
	held := people.NewRoster()

	var size int
	held.RDo(func(v people.RosterView) { size = v.Cap() })

	held.Do(func(v people.RosterView) {
		for i := range size {
			v.Push(listed(i, "somebody"))
		}
	})

	return held
}

// A write scope, against the same write under a lock somebody wrote by hand.
//
// The pair is the claim the layer lives on: what a generated lock costs over
// the one a careful person would have written has to be nothing, because a lock
// is four lines and nobody generates four lines to pay for them.
//
// What the generated form does that the hand-written one does not is build a
// view and call through a closure, so this is the price of the arrangement that
// makes the unlocked methods unreachable. That price is the whole subject of the
// measurement.
func BenchmarkGuardedScope(b *testing.B) {
	held := stocked()
	one := listed(1, "Ada")

	b.ReportAllocs()

	for b.Loop() {
		held.Do(func(v people.RosterView) { v.Push(one) })
	}
}

// The same write, under a mutex and into a ring both written out here.
func BenchmarkGuardedScopeByHand(b *testing.B) {
	held := stockedByHand()
	one := listed(1, "Ada")

	b.ReportAllocs()

	for b.Loop() {
		held.mu.Lock()
		held.push(one)
		held.mu.Unlock()
	}
}

// A count, which is the one thing a caller reads without opening a scope.
//
// It is the cheapest thing the type does and so the one where the overhead of
// generating it would show up first: everything else a caller reaches through
// costs at least a closure, and this costs a read lock and a field read.
func BenchmarkGuardedLen(b *testing.B) {
	held := stocked()

	b.ReportAllocs()

	for b.Loop() {
		sink(held.Len())
	}
}

// The same count, under a hand-written read lock.
func BenchmarkGuardedLenByHand(b *testing.B) {
	held := stockedByHand()

	b.ReportAllocs()

	for b.Loop() {
		held.mu.RLock()
		sink(held.n)
		held.mu.RUnlock()
	}
}

// The copy, which is what a caller pays to walk something a writer may be
// changing.
//
// Unpaired, because there is nothing to pair it with: a hand-written copy of
// the same elements costs the same allocation and the same memmove, and writing
// one out would be measuring `slices.Collect` twice. What this is here for is
// the number itself — one allocation of the whole container per call — since it
// is the cost the layer's documentation tells a caller they are paying, and a
// documented cost nothing measures is a cost that drifts.
func BenchmarkGuardedSnapshot(b *testing.B) {
	held := stocked()

	b.ReportAllocs()

	for b.Loop() {
		sinkPeople(held.Snapshot())
	}
}

// The document, written the way a guarded stack writes one: the elements copied
// under the read lock, and the copy encoded with nothing held.
//
// The copy is the whole difference between this and encoding a container
// nothing is guarding, and it is deliberate — the alternative holds the lock
// against every writer for as long as the caller's writer takes, which for a
// socket that has stopped reading is as long as it takes to time out.
//
// So the figure to read here is one allocation — the snapshot — over the
// unguarded encode beside it, and what that allocation buys is that a slow
// reader cannot stop the writers.
func BenchmarkGuardedEncode(b *testing.B) {
	held := stocked()

	// Warmed, so that growing the buffer to fit the document is not divided by
	// however many iterations the run happens to do.
	var buf []byte
	var err error
	if buf, err = held.AppendJSON(buf[:0]); err != nil {
		b.Fatalf("writing the roster: %v", err)
	}

	b.ReportAllocs()

	for b.Loop() {
		if buf, err = held.AppendJSON(buf[:0]); err != nil {
			b.Fatalf("writing the roster: %v", err)
		}
	}
}

// rosterCap is what the declaration says the roster holds, written out here so
// that the hand-written comparison holds the same number of elements.
const rosterCap = 64

// lockedRing is the container the comparisons above are against: a ring and a lock,
// written the way somebody reaching for `sync.RWMutex` would write them.
//
// Deliberately the smallest thing that does the same work. What is being
// measured is what generation costs over hand-writing, so anything here that is
// not in the generated form would be measuring this file instead.
type lockedRing struct {
	mu   sync.RWMutex
	buf  []people.Person
	head int
	n    int
}

// stockedByHand returns one with every slot taken, so that both sides of every
// pair above are measured against a container in the same state.
func stockedByHand() *lockedRing {
	held := &lockedRing{buf: make([]people.Person, rosterCap)}
	for i := range rosterCap {
		held.push(listed(i, "somebody"))
	}

	return held
}

// push adds an element, dropping the oldest when the buffer is full. It takes
// no lock: every caller above takes one, which is what makes the pair a fair
// comparison against a generated method that takes its own.
func (h *lockedRing) push(v people.Person) {
	if h.n == len(h.buf) {
		h.buf[h.head] = v
		h.head = (h.head + 1) % len(h.buf)
		return
	}

	h.buf[(h.head+h.n)%len(h.buf)] = v
	h.n++
}

// sinkPeople keeps a slice from being optimised away, as [sink] does a number.
func sinkPeople(held []people.Person) { kept = len(held) }
