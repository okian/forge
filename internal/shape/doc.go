// Package shape describes what a layer exposes to the layer above it.
//
// Composition is checked in these terms rather than in terms of which layers
// may sit on which. A rule written per pair grows with the square of the
// catalog and has to be revisited every time a layer is added; a rule written
// against capabilities — this layer needs something ordered, that one makes
// what is beneath it concurrent — is written once and holds for layers nobody
// has thought of yet. That is the whole reason this vocabulary exists.
//
// A shape carries the method surface as well as the capability bits, because
// the bits answer only whether a stack may be built. A decorator has to wrap or
// withdraw every method on the declared type beneath it, and cannot wrap what
// it cannot enumerate; the stage that merges units has to know which names are
// already spoken for. Bits alone would paint decorators into a corner that is
// expensive to get out of later.
//
// Withdrawing is as important as adding. A lock that hands out an iterator is
// broken whichever way it is used, so the lock takes iteration away and offers
// scoped access instead — and the shape above it must not claim what is no
// longer there.
package shape
