// Package other holds a secret in a package the generated file is not written
// into.
//
// A method cannot be declared on a type from another package, so there is
// nowhere to put a log value for this one — which makes it the same refusal as
// a struct written in place, reached by a different route.
package other

// Credentials is the secret next door.
type Credentials struct {
	User  string
	Token string `redact:""`
}
