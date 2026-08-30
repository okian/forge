// Package arity holds declarations written against a marker package of its
// own, which no default resolution claims.
package arity

import "stacksfixture/markers"

// Pipes names a marker written with two type arguments.
type Pipes markers.Pipeline[string, int]

// Names names a single-argument marker from the same package, so that a
// resolver pointed at it has something to succeed on.
type Names markers.Collection[string]

// Opaques is specialised to a type from the marker package that is not generic,
// which is a subject and not a layer.
type Opaques markers.Collection[markers.Opaque]

// Nested puts the two-argument marker under a layer, so the report has a stack
// above it to point through.
type Nested markers.Collection[markers.Pipeline[string, int]]
