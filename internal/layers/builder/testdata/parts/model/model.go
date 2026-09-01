// Package model holds the subjects the builders are exercised against.
//
// One subject per question rather than one holding every field, so that a
// failure names the question rather than a line in a struct of forty fields.
package model

import (
	"sync"

	"builderfixture/other"
)

// Person carries fields of both the kinds a rule can say are mandatory, beside
// ones that are not.
//
// required is what a value that can be absent takes and nonzero is what the
// language will compare, so between them they cover every type — and a builder
// that read only one of them would demand a name and not an age for no reason
// anybody wrote down.
type Person struct {
	Name    string `validate:"required,max=64"`
	Age     int    `validate:"nonzero"`
	Email   string `validate:"required"`
	Nick    string
	Ratio   float64
	Aliases []string

	// Home is a type declared beside this one, so that what the builder is
	// written for decides how the setter spells it: from here it is Address,
	// and from anywhere else it would need the package's name in front.
	Home Address
}

// Address is declared beside the subject, and is what makes the spelling
// load-bearing.
type Address struct {
	City string
}

// Open asks for nothing, so its builder hands the value back whatever was set.
type Open struct {
	Name string
	Age  int
}

// Held holds types a setter's signature has to name from elsewhere.
type Held struct {
	Where other.Place `validate:"nonzero"`
	Also  *other.Place
	Many  map[string][]other.Place
}

// Keeping keeps a field to itself, which a builder does not offer and does not
// have to: a caller could not have named it anyway.
type Keeping struct {
	Name   string `validate:"required"`
	secret string
}

// Demanding keeps a field to itself and says a value has to carry it, which is
// a contradiction rather than something to work around.
type Demanding struct {
	secret string `validate:"required"`
}

// Ending has a field named after the one method a builder needs of its own.
type Ending struct {
	Build string
}

// Spaced writes a rule after a comma and a space, which is how a person writes
// a list and how two readers of one tag come to disagree.
type Spaced struct {
	Padded string `validate:"max=64, required"`
	Plain  string `validate:"max=64,required"`
}

// Locked holds a value the language will copy and the vet everybody runs will
// complain about copying.
type Locked struct {
	Guard sync.Mutex
	Name  string
}

// Naming holds a type whose own fields include one nothing outside its package
// can name — which is that type's business rather than this one's, because a
// setter names the field's type and nothing inside it.
type Naming struct {
	Held other.Holder
	Name string
}

// Bare has nothing a caller could give, so a builder over it would name nothing
// at the call site — which is the whole of what a builder is for.
type Bare struct {
	first  int
	second string
}
