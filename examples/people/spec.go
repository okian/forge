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
// Eight layers over one subject, which is the composition the whole design is
// for. Json writes a codec for [Person] and, because there is a container above
// it, one for this type as well — so the ring encodes in a single pass over its
// elements, calling the codec written for each of them, and decodes straight
// back into the ring without the document ever existing as a slice in between.
// Neither layer could do that alone: the ring knows nothing about what it
// holds, and the codec knows nothing about how many there are.
//
// Validate, Clone, Hash, Builder and Patch sit beside Json, which is what
// element layers over one subject look like: each writes about [Person] and
// none of them knows about the others, so the person read off the wire is the
// person whose rules can be asked about, the person a caller can take a copy
// of, the person two of whom can be told apart by a number, the person a caller
// can assemble a field at a time, and the person a caller can change part of.
//
// Builder and Validate are the pair worth looking at. The builder reads the
// same tags the check does and asks a different question of them — was anything
// given — so a value that leaves the builder has every field the author called
// for, and a value that passes the check has fields that are any good. Neither
// layer knows the other is there.
//
// Ring is what makes it bounded. A producer that outruns whatever reads this
// costs a thousand elements of memory rather than an increasing amount, and the
// thousand is decided here rather than discovered in production.
//
//forge:ring cap=1024
type Recent forge.Collection[forge.Ring[forge.Json[forge.Validate[forge.Clone[forge.Hash[forge.Builder[forge.Patch[Person]]]]]]]]

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

// Statuses asks for the closed set [Status] already was.
//
// What it buys lands on [Status] rather than here, which is what an element
// layer is: the API of a closed set is about one value, so [Status.String],
// [Status.Valid], [ParseStatus] and [ValuesStatus] are the four of it a caller
// reaches for by name. Nothing says what the members are — the const block is the
// declaration, so a status added there is in the set without anybody editing a
// second list.
//
// This type is a slice of them, which is the storage a declaration that names
// no container falls back to. It is worth having and is not the point: a list
// of statuses is a reasonable thing to hold, and the reason to write this line
// is the seven the declaration produces. Five are methods on [Status]: the two
// above, and [Status.MarshalText], [Status.AppendText] and
// [Status.UnmarshalText], which carry a member over a wire under its name. The
// other two are functions of the package, since a parser has no value to be
// called on and a list of the members is not about any one of them.
//
// It goes in a spec file for the reason every declaration whose underlying type
// is forge's does: [forge.Enum] is a marker holding nothing, so an inline
// declaration would make this a zero-sized struct rather than the slice the
// generated file declares. The build tags are what let the two spellings of one
// name exist in one package.
//
// The text codec is what carries a member over a wire, and it is reached from
// two directions. The standard library asks a type for one before writing it,
// and so does the codec forge generates for a struct holding a Status — which
// is how [Credential.State] goes out as "revoked" rather than as 2, though
// nothing in either declaration mentions the other. Both being declared in this
// package is what puts them together; a closed set declared elsewhere is not
// seen, and its members go over forge's own wire as their numbers.
//
// It is also what refuses a value the set has no name for. A document holding a
// number, or a name the set does not hold, is refused where it is read: a
// status that got in would render as the type and the number for the rest of
// its life, and the log line holding it would carry an error where the state
// should be.
//
//forge:enum
type Statuses forge.Enum[Status]

// Credentials is a directory of credentials whose elements cannot be logged
// carelessly.
//
// Two element layers over one subject, doing unrelated things and neither
// knowing about the other. Json gives [Credential] a codec, and Redact gives it
// and everything it reaches a value that a handler prints instead of the
// fields — with [Secret.Token] replaced by a fixed string wherever it appears.
//
// The redaction is what makes the pairing worth reading. A codec and a log
// value disagree about a secret on purpose: the token has to go over the wire
// or the credential is useless, and it must not go into a log or the credential
// is compromised. Nothing about the subject says which of the two a caller is
// doing, so each layer answers for its own channel and the tag says which is
// which.
//
// Its elements and not itself. This type is a slice, and slog resolves the
// value it is handed rather than what is inside one: handed a Credentials it
// formats the slice, and the method on the element is never called. That is the
// same limit the layer refuses a secret behind a slice field for — the
// difference is only that a declared container cannot be refused, since being a
// slice of the subject is what it is for.
//
// What that costs here is nothing, and the reason is luck rather than the
// layer: [Credential.Secret] is a pointer, so formatting prints an address. A
// subject holding its secret by value would have it printed in full. Log the
// element.
//
// sort= gives a view ordered by owner and index= a lookup by the same field.
// One owner can hold several credentials, and a lookup keeps the last of any
// that share a key, so [Credentials.ByOwner] answers with one credential per
// owner rather than all of them. That is what an index is; the sorted view is
// what to walk when every credential for an owner is wanted.
//
//forge:collection sort=Owner index=Owner
type Credentials forge.Collection[forge.Json[forge.Redact[Credential]]]
