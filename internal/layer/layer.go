package layer

import (
	"go/ast"
	"go/token"
	"strconv"
	"strings"

	"github.com/okian/forge/internal/emit"
	"github.com/okian/forge/internal/model"
	"github.com/okian/forge/internal/shape"
)

// Layer is one entry in a stack: the thing that claims a marker and generates
// for it.
type Layer interface {
	// Origin identifies the marker this layer claims, without type arguments.
	// Two layers may not claim the same one.
	Origin() model.TypeRef

	// Kind says where in a stack the layer may appear and what it contributes.
	Kind() model.Kind

	// OptionSchema declares every option the layer accepts. An option written
	// for it and not declared here is an error rather than a warning, which is
	// what keeps a misspelled option from being silently defaulted.
	OptionSchema() []OptionDef

	// Accepts reports whether the layer can sit on the shape beneath it,
	// returning an error naming what is missing when it cannot.
	//
	// The error carries no position and no code, because a layer has neither:
	// it is handed a shape and knows nothing about the declaration that
	// produced it. Composition turns what it says into a diagnostic pointing at
	// the stack entry that said it.
	Accepts(below shape.Shape) error

	// Shape returns what this layer exposes to the layer above it, which is the
	// shape beneath with whatever this layer adds — or withdraws.
	//
	// The declaration is passed because a layer's surface can depend on it. A
	// storage layer's methods are the same whatever it holds, but a layer that
	// names its methods after the subject's fields — a projection per field, a
	// sorted view per declared key — cannot say what it emits without knowing
	// which fields and which keys. A decorator above it has to wrap those by
	// name, so leaving them out of the shape would leave them unwrappable.
	//
	// A layer handed no declaration reports what it can without one rather than
	// refusing: what asks that way is asking what the layer is, not what it
	// would emit here. Three things count as none and a layer has to survive
	// all of them — a nil context, a context carrying no model, and a model
	// carrying no subject — because a declaration is described before its
	// subject is known to be one and the stages in between all ask.
	//
	// Only the surface may be shorter for it. The capabilities a layer adds and
	// withdraws are what the layers above are checked against, and a layer that
	// reported them differently with and without a declaration would compose
	// one way and be described another.
	//
	// Every method a layer puts on the surface carries that layer as its owner.
	// A method of a name already there is a method wrapped rather than a second
	// method, and the owner is what says which of the two the surface now
	// holds: a layer that left it unset and wrapped a method would be reported
	// as having done nothing, since the name did not change either.
	Shape(ctx *Context, below shape.Shape) shape.Shape

	// Generate returns the declarations this layer contributes.
	Generate(ctx *Context, below shape.Shape) (Unit, error)
}

// Context is what a layer generates against.
type Context struct {
	// Model is the declaration being generated for: its subject, its options
	// and its position.
	//
	// Its stack is the stack as the author wrote it, which is not always the
	// stack that will be generated: a refining layer written over no storage
	// has one filled in beneath it, and that happens after a layer has been
	// asked what it exposes — a layer asked before the insertion cannot be
	// handed the result of it. What is beneath a layer reaches it as the shape
	// it is given, which is the answer to the question the stack would only be
	// a way of guessing at.
	Model *model.Model

	// Options are the options written for this layer, already validated against
	// its schema, so a layer reads them without checking them again.
	Options model.Options
}

// ContextFor returns what one layer of a declaration is asked against, or nil
// when there is no declaration to ask about.
//
// The options are picked out here rather than handed down whole, so that a
// layer reads its own and cannot read another's. Every stage that asks a layer
// anything asks through this, because a stage that picked them out itself would
// be a second answer to the question of which options belong to which layer —
// and the two would agree until somebody changed one.
func ContextFor(held *model.Model, ref model.LayerRef) *Context {
	if held == nil {
		return nil
	}

	directive := ref.Directive()
	if written, ok := held.OptionsFor(directive); ok {
		return &Context{Model: held, Options: written}
	}
	return &Context{Model: held, Options: model.Options{Layer: directive}}
}

// Unit is what a layer contributes to a package.
//
// It is declarations rather than "methods on the declared type", and that is
// the load-bearing choice: an element layer's receiver is the subject, not the
// container the declaration names, so a unit that could only describe methods
// on the declared type would have no way to say what a codec for Person is.
type Unit struct {
	// Decls holds the declarations to emit, in the order they should appear.
	Decls []ast.Decl

	// Comments holds every comment group from the file the declarations were
	// parsed from, and Fset resolves the positions they all carry.
	//
	// The three belong together and a layer that parsed anything owes all
	// three. A comment is not reachable from the declaration it documents — the
	// printer finds it by position, in a list that belongs to the file — so
	// declarations handed over without them are printed without every comment
	// inside a function body, and printed against the wrong file set they are
	// printed with the comments in the wrong places. Both produce Go that
	// compiles, and the output is committed.
	//
	// A layer that builds its declarations rather than parsing them carries no
	// positions and leaves both empty; a doc comment hangs off the declaration
	// itself and travels with it.
	Comments []*ast.CommentGroup
	Fset     *token.FileSet

	// Imports holds the imports the declarations need. Generated code imports
	// the standard library and the subject's own dependencies and nothing else,
	// so that a generated file never needs a runtime package to be in step with
	// the binary that wrote it.
	//
	// Each carries the name it is bound to as well as its path, because the
	// declarations name a package by that name and nothing downstream can
	// recover it: an import binds a package to the name it declares, which is
	// not always the last element of its path, and a layer whose subject comes
	// from a package called like one the layer already imports has to bind it to
	// something else. A path alone would leave the file importing one thing and
	// the bodies naming another.
	Imports []emit.Import

	// Assertions holds the compile-time claims to emit for what was generated.
	Assertions []Assertion

	// Requires names the helper types this unit calls into and does not itself
	// declare. Two declarations reaching one helper name it twice and it is
	// emitted once, which is what keeps a codec for a shared subject from being
	// written into a package twice over.
	Requires []model.TypeRef
}

// Assertion is a compile-time claim about generated code.
//
// The claim is worth making because it fails at build time in the generated
// file rather than at run time in the caller's, and because it documents what
// the declaration bought: a reader who cannot see forty methods can see the
// interfaces they add up to.
type Assertion struct {
	// Interface is the interface being claimed.
	Interface model.TypeRef

	// Method, when set, claims one method's signature rather than the whole
	// interface, as a method expression:
	//
	//	var _ func(*Persons) iter.Seq[Person] = (*Persons).All
	//
	// A method expression is checked at compile time and calls nothing, so it
	// costs no allocation and no initialisation — unlike an interface
	// assertion, which has to name a value.
	Method string

	// Signature is the function type a method expression is checked against,
	// receiver included. It is empty for a whole-interface claim.
	Signature string
}

// ValueKind says what shape an option's value takes.
//
// It is what lets one validator check every layer's options rather than each
// layer checking its own: a value that names a field is resolved against the
// subject's fields wherever it appears, so a renamed field is an error on every
// option that named it and not only on the ones whose layer remembered to look.
type ValueKind uint8

const (
	// ValueNone is an option written on its own, with no value at all.
	ValueNone ValueKind = iota

	// ValueString is free text.
	ValueString

	// ValueBool is true or false.
	ValueBool

	// ValueInt is a whole number.
	ValueInt

	// ValueEnum is one of the values the option declares.
	ValueEnum

	// ValueField names one field of the subject.
	ValueField

	// ValueFields names one or more fields of the subject, separated by commas.
	ValueFields
)

// valueKindNames gives each kind the spelling the list command prints.
var valueKindNames = [...]string{
	ValueNone:   "none",
	ValueString: "string",
	ValueBool:   "bool",
	ValueInt:    "int",
	ValueEnum:   "enum",
	ValueField:  "field",
	ValueFields: "fields",
}

// String returns the kind's lower-case name.
func (k ValueKind) String() string {
	if int(k) >= len(valueKindNames) {
		return "value(" + strconv.Itoa(int(k)) + ")"
	}
	return valueKindNames[k]
}

// Scope says what an option is written about.
type Scope uint8

const (
	// ScopeDeclaration is the ordinary case: the option is written above the
	// declaration and applies to the whole of it.
	ScopeDeclaration Scope = iota

	// ScopeField is an option written above one field of the subject, applying
	// to that field alone.
	//
	// It exists for the questions a layer can only answer per field. A codec
	// asked to fall back on reflection for one field it cannot see through is
	// the case that forces it: written on the declaration, the option would
	// either name a field in its value — a second grammar, with a second way to
	// misspell a field — or turn reflection on for every field at once, which
	// is the opposite of marking a boundary.
	//
	// A field-scoped option is written where the field is, which is where the
	// author is looking when they decide the field needs one and where a reader
	// finds it without knowing to look elsewhere. That places it in the
	// subject's own source, so the stage that walks fields is the stage that
	// collects it; validating what is written above a *declaration* — which is
	// what this package does — refuses it and says where it belongs.
	ScopeField
)

// scopeNames gives each scope the spelling a diagnostic uses.
var scopeNames = [...]string{
	ScopeDeclaration: "declaration",
	ScopeField:       "field",
}

// String returns the scope's lower-case name.
func (s Scope) String() string {
	if int(s) >= len(scopeNames) {
		return "scope(" + strconv.Itoa(int(s)) + ")"
	}
	return scopeNames[s]
}

// OptionDef declares one option a layer accepts.
type OptionDef struct {
	// Key is the option's name, the "sort" of sort=Age.
	Key string

	// Scope says whether the option is written about the declaration or about
	// one field of the subject. The zero value is the declaration, which is
	// what nearly every option is about.
	Scope Scope

	// Value says what shape the option's value takes.
	Value ValueKind

	// Values holds the accepted values of a ValueEnum option, in the order they
	// should be offered. It is empty for every other kind.
	Values []string

	// Default is the value used when the option is not written, rendered as it
	// would be written. An option with no default and no value written is
	// simply absent.
	Default string

	// Required records that the layer cannot generate without this option, so
	// leaving it out is an error rather than a default.
	Required bool

	// Doc is the one-line summary the list command prints beside the option. It
	// begins in lower case and carries no terminating punctuation.
	Doc string
}

// String returns the option as it would be written, with its accepted values
// where it has a closed set: "overflow=overwrite|error".
func (d OptionDef) String() string {
	switch {
	case d.Value == ValueNone:
		return d.Key
	case len(d.Values) > 0:
		return d.Key + "=" + strings.Join(d.Values, "|")
	default:
		return d.Key + "=<" + d.Value.String() + ">"
	}
}
