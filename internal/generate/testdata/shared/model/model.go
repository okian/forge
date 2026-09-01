// Package model holds two subjects that reach one struct between them.
//
// It is the arrangement the shared file exists for: whatever a layer writes
// about Address is written once however many declarations reach it, because a
// package holding one function twice does not compile.
package model

// Address is the struct both subjects reach, and the one whose helpers there
// must be exactly one of.
type Address struct {
	City  string `validate:"required"`
	Lines []string
}

// Person reaches it directly.
type Person struct {
	Name  string `validate:"required"`
	Home  Address
	Tags  []string
	Notes map[string]string
}

// Employer reaches the same Address, from a different subject.
type Employer struct {
	Title string
	Site  Address
}
