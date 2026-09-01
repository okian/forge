package scalars

import (
	"strings"

	"github.com/okian/forge/internal/diag"
	"github.com/okian/forge/internal/layer"
	"github.com/okian/forge/internal/model"
)

// texting writes the text codec of a struct that wraps one scalar.
//
// No tag asks for it, and none needs to. Every other emitter here is answering
// a question an author had to have an opinion about; this one is answering a
// question with one answer. A struct holding a single string has exactly one
// text form, and a type that goes to text and back through it comes back the
// same — so the choice is not what the text should be but whether to write the
// method, and a wrapper that cannot be written down is a wrapper that has to be
// unwrapped at every boundary.
func texting(of Asked, held model.Spelling, diags *diag.Set) (layer.Unit, bool, error) {
	field, wraps := wrapping(of.Subject)
	if !wraps {
		return layer.Unit{}, false, nil
	}
	if clashes(of, diags, appendMethod, marshalMethod, unmarshalMethod) {
		return layer.Unit{}, false, nil
	}

	written, _ := scalar(field.Type)

	w := &strings.Builder{}
	appendText(w, held, field, written)
	marshalText(w, held)
	unmarshalText(w, held, field, written)

	made, err := unit(w, held, textNeeds(written)...)
	return made, err == nil, err
}

// textNeeds returns what the codec reaches for.
func textNeeds(of kind) []model.Import {
	if of.converts {
		return []model.Import{stdStrconv}
	}
	return nil
}

// appendText writes the appending half, which is the one that does the work.
//
// The other two are written in terms of it. encoding.TextAppender exists
// because a caller with a buffer should not have to take an allocation to fill
// it, and a MarshalText that did its own formatting would be a second copy of
// this to keep in step with the first.
func appendText(w *strings.Builder, held model.Spelling, field model.Field, of kind) {
	w.WriteString("// AppendText appends the wrapped value's text form to b.\n")
	w.WriteString("//\n")
	w.WriteString("// The type wraps one scalar, so its text is that scalar's and nothing is\n")
	w.WriteString("// decided here. Appending rather than returning is what lets a caller who\n")
	w.WriteString("// has a buffer keep it: encoding.TextAppender exists for exactly that, and\n")
	w.WriteString("// MarshalText below is written in terms of this rather than beside it.\n")
	w.WriteString("func (v " + held.Text + ") AppendText(b []byte) ([]byte, error) {\n")
	w.WriteString("\treturn " + of.appends("b", "v."+field.Name) + ", nil\n")
	w.WriteString("}\n\n")
}

// marshalText writes the allocating half, in terms of the other.
func marshalText(w *strings.Builder, held model.Spelling) {
	w.WriteString("// MarshalText returns the wrapped value's text form.\n")
	w.WriteString("//\n")
	w.WriteString("// It allocates, because its signature says it hands back a slice the caller\n")
	w.WriteString("// owns. A caller who would rather not can call AppendText with a buffer of\n")
	w.WriteString("// their own, which is the same work without the allocation.\n")
	w.WriteString("func (v " + held.Text + ") MarshalText() ([]byte, error) {\n")
	w.WriteString("\treturn v.AppendText(nil)\n")
	w.WriteString("}\n\n")
}

// unmarshalText writes the reading half.
func unmarshalText(w *strings.Builder, held model.Spelling, field model.Field, of kind) {
	w.WriteString("// UnmarshalText reads the wrapped value back out of its text form.\n")
	w.WriteString("//\n")
	w.WriteString("// The receiver is a pointer because this assigns to the value, which is the\n")
	w.WriteString("// one method here that does. Text that does not parse leaves the value\n")
	w.WriteString("// untouched rather than half written: a caller who ignores the error and\n")
	w.WriteString("// carries on gets what they had, not a field from one input beside a field\n")
	w.WriteString("// from another.\n")
	w.WriteString("func (v *" + held.Text + ") UnmarshalText(b []byte) error {\n")

	// Read first and assigned last, so that text which does not parse leaves
	// the receiver as it was.
	if of.parses != nil {
		w.WriteString(of.parses("b"))
		w.WriteString("\n")
	}

	w.WriteString("\tv." + field.Name + " = " + of.from("b") + "\n")
	w.WriteString("\treturn nil\n}\n\n")
}
