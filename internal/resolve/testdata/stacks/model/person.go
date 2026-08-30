// Package model holds the declarations resolution has to follow, written with
// qualified markers.
package model

import (
	"github.com/okian/forge"

	"stacksfixture/other"
)

// Person is the subject most of these declarations are specialised to.
type Person struct {
	Name string
	Age  int
}

// Pairing is a generic type of the author's own, used as a subject that is
// itself an instantiation.
type Pairing[K comparable, V any] struct {
	Key   K
	Value V
}

// Bucket is a generic type of the author's own that no marker claims.
type Bucket[T any] []T

// People names one layer over the subject.
type People forge.Collection[Person]

// Recent names two.
type Recent forge.Collection[forge.Ring[Person]]

// Counts instantiates a type of the author's own, which is an ordinary
// declaration and not a request.
type Counts Bucket[int]

// Sessions is specialised to a subject declared in another package.
type Sessions forge.Collection[other.Session]

// Pairs has a subject that is an instantiation, which resolution follows like
// any other named type.
type Pairs forge.Collection[Pairing[string, int]]

// Pointers has a pointer subject, which resolution reports as written and
// leaves for the rules to reject.
type Pointers forge.Collection[*Person]

// Degrees has a basic subject, which is the same story.
type Degrees forge.Collection[int]

// Wrapper is generic itself, so its subject is still a type parameter.
type Wrapper[T any] forge.Collection[T]

// Window is an alias to an instantiation, so a declaration naming it has an
// alias in the middle of its stack.
type Window = forge.Ring[Person]

// Buffered names its storage layer through that alias.
type Buffered forge.Collection[Window]

// Human is an alias to the subject, so a declaration naming it has an alias at
// the bottom of its stack rather than in the middle.
type Human = Person

// Aliased is specialised to the subject through that alias.
type Aliased forge.Collection[Human]

// Wrapped puts a marker inside a generic of the author's own. The outermost
// type is what a stack would have to be built from, and no marker claims it.
type Wrapped Bucket[forge.Json[Person]]
