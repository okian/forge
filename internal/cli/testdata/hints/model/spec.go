//go:build forgespec

package model

import "github.com/okian/forge"

// UserPerson is the bridge the hints below are for.
type UserPerson forge.Map[User, Person]

// fromUser is the hint that matches: its parameters name the declaration's
// source and subject.
//
//forge:map hint
func fromUser(src *User, dst *Person) {
	dst.Name = src.Name
}

// fromUserAgain is a second hint for the same mapping, which is a
// contradiction rather than an order.
//
//forge:map hint
func fromUserAgain(src *User, dst *Person) {
	dst.Name = src.Name
}

// tipped writes a verb the map layer does not take on a function.
//
//forge:map tip
func tipped(src *User, dst *Person) {
	dst.Name = src.Name
}

// misshapen is not func(src *S, dst *T): its first parameter is a value.
//
//forge:map hint
func misshapen(src User, dst *Person) {
	dst.Name = src.Name
}

// adrift reads a source no declaration in the package names.
//
//forge:map hint
func adrift(src *Wrong, dst *Person) {
	dst.Name = src.Name
}
