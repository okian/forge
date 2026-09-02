package plugin

import (
	"go/types"

	"github.com/okian/forge/internal/model"
)

// MarkerPkg is the import path a marker type is declared in, which is what
// TypeRef.Pkg holds for every built-in marker.
//
// A layer outside forge declares its marker in its own package and claims that,
// so this is here to be read rather than used: it is how a report tells a
// marker forge ships from one somebody added.
const MarkerPkg = model.MarkerPkg

// Model is the declaration being generated for, as forge read it.
type Model = model.Model

// Form says whether a declaration was written in an ordinary file or in one
// under the marker build tag.
type Form = model.Form

// The two forms a declaration may be written in, and the zero that is neither.
//
// A declaration whose underlying type is the author's own — a slice of the
// subject, say — is written inline, in a file with no build tag. One whose
// underlying type is forge's marker is written in a spec file, so that the
// marker's spelling and the generated type's can both exist under one name.
const (
	FormInvalid = model.FormInvalid
	FormInline  = model.FormInline
	FormSpec    = model.FormSpec
)

// LayerRef is one marker as the declaration named it.
type LayerRef = model.LayerRef

// Directive is one //forge: comment: which layer it names, and what was written
// after that.
//
// A field carries them as well as a declaration. What a layer reads from a
// field is usually its struct tag, but a directive above a field is where an
// option goes that a tag cannot hold — anything with a comma or an equals sign
// in it, or anything long enough that a tag would stop being readable.
type Directive = model.Directive

// DirectivePrefix opens a directive comment, and is what tells one from an
// ordinary comment above the same declaration.
const DirectivePrefix = model.DirectivePrefix

// Written returns the directives naming one layer, in the order they appear.
//
// Two directives for one layer are both returned rather than one being dropped.
// What two of them mean is the layer's question — one may repeat an option and
// one may add to it — and answering it here would be answering it for every
// layer at once.
func Written(held []Directive, layer string) []Directive {
	return model.Written(held, layer)
}

// TypeRef identifies a named type by its package and its own name.
type TypeRef = model.TypeRef

// RefOf returns the reference to a named type.
func RefOf(named *types.Named) TypeRef { return model.RefOf(named) }

// Struct is the subject as forge modelled it: its fields, the structs it
// reaches, what it already satisfies, and what it already declares.
//
// Named Struct for the common case and used for every subject, including one
// that is not a struct at all — a closed set is declared over a named integer,
// and what a layer wants to know about either is the same list of questions.
type Struct = model.Struct

// Field is one field of the subject, or of a struct it reaches.
type Field = model.Field

// Classified is a field's type together with what forge worked out about it,
// so that a layer reads an answer rather than walking go/types itself.
type Classified = model.Classified

// Class says what kind of thing a field's type is, which is the distinction a
// layer branches on before it looks at anything finer.
type Class = model.Class

// The classes a field's type may be, and the zero that is none of them.
//
// Published with the type, because a type whose values cannot be named is one
// a layer can print and not branch on.
const (
	ClassInvalid   = model.ClassInvalid
	ClassBasic     = model.ClassBasic
	ClassStruct    = model.ClassStruct
	ClassPointer   = model.ClassPointer
	ClassSlice     = model.ClassSlice
	ClassArray     = model.ClassArray
	ClassMap       = model.ClassMap
	ClassInterface = model.ClassInterface
	ClassChan      = model.ClassChan
	ClassFunc      = model.ClassFunc
)

// Import is one package a spelling depends on, and one a generated file has to
// bind.
//
// One type for both, because they are one fact: what a layer is told a type
// needs is what the file writing it has to bind.
type Import = model.Import

// Spelling is a type written the way generated code in one package has to write
// it, together with what that package must import for the writing to resolve.
//
// The two are one answer. A layer that took the text and worked the imports out
// for itself would be re-deriving from a string something the type already
// knows.
type Spelling = model.Spelling

// Spell returns how a type is written from inside a package, given what that
// package's files already bind.
//
// What is already bound matters and is not optional. A subject from a package
// called slices, in a file where a layer bound the standard library's, has to be
// written under some other name — and a layer spelling against its own imports
// instead would be right about the half of a file that does not compile.
// Context.Bound is what to pass.
func Spell(t types.Type, local string, bound []Import) Spelling {
	return model.Spell(t, local, bound)
}

// TypeString writes a type the way a reader reads it: named types by their own
// names, with no import paths in the middle.
//
// For a diagnostic or a comment rather than for generated code. What generated
// code needs is [Spell], which answers about one package and says what that
// package must import.
func TypeString(t types.Type) string { return model.TypeString(t) }

// TypeIdentity returns the string that tells one type from another, which is
// what a layer keys a map of types by.
//
// Two types that are the same type give one identity and two that are not give
// two, including two of one name from two packages.
func TypeIdentity(t types.Type) string { return model.TypeIdentity(t) }

// Camel writes a Go name with its first word lowered, which is what a member of
// a wire format or a closed set is usually called.
//
// A word at a time rather than a letter, because an exported name often opens
// with an initialism: ID becomes id rather than iD, and JSONValue becomes
// jsonValue. One rule in one place, so that a layer naming a wire member and a
// layer naming a set member do not disagree about what a Go name looks like.
func Camel(name string) string { return model.Camel(name) }

// Lower writes a name with its first letter lowered, and the rest of it exactly
// as it was.
//
// A letter and not a word, which is the difference from [Camel]: ID becomes iD
// here and id there. What it is for is joining fragments into an identifier —
// the second half of a name whose first half decides the case — rather than
// naming anything a reader of a document sees. [Camel] is what names those.
func Lower(name string) string { return model.Lower(name) }

// Upper writes a name with its first letter raised, and the rest of it exactly
// as it was.
//
// The other half of the same job as [Lower]: a generated identifier is often
// two names joined, and whichever goes second has to start the way the join
// needs rather than the way it was declared.
func Upper(name string) string { return model.Upper(name) }

// Through returns the sentence a diagnostic uses to say a subject cannot be
// reached, given the verb and what was being attempted.
//
// Worth using rather than writing: a layer refusing a subject it cannot name
// should refuse it in the same words as the layer beside it, and an author
// reading two reports should not have to work out that they say the same thing.
func Through(of *Struct, verb, what, into string) string {
	return model.Through(of, verb, what, into)
}

// Unattachable returns why a subject cannot carry a generated method, or the
// empty string where it can.
//
// A method belongs to its type and only that type's own package may declare
// one, so a subject from elsewhere takes nothing. A layer putting methods on
// the subject asks this before it plans them.
func Unattachable(s *Struct, pkg string) string { return model.Unattachable(s, pkg) }

// Uncopyable returns why a type must not be copied, and whether it must not be.
//
// A mutex, a value holding one, anything with a noCopy field: copying it is a
// bug the compiler does not catch and vet does. A layer generating a copy asks
// this first.
func Uncopyable(t types.Type) (string, bool) { return model.Uncopyable(t) }

// Unnameable returns why a type cannot be written from inside a package, and
// whether it cannot be.
//
// An unexported type from another package is the case: it exists, it can be
// held, and there is no way to write its name in a file generated here.
func Unnameable(t types.Type, pkg string) (string, bool) { return model.Unnameable(t, pkg) }
