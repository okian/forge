// Package other holds a type a subject's field is written in terms of, so that
// a patch has a field whose type names a package it has to import.
package other

// Place is what a field of a subject somewhere else holds.
type Place struct {
	City string
}

// hidden is a type nothing outside this package can name.
type hidden struct{ Note string }

// Holder holds one, so that a patch carrying a field of that type would declare
// something nothing elsewhere could write.
type Holder struct {
	Thing hidden
	Name  string
}
