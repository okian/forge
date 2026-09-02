// Package domain is the third party's own package, holding their subject and a
// declaration over their own marker.
package domain

// Person is an ordinary struct, written without regard for what a layer will
// make of it.
type Person struct {
	ID   int
	City string
}
