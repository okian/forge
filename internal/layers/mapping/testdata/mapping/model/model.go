// Package model holds the pairs a constructor is written for: one pair per
// question the ladder has to answer.
package model

// User is a struct source: fields and methods both.
type User struct {
	ID    int
	Email string
	Age   int
}

// FullName is a method candidate: no parameters, one result. Person's field of
// the same name is settled by it on the ladder's second rung.
func (u User) FullName() string { return "u" }

// NickName reaches Person's Nickname on the third rung: no member matches it
// exactly, and this is the one whose folded spelling does.
func (u User) NickName() string { return "n" }

// Person is the plain target.
type Person struct {
	ID       int    // first rung: the field of the same name
	Email    string // first rung
	Age      int    // first rung
	FullName string // second rung: the method of the same name
	Nickname string // third rung: folded to NickName
}

// Reader is an interface source: methods are all it offers.
type Reader interface {
	ID() int
	Label() string
}

// Card is Reader's target.
type Card struct {
	ID    int
	Label string
}

// Entitled is the source whose Title reaches a target field the target's own
// package cannot export.
type Entitled struct {
	ID    int
	Title string
}

// Titled has an unexported field, which is reachable because the constructor
// is generated into this package: it joins the members and the fold settles
// it.
type Titled struct {
	ID    int
	title string
}

// Renamed is a target the ladder alone refuses: nothing on User is spelled
// like Moniker, so a hint is the only way to settle it.
type Renamed struct {
	ID      int
	Moniker string
}

// Converted is a target whose Age member needs a conversion: User's Age is an
// int, and an int does not assign to a float64.
type Converted struct {
	Age float64
}

// Base carries the field Based promotes.
type Base struct {
	Core int
}

// Based has a promoted member: dst.Core type-checks, and Core is not a field
// of Based itself, which is the one way a type-checked hint can assign a
// member the target does not declare.
type Based struct {
	Base
	Own int
}

// Terse offers less than Sparse asks for, which is what ignore is for.
type Terse struct {
	ID int
}

// Sparse has a member left unset on purpose: the declaration writes
// ignore=Note.
type Sparse struct {
	ID   int
	Note string
}
