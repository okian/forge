package racetest_test

import (
	"strings"
	"testing"

	"github.com/okian/forge/internal/goldentest"
	"github.com/okian/forge/internal/racetest"
)

// asked is a declaration offering everything a stress test can exercise.
func asked() racetest.Asked {
	return racetest.Asked{
		Package:   "model",
		Declared:  "Persons",
		Make:      "NewPersons(64)",
		Elem:      "Person",
		View:      "PersonsView",
		Scope:     "Do",
		ReadScope: "RDo",
		Walk:      "All",
		Append:    "AppendSeq",
		Reads:     []string{"Len", "Snapshot"},
		Encodes:   "MarshalJSON",
	}
}

// What the harness writes, for a declaration offering everything and for one
// offering the least it can.
//
// Recorded rather than asserted on, because what this produces is source
// somebody has to read: the thing worth reviewing when it changes is the test
// that will run, not a list of the substrings in it.
//
// Both ends of the range, because what the two have in common is the whole of
// what a concurrent layer is required to offer, and what they differ by is
// everything a declaration may or may not turn out to have.
func TestWhatTheHarnessWrites(t *testing.T) {
	cases := map[string]racetest.Asked{
		"a declaration offering everything": asked(),

		// The least a stress test can be written against: no way in but the
		// zero value, no count, no copy, no codec. What is left is the scoped
		// access itself, which is the one thing a concurrent layer cannot be
		// without.
		"a declaration offering the least": {
			Package:   "model",
			Declared:  "Persons",
			Elem:      "Person",
			View:      "PersonsView",
			Scope:     "Do",
			ReadScope: "RDo",
			Walk:      "All",
			Append:    "AppendSeq",
		},
	}

	for name, held := range cases {
		t.Run(name, func(t *testing.T) {
			out, err := racetest.Write(held)
			if err != nil {
				t.Fatalf("writing the stress test: %v", err)
			}

			goldentest.Check(t, goldentest.Package{
				Path: "model",
				Files: []goldentest.Source{
					{Name: "container.go", Content: []byte(container)},
					{Name: "stress_test.go", Content: out, Generated: true},
				},
			})
		})
	}
}

// container is a stand-in for what a concurrent layer generates, so that what
// the harness writes is compiled rather than only parsed.
//
// It exists because parsing is not enough and the difference is not academic:
// the first version of this harness wrote `&held.Do(...)`, which go/parser
// accepts as an expression and go/types refuses as a statement, and it was
// recorded as a golden nobody could have read as wrong.
//
// Written by hand rather than generated. What is under test here is the
// harness, and generating the thing it is tested against would make one change
// able to move both sides at once — which the matrix beside this already does
// deliberately, against the real layers.
const container = `package model

import (
	"iter"
	"slices"
)

// Person is what the container holds.
type Person struct {
	ID   int
	Name string
}

// Persons is the container, behind whatever a concurrent layer would put there.
type Persons struct {
	held []Person
}

// NewPersons returns one holding at most the given number of elements.
func NewPersons(size int) *Persons { return &Persons{held: make([]Person, 0, size)} }

// Do and RDo are the two ways in.
func (p *Persons) Do(f func(v PersonsView))  { f(PersonsView{held: &p.held}) }
func (p *Persons) RDo(f func(v PersonsView)) { f(PersonsView{held: &p.held}) }

// Len and Snapshot are what a caller reaches without opening a scope.
func (p *Persons) Len() int         { return len(p.held) }
func (p *Persons) Snapshot() []Person { return slices.Clone(p.held) }

// MarshalJSON writes the container as a JSON array.
func (p *Persons) MarshalJSON() ([]byte, error) { return []byte("[]"), nil }

// PersonsView is what a scope hands over.
type PersonsView struct {
	held *[]Person
}

func (v PersonsView) All() iter.Seq[Person]              { return slices.Values(*v.held) }
func (v PersonsView) AppendSeq(seq iter.Seq[Person])     { *v.held = slices.AppendSeq(*v.held, seq) }
`

// A declaration missing any part of the contract is refused, and the refusal
// says which part.
//
// Refused rather than written shorter. A concurrent layer with no stress test
// is what this package exists to prevent, and a test that quietly exercised
// half of one would be the same outcome with a green tick on it — so a layer
// whose surface is something other than scoped access has to be noticed, not
// worked around.
func TestADeclarationTheHarnessCannotWriteFor(t *testing.T) {
	cases := map[string]func(*racetest.Asked){
		"no package":     func(of *racetest.Asked) { of.Package = "" },
		"no type":        func(of *racetest.Asked) { of.Declared = "" },
		"no element":     func(of *racetest.Asked) { of.Elem = "" },
		"no view":        func(of *racetest.Asked) { of.View = "" },
		"no write scope": func(of *racetest.Asked) { of.Scope = "" },
		"no read scope":  func(of *racetest.Asked) { of.ReadScope = "" },
		"no walk":        func(of *racetest.Asked) { of.Walk = "" },
		"no way to add":  func(of *racetest.Asked) { of.Append = "" },
	}

	for name, without := range cases {
		t.Run(name, func(t *testing.T) {
			held := asked()
			without(&held)

			out, err := racetest.Write(held)
			if err == nil {
				t.Fatalf("a declaration with %s was written for:\n%s", name, out)
			}
			if !strings.Contains(err.Error(), "stress test needs") {
				t.Errorf("the refusal does not say what is missing: %v", err)
			}
		})
	}
}

// The declarations the matrix generates from are written from the layers it
// covers.
func TestTheDeclarationsTheHarnessWrites(t *testing.T) {
	out, err := racetest.Spec("model", "github.com/okian/forge", []racetest.Declared{
		{
			Name: "GuardedPersons", Layer: "Guarded", Subject: "Person",
			Stack: []string{"Guarded", "Ring", "Json"}, Directives: []string{"forge:ring cap=64"},
		},
		{
			// A second one, so that the file is exercised as a file rather than
			// as one declaration with a package clause above it.
			Name: "AtomicPersons", Layer: "Atomic", Subject: "Person",
			Stack: []string{"Atomic", "Slice"},
		},
	})
	if err != nil {
		t.Fatalf("writing the declarations: %v", err)
	}

	goldentest.Compare(t, "spec.go", out)
}

// A spec file nothing could be generated from is refused.
func TestDeclarationsTheHarnessCannotWrite(t *testing.T) {
	one := racetest.Declared{
		Name: "GuardedPersons", Layer: "Guarded", Subject: "Person", Stack: []string{"Guarded", "Slice"},
	}

	cases := map[string]struct {
		pkg, marker string
		held        []racetest.Declared
	}{
		"no package":      {marker: "example.com/forge", held: []racetest.Declared{one}},
		"no markers":      {pkg: "model", held: []racetest.Declared{one}},
		"no declarations": {pkg: "model", marker: "example.com/forge"},
		"a declaration with no name": {
			pkg: "model", marker: "example.com/forge",
			held: []racetest.Declared{{Layer: "Guarded", Subject: "Person", Stack: []string{"Guarded"}}},
		},
		"a declaration with no stack": {
			pkg: "model", marker: "example.com/forge",
			held: []racetest.Declared{{Name: "GuardedPersons", Layer: "Guarded", Subject: "Person"}},
		},
	}

	for name, held := range cases {
		t.Run(name, func(t *testing.T) {
			out, err := racetest.Spec(held.pkg, held.marker, held.held)
			if err == nil {
				t.Fatalf("%s was written for:\n%s", name, out)
			}
		})
	}
}
