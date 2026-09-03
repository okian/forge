package people_test

import (
	"bytes"
	json "encoding/json/v2"
	"math/rand/v2"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/okian/forge/examples/people"
)

// twin is [people.Person] under a name that inherits none of its methods.
//
// A defined type does not take the methods of the type it is defined from, so
// this has the same fields and no codec — which is what makes a comparison
// possible at all. The standard library reflects over it, the subject dispatches
// to what forge wrote, and any disagreement between the two is a value that
// would leave one program and arrive wrong in another.
type twin people.Person

// same reports whether two people hold the same values.
//
// Written out because a Person holds a slice and so cannot be compared with ==,
// and written strictly because the difference between a nil slice and an empty
// one is a difference the wire carries: null and [] are two documents, and a
// comparison that treated them alike would let a codec turn either into the
// other and never say so.
func same(a, b people.Person) bool {
	if (a.Aliases == nil) != (b.Aliases == nil) {
		return false
	}
	return a.ID == b.ID && a.Name == b.Name && a.Email == b.Email && a.Age == b.Age &&
		slices.Equal(a.Aliases, b.Aliases)
}

// sameTwin asks the same of the type reflection is given.
func sameTwin(a, b twin) bool { return same(people.Person(a), people.Person(b)) }

// survives is the same question about a value that has been through JSON.
//
// Looser in exactly one place, and the place is the format's rather than the
// codec's: a nil slice and an empty one are both written [], by this codec and
// by the reflective one alike, so a document cannot carry the difference and a
// value read back from one cannot have it. Comparing strictly across a round
// trip would be asserting something JSON does not offer.
//
// The strict comparison is still what the differential tests use, because there
// both sides start from the same value and any difference between them is one
// of the two getting it wrong.
func survives(a, b people.Person) bool {
	a.Aliases, b.Aliases = held(a.Aliases), held(b.Aliases)
	return same(a, b)
}

// survivesTwin asks it of the type reflection is given.
func survivesTwin(a, b twin) bool { return survives(people.Person(a), people.Person(b)) }

// held returns a slice with nil and empty made alike, so that the one
// difference a document cannot carry is not compared.
func held(of []string) []string {
	if len(of) == 0 {
		return nil
	}
	return of
}

// twins returns the elements of a container as the type the standard library
// will reflect over.
func twins(held *people.Recent) []twin {
	out := make([]twin, 0, held.Len())
	for _, one := range slices.Collect(held.All()) {
		out = append(out, twin(one))
	}
	return out
}

// The twin really is reflected over, and the subject really is not.
//
// Everything below rests on this and nothing below would notice if it stopped
// being true. If the twin picked up the generated codec — by being written as an
// alias rather than a defined type, or by forge one day attaching methods
// somewhere this does not expect — then every comparison in this file would be
// the generated codec agreeing with itself, and would pass whatever it did.
func TestTheComparisonComparesTwoImplementations(t *testing.T) {
	var (
		reflected any = twin{}
		generated any = people.Person{}
	)

	if _, is := reflected.(json.Marshaler); is {
		t.Error("the twin carries a codec, so nothing here is compared against reflection")
	}
	if _, is := generated.(json.Marshaler); !is {
		t.Error("the subject carries no codec, so nothing here is comparing what forge wrote")
	}

	// And the same for the reading half, which is declared on the pointer.
	if _, is := any(&twin{}).(json.Unmarshaler); is {
		t.Error("the twin carries a reader, so nothing here is compared against reflection")
	}
	if _, is := any(&people.Person{}).(json.Unmarshaler); !is {
		t.Error("the subject carries no reader, so nothing here is comparing what forge wrote")
	}
}

// The container's document is the document the standard library writes for the
// same elements, over many shapes of value rather than one.
//
// A round trip through the generated codec alone would agree with itself about
// a wrong name, a wrong order and a wrongly quoted number. What settles it is
// the other implementation, run over values chosen to reach the corners: empty
// strings, negative and zero numbers, characters that have to be escaped, and
// lengths from none to more than the container holds.
func TestTheContainerAgreesWithReflectionOverManyValues(t *testing.T) {
	random := rand.New(rand.NewPCG(1, 2))

	for _, size := range []int{0, 1, 2, 7, 64, 1023, 1024} {
		t.Run(strconv.Itoa(size)+" elements", func(t *testing.T) {
			held := people.NewRecent()
			for range size {
				held.Push(person(random))
			}

			got, err := json.Marshal(held)
			if err != nil {
				t.Fatalf("marshaling the container: %v", err)
			}

			want, err := json.Marshal(twins(held))
			if err != nil {
				t.Fatalf("marshaling the same elements reflectively: %v", err)
			}

			if !bytes.Equal(got, want) {
				t.Errorf("the container and reflection disagree\n\tcontainer:  %s\n\treflection: %s",
					clipped(got), clipped(want))
			}
		})
	}
}

// What the container wrote reads back as what it held, for the same values.
//
// The other half of the claim: agreeing with reflection about the document says
// the document is right, and this says the document is enough to rebuild from.
// A codec can pass either alone — one that dropped a field would agree with a
// reflection over a type missing it, and one that wrote a private encoding
// would round-trip perfectly and be unreadable to anybody else.
func TestTheContainerRoundTripsManyValues(t *testing.T) {
	random := rand.New(rand.NewPCG(3, 4))

	for _, size := range []int{0, 1, 2, 7, 64, 1023, 1024} {
		t.Run(strconv.Itoa(size)+" elements", func(t *testing.T) {
			held := people.NewRecent()
			for range size {
				held.Push(person(random))
			}

			written, err := json.Marshal(held)
			if err != nil {
				t.Fatalf("marshaling: %v", err)
			}

			read := people.NewRecent()
			if err := json.Unmarshal(written, read); err != nil {
				t.Fatalf("unmarshaling: %v", err)
			}

			if got, want := slices.Collect(read.All()), slices.Collect(held.All()); !slices.EqualFunc(got, want, survives) {
				t.Errorf("%d elements did not survive the round trip", size)
			}
		})
	}
}

// A document the standard library wrote is one the container reads, which is
// the direction a round trip through one codec never reaches.
//
// It is where a reader's defects live. Writing and reading with the same code
// hides a member the writer never emits and the reader never looks for; reading
// somebody else's document does not.
func TestTheContainerReadsWhatReflectionWrote(t *testing.T) {
	random := rand.New(rand.NewPCG(5, 6))

	want := make([]twin, 0, 32)
	for range 32 {
		want = append(want, twin(person(random)))
	}

	written, err := json.Marshal(want)
	if err != nil {
		t.Fatalf("marshaling reflectively: %v", err)
	}

	held := people.NewRecent()
	if err := json.Unmarshal(written, held); err != nil {
		t.Fatalf("reading a document reflection wrote: %v", err)
	}

	if got := twins(held); !slices.EqualFunc(got, want, survivesTwin) {
		t.Errorf("read back %d elements, want %d", len(got), len(want))
	}
}

// person returns a person built from the given source of randomness.
//
// Deliberately awkward values rather than plausible ones. What a codec gets
// wrong is the edges — an empty string, a zero, a negative number, a quote or a
// backslash that has to be escaped, a rune outside ASCII — and a generator of
// realistic names would never produce one.
func person(random *rand.Rand) people.Person {
	return people.Person{
		ID:      random.IntN(2001) - 1000,
		Name:    awkward(random),
		Email:   awkward(random),
		Age:     random.IntN(2001) - 1000,
		Aliases: aliases(random),
	}
}

// aliases returns a slice that is sometimes absent, sometimes empty and
// sometimes full.
//
// All three, because they are three different documents — null, [] and a list —
// and a codec that wrote any two of them alike would be wrong in a way only a
// fixture holding all three would show.
func aliases(random *rand.Rand) []string {
	switch random.IntN(3) {
	case 0:
		return nil
	case 1:
		return []string{}
	}

	out := make([]string, random.IntN(3)+1)
	for i := range out {
		out[i] = awkward(random)
	}
	return out
}

// awkward returns a string of characters chosen to need escaping, or an empty
// one.
func awkward(random *rand.Rand) string {
	const alphabet = `ab"\/` + "\n\t\x00é \U0001F600"

	length := random.IntN(8)
	if length == 0 {
		return ""
	}

	var out strings.Builder
	for range length {
		out.WriteString(string([]rune(alphabet)[random.IntN(len([]rune(alphabet)))]))
	}
	return out.String()
}

// clipped shortens a document so that a failure over a thousand elements is
// readable.
func clipped(document []byte) string {
	const most = 300
	if len(document) <= most {
		return string(document)
	}
	return string(document[:most]) + "… (" + strconv.Itoa(len(document)) + " bytes)"
}
