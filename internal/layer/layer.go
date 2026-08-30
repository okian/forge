package layer

import (
	"go/ast"
	"strconv"
	"strings"

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
	Shape(below shape.Shape) shape.Shape

	// Generate returns the declarations this layer contributes.
	Generate(ctx *Context, below shape.Shape) (Unit, error)
}

// Context is what a layer generates against.
type Context struct {
	// Model is the declaration being generated for: its subject, its stack and
	// its position.
	Model *model.Model

	// Options are the options written for this layer, already validated against
	// its schema, so a layer reads them without checking them again.
	Options model.Options
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

	// Imports holds the import paths the declarations need. Generated code
	// imports the standard library and the subject's own dependencies and
	// nothing else, so that a generated file never needs a runtime package to
	// be in step with the binary that wrote it.
	Imports []string

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

// OptionDef declares one option a layer accepts.
type OptionDef struct {
	// Key is the option's name, the "sort" of sort=Age.
	Key string

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
