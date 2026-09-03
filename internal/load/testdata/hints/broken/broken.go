// Package broken holds a hint that does not type-check, which the load has to
// report rather than hand onward.
package broken

// User is the source a hint reads from.
type User struct {
	Name string
}

// Person is the target a hint writes to.
type Person struct {
	Name string
}

//forge:map hint
func personFromUser(src *User, dst *Person) {
	dst.Name = src.Missing
}
