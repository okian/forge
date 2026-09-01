package scalars

import (
	"strings"

	"github.com/okian/forge/internal/diag"
	"github.com/okian/forge/internal/layer"
	"github.com/okian/forge/internal/model"
)

// masked is what a redacted field logs instead of its value.
//
// A fixed string rather than the value shortened, starred or hashed. Every one
// of those leaks something — a length, a prefix, whether two records hold the
// same secret — and a field marked as not for logs was marked by somebody who
// did not want to reason about which of those is safe.
const masked = "[redacted]"

// logging writes the subject's LogValue, when a field asked not to be logged.
//
// The tag is the whole trigger. A type with nothing to hide logs perfectly well
// without this — slog reaches for its fields and prints them — so writing one
// anyway would replace a working default with a copy of itself that has to be
// regenerated whenever a field is added. What the tag says is that the default
// is wrong, and this is the only place that can be fixed: slog has no way to be
// told about a field, only about a type.
func logging(of Asked, held model.Spelling, diags *diag.Set) (layer.Unit, bool, error) {
	if !redacted(of.Subject) {
		return layer.Unit{}, false, nil
	}
	if clashes(of, diags, logMethod) {
		return layer.Unit{}, false, nil
	}

	w := &strings.Builder{}
	w.WriteString("// LogValue returns the subject as it may be logged.\n")
	w.WriteString("//\n")
	w.WriteString("// Every exported field, with the ones tagged redact replaced by a fixed\n")
	w.WriteString("// string. Fixed rather than shortened or starred: a length is something,\n")
	w.WriteString("// a prefix is more, and a field marked as not for logs was marked by\n")
	w.WriteString("// somebody who did not want to work out which of those is safe.\n")
	w.WriteString("//\n")
	w.WriteString("// Implementing this is what takes the field out of a log. slog reaches for\n")
	w.WriteString("// a value's fields when the value does not say otherwise, so a type with a\n")
	w.WriteString("// secret in it and no LogValue prints the secret.\n")
	w.WriteString("func (v " + held.Text + ") LogValue() slog.Value {\n")
	w.WriteString("\treturn slog.GroupValue(\n")

	for _, field := range of.Subject.Fields {
		if !field.Exported {
			continue
		}
		w.WriteString("\t\t" + logged(field) + ",\n")
	}

	w.WriteString("\t)\n}\n\n")

	made, err := unit(w, held, stdSlog)
	return made, err == nil, err
}

// logged returns the attribute one field contributes.
func logged(field model.Field) string {
	if hidden(field) {
		return Masked(field.Name)
	}
	return Attr(field.Name, "v."+field.Name, field.Type)
}

// Masked returns the attribute a field kept out of logs contributes.
//
// Exported alongside [Attr] because the redaction layer writes the fuller
// version of this method — the same masking, over everything the subject
// reaches rather than over the subject alone — and two answers to what a
// redacted field looks like would be two answers a reader has to notice are
// meant to be one.
func Masked(name string) string {
	return "slog.String(" + quoted(name) + ", " + quoted(masked) + ")"
}

// Held returns the expression building the slog attribute for a value that is
// already a slog.Value.
//
// Beside [Attr] rather than folded into it, because the two take different
// things: Attr is given an expression of the field's own type and picks the
// constructor for it, and this is given one that is already the answer. Putting
// a slog.Value through Attr would reach slog.Any, which takes an interface — so
// the value is boxed for AnyValue to unwrap again, at an allocation and about
// half as long again per field per record.
//
// Exported for the redaction layer, which works a pointer field's value out
// before the attribute list so that a nil one is logged as nothing rather than
// as the stack trace a method call through nothing leaves.
func Held(name, from string) string {
	return "slog.Attr{Key: " + quoted(name) + ", Value: " + from + "}"
}

// Attr returns the expression building the slog attribute for one value.
//
// A typed constructor where the type is one this package knows, and slog.Any
// where it is not. Typed matters: slog.Any takes an interface, so a string
// through it is a string boxed, and this is written into a path that runs per
// field per record.
//
// slog.Any is the right answer for everything else rather than a fallback. A
// field whose type has its own LogValue gets to use it, and one that does not
// is resolved exactly the way it would have been if the method had never been
// written.
func Attr(name, from string, held model.Classified) string {
	quotedName := quoted(name)

	if of, known := scalar(held); known {
		return of.logs(quotedName, from)
	}
	return "slog.Any(" + quotedName + ", " + from + ")"
}
