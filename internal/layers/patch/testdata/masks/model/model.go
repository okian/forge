// Package model holds the subjects the patches are exercised against.
//
// One subject per question rather than one holding every field, so that a
// failure names the question rather than a line in a struct of forty fields.
package model

import (
	"sync"

	"patchfixture/other"
)

// Person is the ordinary case: fields of the kinds a partial update usually
// carries, tagged the way a subject that goes over a wire is tagged.
//
// The tags are the point rather than decoration. A patch and the subject travel
// the same way, so a member of one has to be named as the member of the other
// is — or a request written with the names a reply used names nothing.
type Person struct {
	Name    string   `json:"name"`
	Age     int      `json:"age,omitempty"`
	Aliases []string `json:"aliases" validate:"max=8"`
	Ratio   float64

	// Home is a type declared beside this one, so that what the patch is
	// written for decides how the field is spelled: from here it is Address,
	// and from anywhere else it would need the package's name in front.
	Home Address
}

// Address is declared beside the subject, and is what makes the spelling
// load-bearing.
type Address struct {
	City string
}

// Indirect already holds a pointer, so its patch holds a pointer to one — which
// is as ugly as it sounds and is the honest spelling: the outer one says whether
// the patch mentions the field and the inner one is the value.
type Indirect struct {
	Count *int
	Where *other.Place
}

// Held holds types a patch's own fields have to name from elsewhere.
type Held struct {
	Where other.Place
	Many  map[string][]other.Place
	Fixed [2]other.Place
}

// Keeping keeps a field to itself, which a patch does not carry and could not
// have been filled in from outside anyway.
type Keeping struct {
	Name   string
	secret string
}

// Naming has a field named after one of the two methods a patch needs of its
// own.
type Naming struct {
	Apply string
}

// Asking has a field named after the other one.
type Asking struct {
	IsZero bool
}

// Locked holds a value the language will copy and the vet everybody runs will
// complain about copying.
type Locked struct {
	Guard sync.Mutex
	Name  string
}

// Bare has nothing a caller could change, so a patch over it would be a request
// that cannot say anything.
type Bare struct {
	first  int
	second string
}
