//go:build forgespec

package model

import "declsfixture/markers"

// Persons is a spec declaration: a candidate, and one forge owns outright.
//
//forge:collection sort=Age,LastName index=Name
//forge:ring cap=1024 overflow=overwrite
//forge:json omitzero=true
type Persons markers.Collection[markers.Ring[markers.Json[Person]]]

type (
	// Recent is declared inside a group, so its directive attaches to the spec
	// rather than to the declaration.
	//
	//forge:ring cap=16
	Recent markers.Ring[Person]

	// Undirected carries no directives at all.
	Undirected markers.Collection[Person]
)

// User is the source a bridge reads, here so a hint has something to name.
type User struct {
	Name string
}

//forge:map hint
func personFromUser(src *User, dst *Person) {
	dst.Name = src.Name
}
