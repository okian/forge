// Package model holds the subject and the declaration written against it.
package model

// Person is the subject.
type Person struct {
	Name string `json:"name"`
	Age  int    `json:"age"`
}
