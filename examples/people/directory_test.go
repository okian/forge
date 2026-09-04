package people_test

import (
	"errors"
	"fmt"
	"slices"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/okian/forge/examples/people"
)

// census returns n people with distinct IDs and a name shared by every third
// of them, which is what a secondary lookup is for.
func census(n int) []people.Person {
	out := make([]people.Person, 0, n)
	for i := range n {
		name := fmt.Sprintf("person %d", i%3)
		out = append(out, people.Person{ID: i + 1, Name: name, Email: "p@example.com", Age: 30})
	}
	return out
}

// A directory finds what it holds: the primary lookup answers a pointer to
// the element itself, and the secondary walks everyone sharing the value.
func TestADirectoryFindsWhatItHolds(t *testing.T) {
	held := people.NewDirectory(census(9)...)

	if got := held.Len(); got != 9 {
		t.Fatalf("nine people went in and Len says %d", got)
	}

	found, ok := held.ByID(4)
	if !ok || found.ID != 4 {
		t.Fatalf("ByID(4) answered %v, %t", found, ok)
	}

	if _, ok := held.ByID(40); ok {
		t.Error("an ID nothing holds was found")
	}

	var ids []int
	for p := range held.ByName("person 0") {
		ids = append(ids, p.ID)
	}
	if want := []int{1, 4, 7}; !slices.Equal(ids, want) {
		t.Errorf("the name reaches %v rather than %v", ids, want)
	}
}

// The pointer a lookup answers stays good while neighbours come and go, which
// is what lets a caller find someone and edit them in place — fields the
// lookups are not keyed by, that is.
func TestEditingThroughTheLookup(t *testing.T) {
	held := people.NewDirectory(census(5)...)

	found, ok := held.ByID(3)
	if !ok {
		t.Fatal("the person to edit is not held")
	}

	held.Remove(1)
	held.Remove(5)
	found.Age = 44

	again, ok := held.ByID(3)
	if !ok || again.Age != 44 {
		t.Errorf("the edit did not land where the lookup answers: %v, %t", again, ok)
	}
}

// An add whose key is already held is refused as the declaration's own
// sentinel, and what was added stays.
func TestADirectoryRefusesASharedKey(t *testing.T) {
	held := people.NewDirectory(census(3)...)

	err := held.AppendSeq(slices.Values([]people.Person{
		{ID: 4, Name: "late"},
		{ID: 2, Name: "later"},
		{ID: 5, Name: "unreached"},
	}))

	if !errors.Is(err, people.ErrDirectoryDuplicate) {
		t.Fatalf("a held key came back as %v rather than the sentinel", err)
	}

	if got := held.Len(); got != 4 {
		t.Errorf("the refusal changed what stays: Len says %d, want 4", got)
	}
	if found, ok := held.ByID(2); !ok || found.Name == "later" {
		t.Errorf("the element under the held key was displaced: %v, %t", found, ok)
	}
	if _, ok := held.ByID(5); ok {
		t.Error("the sequence was read past the element that was refused")
	}
}

// Removal unfiles the element everywhere: the primary misses, the secondary
// no longer yields it, the walk no longer covers it, and the key is free to
// be held again.
func TestRemovalRepairsTheLookups(t *testing.T) {
	held := people.NewDirectory(census(6)...)

	if !held.Remove(4) {
		t.Fatal("removing a held key reported nothing held")
	}
	if held.Remove(4) {
		t.Error("removing it again reported something held")
	}

	if _, ok := held.ByID(4); ok {
		t.Error("a removed ID is still found")
	}
	for p := range held.ByName("person 0") {
		if p.ID == 4 {
			t.Error("a removed person still answers to their name")
		}
	}
	if got := held.Len(); got != 5 {
		t.Errorf("one of six was removed and Len says %d", got)
	}

	if err := held.AppendSeq(slices.Values([]people.Person{{ID: 4, Name: "returned"}})); err != nil {
		t.Errorf("a removed key is not free to be held again: %v", err)
	}
}

// Reset empties every way in at once, and the directory fills again without
// complaint.
func TestADirectoryResets(t *testing.T) {
	held := people.NewDirectory(census(4)...)

	held.Reset()

	if got := held.Len(); got != 0 {
		t.Fatalf("a reset directory holds %d", got)
	}
	if _, ok := held.ByID(1); ok {
		t.Error("a reset directory still finds someone")
	}

	if err := held.AppendSeq(slices.Values(census(4))); err != nil {
		t.Errorf("refilling after a reset was refused: %v", err)
	}
	if got := held.Len(); got != 4 {
		t.Errorf("the refill left %d of 4", got)
	}
}

// The registry under contention: writers add and remove through the write
// scope, readers look up and walk through the read scope, and the counts
// prove the scopes ran rather than merely locked.
//
// The pointer a lookup answers is used inside the scope and dropped before it
// ends, which is the discipline the scoped access exists to make easy.
func TestARegistryUnderContention(t *testing.T) {
	var (
		held    people.Registry
		wrote   atomic.Int64
		read    atomic.Int64
		started sync.WaitGroup
	)

	const (
		writers = 4
		readers = 4
		rounds  = 200
	)

	started.Add(writers + readers)

	for w := range writers {
		go func() {
			defer started.Done()
			for i := range rounds {
				id := w*rounds + i + 1
				held.Do(func(v people.RegistryView) {
					if err := v.AppendSeq(slices.Values([]people.Person{{ID: id, Name: "held"}})); err == nil {
						wrote.Add(1)
					}
					if i%3 == 0 && v.Remove(id) {
						wrote.Add(1)
					}
				})
			}
		}()
	}

	for range readers {
		go func() {
			defer started.Done()
			for i := range rounds {
				held.RDo(func(v people.RegistryView) {
					if _, ok := v.ByID(i + 1); ok {
						read.Add(1)
					}
					for range v.All() {
						read.Add(1)
						break
					}
				})
				_ = held.Len()
				if i%50 == 0 {
					if _, err := held.MarshalJSON(); err != nil {
						t.Errorf("the locked codec refused: %v", err)
					}
					_ = held.Snapshot()
				}
			}
		}()
	}

	started.Wait()

	if wrote.Load() == 0 {
		t.Error("no write scope did any writing")
	}
	if read.Load() == 0 {
		t.Error("no read scope did any reading")
	}

	want := writers * rounds
	removed := 0
	held.RDo(func(v people.RegistryView) {
		removed = want - v.Len()
	})
	if removed <= 0 {
		t.Errorf("%d adds and no removals landed", want)
	}
}
