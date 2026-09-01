// Package model holds the subjects the copies are exercised against.
//
// One subject per question rather than one holding every field, so that a
// failure names the question rather than a line in a struct of forty fields.
package model

import "clonefixture/other"

// Flat holds only what assignment already copies, so its copy is the assignment
// and nothing else.
type Flat struct {
	Name  string
	Count int
	Ready bool
	Fixed [3]int
}

// Referring holds the three things an assignment copies the reference of rather
// than what it refers to, which is the whole reason this layer exists.
type Referring struct {
	Tags   []string
	Lookup map[string]int
	Count  *int
	Name   string
}

// Deep holds references to things that themselves hold references, so that the
// copy has to go all the way down rather than one level.
type Deep struct {
	Rows   [][]string
	Nested map[string][]int
	Deeper **int
	Fixed  [2][]string
}

// Address is a struct another struct holds, so that a copy of the holder calls
// the held one's own copy.
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

// Node reaches itself, which is the shape that would make a generator that
// inlined its fields run for ever.
type Node struct {
	Label string
	Next  *Node
}

// Owning brings a copy of its own, which is called rather than duplicated.
type Owning struct {
	Held Counter
}

// Counter copies itself, and does it in a way nothing generated would guess:
// the copy starts again from zero.
type Counter struct {
	Seen  int
	Notes []string
}

// Clone is the author's own, and is what a field holding one calls.
func (c Counter) Clone() Counter { return Counter{Notes: append([]string(nil), c.Notes...)} }

// Mistaken has a method called Clone that is not a copy, so that the signature
// is what decides rather than the name.
type Mistaken struct {
	Held Misdeclared
}

// Misdeclared only looks like it copies itself.
type Misdeclared struct {
	Notes []string
}

// Clone here answers with something else, which is not the method a field is
// copied by.
func (m Misdeclared) Clone() []string { return m.Notes }

// Opaque holds the three things nothing can copy without being told what a copy
// of them would mean.
type Opaque struct {
	Anything any
	Updates  chan int
	Do       func()
}

// Marked holds the same three, each said to be carried across as it is.
type Marked struct {
	//forge:clone aliasing=share
	Anything any

	//forge:clone aliasing=share
	Updates chan int

	//forge:clone aliasing=share
	Do func()

	// And a slice that is shared while the rest of the subject is copied,
	// which is the case the option exists for.
	//forge:clone aliasing=share
	Shared []string

	// Beside one that is not.
	Copied []string
}

// Misoptioned writes something above a field that is not the option a field
// takes.
type Misoptioned struct {
	//forge:clone whatever=share
	Tags []string
}

// Misvalued writes the option with a value it does not take on a field.
type Misvalued struct {
	//forge:clone aliasing=copy
	Tags []string
}

// Elsewhere holds a struct declared in a package of its own, every way a struct
// can hold one.
//
// The copy for that struct cannot be a method, because Go puts a method only
// where its type is, so it is a function in this package — and every one of
// these fields has to call it rather than call a method that cannot exist.
type Elsewhere struct {
	Home    other.Place
	Work    *other.Place
	Past    []other.Place
	ByName  map[string]other.Place
	Windows [2]other.Place
}

// Pair is a generic, so that an instantiation of it is a type this package
// declares and still cannot carry a method.
//
// It is the other reason a copy has to be a function, and it is not the one a
// reader would guess: the type is right here, and Go has nowhere to put the
// method because the method would belong to the generic and so to every
// instantiation of it.
type Pair[A any, B any] struct {
	First  A
	Second B

	// notes is unexported and is this package's, so a copy written here copies
	// it — which is what tells the two reasons apart.
	notes []string
}

// Instantiated holds one instantiation of that generic.
type Instantiated struct {
	Held Pair[string, int]
}
