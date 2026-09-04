// Package refused holds the pairs a constructor is refused for: one pair per
// way the ladder can fail to settle a member.
package refused

// Src carries what most of the targets below are matched against.
type Src struct {
	A int
}

// Unmatched has a member no source member reaches: B is settled no way.
type Unmatched struct {
	A int
	B string
}

// Forked offers two members whose folded spellings collide, so a target
// written against either fold cannot say which it means.
type Forked struct {
	Foo_Bar int //nolint:revive // the underscore is the collision under test
	FooBar  int
}

// Foobar is spelled like both of Forked's members once folded, and like
// neither exactly.
type Foobar struct {
	Foobar int
}

// Aged matches Src's A by name and not by type: an int does not assign to a
// string.
type Aged struct {
	A string
}

// Empty offers nothing to read: no exported field, no method.
type Empty struct {
	hidden int
}

// Sealed has an unexported field, which is out of reach for a constructor
// generated into any other package.
type Sealed struct {
	ID     int
	secret string
}

// Mistagged pins a member the source does not offer.
type Mistagged struct {
	A int `from:"Src.Nothing"`
}

// Malformed carries an entry that is not Source.Member or Member.
type Malformed struct {
	A int `from:"a.b.c"`
}

// AssertedField writes parens on a member that turns out to be a field.
type AssertedField struct {
	A int `from:"Src.A()"`
}

// TaggedTwice carries two entries that both answer one mapping.
type TaggedTwice struct {
	A int `from:"Src.A, Src.A"`
}

// AgedTag pins a member whose type does not assign, so the way out the ladder
// would have offered — matching nothing — is closed on purpose.
type AgedTag struct {
	A string `from:"Src.A"`
}

// QualifierGarbled writes parens into the qualifier, which is not a source's
// name and must not be mistaken for a sibling mapping's entry.
type QualifierGarbled struct {
	A int `from:"Src().A"`
}

// LeadingDot opens an entry with the separator, which names no source and is
// not the bare form either.
type LeadingDot struct {
	A int `from:".A"`
}

// BlankTag carries a from tag that says nothing.
type BlankTag struct {
	A int `from:" , "`
}

// MaskedBare pairs a qualified entry that answers with bare entries that
// contradict each other: the contradiction is refused whichever entry would
// have answered.
type MaskedBare struct {
	A int `from:"Src.A, X, X"`
}

// NonIdentifier names a member no Go declaration could.
type NonIdentifier struct {
	A int `from:"Src.-"`
}
