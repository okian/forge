package validator_test

import (
	"testing"

	playground "github.com/go-playground/validator/v10"

	"github.com/okian/forge/examples/people"
)

// twin is the example's subject with the same rules written the other way.
//
// The same fields in the same order, and the same rules with one exception,
// because a comparison between two checks that check different things measures
// nothing. Written as a type of its own rather than by adding a second tag to
// the subject, so that the example stays an example and this stays a
// measurement.
//
// The exception is Email, and it is not one that can be removed. The subject
// asks for a pattern, which forge compiles to a match against it.
// go-playground has no pattern rule in its tag vocabulary, so the nearest
// thing it offers is `email`, and that parses the address to RFC 5322 with
// net/mail instead. Those are different amounts of work, and five of the six
// allocations the reflective check makes are inside that one rule.
//
// So read the figures as two checks over the rules each side can express,
// which is the comparison somebody choosing between them actually faces —
// not as the same work done twice.
type twin struct {
	ID    int    `validate:"min=1"`
	Name  string `validate:"required,max=64"`
	Email string `validate:"required,email"`
	Age   int    `validate:"min=0,max=150"`
}

// valid is a value that satisfies the rules, which is the path a check spends
// its life on: validation runs on every request and almost always finds nothing.
func valid() (people.Person, twin) {
	return people.Person{ID: 1, Name: "Ada", Email: "ada@example.com", Age: 36},
		twin{ID: 1, Name: "Ada", Email: "ada@example.com", Age: 36}
}

// The two agree about a value that satisfies the rules, which is what makes the
// benchmarks below a comparison rather than two unrelated numbers.
//
// A benchmark of a check that accepts everything is a benchmark of nothing, and
// the mistake is invisible in the figures: it would simply look fast.
func TestBothAcceptTheSameValue(t *testing.T) {
	held, other := valid()

	if err := held.Validate(); err != nil {
		t.Errorf("the generated check refused a valid value: %v", err)
	}
	if err := playground.New().Struct(other); err != nil {
		t.Errorf("the reflective check refused a valid value: %v", err)
	}
}

// And both refuse a value that does not, field by field, so that neither is
// measured while quietly checking less than the other.
func TestBothRefuseTheSameValues(t *testing.T) {
	cases := map[string]func(*people.Person, *twin){
		"an unnumbered person": func(p *people.Person, o *twin) { p.ID, o.ID = 0, 0 },
		"a nameless person":    func(p *people.Person, o *twin) { p.Name, o.Name = "", "" },
		"no address at all":    func(p *people.Person, o *twin) { p.Email, o.Email = "", "" },
		"an address with no at": func(p *people.Person, o *twin) {
			p.Email, o.Email = "ada", "ada"
		},
		"an implausible age": func(p *people.Person, o *twin) { p.Age, o.Age = 151, 151 },
	}

	checker := playground.New()

	for name, spoil := range cases {
		t.Run(name, func(t *testing.T) {
			held, other := valid()
			spoil(&held, &other)

			if held.Validate() == nil {
				t.Error("the generated check accepted it")
			}
			if checker.Struct(other) == nil {
				t.Error("the reflective check accepted it")
			}
		})
	}
}

// The generated check over a value that satisfies the rules.
func BenchmarkGenerated(b *testing.B) {
	held, _ := valid()

	b.ReportAllocs()

	for b.Loop() {
		if held.Validate() != nil {
			b.Fatal("the fixture does not satisfy its own rules")
		}
	}
}

// The reflective check over the same value and the same rules.
//
// The validator is built once, outside the loop, which is how it is meant to be
// used and what its own documentation says: it caches what it learns about a
// type, and building one per call would measure the cache being filled rather
// than the check being made.
func BenchmarkReflective(b *testing.B) {
	_, other := valid()
	checker := playground.New()

	// Warmed, for the same reason: the first call over a type is the one that
	// reads its tags, and a benchmark that included it would report that work
	// divided by however many iterations the run happened to do.
	if err := checker.Struct(other); err != nil {
		b.Fatalf("the fixture does not satisfy its own rules: %v", err)
	}

	b.ReportAllocs()

	for b.Loop() {
		if checker.Struct(other) != nil {
			b.Fatal("the fixture does not satisfy its own rules")
		}
	}
}
