package people_test

import (
	"regexp"
	"testing"

	"github.com/okian/forge/examples/people"
)

// The generated check over a person who satisfies the rules.
//
// The path that matters: a check runs on every request and almost always finds
// nothing, so what it costs when everything is in order is what it costs. The
// figure to look at is the allocations, which are none.
func BenchmarkValidate(b *testing.B) {
	held := valid()

	b.ReportAllocs()

	for b.Loop() {
		if held.Validate() != nil {
			b.Fatal("the fixture does not satisfy its own rules")
		}
	}
}

// The same rules written by hand, which is what the generated check is worth
// measuring against.
//
// Not a reflective validator — that comparison lives in its own module, so that
// the module everybody builds keeps its one direct dependency. This is the
// other question, and the more demanding one: a generated check should cost
// what somebody would have written themselves, because if it costs more there
// is no reason to generate it.
func BenchmarkValidateByHand(b *testing.B) {
	held := valid()

	b.ReportAllocs()

	for b.Loop() {
		if byHand(held) != nil {
			b.Fatal("the fixture does not satisfy its own rules")
		}
	}
}

// The same rules by hand, but matching the address against the same compiled
// pattern the generated check uses.
//
// The pair above and this one answer two different questions, and reading
// either alone answers the wrong one. Against the scan, the generated check
// looks an order of magnitude slower — and almost all of that is the regexp
// engine, which the generated check has no choice about because a pattern is
// what the tag asked for. Against the same pattern, what is left is the
// difference the generator makes, which is what a reader deciding whether to
// generate actually wants to know.
func BenchmarkValidateByHandWithThePattern(b *testing.B) {
	held := valid()

	b.ReportAllocs()

	for b.Loop() {
		if byHandWithThePattern(held) != nil {
			b.Fatal("the fixture does not satisfy its own rules")
		}
	}
}

// byHand is the same rules, written out.
//
// Deliberately the shape somebody would write rather than the shape the
// generator writes: the first failure returned, no list built, no path
// recorded. It is the cheapest honest implementation of the same rules, which
// is what makes it the floor rather than a strawman.
func byHand(p people.Person) error {
	switch {
	case p.ID < 1:
		return errRule
	case p.Name == "", len(p.Name) > 64:
		return errRule
	case p.Email == "", !emailByHand(p.Email):
		return errRule
	case p.Age < 0, p.Age > 150:
		return errRule
	}
	return nil
}

// emailByHand is the pattern, written as the scan it is, so that the hand-written
// version is not measured running a regexp engine the generated one also runs.
func emailByHand(address string) bool {
	at := -1
	for i := range len(address) {
		switch address[i] {
		case '@':
			if at >= 0 {
				return false
			}
			at = i
		case ' ', '\t', '\n', '\r':
			return false
		}
	}
	return at > 0 && at < len(address)-1
}

// errRule is what the hand-written check reports, as a value so that reporting
// costs nothing.
var errRule = ruleError("a rule was not satisfied")

// ruleError is an error with nothing but a message.
type ruleError string

func (e ruleError) Error() string { return string(e) }

// byHandWithThePattern is byHand with the scan replaced by the pattern, so that
// what separates it from the generated check is the generation and nothing
// else.
func byHandWithThePattern(p people.Person) error {
	switch {
	case p.ID < 1:
		return errRule
	case p.Name == "", len(p.Name) > 64:
		return errRule
	case p.Email == "", !pattern.MatchString(p.Email):
		return errRule
	case p.Age < 0, p.Age > 150:
		return errRule
	}
	return nil
}

// pattern is the same pattern the generated check compiles, compiled the same
// way and once.
var pattern = regexp.MustCompile(`^[^@[:space:]]+@[^@[:space:]]+$`)
