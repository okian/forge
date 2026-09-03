// Package app holds a function a directive marks, whose body a stage reads.
package app

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
	dst.Name = src.Name
}

// helper carries no directive, so its body is bulk the pipeline never reads.
func helper() int {
	return 1
}
