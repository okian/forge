package explain

import (
	"github.com/okian/forge/internal/model"
)

// Declaration is everything about one declaration that explaining it needs.
//
// It is a value rather than the resolved declaration itself, because the two
// stages that know these things are different ones: resolution knows the stack
// and where it was written, and the subject builder knows the model — and this
// package should not depend on either to be told what they found.
type Declaration struct {
	// Name is the declared type's own name.
	Name string

	// Package is the import path the declaration lives in, and Position where
	// it was written, as file:line:col.
	Package  string
	Position string

	// Form records whether it was written inline or in a spec file, which
	// decides which layers may appear in it.
	Form model.Form

	// Stack is the layers the declaration names, outermost first.
	Stack []model.LayerRef

	// Subject is the model of the type the stack is specialised to, and is nil
	// when no model could be built from it. SubjectName is how the declaration
	// spelled it, which is known either way.
	Subject     *model.Struct
	SubjectName string

	// Layout is the declaration rendered with the position of each stack entry
	// inside it, so that a report can underline one layer of a nested stack.
	Layout model.Layout

	// Model is the whole declaration, which is what a layer is handed when it
	// is asked what it exposes. It is nil when the subject could not be
	// modelled, and a layer given none reports the part of its surface that
	// does not depend on one.
	//
	// The fields above are what this package renders and are not taken from
	// here, because most of them are known when the model is not: a declaration
	// whose subject was refused still has a name, a position and a stack, and
	// explaining what forge made of it is exactly what somebody in that
	// position is asking for.
	Model *model.Model
}
