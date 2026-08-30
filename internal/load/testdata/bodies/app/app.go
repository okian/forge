package app

import (
	"fmt"
	str "strings"
)

// Person is a subject with nothing unusual about it.
type Person struct{ Name string }

// Greet uses both imports, and only from inside the body.
func Greet(p Person) string {
	return fmt.Sprintf("hello %s", str.ToUpper(p.Name))
}

// Describe returns a value, so an emptied body rather than an absent one would
// be a missing return.
func Describe(p Person) (string, error) {
	return p.Name, nil
}
