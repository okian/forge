// Package model holds every way a map hint can be written, one of each.
package model

// User is the source the bridge reads.
type User struct {
	Name string
}

// Person is the target the bridge writes.
type Person struct {
	Name string
}

// Wrong is a source no declaration names, so a hint reading it matches
// nothing.
type Wrong struct {
	Name string
}
