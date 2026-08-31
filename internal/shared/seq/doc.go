// Package seq is the shared sequence view: the lazy half of the query surface,
// written once and emitted once per package.
//
// It is not a layer. Nothing declares it, no marker claims it, and no
// declaration is generated for it — a layer names it among what it requires,
// and the stage that assembles a package's output emits it once for however
// many declarations asked. That is what the requiring is for: two declarations
// over one package share the view, and a package with twenty of them holds one
// copy rather than twenty.
//
// The division it sits on is between what a combinator needs to know about an
// element and what it does not. Filtering, taking, skipping and mapping hand
// elements along without looking at them, so they can be written once against a
// type parameter; a predicate over a field or a projection to one cannot be,
// and is generated against the subject. The generated half hands its results
// here, so a chain that starts subject-aware and becomes type-agnostic keeps
// chaining rather than ending at the boundary.
//
// The view's own bodies live in the package beside this one, as compiling Go,
// for the same reason a layer's do: a mistake in them is a build failure where
// they were written rather than a syntax error in somebody's generated file.
// They are emitted unchanged, since a view generic over its element has nothing
// in it that depends on which declaration asked.
package seq
