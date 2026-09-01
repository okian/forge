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

	// Exposed is what the declared type offers once every layer has been asked
	// what it exposes, which is not the same question as what is beneath this
	// one.
	//
	// An element layer needs it and nothing else does. What an element layer
	// writes is about the subject, so it sits at the bottom of a stack and is
	// handed the subject — and a codec for the container holding subjects is
	// written against a walk it can only learn about by looking up. Everything
	// else reads the shape it is given.
	//
	// It is the zero shape while a stack is being composed, because it is what
	// composing works out: a layer asked what it exposes is being asked for one
	// of the answers this is made of, and reading it there would be reading a
	// tally of the layers that happened to have been asked first. Nothing may
	// be decided from it except what to generate.
	//
	// What is here has already been through every layer above, masking
	// included. A decorator that withdraws the walk leaves an element layer
	// with nothing to write a container codec over, which is the correct
	// answer rather than an obstacle: the decorator withdrew the walk because
	// walking is no longer safe, and it owns whatever replaces it.
	Exposed shape.Shape

	// declared is the type this layer puts its methods on, which is not always
	// the one the author wrote.
	//
	// A layer beneath an enclosing decorator declares onto a type of that
	// decorator's making, because the decorator is the declaration and what is
	// beneath it is a field. Read through [Context.Declared] rather than
	// directly, so that a layer with no decorator above it needs to know
	// nothing about any of this.
	declared string

	// holds is how the type this layer encloses is made, where it has to be
	// made at all.
	//
	// Only an enclosing decorator has one, and only where what it holds is a
	// container that cannot start as its zero value. Read through
	// [Context.Holds].
	holds *Constructor
}

// Holds returns how the type this layer encloses is made, and whether it has to
// be made at all.
//
// Nothing for every layer that encloses nothing, and nothing for a decorator
// over a container whose zero value is ready to use — which is the difference
// between a lock that needs a way in and one that does not.
func (c *Context) Holds() (Constructor, bool) {
	if c == nil || c.holds == nil {
		return Constructor{}, false
	}
	return *c.holds, true
}

// Holding returns the context told how the type this layer encloses is made.
//
// A copy, like [Context.Generating] and [Context.Declaring], and for the same
// reason: a context is handed to one layer and read by it.
func (c *Context) Holding(made *Constructor) *Context {
	if c == nil {
		return nil
	}

	held := *c
	held.holds = made

	return &held
}

// Declared returns the type this layer puts its methods on.
//
// The declaration's own name in the ordinary case, which is every case with no
// enclosing decorator in the stack. Where there is one, what is beneath it is
// held as a field rather than being the declared type, so it needs a name of
// its own — and a layer that wrote onto the declaration instead would be
// putting the unlocked methods on the locked type, which is the whole of what
// the decorator exists to prevent.
func (c *Context) Declared() string {
	if c == nil {
		return ""
	}
	if c.declared != "" {
		return c.declared
	}
	if c.Model == nil {
		return ""
	}
	return c.Model.Name
}

// Declaring returns the context with the name a layer declares onto, which is
// what a stack with an enclosing decorator in it hands the layers beneath.
//
// A copy, like [Context.Generating]: a context is handed to one layer and read
// by it, and one that could be changed by whoever holds it would be a way for
// two layers to disagree about a declaration neither of them owns.
func (c *Context) Declaring(name string) *Context {
	if c == nil {
		return nil
	}

	held := *c
	held.declared = name

	return &held
}

// Enclosing is a decorator that keeps what is beneath it rather than being it.
//
// A decorator that adds methods to what is below can leave it where it is: the
// methods land on the same type and the stack is one type deep. One that has to
// own something — a lock, a transaction — cannot, because the methods it wraps
// have to become unreachable without going through it, and a method on the same
// type is reachable by anybody holding one.
//
// So what is beneath moves to a type of the decorator's naming, and the
// declaration becomes a struct holding it. This is how a decorator says so, and
// what it returns is the name the layers beneath it will declare onto.
type Enclosing interface {
	// Encloses returns the name for what sits beneath this layer, given the
	// name whatever is above it declares onto.
	//
	// Given rather than assumed, because decorators compose: a lock inside a
	// lock encloses what the outer one already enclosed, and a name derived
	// from the declaration alone would have the two fighting over it.
	Encloses(declared string) string
}

// Declaring returns the type each layer of a stack puts its methods on, given
// the name the declaration was written under and the layers outermost first.
//
// Outermost inward, which is the only order this can be worked out in: what a
// layer declares onto is decided by the decorators above it, so the names are
// settled before anything reads them.
//
// A stack with no [Enclosing] layer gives every layer the declaration's own
// name, which is every stack anybody has written until one of those appears in
// it. A hole in the list — a marker nothing in the registry claims — encloses
// nothing, which needs no test of its own: a nil interface satisfies no
// assertion to a non-empty one.
//
// Here rather than where a stack is composed, because more than one stage
// answers for what a declaration will look like: composing decides it and
// describing has to agree, and two walks written to the same rule stay in step
// until the day one of them is edited.
func Declaring(declared string, layers []Layer) []string {
	out := make([]string, len(layers))

	held := declared
	for i, one := range layers {
		out[i] = held

		// Asked of every layer rather than of the decorators, because what may
		// enclose is a layer's own answer rather than something a kind decides:
		// a refining layer that had to own what it wrapped would be saying the
		// same thing.
		if enclosing, is := one.(Enclosing); is {
			held = enclosing.Encloses(held)
		}
	}

	return out
}

// Constructing is a layer whose type has to be made rather than declared.
//
// Most generated containers are ready as their zero value, and those say
// nothing here. One that is not — a bounded container has to be told how much
// it holds, and has nowhere to be told it but a call — declares the function
// that makes one, so that a decorator holding that container as a field can
// hand a caller a way to fill it in.
//
// It exists for that one reader. Nothing else needs it: the constructor is
// written by the layer that declares it, and every other caller reaches it by
// name in the package it was written into.
//
// An [Enclosing] layer that writes a constructor of its own belongs here too,
// for the case of one enclosing another: what the outer one holds is then the
// inner one's type, and the inner one is the only thing that knows how it is
// made. Nothing in this build composes that way — a stack naming one layer
// twice is refused, and there is one enclosing layer — so it is a note rather
// than a gap.
type Constructing interface {
	// Constructor returns the function that makes one of the type this layer
	// declares onto, and whether the type needs one at all.
	//
	// Asked of the context because the answer depends on the declaration: a
	// container told its size in the declaration takes nothing, and one whose
	// size is the caller's takes it as a parameter.
	Constructor(ctx *Context) (Constructor, bool)
}

// Constructor is the function that makes one of a generated type, described so
// that a decorator above it can forward to it.
//
// As text rather than as syntax. A decorator writes a call and a signature into
// a file of its own, and what it needs is how the parameters are spelled there
// — which is the spelling the layer that wrote the constructor already decided
// and already emitted.
type Constructor struct {
	// Name is the function's own name.
	Name string

	// Params are its parameters as a signature writes them, each with its own
	// name — "size int" — and Args are what a call forwarding to it passes,
	// each matching the parameter of the same index. A variadic parameter is
	// spelled "elems ...Person" and forwarded as "elems...".
	//
	// Two lists rather than one, because a forwarding call is not the
	// signature with the types removed: the spread on a variadic argument is
	// written at the call and nowhere else.
	Params []string
	Args   []string

	// Pointer records that the function answers with a pointer to the type,
	// which decides whether a caller storing the result into a field has to
	// dereference it.
	Pointer bool
}

// Call returns the constructor as a call forwarding the arguments it was given.
func (c Constructor) Call() string { return c.Name + "(" + strings.Join(c.Args, ", ") + ")" }

// Signature returns the parameters as a function declaring them writes them.
func (c Constructor) Signature() string { return strings.Join(c.Params, ", ") }

// Generating returns the context a layer is asked to generate against, which is
// what it was composed against plus what the composed stack turned out to
// expose.
//
// A method rather than a parameter on [ContextFor], because the two are known
// at different times: the options a layer reads are picked out while the stack
// is still being composed, and what the stack exposes is the result of having
// composed it. A constructor taking both would have to be called with a shape
// nobody has yet, at the only call site that has no use for one.
//
// A copy rather than a field written in place, so that what a layer is handed
// is decided in one place. A context built by [ContextFor] describes a layer of
// a declaration and nothing about a run; one that has been through here
// describes a layer of a run that is generating. Writing the field would leave
// the two indistinguishable, and a stage that read a context it had not built
// would have no way of knowing which it had.
func (c *Context) Generating(exposed shape.Shape) *Context {
	if c == nil {
		return nil
	}

	out := *c
	out.Exposed = exposed

	return &out
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

	// Provides holds work that belongs to something other than this
	// declaration, keyed by what it is about.
	//
	// The subject, in every case there is. An element layer contributes to the
	// subject rather than to the container, and two declarations over one
	// subject each ask their element layers for the same thing and each get it
	// — so what arrives is the same declarations twice, and what the package
	// needs is one. The key says which of them are the same.
	//
	// Distinct from Requires, which names a helper this build knows how to
	// write and this unit merely wants. What is here is a helper the layer has
	// already written, because nothing else could: it depends on the subject,
	// and the layer is what was given one.
	//
	// Keyed by a string rather than by a reference so that a caller can key by
	// whatever makes two contributions the same — a subject's own name, or that
	// and the layer's, where one subject can be contributed to twice.
	Provides map[string]Unit

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
