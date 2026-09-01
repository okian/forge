package people_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/okian/forge/examples/people"
)

// valid is a person who satisfies every rule written on the subject, and is
// what each case below spoils one field of.
func valid() people.Person {
	return people.Person{ID: 1, Name: "Ada", Email: "ada@example.com", Age: 36}
}

// A person who satisfies the rules is reported as satisfying them.
//
// The first thing to check and the easiest to get wrong: a check that refused
// everything would pass every test below and be worse than no check at all.
func TestAValidPersonIsValid(t *testing.T) {
	if err := valid().Validate(); err != nil {
		t.Errorf("a valid person was refused: %v", err)
	}
}

// Each rule refuses what it is written to refuse, and says which field and
// which rule.
func TestWhatEachRuleRefuses(t *testing.T) {
	cases := map[string]struct {
		spoil func(*people.Person)
		field string
		rule  string
	}{
		"an unnumbered person":  {func(p *people.Person) { p.ID = 0 }, "ID", "min=1"},
		"a nameless person":     {func(p *people.Person) { p.Name = "" }, "Name", "required"},
		"an over-long name":     {func(p *people.Person) { p.Name = strings.Repeat("a", 65) }, "Name", "max=64"},
		"no address at all":     {func(p *people.Person) { p.Email = "" }, "Email", "required"},
		"an address with no at": {func(p *people.Person) { p.Email = "ada" }, "Email", "regexp"},
		"an address with a gap": {func(p *people.Person) { p.Email = "a da@example.com" }, "Email", "regexp"},
		"a negative age":        {func(p *people.Person) { p.Age = -1 }, "Age", "min=0"},
		"an implausible age":    {func(p *people.Person) { p.Age = 151 }, "Age", "max=150"},
	}

	for name, want := range cases {
		t.Run(name, func(t *testing.T) {
			held := valid()
			want.spoil(&held)

			err := held.Validate()
			if err == nil {
				t.Fatal("the rule accepted what it is written to refuse")
			}

			var failures people.ValidationErrors
			if !errors.As(err, &failures) {
				t.Fatalf("%v is not a list of failures", err)
			}
			if len(failures) != 1 {
				t.Fatalf("one spoiled field reported %d failures: %v", len(failures), err)
			}

			if got := failures[0].Path; got != want.field {
				t.Errorf("the failure is about %s, want %s", got, want.field)
			}
			if got := failures[0].Rule; !strings.HasPrefix(got, want.rule) {
				t.Errorf("the failure names %s, want %s", got, want.rule)
			}
			if failures[0].Want == "" {
				t.Error("the failure does not say what the rule wanted")
			}
		})
	}
}

// A person with three things wrong reports three, in the order the fields are
// declared.
//
// It is what a caller showing somebody a form needs: three round trips to fix
// three fields is what reporting the first would cost them.
func TestEveryFailureIsReported(t *testing.T) {
	held := people.Person{ID: 0, Name: "", Email: "", Age: -1}

	err := held.Validate()
	if err == nil {
		t.Fatal("a person with nothing right was accepted")
	}

	var failures people.ValidationErrors
	if !errors.As(err, &failures) {
		t.Fatalf("%v is not a list of failures", err)
	}

	paths := make([]string, len(failures))
	for i, one := range failures {
		paths[i] = one.Path
	}

	if got := strings.Join(paths, ","); got != "ID,Name,Email,Age" {
		t.Errorf("the failures are %s, want one per field in declaration order", got)
	}
	if !strings.Contains(err.Error(), "4 failures") {
		t.Errorf("the message does not say how many there are:\n%v", err)
	}
}

// Nothing is allocated by a person who satisfies the rules.
//
// The whole reason to generate the check rather than reflect over the tags: the
// path that runs on every request costs the comparisons and no memory. Stated
// as zero rather than as a budget, because a check that allocates once
// allocates once per call and there is nothing to amortise it over.
func TestCheckingAValidPersonAllocatesNothing(t *testing.T) {
	held := valid()

	got := testing.AllocsPerRun(100, func() {
		if held.Validate() != nil {
			sink(1)
		}
	})
	if got != 0 {
		t.Errorf("checking a valid person allocated %.0f times, want none", got)
	}
}

// Each failure reads as a sentence naming the field, so that a message can be
// shown to somebody who has never seen the tag.
func TestWhatAFailureReads(t *testing.T) {
	held := valid()
	held.Name = ""

	err := held.Validate()
	if err == nil {
		t.Fatal("a nameless person was accepted")
	}

	if got, want := err.Error(), "Name: required wants a value"; got != want {
		t.Errorf("the failure reads %q, want %q", got, want)
	}
}

// A failure is reachable through errors.As one at a time, which is what a
// caller picking out the one about a particular field does.
func TestAFailureIsReachableOnItsOwn(t *testing.T) {
	held := valid()
	held.Age = -1

	var one people.ValidationError
	if !errors.As(held.Validate(), &one) {
		t.Fatal("no single failure could be reached")
	}
	if one.Path != "Age" {
		t.Errorf("the failure reached is about %s, want Age", one.Path)
	}
}

// The check reaches every person a container holds, because it is written on
// the element rather than on the container.
//
// It is what the element layer's kind means: two declarations over one subject
// share one check, and a container of subjects is a container of things that
// can each be asked.
func TestEveryPersonInAContainerCanBeAsked(t *testing.T) {
	held := people.NewRecent()
	held.Push(valid())
	held.Push(people.Person{ID: 2, Name: "", Email: "", Age: 0})

	refused := 0
	for one := range held.All() {
		if one.Validate() != nil {
			refused++
		}
	}

	if refused != 1 {
		t.Errorf("%d of two people were refused, want one", refused)
	}
}
