// Package options reads what an author wrote on a declaration and holds it to
// what the layers accept.
//
// An option is a directive comment above the declaration it configures:
//
//	//forge:collection sort=Age,LastName index=Name
//	//forge:ring cap=1024 overflow=overwrite
//	type Persons Collection[Ring[Json[Person]]]
//
// The whole of this package exists because that text has no compiler. Struct
// tags are the warning: a misspelled key in one does nothing, silently, for as
// long as it takes somebody to notice the behaviour they configured never
// happened. So an unknown key is an error, a value that names a field is
// resolved against the subject's real fields, and a value with a closed set of
// answers is held to it — each of them reported at the option rather than at
// the declaration, because that is the text the author has to change.
//
// Two rules bend, and both bend towards saying something more useful rather
// than less.
//
// An option written for a layer this release does not ship is not an unknown
// key: the schema it would be checked against is provisional, and the answer
// its author needs is that the layer is not in this release, which generation
// gives them. That leniency is narrower than it sounds. A key the schema does
// not list is passed over, and an option the layer would need is not demanded —
// both are questions a finished schema answers. But a key the schema *does*
// list is held to its value, because a field that does not exist is not a field
// whoever finishes the layer. The cost is that directives which were silent
// become errors when the layer ships, which is the better half of the trade
// against unknown-key errors from a schema nobody has written.
//
// And an option that belongs on a field is refused with the fact that it does,
// rather than as a key nobody has heard of. Where such an option is written is
// decided — above the field it applies to — and nothing here reads one from
// there yet, so the refusal says to remove it rather than sending its author to
// write a directive that the stage collecting declarations would report as
// attached to nothing.
package options
