// Package matrix holds one declaration per concurrent layer, and the stress
// test each of them is put through.
//
// It is a package rather than a set of fixtures because the point is to run. A
// race is found by the detector while goroutines are actually contending, and
// nothing that only type-checks can see one — so the declarations are generated
// and committed here, the stress tests are generated beside them, and an
// ordinary run under the detector covers the lot.
//
// Nothing here is written by hand except this file and the declarations beside
// it. What is recorded is held to what the harness produces today, so a layer
// whose surface changes is a diff to read before it is a test that is missing.
package matrix

// Person is what the containers hold.
//
// Small on purpose. What these tests are about is the container and the lock
// around it, and a subject with anything interesting in it would only make the
// generated code longer to read.
type Person struct {
	// ID identifies the person.
	ID int

	// Name is how they are listed.
	Name string
}
