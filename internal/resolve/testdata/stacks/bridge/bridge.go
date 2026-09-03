// Package bridge holds a declaration over the two-parameter marker, whose
// first argument is a source to carry rather than a layer to descend into.
package bridge

import "github.com/okian/forge"

// User is the source the bridge reads.
type User struct {
	Name string
}

// Person is the target the bridge writes.
type Person struct {
	Name string
}

// UserPerson names the bridge: a constructor building Person from User.
type UserPerson forge.Map[User, Person]
