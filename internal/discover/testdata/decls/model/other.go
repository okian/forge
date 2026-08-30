package model

// Numbers instantiates a generic type of the author's own. Discovery cannot
// tell it from a generation request, so it is a candidate here and is dropped
// by resolution.
type Numbers Box[int]

// Trailing carries its directive after the declaration, on the same line.
type Trailing Box[int] //forge:ring cap=4
