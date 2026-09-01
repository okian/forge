// Package model holds the subjects the rules are exercised against.
//
// One subject per question rather than one holding every field, so that a
// failure names the question rather than a line number in a struct of forty
// fields.
package model

import "validatefixture/other"

// Person carries one of every rule, on a field the rule applies to.
type Person struct {
	Name    string `validate:"required,min=2,max=64"`
	Email   string `validate:"required,regexp=^[^@,]+@[^@]+$"`
	Age     int    `validate:"min=0,max=130"`
	Role    string `validate:"oneof=admin editor reader"`
	Tags    []string
	Country string `validate:"len=2"`
}

// Scalars carries the rules that ask about numbers.
type Scalars struct {
	Whole    int     `validate:"min=1,max=10"`
	Unsigned uint8   `validate:"nonzero"`
	Fraction float64 `validate:"min=0.5,max=99.5"`
	Flag     bool    `validate:"nonzero"`
	Ranked   int     `validate:"oneof=1 2 3"`
}

// Composites carries the rules that ask about length and presence.
type Composites struct {
	Items   []string          `validate:"required,min=1,max=8"`
	Lookup  map[string]int    `validate:"required,len=3"`
	Fixed   [4]int            `validate:"len=4"`
	Pointer *int              `validate:"required"`
	Any     any               `validate:"required"`
	Nested  map[string]string `validate:"max=2"`
}

// Named carries rules on types declared over the ones the rules understand,
// because an author who writes one expects the rule to reach through it.
type Named struct {
	Kind Kind  `validate:"oneof=fast slow"`
	Size Size  `validate:"min=1"`
	Rate Ratio `validate:"max=1.0"`
}

// The named scalars Named holds.
type (
	Kind  string
	Size  int
	Ratio float64
)

// Address is a struct another struct holds, so that a failure inside it is
// reported under a path that reaches it.
type Address struct {
	City     string `validate:"required,min=2"`
	Postcode string `validate:"len=5"`
}

// Nested holds an address by value and another by pointer.
type Nested struct {
	Home Address
	Work *Address
	Name string `validate:"required"`
}

// Hooked declares a check of its own for one of its fields, which the generated
// one calls where that field's own rules are checked.
type Hooked struct {
	Slug  string `validate:"required"`
	Token string
}

// ValidateToken is the author's own check, which no tag could express.
func (h Hooked) ValidateToken() error {
	if len(h.Token) != 0 && h.Token[0] != 't' {
		return errBadToken
	}
	return nil
}

// errBadToken is what the hook reports, as a value so that a caller can compare
// against it.
var errBadToken = errorString("a token starts with t")

// errorString is an error with nothing but a message, so that this fixture
// imports nothing.
type errorString string

func (e errorString) Error() string { return string(e) }

// Quiet carries no rules at all, so nothing is written for it and nothing that
// holds it calls anything.
type Quiet struct {
	Note string
}

// Holder holds a struct with nothing to check, which is the case that decides
// whether an empty check is written.
type Holder struct {
	Inside Quiet
	Name   string `validate:"required"`
}

// Postcode brings a check of its own, on a type that is not a struct.
//
// It is the case that decides whether a field is checked by calling what its
// type already has: a struct this run writes for is answered by the plan, and
// everything else by asking the type.
type Postcode string

// Validate is the author's own, and is what the field holding one calls.
func (p Postcode) Validate() error {
	if len(p) != 5 {
		return errBadPostcode
	}
	return nil
}

// errBadPostcode is what Postcode reports.
var errBadPostcode = errorString("a postcode is five characters")

// Carrying holds a value whose type checks itself.
type Carrying struct {
	Where Postcode
}

// Misdeclared has a method called Validate that is not a check, so that the
// signature is what decides rather than the name.
type Misdeclared string

// Validate here takes an argument, which is not the method a field is checked
// by.
func (m Misdeclared) Validate(strict bool) error {
	if strict && m == "" {
		return errBadPostcode
	}
	return nil
}

// Carrying the one that only looks like a check.
type Mistaken struct {
	What Misdeclared `validate:"required"`
}

// Zeroed asks about the zero of things the language compares and has no literal
// for.
type Zeroed struct {
	Point  Coordinate `validate:"nonzero"`
	Window [2]int     `validate:"nonzero"`
}

// Coordinate is a struct held by value, whose zero is a composite literal.
type Coordinate struct {
	X int
	Y int
}

// Ranged lists the numbers a fraction may be.
type Ranged struct {
	Share float64 `validate:"oneof=0.25 0.5 1.0"`
}

// Elsewhere holds a struct declared in a package of its own, by value and by
// pointer.
//
// The check for that struct cannot be a method, because Go puts a method only
// where its type is, so it is a function in this package — and both fields have
// to call it rather than call a method that cannot exist.
type Elsewhere struct {
	Home other.Place
	Work *other.Place
}

// Trusting holds a struct in another package that checks itself, so that the
// author's check is what runs rather than a second one derived from the tags.
//
// It is the case where the two would disagree. What the author checks is an
// invariant on a field nothing here can read, so a check written here would
// pass a value the type itself refuses.
//
// Beside a field whose type asks for nothing, which is what makes the case
// worth writing twice: the plan for [Boring] is dropped because nothing would
// be written for it, and the plan for [other.Guarded] is dropped because
// somebody already wrote it. The two look alike from the inside and mean
// opposite things.
type Trusting struct {
	Where  other.Guarded
	Boring Boring
}

// Boring asks for nothing, so no check is written for it.
type Boring struct {
	Note string
}

// Wrongly holds a struct that declares something called Validate which is not a
// check, so that the signature is what decides rather than the name.
type Wrongly struct {
	Held Confused
}

// Confused only looks like it checks itself.
type Confused struct {
	Name string `validate:"required"`
}

// Validate here takes an argument, so it is neither a check to call nor a name
// generated code can write a second method under.
func (c Confused) Validate(strict bool) error {
	if strict && c.Name == "" {
		return errBadPostcode
	}
	return nil
}
