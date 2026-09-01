// Package model holds the subjects the hashes are exercised against.
//
// One subject per question rather than one holding every field, so that a
// failure names the question rather than a line in a struct of forty fields.
package model

import "hashfixture/other"

// Flat holds one of each thing the language gives a value directly.
type Flat struct {
	Name   string
	Count  int
	Size   uint16
	Ratio  float64
	Small  float32
	Ready  bool
	Signal complex128
}

// Referring holds the three shapes that carry more than what is in them: one
// that may not be there, one that is ordered, and one that is not.
type Referring struct {
	Tags   []string
	Lookup map[string]int
	Count  *int
	Fixed  [3]int
}

// Deep holds references to things that themselves hold references, so that a
// hash has to go all the way down rather than one level.
type Deep struct {
	Rows   [][]string
	Nested map[string][]int
	Deeper **int
}

// Address is a struct another struct holds, so that a hash of the holder calls
// the held one's own.
type Address struct {
	City  string
	Lines []string
}

// Holding holds an address every way a struct can hold one.
type Holding struct {
	Home    Address
	Work    *Address
	Past    []Address
	ByName  map[string]Address
	Windows [2]Address
}

// Node reaches itself, which is the shape that would make a generator inlining
// its fields run for ever.
type Node struct {
	Label string
	Next  *Node
}

// Anonymous holds a struct written in place, which has no name to hang a method
// on and so is taken apart where it is used.
type Anonymous struct {
	At struct {
		Line   int
		Column int
	}
}

// Owning brings a hash of its own, which is called rather than duplicated.
type Owning struct {
	Held Counter
}

// Counter hashes itself, and does it in a way nothing generated would guess:
// only what it has seen counts.
type Counter struct {
	Seen  int
	Notes []string
}

// Hash is the author's own, and is what a field holding one calls.
func (c Counter) Hash() uint64 { return uint64(c.Seen) }

// Mistaken has a method called Hash that is not one, so that the signature is
// what decides rather than the name.
type Mistaken struct {
	Held Misdeclared
}

// Misdeclared only looks like it hashes itself.
type Misdeclared struct {
	Notes []string
}

// Hash here answers with something else, which is not the method a field is
// hashed by.
func (m Misdeclared) Hash() []string { return m.Notes }

// Opaque holds the things whose identity is not their content, two of them
// twice — so that every field is reported rather than the first of each type.
type Opaque struct {
	Anything any
	Updates  chan int
	Pending  chan int
	Do       func()
	Undo     func()
	Where    uintptr
}

// Marked holds the same three, each said to be left out.
type Marked struct {
	//forge:hash ignore
	Anything any

	//forge:hash ignore
	Updates chan int

	//forge:hash ignore
	Do func()

	// And a field that is perfectly hashable and is not part of what the value
	// is, which is the case the option exists for.
	//forge:hash ignore
	LastRead int64

	// Beside one that is.
	Name string
}

// Misoptioned writes something above a field that is not the option a field
// takes.
type Misoptioned struct {
	//forge:hash whatever
	Tags []string
}

// Elsewhere holds a struct declared in a package of its own, which the hash for
// has to be a function rather than a method.
type Elsewhere struct {
	Home    other.Place
	Work    *other.Place
	Past    []other.Place
	ByName  map[string]other.Place
	Windows [2]other.Place
}

// Sealing holds a struct whose content cannot be read from here, so no honest
// hash of one can be written here either.
type Sealing struct {
	Held other.Sealed
}

// Age is a subject that is not a struct at all, which is the shape an
// enumeration has.
type Age int

// Names is a name over a slice, so that a subject with no fields still has
// something to hash.
type Names []string

// Endless contains itself with no struct in between, which is a chain no
// generated hash can be written for.
type Endless []Endless

// Cyclic holds one, so that the refusal has a field to point at.
type Cyclic struct {
	Loop Endless
}

// Twice holds two maps side by side, so that whatever each of them binds has
// to be a name the other does not.
type Twice struct {
	First  map[string]int
	Second map[string]int
	Deeper map[string]map[string]int
}

// Nested holds a struct whose own fields are all exported and which keeps
// something to itself further down.
//
// The refusal has to point here, at the field somebody wrote, rather than at
// the member inside a package they cannot edit.
type Nested struct {
	Held other.Holder
}
