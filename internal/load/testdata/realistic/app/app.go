package app

import "io"

// Number is an ordinary constraint.
type Number interface{ ~int | ~float64 }

var registry = map[string]int{}

func init() {
	registry["seeded"] = 1
}

// Sum is a generic function, which many packages have.
func Sum[T Number](xs []T) T {
	var total T
	for _, x := range xs {
		total += x
	}
	return total
}

// Persons is the declared type; WriteTo is generated and does not exist yet.
type Persons []int

// The canonical compile-time assertion that a generated method exists.
var _ io.WriterTo = (*Persons)(nil)

// Describe keeps its body, because a function literal is part of an expression
// the type-checker still has to evaluate.
var Describe = func(p Persons) int { return len(p) }
