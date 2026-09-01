package validate

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/okian/forge/internal/emit"
)

// The names the generated code binds, written once so that every check agrees
// on them.
const (
	valueVar  = "v"
	failedVar = "failed"
	errVar    = "err"
)

// writer assembles the source of a check.
//
// Text rather than syntax, for the reason the codec's writer gives: what is
// assembled is a function of conditions, and a tree for one is many times its
// own size. What it costs is the possibility of writing something that is not
// Go, and that cost is paid where the layer can still be stopped — the source
// is parsed before it leaves the package.
type writer struct{ out strings.Builder }

// line writes one line of the body. Indentation is left to gofmt, which the
// emitter runs over everything anyway.
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

// wrapped writes a sentence as as many comment lines as it takes, so that a
// long one does not run off the side of a file the rest of which is wrapped.
func (w *writer) wrapped(text string) {
	for _, line := range emit.Wrapped(text, emit.CommentWidth) {
		w.line("// %s", line)
	}
}

// String returns the assembled source.
func (w *writer) String() string { return w.out.String() }

// patterns writes the package-level variables the regexp rules are compiled
// into.
//
// Compiled once when the package loads rather than once per call, which is the
// whole reason a pattern is worth generating for: compiling one is many times
// the cost of matching against it. MustCompile cannot fail here, because the
// pattern was compiled while the rule was read and a tag that did not compile
// never reached this.
func (w *writer) patterns(held *plan) {
	for _, one := range held.fields {
		for _, asked := range one.rules {
			if asked.name != ruleRegexp {
				continue
			}

			w.line("// %s is the pattern %s is checked against.", one.pattern, one.path)
			w.line("//")
			w.line("// Compiled when the package loads rather than at each check: compiling a")
			w.line("// pattern costs many times what matching against one does, and the pattern")
			w.line("// is the same every time because it was written in the source.")
			w.line("var %s = regexp.MustCompile(%s)", one.pattern, quoted(asked.pattern))
			w.blank()
		}
	}
}

// through writes the function everything generated calls, which forwards to the
// method.
func (w *writer) through(held *plan, name string) {
	spelled := held.spelled.Text

	w.line("// %s reports every rule %s does not satisfy.", name, valueVar)
	w.line("//")
	w.line("// The value's own method holds the body; this is what generated code")
	w.line("// calls, so that a caller names one function whether or not the type")
	w.line("// is one a method could be declared on.")
	w.line("func %s(%s %s) error {", name, valueVar, spelled)
	w.line("return %s.%s()", valueVar, method)
	w.line("}")
	w.blank()
}

// check writes one type's whole check, as the method where the type can carry
// one and as the function everything calls where it cannot.
//
// The second is not a lesser form of the first. A struct the subject reaches in
// another package, and an instantiation of a generic anywhere, both have
// nowhere to put a method — and a check written as one there is a file that does
// not compile rather than a check that is missing something. Which of the two it
// was goes into the comment, because a reader looking at a function where they
// expected a method is asking exactly that.
func (w *writer) check(held *plan, name string) {
	spelled := held.spelled.Text

	if held.attach {
		w.line("// %s reports every rule the %s does not satisfy, and nothing when it", method, spelled)
		w.line("// satisfies them all.")
	} else {
		w.line("// %s reports every rule %s does not satisfy, and nothing when it", name, valueVar)
		w.line("// satisfies them all.")
		w.line("//")
		w.wrapped("A function rather than a method, because " + held.why + ".")
	}

	w.line("//")
	w.line("// Every failure rather than the first, because a value with three things")
	w.line("// wrong is not three round trips. Nothing is allocated until something")
	w.line("// fails, so a value that is in order costs the comparisons and no memory.")

	if held.attach {
		w.line("func (%s %s) %s() error {", valueVar, spelled, method)
	} else {
		w.line("func %s(%s %s) error {", name, valueVar, spelled)
	}

	w.line("var %s ValidationErrors", failedVar)
	w.blank()

	for _, one := range held.fields {
		w.field(one)
	}

	// Returned as nothing rather than as the empty list. A nil slice held in
	// an interface is an interface that is not nil, and a caller writing
	// `if err != nil` would find one every time.
	w.line("if len(%s) == 0 {", failedVar)
	w.line("return nil")
	w.line("}")
	w.line("return %s", failedVar)
	w.line("}")
	w.blank()
}

// field writes everything asked of one field, stopping at the first thing wrong
// with it.
//
// One failure per field rather than all of them. An empty address does not
// match a pattern either, and reporting both tells somebody two things about
// one mistake — the second of which is about a value that is not there. So the
// rules are a chain: the first that fails is the one reported, and what follows
// is only asked of a value the rules before it accepted.
//
// Across fields it is the other way round, and for the same reason: three bad
// fields are three mistakes, and a caller showing somebody a form wants all of
// them at once.
func (w *writer) field(one checked) {
	after := one.nested || one.hook

	switch {
	case len(one.rules) == 0:
		w.after(one)

	case len(one.rules) == 1 && !after:
		w.line("if %s {", condition(one, one.rules[0]))
		w.fails(one, one.rules[0])
		w.line("}")

	default:
		w.line("switch {")
		for _, asked := range one.rules {
			w.line("case %s:", condition(one, asked))
			w.fails(one, asked)
		}
		if after {
			w.line("default:")
			w.after(one)
		}
		w.line("}")
	}
}

// after writes what runs once a field's own rules have accepted it: the check
// its type carries, and the one the author wrote.
//
// Both only then. A struct that is not there has nothing to check inside it,
// and an author's check handed a value the rules refused is being asked about
// something nobody claims is a value.
func (w *writer) after(one checked) {
	if one.nested {
		// A pointer is asked about before it is followed. A field that is not
		// there has nothing to check, and calling through a nil one would stop
		// the program instead of reporting anything — which is a rule's job,
		// and required is the rule that says a field has to be there.
		if one.indirect {
			w.line("if %s.%s != nil {", valueVar, one.path)
		}

		if one.through != "" {
			// A pointer is dereferenced at the call, because a function takes
			// the value where a method took the receiver and did it for us.
			// Reached only inside the nil check above, so what is dereferenced
			// is a pointer that is there.
			w.line("if %s := %s(%s%s.%s); %s != nil {",
				errVar, one.through, deref(one), valueVar, one.path, errVar)
		} else {
			w.line("if %s := %s.%s.%s(); %s != nil {", errVar, valueVar, one.path, method, errVar)
		}
		w.line("%s = nestedValidation(%s, %s, %s)", failedVar, failedVar, quoted(one.path), errVar)
		w.line("}")

		if one.indirect {
			w.line("}")
		}
	}

	if one.hook {
		w.line("if %s := %s.%s%s(); %s != nil {", errVar, valueVar, method, one.field.Name, errVar)
		w.line("%s = append(%s, ValidationError{Path: %s, Cause: %s})",
			failedVar, failedVar, quoted(one.path), errVar)
		w.line("}")
	}
}

// deref is the star a call through a function needs where a method call needed
// nothing, and nothing where the field is not a pointer.
func deref(one checked) string {
	if one.indirect {
		return "*"
	}
	return ""
}

// fails writes what a rule reports when it is not met.
func (w *writer) fails(one checked, asked rule) {
	w.line("%s = append(%s, ValidationError{Path: %s, Rule: %s, Want: %s})",
		failedVar, failedVar, quoted(one.path), quoted(asked.written), quoted(wanted(one, asked)))
}

// condition returns the expression that is true when a rule was not met.
//
// The failing side rather than the passing one, because that is what the
// generated code branches on and reading it forwards is what a person does. A
// rule and its condition are one decision, so they are written here together
// rather than in a table somebody has to hold two halves of in mind.
func condition(one checked, asked rule) string {
	held := valueVar + "." + one.path

	switch asked.name {
	case ruleRequired:
		return absent(held, one.form)

	case ruleNonzero:
		return held + " == " + inCondition(zero(one))

	case ruleMin:
		return measured(held, one.form) + " < " + asked.number

	case ruleMax:
		return measured(held, one.form) + " > " + asked.number

	case ruleLen:
		return "len(" + held + ") != " + asked.number

	case ruleOneOf:
		return outside(held, one, asked)

	case ruleRegexp:
		return "!" + one.pattern + ".MatchString(string(" + held + "))"

	default:
		// Every rule the grammar accepts has a row above, and one that reached
		// here without one is this file having drifted from the grammar beside
		// it. A condition that is never true is the safe form of that mistake.
		return "false"
	}
}

// absent returns the condition under which a value is not there.
//
// Which question that is depends on what the value is: a pointer is absent when
// it is nil, and a string is absent when it is empty. A slice or a map is both,
// and its length answers for both at once — a nil slice has no elements.
//
// A string is compared against the empty one rather than measured, because that
// is what somebody would have written and generated code is read.
func absent(held string, of form) string {
	switch {
	case of.text:
		return held + ` == ""`
	case of.sized:
		return "len(" + held + ") == 0"
	default:
		return held + " == nil"
	}
}

// zero returns how the zero value of a field's type is written.
//
// A literal where the language has one and a declared variable where it does
// not, because a struct has no literal that can be written inline in a
// comparison without naming its type — and naming it is what the spelling is
// for.
func zero(one checked) string {
	switch {
	case one.form.text:
		return `""`
	case one.form.numeric:
		return "0"
	case one.form.boolean:
		return "false"
	case one.form.nilable:
		return "nil"
	default:
		return one.spelled.Text + "{}"
	}
}

// inCondition returns a zero as it has to appear inside a condition.
//
// A composite literal is parenthesised, because Go will not read one at the top
// level of an if or a case: the opening brace there is the block's, and the
// parser says so rather than guessing. It is only a condition that needs them —
// the same text in a sentence a person reads is better without.
func inCondition(zero string) string {
	if strings.HasSuffix(zero, "{}") {
		return "(" + zero + ")"
	}
	return zero
}

// measured returns what a bound is compared against: the value itself for a
// number, and the length for everything else.
func measured(held string, of form) string {
	if of.numeric {
		return held
	}
	return "len(" + held + ")"
}

// outside returns the condition under which a value is none of the ones listed.
//
// Written out as a chain rather than as a slice searched at run time, because
// the values are known when the code is written: a chain of comparisons costs
// nothing and allocates nothing, and a slice would be built on every call.
func outside(held string, one checked, asked rule) string {
	parts := make([]string, 0, len(asked.members))
	for _, member := range asked.members {
		parts = append(parts, held+" != "+literal(member, one))
	}
	return strings.Join(parts, " && ")
}

// literal writes one of oneof's values as the field's own type.
//
// Converted where the field is a named type over a string or a number, because
// a named type is not its underlying type as far as a comparison is concerned.
func literal(member string, one checked) string {
	if one.form.text {
		return quoted(member)
	}
	return member
}

// wanted says what a rule wanted, in the words a person reading a failure would
// use.
func wanted(one checked, asked rule) string {
	switch asked.name {
	case ruleRequired:
		return "a value"

	case ruleNonzero:
		return "something other than " + zero(one)

	case ruleMin:
		return "at least " + bound(asked.number, one)

	case ruleMax:
		return "at most " + bound(asked.number, one)

	case ruleLen:
		return "exactly " + bound(asked.number, one)

	case ruleOneOf:
		return "one of " + strings.Join(asked.members, ", ")

	case ruleRegexp:
		return "a value matching " + asked.pattern

	default:
		return ""
	}
}

// bound writes a bound as the sentence a person reads: a number on its own
// where the bound is on the value, and a number with what it counts after it
// where the bound is on a length.
func bound(number string, one checked) string {
	switch {
	case one.form.numeric:
		return number
	case one.form.text:
		return number + " characters"
	default:
		return number + " elements"
	}
}

// quoted writes a string as a Go literal.
func quoted(text string) string { return strconv.Quote(text) }
