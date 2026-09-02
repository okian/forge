package plugin

import (
	"go/types"

	"github.com/okian/forge/internal/model"
	"github.com/okian/forge/internal/words"
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
	ClassInvalid = model.ClassInvalid

	// ClassNamed is a defined type, whatever it is defined over, and is the
	// class most fields of a real subject carry. A struct somebody declared is
	// named; ClassStruct is the unnamed kind, written inline in the field.
	ClassNamed = model.ClassNamed

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

// Every question forge answers about an English word or a Go identifier is
// answered in one place, and these are how a layer asks it.
//
// A layer author must not roll their own. That is not a style preference: a
// codec naming a wire member, an enumeration naming a set member and a
// collection naming a projection are three layers answering one question, and
// three answers to it is one library with three opinions about what a Go name
// looks like. The author of the subject then has to live with all three, in
// files they cannot edit.
//
// What is behind them is a real English dictionary compiled into the forge
// binary, and the Go initialism set the language's own linters hold each other
// to. A layer that appended an s would get Persons, Childs and Aliaseses; one
// that concatenated a prefix would get SortedById in a tree where revive is
// already refusing it.

// Plural returns the name a collection of these is called: Person is People,
// Box is Boxes, Data is Data, ID is IDs.
//
// Only the last word inflects, so HomeAddress is HomeAddresses. A name that is
// already plural comes back unchanged, which is what stops Aliaseses.
func Plural(name string) string { return words.Plural(name) }

// Singular returns the name one of these is called: People is Person, Boxes is
// Box. A name that is already singular comes back unchanged.
func Singular(name string) string { return words.Singular(name) }

// IsPlural reports whether a name is already the plural of something, which is
// the question to ask before deriving a name from a field that may be a slice.
func IsPlural(name string) bool { return words.IsPlural(name) }

// Words takes an identifier apart into the words a reader would say it in:
// UserIDToken is User, ID and Token.
//
// A run of capitals is one word, and an initialism made plural keeps its s —
// so UserIDs is User and IDs rather than User, I and Ds.
func Words(name string) []string { return words.Words(name) }

// Join writes words as one exported Go identifier: Join("user", "id") is
// UserID, and so is Join("userId").
//
// Whole names are as good as single words, because each part is taken apart
// before it is put back together. An initialism is spelled in full case
// wherever it falls.
func Join(parts ...string) string { return words.Join(parts...) }

// Export writes one name as an exported Go identifier, which is [Join] for the
// case a layer asks for most: userId is UserID, and http_server is HTTPServer.
func Export(name string) string { return words.Export(name) }

// Around returns a name built around one that is already spelled: New around
// Persons is NewPersons, and Err around persons with Full after it is
// errPersonsFull.
//
// This rather than [Join] wherever the middle is a name somebody wrote. A
// constructor is named after the type it builds and a sentinel error after the
// type that refuses, and the declaration's own name has to come through exactly
// as its author spelled it — NewMyIdThing belongs to MyIdThing, and NewMyIDThing
// reads as belonging to something else. Everything around it is spelled.
//
// The visibility is the caller's because it is the declaration's: a constructor
// for an unexported container has no business being reachable from outside the
// package the container is unexported in.
func Around(exported bool, before, held string, after ...string) string {
	return words.Around(exported, before, held, after...)
}

// Block is the names visible inside one generated function body.
//
// What it is for beyond uniqueness is shadowing. A local named slices in a file
// that imports slices does not fail to compile; it fails on the next line that
// meant the package, in generated code the author cannot edit. A layer writing
// nested bodies has the same problem one level down, where a copy of a slice of
// slices binds two variables that would otherwise have one name.
type Block = words.Block

// Locals returns a scope for one generated function body, given the names
// already visible in it: the packages the file imports, the receiver, and the
// parameters.
//
// A local that collides is renamed rather than refused, because nothing outside
// the function can see it and the rename costs a reader nothing. Numbered from
// two, so that held2 says what it is.
func Locals(taken ...string) *Block { return words.Locals(taken...) }

// Camel writes a Go name with its first word lowered, which is what a member of
// a wire format or a closed set is usually called.
//
// A word at a time rather than a letter, because an exported name often opens
// with an initialism: ID becomes id rather than iD, and JSONValue becomes
// jsonValue. Only the case of the first word changes — a name that goes out on
// a wire keeps every other letter it was written with.
func Camel(name string) string { return words.Camel(name) }

// Lower writes a name with its first letter lowered, and the rest of it exactly
// as it was.
//
// A letter and not a word, which is the difference from [Camel]: ID becomes iD
// here and id there. What it is for is joining fragments into an identifier —
// the second half of a name whose first half decides the case — rather than
// naming anything a reader of a document sees. [Camel] is what names those, and
// [Join] is what builds one out of parts.
func Lower(name string) string { return words.Lower(name) }

// Upper writes a name with its first letter raised, and the rest of it exactly
// as it was.
//
// The other half of the same job as [Lower]: a generated identifier is often
// two names joined, and whichever goes second has to start the way the join
// needs rather than the way it was declared.
func Upper(name string) string { return words.Upper(name) }

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
