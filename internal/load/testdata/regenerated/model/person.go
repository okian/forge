// Package model stands in for a package that has been generated for once
// already, which is every package after the first run.
package model

// Person is the subject, written by hand.
type Person struct {
	Name string
}

// Rename is a method the author wrote, beside the ones a generator did.
func (p Person) Rename(to string) Person { return Person{Name: to} }
