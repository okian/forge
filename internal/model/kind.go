package model

import "strconv"

// Kind classifies a layer by where it may appear in a stack and what it
// contributes to the generated type.
//
// A layer declares its kind once. Composition is then checked in terms of
// kinds and of the capabilities each layer exposes upward, rather than in terms
// of individual layers, which is what keeps the rule set from growing with the
// square of the catalog. Capabilities are a layer's own vocabulary and are
// defined alongside the layer registry; a kind is all this package needs.
type Kind uint8

const (
	// KindInvalid is the zero value: the kind is not known. A stack entry
	// holds it until the registry is consulted, and keeps it for a marker no
	// registered layer claims.
	KindInvalid Kind = iota

	// KindSubject classifies the concrete type a stack is specialised to: the
	// only type whose fields the generated code can see.
	//
	// It never appears in [Model.Stack], which holds layers and a subject is
	// not one — it is held separately in [Model.Subject]. The kind exists so
	// that a rendering which walks a whole declaration, subject included, has a
	// name for what it is looking at.
	KindSubject

	// KindElement attaches capabilities to the subject rather than to the
	// container around it: the methods it generates take the subject as their
	// receiver, and the element type seen by the layers above is unchanged.
	KindElement

	// KindStorage fixes the underlying representation. At most one appears in a
	// stack, and a refining layer written with none beneath it gets the default.
	KindStorage

	// KindRefining adds API over the storage beneath it without replacing it.
	KindRefining

	// KindDecorator wraps everything beneath it without changing the element
	// type. A decorator may withdraw capabilities as well as add them — which
	// is how a lock removes direct iteration from the surface it guards — and
	// the order decorators are written in is significant, outermost first.
	KindDecorator

	// KindTransport terminates a stack with an encoding or an I/O boundary. At
	// most one appears in a stack, and it must be the outermost entry.
	KindTransport
)

// kindNames gives each kind the spelling that appears in diagnostics and in
// the output of the explain and list commands.
var kindNames = [...]string{
	KindInvalid:   "invalid",
	KindSubject:   "subject",
	KindElement:   "element",
	KindStorage:   "storage",
	KindRefining:  "refining",
	KindDecorator: "decorator",
	KindTransport: "transport",
}

// String returns the kind's lower-case name.
func (k Kind) String() string {
	if int(k) >= len(kindNames) {
		return "kind(" + strconv.Itoa(int(k)) + ")"
	}
	return kindNames[k]
}

// Valid reports whether the kind is one of the defined kinds.
func (k Kind) Valid() bool { return k != KindInvalid && int(k) < len(kindNames) }

// Form distinguishes the two ways a declaration can be written, which decides
// what generation emits for it.
type Form uint8

const (
	// FormInvalid is the zero value: the declaration's provenance is unknown.
	FormInvalid Form = iota

	// FormInline is a declaration in an ordinary, unconstrained file. Its
	// underlying type is real and usable as written, so generation adds methods
	// and never redeclares the type. Only a stack every layer of which upholds
	// its invariants over the raw underlying type may be written this way.
	FormInline

	// FormSpec is a declaration in a file guarded by //go:build forgespec. The
	// spec file is type-checked but never linked, and generation owns the real
	// declaration in a complementary //go:build !forgespec file. Nesting, and
	// any layer whose representation carries invariants, requires this form.
	FormSpec
)

// formNames gives each form the spelling used in diagnostics.
var formNames = [...]string{
	FormInvalid: "invalid",
	FormInline:  "inline",
	FormSpec:    "spec",
}

// String returns the form's lower-case name.
func (f Form) String() string {
	if int(f) >= len(formNames) {
		return "form(" + strconv.Itoa(int(f)) + ")"
	}
	return formNames[f]
}

// Valid reports whether the form is one of the defined forms.
func (f Form) Valid() bool { return f != FormInvalid && int(f) < len(formNames) }
