package app

// Mistyped is a genuine error in the author's own code, outside any function
// body, so stripping bodies cannot make it go away.
var Mistyped int = "not an int"
