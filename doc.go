// Package forge declares the marker types that turn a Go type declaration
// into a code generation request.
//
// A declaration whose right-hand side instantiates one or more markers asks
// the forge command for an implementation:
//
//	//forge:collection sort=Age index=Name
//	//forge:ring cap=1024 overflow=overwrite
//	type Persons Collection[Ring[Json[Person]]]
//
// The command resolves that instantiation into a layer stack — Collection over
// Ring over Json over Person — and emits a concrete type with the combined
// API: ring-buffer storage, query methods specialised to Person's fields, a
// reflection-free JSON codec, and the standard library interfaces those layers
// imply.
//
// Markers exist only to be named in declarations. They carry no behavior, hold
// no state, and generated code never refers to them: it imports the standard
// library and the subject's own dependencies, nothing more.
//
// # Layer kinds
//
// Reading a stack outward, the innermost type argument is the subject and each
// marker wrapping it is one layer. Every marker declares a kind, which decides
// where it may appear and what it contributes.
//
//   - Subject — the concrete type the whole stack is specialised to, and the
//     only one whose fields the generated code can see. Exactly one appears in
//     a stack, innermost. It is the author's own type, not a marker.
//   - Element — attaches capabilities to the subject rather than to the
//     container around it, so the methods it generates take the subject as
//     their receiver and the element type seen above it is unchanged.
//   - Storage — fixes the underlying representation. At most one appears in a
//     stack, and a refining layer written with none beneath it gets [Slice].
//   - Refining — adds API over the storage beneath it without replacing it.
//   - Decorator — wraps everything beneath it without changing the element
//     type. A decorator may remove capabilities as well as add them, and the
//     order decorators are written in is significant, outermost first.
//   - Transport — terminates a stack with an encoding or an I/O boundary. At
//     most one appears in a stack, and it must be outermost.
//
// Each marker's documentation names its kind, the //forge: directive it takes
// options from, and its stage. A marker documented as stage v1 is implemented.
// One documented as v1.x is declared so that a declaration naming it
// type-checks and so that generation can report it as not yet implemented,
// which is a better answer than an undefined identifier.
//
// # Declaration forms
//
// A marker's own underlying type is a placeholder chosen so that declarations
// type-check. It is not the representation forge generates. Storage, refining,
// decorator and transport markers are declared as slices, so the underlying
// type of a single-layer declaration such as Collection[Person] really is
// []Person and the value is usable exactly as written. That is the inline
// form: an ordinary file, no build tag, no editor configuration.
//
// Nesting does not compose that way. The underlying type of
// Collection[Ring[Person]] is []Ring[Person] rather than []Person, which is
// meaningless as a representation and dangerous to expose. Element markers
// have the same problem for a different reason: the transparent form they want
// is a generic alias to its own type parameter, which Go does not allow, so
// they are zero-sized phantom structs and a stack containing one has no honest
// underlying type either.
//
// Those declarations go in a spec file guarded by //go:build forgespec, which
// is type-checked but never linked, while forge owns the real declaration in a
// complementary //go:build !forgespec file. The two tags are complements, so
// exactly one declaration is ever in scope, and the spec file still fails to
// compile if the subject is renamed or deleted.
//
// A spec file may import this package qualified, or dot-import it for the
// unqualified spelling the examples use. Prefer the qualified form in a file
// that declares types of its own: a dot import brings two dozen ordinary nouns
// — Set, Index, Page, Default, Slice, Hash, Clone — into file scope, where any
// of them can collide with the author's own declarations.
//
// # Running the generator
//
// Generation is driven by the forge command, usually through go generate:
//
//	//go:generate go run github.com/okian/forge/cmd/forge generate ./...
//
// Put that directive in a file with no build constraints, such as a package's
// doc.go. The go generate tool honors build constraints, so a directive placed
// in a file guarded by //go:build forgespec is skipped without a word.
package forge
