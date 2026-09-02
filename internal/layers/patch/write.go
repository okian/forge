package patch

import (
	"fmt"
	"strings"

	"github.com/okian/forge/plugin"
)

// The names the generated patch binds, written once so that the type and both
// its methods agree on them.
const (
	patchVar = "p"
	intoVar  = "into"
)

// writer assembles the source of a patch.
//
// Text rather than syntax, for the reason the other layers' writers give: what
// is assembled is a type and two methods with a branch per field, which is many
// times its own size as a tree. The cost is the possibility of writing
// something that is not Go, and it is paid where the layer can still be stopped
// — the source is parsed before it leaves the package.
type writer struct{ out strings.Builder }

// line writes one line. Indentation is left to gofmt, which the emitter runs
// over everything anyway.
func (w *writer) line(format string, args ...any) {
	if len(args) == 0 {
		w.out.WriteString(format)
	} else {
		fmt.Fprintf(&w.out, format, args...)
	}
	w.out.WriteByte('\n')
}

// blank separates two declarations.
func (w *writer) blank() { w.out.WriteByte('\n') }

// wrapped writes a sentence over however many comment lines it takes, so that
// a long one does not run off the side of a file the rest of which is wrapped.
func (w *writer) wrapped(text string) {
	for _, line := range plugin.Wrapped(text, plugin.CommentWidth) {
		w.line("// %s", line)
	}
}

// String returns the assembled source.
func (w *writer) String() string { return w.out.String() }

// patch writes the type and both its methods.
func (w *writer) patch(held *plan) {
	w.declare(held)
	w.apply(held)
	w.zero(held)
}

// declare writes the patch's own type.
func (w *writer) declare(held *plan) {
	w.wrapped(held.declared + " is a partial " + held.spelled.Text +
		": the fields it is asked to change, and nothing about the rest.")
	w.line("//")
	w.wrapped("Each field is a pointer because a pointer is how Go says there is " +
		"something here about a value that has a zero. A nil one is a field the " +
		"patch was not asked about; one pointing at the zero value is a field it " +
		"was asked to clear, and those are different instructions.")

	if held.kept {
		w.line("//")
		w.wrapped("The unexported fields of the " + held.spelled.Text + " are not among them: " +
			"a patch is filled in from outside the package that declares the subject, " +
			"which is what makes it a patch rather than an assignment.")
	}

	w.line("type %s struct {", held.declared)

	for i, one := range held.fields {
		if i > 0 {
			w.blank()
		}
		w.wrapped(one.name + " is what to set the " + one.name + " to, and is nil where the " +
			"patch says nothing about it.")
		w.line("%s *%s %s", one.name, one.spelled.Text, one.tag)
	}

	w.line("}")
	w.blank()
}

// apply writes the method that puts the patch over a value.
func (w *writer) apply(held *plan) {
	w.wrapped(applyMethod + " writes the fields the patch sets over the " + held.spelled.Text +
		" given, and leaves the rest as they were.")
	w.line("//")
	w.wrapped("It replaces rather than merges: a field holding a slice becomes that " +
		"slice rather than gaining its elements, and one holding a struct becomes " +
		"that struct rather than being patched inside. A partial update of a value " +
		"inside another is a patch for that value.")
	w.line("//")
	w.wrapped("Nothing is checked here. Whether what the patch holds is something the " +
		"rules would accept is a question about the whole value, and the whole value " +
		"exists only once this has run.")
	w.line("//")
	w.wrapped("Nothing is copied either. A field holding a slice, a map or a pointer " +
		"leaves the value sharing it with whatever filled the patch in, so a patch " +
		"applied twice gives two values that share, and writing through what was put " +
		"into the patch writes into both. Where that matters, copy before applying.")

	w.line("func (%s %s) %s(%s *%s) {", patchVar, held.declared, applyMethod, intoVar, held.spelled.Text)

	if len(held.fields) == 0 {
		w.line("// Nothing a caller could have asked for, so nothing to write.")
	}
	for _, one := range held.fields {
		w.line("if %s.%s != nil {", patchVar, one.name)
		w.line("%s.%s = *%s.%s", intoVar, one.name, patchVar, one.name)
		w.line("}")
	}

	w.line("}")
	w.blank()
}

// zero writes the method that says the patch asks for nothing.
func (w *writer) zero(held *plan) {
	w.wrapped(zeroMethod + " reports that the patch sets no field, which is a patch nobody " +
		"asked for anything by.")
	w.line("//")
	w.wrapped("It is also the name a codec looks for: a member of a struct tagged omitzero " +
		"is left out when its value says it is zero, so a patch held as a field of " +
		"something larger goes over the wire as nothing rather than as an object of " +
		"absent members. A patch that is the whole document is written out in full " +
		"either way, since there is no member for the tag to be on.")

	if len(held.fields) == 0 {
		w.line("func (%s %s) %s() bool { return true }", patchVar, held.declared, zeroMethod)
		w.blank()
		return
	}

	w.line("func (%s %s) %s() bool {", patchVar, held.declared, zeroMethod)
	w.line("return %s", conditions(held))
	w.line("}")
	w.blank()
}

// conditions is the expression that holds when no field of the patch is set.
func conditions(held *plan) string {
	out := make([]string, 0, len(held.fields))
	for _, one := range held.fields {
		out = append(out, patchVar+"."+one.name+" == nil")
	}
	return strings.Join(out, " &&\n")
}
