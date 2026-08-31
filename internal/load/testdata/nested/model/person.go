// Package model belongs to the module being generated for, and reaches a
// package that shares its import path prefix and does not.
package model

import (
	"nestedfixture/domain"
	"nestedfixture/inner"
)

// Person is what a declaration here would be about.
type Person struct {
	Where domain.Place
	Other inner.Thing
}
