//go:build forgespec

package model

import "github.com/okian/forge"

// Streams names three layers.
type Streams forge.Collection[forge.Ring[forge.Json[Person]]]

// Persons names four: a decorator over a refining layer over storage over an
// element layer.
type Persons forge.Guarded[forge.Collection[forge.Ring[forge.Json[Person]]]]

// Encoded names an element layer on its own, with nothing between it and the
// subject.
type Encoded forge.Json[Person]
