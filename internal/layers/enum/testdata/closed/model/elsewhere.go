package model

// The constants of Elsewhere, declared away from the type they belong to.
//
// A large closed set is usually written this way, and a walk that only looked
// at the file the type is in would find none of it — so what is walked is the
// package's scope rather than one file's declarations.
const (
	ElsewhereNear Elsewhere = iota
	ElsewhereFar
)

// Split is a set whose members are spread over two files, which is the shape
// that catches an order taken from a raw position.
//
// A package's files are parsed in parallel into one file set, so which of them
// gets the lower base is decided by which goroutine finished first. A set
// entirely inside one file is ordered correctly by accident.
type Split int

// The first half, in the file that sorts second.
const (
	SplitThird Split = iota + 2
	SplitFourth
)
