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
// # Interfaces
//
// A generated type claims the standard library interfaces its methods add up
// to, in a var block near the end of its file:
//
//	var (
//		_ io.WriterTo      = (*Persons)(nil)
//		_ json.MarshalerTo = (*Persons)(nil)
//	)
//
// What is claimed is read off what was written rather than off what the stack
// was expected to write, so the claims are an account of the file they are in.
// They cost nothing at run time and are checked when the package is built: a
// stack that stops satisfying one fails there rather than at a call site.
//
// A claim can be turned off one at a time:
//
//	//forge:skip io.WriterTo
//	type Persons Collection[Json[Person]]
//
// The methods are still generated; only the claim goes. Skipping something the
// declaration was not going to claim is reported rather than passed over, since
// an author who wrote it believes the declaration does something it does not.
// The name All turns off the claim about the walk.
//
// Unlike the directives above, skip is forge's own rather than a layer's, and
// no layer may take a directive by that name.
//
// # Tags on the subject
//
// Some of what forge writes comes from the subject rather than from a marker.
// Two struct tags ask for it:
//
//	type Person struct {
//		Name  string `display:""`
//		Age   int    `display:"age"`
//		Email string `redact:""`
//	}
//
// A display tag puts the field in the type's String, in the order the fields
// were declared; a tag with a name labels it, so the Person above reads as
// "Ada age=36". Nothing is rendered through fmt, so a String costs what the one
// you would have written costs.
//
// A redact tag writes a LogValue with that field replaced by a fixed string.
// Implementing it is what takes the field out of a log — slog reaches for a
// value's fields when the value does not say otherwise, so a type with a secret
// in it and no LogValue prints the secret.
//
// A subject that is a struct around a single field of a predeclared type, whose
// display tag carries no label, also gets a text codec — MarshalText,
// AppendText and UnmarshalText. Its text is that field's, so there is nothing
// to decide; a struct with two fields has a format, and a format is something
// you pick rather than something forge guesses.
//
// The tag is what asks for it, for the same reason it asks for the String: a
// wrapper's text form and its rendering are one question, and encoding/json
// takes a TextMarshaler for a type with no JSON codec of its own — so a codec
// written unasked would turn {"ID":"x"} into "x" in every document the type
// appears in. A label means the rendering is for a person to read, which is a
// different answer to that question, so a labelled wrapper reads and does not
// encode.
//
// A collection that names exactly one sort key also gets Less and Swap, so that
// sort.Sort takes it directly. One naming several has several orders and no
// reason to prefer any of them, and gets its sorted views without these.
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
