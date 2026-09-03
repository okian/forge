//go:build forgespec

package model

// Renamed is a target the ladder alone refuses: nothing on User is spelled
// like Moniker, so a hint is the only way to settle it.
type Renamed struct {
	ID      int
	Moniker string
}

// Converted is a target whose Age member needs a conversion: User's Age is an
// int, and an int does not assign to a float64.
type Converted struct {
	Age float64
}

// Base carries the field Based promotes.
type Base struct {
	Core int
}

// Based has a promoted member: dst.Core type-checks, and Core is not a field
// of Based itself, which is the one way a type-checked hint can assign a
// member the target does not declare.
type Based struct {
	Base
	Own int
}

//forge:map hint
func renamedFromUser(src *User, dst *Renamed) {
	dst.Moniker = src.Email
}

//forge:map hint
func convertedFromUser(src *User, dst *Converted) {
	dst.Age = float64(src.Age)
}

// personFromUser overrides a member the ladder would settle on its own.
//
//forge:map hint
func personFromUser(src *User, dst *Person) {
	dst.Email = src.Email + "!"
}

// declares says more than a hint may: a local is not an assignment.
//
//forge:map hint
func declares(src *User, dst *Renamed) {
	x := src.Email
	dst.Moniker = x
}

// branches says more than a hint may: a hint has no conditions.
//
//forge:map hint
func branches(src *User, dst *Renamed) {
	if src.Age > 0 {
		dst.Moniker = src.Email
	}
}

// writesBack assigns into the source, which no mapping does.
//
//forge:map hint
func writesBack(src *User, dst *Renamed) {
	src.Email = "x"
	dst.Moniker = "y"
}

// promotes assigns a member the target does not declare: Core is Base's.
//
//forge:map hint
func promotes(src *User, dst *Based) {
	dst.Core = src.ID
	dst.Own = src.Age
}

// twice assigns one member two ways, and a mapping has no last word.
//
//forge:map hint
func twice(src *User, dst *Renamed) {
	dst.Moniker = src.Email
	dst.Moniker = "again"
}
