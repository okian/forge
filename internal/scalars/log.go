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
	name := quoted(field.Name)

	if hidden(field) {
		return "slog.String(" + name + ", " + quoted(masked) + ")"
	}

	if of, known := scalar(field.Type); known {
		return of.logs(name, "v."+field.Name)
	}

	// Not a scalar, so it goes in as itself and slog decides. Which is the
	// right answer rather than a fallback: a field whose type has its own
	// LogValue gets to use it, and one that does not is resolved the way it
	// would have been if this method had never been written.
	return "slog.Any(" + name + ", v." + field.Name + ")"
}
