// Package model holds the subjects the redaction layer is asked about.
//
// One per thing that can be true of a value with a secret in it, so that a test
// naming a subject is naming a shape rather than a set of fields somebody has
// to read the whole file to understand.
package model

import (
	"log/slog"

	"redactfixture/other"
)

// Account is the ordinary case: a value with a secret and some fields that are
// not.
type Account struct {
	Name  string
	Age   int
	Ratio float64
	Token string `redact:""`

	// held is unexported, so the value written for this type leaves it out —
	// which is what stops a handler printing it through %+v.
	held string
}

// Nested holds its secret one struct down, which is the case a method on the
// outer type alone does not cover: slog resolves one value at a time, so
// handing the inner value over unchanged prints what is in it.
type Nested struct {
	Name        string
	Credentials Credentials
}

// Credentials is what Nested holds.
type Credentials struct {
	User  string
	Token string `redact:""`
}

// Indirect reaches its secret through a pointer, which slog resolves like the
// value it points at — so this one can be masked.
type Indirect struct {
	Name  string
	Creds *Credentials
}

// Collected reaches its secret through a slice, which slog does not resolve
// into. There is no way to mask it, so asking is refused.
type Collected struct {
	Name  string
	Creds []Credentials
}

// Keyed reaches its secret through a map's value.
type Keyed struct {
	Name  string
	Creds map[string]Credentials
}

// KeyedBy reaches it through the other half of a map, which is walked because a
// key is printed as readily as a value is.
type KeyedBy struct {
	Name  string
	Creds map[Credentials]string
}

// Arrayed reaches it through an array, which slog resolves into no more than it
// resolves into a slice.
type Arrayed struct {
	Name  string
	Creds [2]Credentials
}

// Named reaches its secret through a named slice type, whose element is not
// something a classification of the field carries.
type Named struct {
	Name  string
	Creds CredentialList
}

// CredentialList is the named slice.
type CredentialList []Credentials

// Cyclic reaches itself, which is an ordinary thing to write and the shape a
// single pass over the closure answers wrongly.
type Cyclic struct {
	Token string `redact:""`
	Next  *Cyclic
}

// Clean has nothing to hide, so asking for redaction over it is asking for a
// method that says what slog already says.
type Clean struct {
	Name string
	Age  int
}

// Reaching holds a value that reaches a secret without having one of its own,
// which is what the settling pass exists to work out.
type Reaching struct {
	Name string
	Deep Nested
}

// Anonymous holds its secret inside a struct written in place, which has no
// name and so can never be given a method.
//
// slog prints what it is handed when the value cannot say otherwise, and this
// one cannot: there is nothing to attach a log value to. Refused.
type Anonymous struct {
	Name string
	Info struct{ Inner Credentials }
}

// AnonymousTag writes the tag inside the in-place struct rather than reaching a
// named one, which is the same refusal by the shorter route.
type AnonymousTag struct {
	Name string
	Info struct {
		Secret string `redact:""`
	}
}

// Instantiated reaches its secret through an instantiation of a generic, which
// is a type this package cannot declare a method on.
//
// The dangerous one. A method written for it would attach to the generic rather
// than to the instantiation — the argument reads as a receiver type parameter —
// so every other instantiation would silently log through it.
type Instantiated struct {
	Name string
	Held Holder[Credentials]
}

// Holder is the generic Instantiated reaches its secret through.
type Holder[T any] struct {
	Held T
}

// Foreign reaches its secret in another package, which this one cannot declare
// a method on.
type Foreign struct {
	Name  string
	Creds other.Credentials
}

// MaskedForeign masks the field that reaches the foreign secret, which is what
// the refusal for Foreign tells an author to do.
//
// The route the walk does not take. A masked field stops it — nothing below one
// is printed, so nothing below one is looked at — and the type it holds is
// still in the closure with something to hide and nowhere to put a method.
// Writing for it produces a method on somebody else's type, which does not
// compile in a file the author cannot edit.
type MaskedForeign struct {
	Name  string
	Creds other.Credentials `redact:""`
}

// MaskedGeneric is the same route to the worse end: a method written for the
// instantiation attaches to the generic, so every other instantiation of it
// silently starts logging through this one.
type MaskedGeneric struct {
	Name string
	Box  Holder[Credentials] `redact:""`
}

// UnexportedForeign reaches the foreign secret through a field slog cannot
// read, which the walk skips for its own sake and which still leaves the type
// in the closure.
type UnexportedForeign struct {
	Token string `redact:""`
	held  other.Credentials
}

// Reaches holds its secret behind an unexported field, which a handler prints
// through %+v along with everything under it.
type Reaches struct {
	Token string `redact:""`
	Held  Session
}

// Session reaches a secret only through a field nothing outside its package can
// read, which does not stop a handler formatting it.
type Session struct {
	Name  string
	creds Credentials
}

// Listed holds the author's own method behind a slice, where slog will not ask
// for it any more than it would ask for one written here.
type Listed struct {
	Token string `redact:""`
	List  []Handwritten
}

// Handwritten declares the method itself, which is the author overriding what
// would otherwise be written for them.
type Handwritten struct {
	Name  string
	Token string `redact:""`
}

// LogValue is the author's own, and is the one that is kept.
func (h Handwritten) LogValue() slog.Value {
	return slog.StringValue("nothing to see")
}

// Delegating holds a value whose author wrote the method, so there is nothing
// here to write and nothing above it to write either.
type Delegating struct {
	Name string
	Held Handwritten
}

// OptedOut writes the ignore form, which is an author saying to leave the field
// alone rather than asking for it to be masked.
type OptedOut struct {
	Name  string
	Token string `redact:"-"`
}
