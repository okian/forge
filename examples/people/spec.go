//go:build forgespec

// This file is the spec form, and it is here because [Recent] is a stack rather
// than a single layer.
//
// A stack has no honest underlying type to write down. Collection over Ring
// over Json would spell out as a slice of a phantom struct, which is neither
// what the type is nor something anybody should be able to construct — so forge
// owns the declaration and the author owns the description of it, and the two
// live under complementary build tags. Exactly one is ever in scope.
//
// The tag means this file is type-checked and never linked: rename [Person] and
// the line below stops compiling under `go build -tags forgespec`, which is the
// compiler check the arrangement exists to buy back. Nothing here has a body,
// and nothing here is in the package a user builds.
package people

import "github.com/okian/forge"

// Recent is the last few people seen, in the order they were seen.
//
// Six layers over one subject, which is the composition the whole design is
// for. Json writes a codec for [Person] and, because there is a container above
// it, one for this type as well — so the ring encodes in a single pass over its
// elements, calling the codec written for each of them, and decodes straight
// back into the ring without the document ever existing as a slice in between.
// Neither layer could do that alone: the ring knows nothing about what it
// holds, and the codec knows nothing about how many there are.
//
// Validate, Clone and Hash sit beside Json, which is what element layers over
// one subject look like: each writes about [Person] and none of them knows
// about the others, so the person read off the wire is the person whose rules
// can be asked about, the person a caller can take a copy of, and the person
// two of whom can be told apart by a number.
//
// Ring is what makes it bounded. A producer that outruns whatever reads this
// costs a thousand elements of memory rather than an increasing amount, and the
// thousand is decided here rather than discovered in production.
//
//forge:ring cap=1024
type Recent forge.Collection[forge.Ring[forge.Json[forge.Validate[forge.Clone[forge.Hash[Person]]]]]]

// Roster is the same bounded ring, behind a read-write lock.
//
// The declaration everything about concurrency in forge exists for. A lock over
// a container is easy to write and hard to write safely, and the hard part is
// iteration: a walk handed out from behind a lock races if the caller walks it
// outside and holds the lock across arbitrary code if they walk it inside.
//
// So the lock does not hand one out. Everything the ring offers moves to a type
// of the lock's own making, unreachable except through [Roster.Do] and
// [Roster.RDo], which run a function with the lock held and hand it a
// [RosterView] for as long as the call lasts. What is left on the outside is
// what can be answered without holding anything open: a count, a copy, and the
// document.
//
// The document is the composition worth reading. Json gives [Person] a codec;
// the lock, having taken the walk away, is what writes the codec for the
// container — over a copy taken under the read lock, so that a slow reader on
// the other end of the encoder's writer cannot hold this against every writer
// for as long as it takes to time out.
//
//forge:ring cap=64
type Roster forge.Guarded[forge.Ring[forge.Json[Person]]]
