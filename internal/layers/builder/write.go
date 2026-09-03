package builder

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/okian/forge/internal/layers/failures"
	"github.com/okian/forge/plugin"
)

// The names the generated builder binds, written once so that the type, the
// setters and Build agree on them.
const (
	heldField  = "held"
	givenField = "given"
	failedVar  = "failed"
)

// The rule a missing field is reported under, and what the report says it
// wanted.
//
// The words a generated check uses for the same field, so that a value that was
// never given and a value that was given and found empty read alike — which is
// what makes one error type worth sharing between the two.
const (
	rule = "required"
	want = "a value"
)

// writer assembles the source of a builder.
//
// Text rather than syntax, for the reason the other layers' writers give: what
// is assembled is a type, a method per field and a function of conditions,
// which is many times its own size as a tree. The cost is the possibility of
// writing something that is not Go, and it is paid where the layer can still be
// stopped — the source is parsed before it leaves the package.
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

// builder writes the type, the function that makes one, every setter, and the
// method that ends it.
func (w *writer) builder(held *plan) {
	w.declare(held)
	w.make(held)

	for _, one := range held.fields {
		w.setter(held, one)
	}

	w.build(held)
}

// declare writes the builder's own type.
func (w *writer) declare(held *plan) {
	w.wrapped(held.declared + " builds one " + held.spelled.Text + ", a field at a time.")
	w.line("//")
	w.wrapped("Each setter answers with the builder, so the fields may be written in " +
		"whatever order suits the caller and the compiler still checks every one. " +
		"The zero value is a builder holding nothing, so a variable of this type is " +
		"ready without being made.")

	if held.demanded > 0 {
		w.line("//")
		w.wrapped(method + " refuses a value whose required fields were never given, and " +
			"reports every one of them rather than the first.")
	}
	if len(held.fields) < len(held.of.Fields) {
		w.line("//")
		w.wrapped("The unexported fields of the " + held.spelled.Text + " are not among the " +
			"ones it offers: what a builder is for is naming the fields at the call " +
			"site, and a caller names what is exported. Those keep whatever the zero " +
			"value gives them.")
	}

	w.line("type %s struct {", held.declared)
	w.line("// %s is the value being built, which every setter writes one field of.", heldField)
	w.line("%s %s", heldField, held.spelled.Text)

	if held.demanded > 0 {
		w.blank()
		w.wrapped(givenField + " records which of the required fields have been given, in " +
			"the order " + method + " reports them: " + listed(held) + ".")
		w.line("//")
		w.line("// Recorded rather than read off the value, because a field given the zero")
		w.line("// value was still given: a caller who set it meant to, and whether the zero")
		w.line("// value will do is a rule rather than an omission.")
		w.line("%s [%d]bool", givenField, held.demanded)
	}

	w.line("}")
	w.blank()
}

// listed names the required fields in the order the builder records them.
func listed(held *plan) string {
	names := make([]string, 0, held.demanded)
	for _, one := range held.required() {
		names = append(names, one.name)
	}
	return strings.Join(names, ", ")
}

// make writes the function that returns a builder.
//
// A function as well as a usable zero value, because a fluent call has to start
// with an expression: NewPersonBuilder().Name("Ada") reads as one thing, and
// the alternative is a variable declared on a line of its own for no reason but
// the syntax.
func (w *writer) make(held *plan) {
	w.wrapped(held.made + " returns a builder for one " + held.spelled.Text + ".")
	w.line("func %s() *%s { return &%s{} }", held.made, held.declared, held.declared)
	w.blank()
}

// setter writes the method that gives one field.
func (w *writer) setter(held *plan, one settable) {
	w.wrapped(one.name + " sets the " + one.name + " of the " + held.spelled.Text + " being built.")

	w.line("func (%s *%s) %s(%s %s) *%s {",
		held.receiver, held.declared, one.name, held.value, one.spelled.Text, held.declared)
	w.line("%s.%s.%s = %s", held.receiver, heldField, one.name, held.value)

	if one.demanded {
		w.line("%s.%s[%s] = true", held.receiver, givenField, strconv.Itoa(one.index))
	}

	w.line("return %s", held.receiver)
	w.line("}")
	w.blank()
}

// build writes the method that hands the value back.
func (w *writer) build(held *plan) {
	w.wrapped(method + " returns the " + held.spelled.Text + " that was built.")

	if held.demanded == 0 {
		w.line("//")
		w.wrapped("It answers with no error, because no field of the " + held.spelled.Text +
			" is tagged as one a value has to carry. The error is in the signature so " +
			"that a caller writes the same thing whether or not that is still true " +
			"tomorrow.")

		w.line("func (%s *%s) %s() (%s, error) { return %s.%s, nil }",
			held.receiver, held.declared, method, held.spelled.Text, held.receiver, heldField)
		w.blank()
		return
	}

	w.line("//")
	w.wrapped("A field the author tagged as one a value has to carry, and whose setter " +
		"was never called, is reported rather than left at its zero. Every one of " +
		"them rather than the first, because a caller filling in a form wants the " +
		"whole list.")
	w.line("//")
	w.wrapped("What was given is not checked here. A caller who set a field has set it, " +
		"and whether what they set is any good is what the rules on the field say — " +
		"which is a different question, asked somewhere a rule added to the tag " +
		"reaches.")

	w.line("func (%s *%s) %s() (%s, error) {",
		held.receiver, held.declared, method, held.spelled.Text)
	w.line("var %s %s", failedVar, failures.Errors)
	w.blank()

	for _, one := range held.required() {
		w.line("if !%s.%s[%d] {", held.receiver, givenField, one.index)
		w.line("%s = append(%s, "+failures.Failure+"{Path: %s, Rule: %s, Want: %s})",
			failedVar, failedVar, quoted(one.name), quoted(rule), quoted(want))
		w.line("}")
	}

	w.blank()
	w.line("if len(%s) > 0 {", failedVar)
	w.line("var zero %s", held.spelled.Text)
	w.line("return zero, %s", failedVar)
	w.line("}")
	w.line("return %s.%s, nil", held.receiver, heldField)
	w.line("}")
	w.blank()
}

// quoted writes a string the way source does.
func quoted(held string) string { return strconv.Quote(held) }
