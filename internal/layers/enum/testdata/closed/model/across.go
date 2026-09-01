package model

// The other half of Split, in the file that sorts first.
//
// Declared here so that the set's members are found in two files and have to be
// put back in the order a reader of the package would see: this file before
// elsewhere.go, and these two before the two there.
const (
	SplitFirst Split = iota
	SplitSecond
)
