package guarded_test

import (
	"strings"
	"testing"

	"github.com/okian/forge/internal/generate"
	"github.com/okian/forge/internal/goldentest"
	"github.com/okian/forge/plugin"
)

// The two stacks a lock is written for, generated whole and recorded.
//
// Whole rather than from the layer alone, because everything this layer writes
// is written against something another layer produced: the type it holds, the
// walk a snapshot collects, the codec the elements carry. A recording of its
// unit on its own would be a recording of half a package, and the half it left
// out is the half that decides whether the other one compiles.
//
// Two stacks and not one. A ring is a container that has to be told how big it
// is and shares a buffer between its elements; a slice is neither. What a lock
// writes is the same for both and what it writes it *around* is not, so a pack
// with one of them in it would record a lock over one kind of container and
// call it a lock.
func TestALockOverTheStacksItIsFor(t *testing.T) {
	cases := map[string]struct {
		markers    []string
		directives []string

		// subject is what the stack is over, and is the plain one where nothing
		// says otherwise.
		subject *plugin.Struct
	}{
		// The composition M4.5 exists for: a bounded container of subjects that
		// carry a codec, behind a lock. Every part of this layer is exercised
		// by it — the type it holds, the scope over that type, the snapshot,
		// and the codec written from the snapshot.
		"a bounded container of encodable elements": {
			markers:    []string{"Guarded", "Ring", "Json"},
			directives: []string{"//forge:ring cap=8"},
		},

		// Shaped like the cache the catalog will hold when there is one. LRU
		// stands in as a slice until it is written: what this is here for is a
		// lock over a container that is not bounded and whose elements have no
		// codec, which is the other half of what a lock has to write for.
		"an unbounded container of plain elements": {
			markers: []string{"Guarded", "Slice"},
		},

		// A refining layer beneath the lock, which is what puts signatures the
		// lock did not write into the scope it hands over. A projection is
		// named after a field and spelled with that field's type, so the
		// subject's timestamp arrives as a method naming a package this layer
		// knows nothing about — and the file has to bind it, which is the
		// collection's doing rather than the lock's.
		"a container with a query API under it": {
			markers: []string{"Guarded", "Collection", "Slice"}, subject: timestamped(),
		},
	}

	for name, held := range cases {
		t.Run(name, func(t *testing.T) {
			subject, source := person(), subjectSource
			if held.subject != nil {
				subject, source = held.subject, timestampedSource
			}

			files, diags := generate.Package(local, "model",
				[]generate.Request{of(subject, "Persons", held.markers, held.directives...)}, config())

			if !diags.Empty() {
				t.Fatalf("generating was refused:\n%s", diags.Render())
			}

			sources := []goldentest.Source{{Name: "person.go", Content: []byte(source)}}
			for _, file := range files {
				sources = append(sources, goldentest.Source{Name: file.Name, Content: file.Content, Generated: true})
			}

			goldentest.Check(t, goldentest.Package{Path: "model", Files: sources})
		})
	}
}

// The declaration that asked for the lock to be held for the length of a write
// gets that, and the one that asked for the lock itself gets that.
//
// Recorded rather than asserted on, because both options change a method body
// rather than a method set: what is worth reviewing is which lines run inside
// the lock, and a test spelling that out in assertions would be spelling out
// the generated code a second time.
func TestALockWrittenTheOtherWayRound(t *testing.T) {
	files, diags := generate.Package(local, "model",
		[]generate.Request{over("Persons",
			[]string{"Guarded", "Ring", "Json"},
			"//forge:ring cap=8",
			"//forge:guarded encode=locked expose=locker")}, config())

	if !diags.Empty() {
		t.Fatalf("generating was refused:\n%s", diags.Render())
	}

	sources := []goldentest.Source{{Name: "person.go", Content: []byte(subjectSource)}}
	for _, file := range files {
		sources = append(sources, goldentest.Source{Name: file.Name, Content: file.Content, Generated: true})
	}

	goldentest.Check(t, goldentest.Package{Path: "model", Files: sources})
}

// A lock over a container whose size is the caller's takes that size and passes
// it on.
//
// The other half of what a way in has to do. A container told its size in the
// declaration is made with no arguments; one whose size is the caller's takes
// it — and the lock writes neither of those itself, it writes whichever the
// layer beneath it declared. Asserted rather than recorded, because what is
// being checked is the one line that differs between the two.
func TestALockOverAContainerTheCallerSizes(t *testing.T) {
	held := generating(t, over("Persons", []string{"Guarded", "Ring"}))

	for _, want := range []string{
		"func NewPersons(size int) *Persons {",
		"return &Persons{held: *newPersonsHeld(size)}",
	} {
		if !strings.Contains(held, want) {
			t.Errorf("the output does not hold %q:\n%s", want, held)
		}
	}
}

// A lock over a container that is ready as its zero value gets no way in, and
// says so.
//
// Not an omission. A constructor that only wrapped a zero value would be a call
// a caller has to learn in order to write what a composite literal already
// says, and the type's own documentation would then be pointing at it.
func TestALockOverAContainerThatNeedsNoMaking(t *testing.T) {
	held := generating(t, over("Persons", []string{"Guarded", "Slice"}))

	if strings.Contains(held, "func NewPersons(") {
		t.Errorf("a container that is ready as its zero value was given a way in:\n%s", held)
	}
	if !strings.Contains(held, "The zero value is ready to use") {
		t.Errorf("the type does not say its zero value is ready:\n%s", held)
	}
}

// generating returns the declaration's own generated file, as source.
func generating(t *testing.T, req generate.Request) string {
	t.Helper()

	files, diags := generate.Package(local, "model", []generate.Request{req}, config())
	if !diags.Empty() {
		t.Fatalf("generating was refused:\n%s", diags.Render())
	}

	for _, file := range files {
		if file.Name == generate.Name() {
			return string(file.Content)
		}
	}

	t.Fatalf("nothing was written for %s", req.Model.Name)
	return ""
}
