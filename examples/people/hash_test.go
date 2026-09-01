package people_test

import (
	"testing"

	"github.com/okian/forge/examples/people"
)

// somebody returns the person the hash is exercised against, so that a case
// changing one field is visibly a case about that field.
func somebody() people.Person {
	return people.Person{
		ID: 1, Name: "Ada", Email: "ada@example.com", Age: 36,
		Aliases: []string{"A. Lovelace", "Ada Byron"},
	}
}

// Two values that are the same all the way down hash to the same number,
// including through the field an assignment would have shared.
//
// This is the whole claim. A [people.Person] cannot be compared with == at all,
// because it holds a slice, so without this there is no cheap way to ask
// whether two of them are the same value.
func TestTheSameValueHashesTheSame(t *testing.T) {
	held, again := somebody(), somebody()
	if held.Hash() != again.Hash() {
		t.Error("two of one value hash differently")
	}

	// Built a second way, so that what is compared is the value rather than the
	// memory it happens to be in.
	other := people.Person{ID: 1, Name: "Ada", Email: "ada@example.com", Age: 36}
	other.Aliases = append(other.Aliases, "A. Lovelace", "Ada Byron")

	if held.Hash() != other.Hash() {
		t.Error("one value built two ways hashes two ways")
	}
}

// A change to any field changes the number, which is what makes it worth taking.
func TestEveryFieldReachesTheHash(t *testing.T) {
	base := somebody().Hash()

	cases := map[string]func(*people.Person){
		"the identifier": func(p *people.Person) { p.ID = 2 },
		"the name":       func(p *people.Person) { p.Name = "Grace" },
		"the address":    func(p *people.Person) { p.Email = "grace@example.com" },
		"the age":        func(p *people.Person) { p.Age = 45 },
		"an alias":       func(p *people.Person) { p.Aliases[0] = "somebody else" },
	}

	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			held := somebody()
			change(&held)

			if held.Hash() == base {
				t.Errorf("changing %s left the hash where it was", name)
			}
		})
	}
}

// The order of a slice is part of what the value is, so reordering one changes
// the number.
func TestASliceIsHashedInOrder(t *testing.T) {
	held := somebody()

	reordered := somebody()
	reordered.Aliases = []string{"Ada Byron", "A. Lovelace"}

	if held.Hash() == reordered.Hash() {
		t.Error("two orders of one pair of aliases hash alike")
	}
}

// A slice that is not there and one that is empty are different values, and the
// hash says so.
//
// The distinction is easy to lose and expensive to lose: a caller who
// deliberately cleared a field would find it indistinguishable from one who
// never set it.
func TestNothingIsNotTheSameAsEmpty(t *testing.T) {
	missing := somebody()
	missing.Aliases = nil

	empty := somebody()
	empty.Aliases = []string{}

	if missing.Hash() == empty.Hash() {
		t.Error("no aliases and an empty list of them hash alike")
	}
}

// Hashing costs no memory, which is what makes it affordable to take on every
// lookup rather than once and cache.
func TestHashingAllocatesNothing(t *testing.T) {
	held := somebody()

	if got := testing.AllocsPerRun(100, func() { _ = held.Hash() }); got != 0 {
		t.Errorf("hashing a person allocates %v times per run", got)
	}
}

// A hash is what lets a value with no comparable form be a map key, which is
// the reason the layer exists.
func TestAHashIsAKey(t *testing.T) {
	seen := map[uint64]people.Person{}

	held := somebody()
	seen[held.Hash()] = held
	seen[somebody().Hash()] = somebody()

	if len(seen) != 1 {
		t.Errorf("one value took %d places in the map", len(seen))
	}

	other := somebody()
	other.Name = "Grace"
	seen[other.Hash()] = other

	if len(seen) != 2 {
		t.Errorf("two values took %d places in the map", len(seen))
	}
}
