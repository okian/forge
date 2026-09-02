//go:build forgespec

package domain

import (
	"example.com/mine/tally"
)

// People is a declaration over a marker forge does not ship.
//
// The storage beneath it is nobody's to write: a refining layer written with
// none has one filled in, which is the same arrangement a declaration over
// forge's own refining layer gets.
//
//forge:tally by=City
type People tally.Tally[Person]
