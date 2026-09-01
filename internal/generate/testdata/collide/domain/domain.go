// Package domain holds a subject that lives somewhere other than the package
// generated into.
//
// It exists so that a claim naming the element type has to qualify it, which is
// the case where two ways of writing one type can disagree.
package domain

// Person is the subject, declared away from the collections over it.
type Person struct {
	Name string
}
