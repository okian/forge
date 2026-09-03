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
