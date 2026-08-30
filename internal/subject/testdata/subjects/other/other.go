// Package other holds a type declared in a second package of the same module,
// which is local however far from the subject it is written.
package other

// Place is inside the module and outside the subject's package.
type Place struct {
	Country string `json:"country"`
}
