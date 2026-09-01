// Package other holds a type a subject's field is written in terms of, so that
// a setter has a signature that names a package the builder has to import.
package other

// Place is what a field of a subject somewhere else holds.
type Place struct {
	City string
}

// hidden is a type nothing outside this package can name.
type hidden struct{ Note string }

// Holder holds one, so that a setter for it would have a signature nothing
// elsewhere could write.
type Holder struct {
	Thing hidden
	Name  string
}
