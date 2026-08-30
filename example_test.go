package forge_test

import (
	"fmt"

	"github.com/okian/forge"
)

// Person is the subject of the declaration below: an ordinary struct, written
// by hand, with no knowledge that anything generates against it.
type Person struct {
	Name string `json:"name"`
	Age  int    `json:"age"`
}

// Persons is an inline declaration. Collection is declared as a slice, so the
// underlying type here really is []Person and the value works as written, with
// or without generated methods.
type Persons forge.Collection[Person]

// This example shows the inline declaration form: an ordinary file, no build
// tag, and an underlying type that means what it says.
func Example() {
	people := Persons{{Name: "Ada", Age: 36}, {Name: "Alan", Age: 41}}

	fmt.Println(len(people), people[0].Name)
	// Output: 2 Ada
}
