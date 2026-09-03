// Package mapping generates a constructor that builds one type from another:
// target members matched to source members by name where that is unambiguous
// and assignable, settled by a //forge:map hint where it is not, and refused
// where they are neither.
//
// The marker is [github.com/okian/forge.Map], the one two-parameter marker in
// the catalog: a bridge over a source and a target rather than a layer over a
// stream. What it generates is a package function — PersonFromUser for
// Map[User, Person] — so the target's method set is untouched and the source
// may be a struct or an interface.
package mapping
