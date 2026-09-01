package people_test

import (
	"bytes"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"slices"
	"sync"
	"testing"

	"github.com/okian/forge/examples/people"
)

// The lock is a way to the container and the only way to it.
//
// What is on the outside is what can be answered without holding anything open:
// how many there are, a copy of them, and the document. Everything that walks
// or changes the container is reached through a scope, which takes the lock for
// as long as the call lasts and hands over a view with no way back out.
func TestWhatAScopeReaches(t *testing.T) {
	held := people.NewRoster()

	held.Do(func(v people.RosterView) {
		v.Push(listed(1, "Ada"))
		v.Push(listed(2, "Grace"))
	})

	if got := held.Len(); got != 2 {
		t.Errorf("the roster holds %d, want 2", got)
	}

	var read []string
	held.RDo(func(v people.RosterView) {
		for who := range v.All() {
			read = append(read, who.Name)
		}
	})

	if want := []string{"Ada", "Grace"}; !slices.Equal(read, want) {
		t.Errorf("a read scope walked %v, want %v", read, want)
	}
}

// A snapshot is nobody else's, so it can be walked with nothing held.
//
// The copy is the price of walking something a writer may be changing, and the
// alternative is not a cheaper walk but a walk that races. What this checks is
// that the copy is one: changing the roster afterwards leaves what was taken
// where it was.
func TestASnapshotIsACopy(t *testing.T) {
	held := people.NewRoster()
	held.Do(func(v people.RosterView) { v.Push(listed(1, "Ada")) })

	taken := held.Snapshot()
	held.Do(func(v people.RosterView) { v.Push(listed(2, "Grace")) })

	if len(taken) != 1 || taken[0].Name != "Ada" {
		t.Errorf("the snapshot changed under the roster: %v", taken)
	}
	if got := held.Len(); got != 2 {
		t.Errorf("the roster holds %d, want 2", got)
	}
}

// The bounded container is still bounded behind the lock, and still drops the
// oldest element rather than growing.
func TestTheRingIsStillARing(t *testing.T) {
	held := people.NewRoster()

	var size int
	held.RDo(func(v people.RosterView) { size = v.Cap() })

	held.Do(func(v people.RosterView) {
		for i := range size + 4 {
			v.Push(listed(i, "somebody"))
		}
	})

	if got := held.Len(); got != size {
		t.Errorf("the roster holds %d, want its capacity of %d", got, size)
	}

	// The four oldest went, so the first one left is the fifth pushed.
	taken := held.Snapshot()
	if len(taken) == 0 {
		t.Fatal("a full roster snapshotted to nothing")
	}
	if taken[0].ID != 4 {
		t.Errorf("the roster kept the wrong elements: the first is %v", taken[0])
	}
}

// The document is the elements as a JSON array, and it round-trips.
//
// The composition M4.5 exists for. The codec for a [people.Person] is the one
// the Json layer wrote; the codec for the container is the lock's, because the
// lock took the walk away and left the layer that writes codecs with nothing to
// write one over. What comes out has to be what the same elements would produce
// anywhere else, which is what comparing against a plain slice checks.
func TestTheDocumentAGuardedRingWrites(t *testing.T) {
	held := people.NewRoster()
	written := []people.Person{listed(1, "Ada"), listed(2, "Grace")}

	held.Do(func(v people.RosterView) { v.AppendSeq(slices.Values(written)) })

	var out bytes.Buffer
	if err := held.MarshalJSONTo(jsontext.NewEncoder(&out)); err != nil {
		t.Fatalf("writing the roster: %v", err)
	}

	want, err := json.Marshal(written)
	if err != nil {
		t.Fatalf("writing the same elements as a slice: %v", err)
	}

	if got := bytes.TrimSpace(out.Bytes()); !bytes.Equal(got, bytes.TrimSpace(want)) {
		t.Errorf("the roster wrote %s, and the same elements as a slice are %s", got, want)
	}

	// And it reads back, through the ordinary decoder into an ordinary slice,
	// which is the whole of what "it is a JSON array" means.
	var read []people.Person
	if err := json.Unmarshal(out.Bytes(), &read); err != nil {
		t.Fatalf("reading the document back: %v", err)
	}
	// Through the comparison the codec tests use for a round trip: a nil slice
	// and an empty one are both written as [], so a value that has been through
	// a document cannot carry the difference between them.
	if !slices.EqualFunc(read, written, survives) {
		t.Errorf("the document read back as %v, want %v", read, written)
	}
}

// An empty roster writes an empty array rather than null.
func TestTheDocumentAnEmptyRosterWrites(t *testing.T) {
	var out bytes.Buffer
	if err := people.NewRoster().MarshalJSONTo(jsontext.NewEncoder(&out)); err != nil {
		t.Fatalf("writing an empty roster: %v", err)
	}

	if got := string(bytes.TrimSpace(out.Bytes())); got != "[]" {
		t.Errorf("an empty roster wrote %s, want []", got)
	}
}

// Writers and readers run against one roster at once, which is what the lock is
// for and what `go test -race` is the check on.
//
// Every reader here takes the lock for a different length of time: a count is
// one field read, a snapshot is a copy, a scope runs a caller's function, and
// the document is a copy and then a write. What is asserted afterwards is only
// that the count is the capacity — the failure worth catching is a race, and
// the detector is what reports it.
func TestManyGoroutinesAgainstOneRoster(t *testing.T) {
	held := people.NewRoster()

	var size int
	held.RDo(func(v people.RosterView) { size = v.Cap() })

	var wg sync.WaitGroup
	for w := range 4 {
		wg.Go(func() {
			for i := range size {
				held.Do(func(v people.RosterView) { v.Push(listed(w*size+i, "somebody")) })
			}
		})
	}

	for range 4 {
		wg.Go(func() {
			for range size {
				_ = held.Len()
				_ = len(held.Snapshot())

				held.RDo(func(v people.RosterView) {
					for range v.All() {
						continue
					}
				})

				var out bytes.Buffer
				if err := held.MarshalJSONTo(jsontext.NewEncoder(&out)); err != nil {
					t.Errorf("writing the roster: %v", err)
					return
				}
			}
		})
	}

	wg.Wait()

	if got := held.Len(); got != size {
		t.Errorf("the roster holds %d after four writers filled it, want %d", got, size)
	}
}

// listed builds a valid element, since what these tests are about is the
// container rather than what it holds.
func listed(id int, name string) people.Person {
	return people.Person{ID: id, Name: name, Email: "somebody@example.com", Age: 30}
}
