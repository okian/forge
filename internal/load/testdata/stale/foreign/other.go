// Package foreign holds another generator's stale output, which is the
// control: the recovery for forge's own files must not be offered for a file
// somebody else's tool wrote, however similar the error looks.
package foreign

// Kept is what the author kept.
type Kept struct {
	Name string
}
