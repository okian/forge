// Package resolve follows a candidate declaration's instantiation down to the
// stack it names and the type that stack is specialised to.
//
// Discovery finds declarations that look like requests; this stage decides
// which of them are. The right-hand side of
//
//	type Persons Collection[Ring[Json[Person]]]
//
// is a chain of instantiations, and following it is one step applied
// repeatedly: take the named type, ask whether a marker declares it, and if so
// descend into its single type argument. The first type argument no marker
// claims is the subject, and the markers passed on the way down are the stack,
// outermost first.
//
// The walk is over types rather than over syntax, which is what makes the
// declaration's spelling stop mattering. A marker written qualified as
// forge.Collection and one written unqualified through a dot import are the
// same type by the time the type-checker is done with them, and a type
// argument that names an alias of an instantiation is followed through the
// alias to the instantiation itself. None of that survives in the syntax tree,
// and all of it is what an author expects to be able to write.
//
// What counts as a marker is a package, not a list: a generic type declared in
// the marker package and applied to a type argument is a layer here. Which
// layer, what kind it is and whether the stack it forms is legal are all
// questions for the registry and the composition rules, so a resolved entry
// carries its origin and nothing more.
//
// This stage's only judgement is arity — a marker takes exactly one type
// argument, because that is what keeps a stack linear. Everything else it
// declines to build, it declines in silence. A declaration whose outermost type
// no marker claims is an ordinary Go declaration over a generic type of the
// author's own, and forge has no business commenting on it; that holds even
// when a marker appears somewhere inside it, because a stack is built from the
// outside in and the outside is where there is nothing to build from.
package resolve
