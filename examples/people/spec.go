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
// Three layers over one subject, which is the composition the whole design is
// for. Json writes a codec for [Person] and, because there is a container above
// it, one for this type as well — so the ring encodes in a single pass over its
// elements, calling the codec written for each of them, and decodes straight
// back into the ring without the document ever existing as a slice in between.
// Neither layer could do that alone: the ring knows nothing about what it
// holds, and the codec knows nothing about how many there are.
//
// Ring is what makes it bounded. A producer that outruns whatever reads this
// costs a thousand elements of memory rather than an increasing amount, and the
// thousand is decided here rather than discovered in production.
//
//forge:ring cap=1024
type Recent forge.Collection[forge.Ring[forge.Json[Person]]]
