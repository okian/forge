package clone

import (
	"fmt"
	"strings"

	"github.com/okian/forge/plugin"
)

// The names the generated code binds, written once so that every copy agrees on
// them.
const (
	valueVar = "v"
	outVar   = "out"
	heldVar  = "held"
)

// writer assembles the source of a copy.
//
// Text rather than syntax, for the reason the other layers' writers give: what
// is assembled is a function of loops and branches, which is many times its own
// size as a tree. The cost is the possibility of writing something that is not
// Go, and it is paid where the layer can still be stopped — the source is
// parsed before it leaves the package.
type writer struct {
	out strings.Builder

	// block is what the function being written already binds: the packages the
	// file imports and the value and the copy every one of them opens with. A
	// local named for one of those does not fail to compile, it fails on the
	// next line that meant the package — in generated code the author cannot
	// edit.
	block *plugin.Block
}

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

// wrapped writes a sentence over however many comment lines it takes, so that
// a long one does not run off the side of a file the rest of which is wrapped.
func (w *writer) wrapped(text string) {
	for _, line := range plugin.Wrapped(text, plugin.CommentWidth) {
		w.line("// %s", line)
	}
}

// String returns the assembled source.
func (w *writer) String() string { return w.out.String() }

// through writes the function everything generated calls, which forwards to the
// method.
func (w *writer) through(held *plan, name string) {
	spelled := held.spelled.Text

	w.wrapped(name + " returns a copy of " + valueVar + " " + promise(held) + ".")
	w.line("//")
	w.line("// The value's own method holds the body; this is what generated code")
	w.line("// calls, so that a caller names one function whether or not the type")
	w.line("// is one a method could be declared on.")
	w.line("func %s(%s %s) %s {", name, valueVar, spelled, spelled)
	w.line("return %s.%s()", valueVar, method)
	w.line("}")
	w.blank()
}

// copy writes one type's whole copy, as the method where the type can carry one
// and as the function everything calls where it cannot.
//
// The second is not a lesser form of the first. A struct the subject reaches in
// another package, and an instantiation of a generic anywhere, both have
// nowhere to put a method — and a copy written as one there is a file that does
// not compile rather than a copy that is missing something. Which of the two it
// was goes into the comment, because a reader looking at a function where they
// expected a method is asking exactly that.
func (w *writer) copy(held *plan, name string) {
	spelled := held.spelled.Text

	if held.attach {
		w.wrapped(method + " returns a copy of the " + spelled + " " + promise(held) + ".")
	} else {
		w.wrapped(name + " returns a copy of " + valueVar + " " + promise(held) + ".")
		w.line("//")
		w.wrapped("A function rather than a method, because " + held.why + ".")
	}

	w.line("//")
	w.wrapped(reached(held) + " is copied, so what is done to the copy is invisible in the " +
		"original and the other way round. The copy starts as an assignment, which is " +
		"already the whole of it for a field holding a number, a string or anything else " +
		"made only of those; what follows is the fields for which an assignment would " +
		"have copied a reference rather than what it refers to.")

	if held.carried {
		w.line("//")
		w.wrapped("What it cannot reach is the unexported fields of a type declared in another " +
			"package, which are not this package's to name. The assignment carries those " +
			"across as they are, so a reference held in one of them is shared with the " +
			"original rather than copied. A type for which that is wrong can say so by " +
			"declaring the copy itself.")
	}

	if held.attach {
		w.line("func (%s %s) %s() %s {", valueVar, spelled, method, spelled)
	} else {
		w.line("func %s(%s %s) %s {", name, valueVar, spelled, spelled)
	}

	w.line("%s := %s", outVar, valueVar)

	if len(held.fields) > 0 {
		w.blank()
	}
	for _, one := range held.fields {
		w.value(outVar+"."+one.path, valueVar+"."+one.path, one.of, 0)
	}

	w.line("return %s", outVar)
	w.line("}")
	w.blank()
}

// reached opens the paragraph about what a copy covers, which is everything for
// most types and everything this package can name for the rest.
func reached(held *plan) string {
	if held.carried {
		return "Everything this package can reach"
	}
	return "Everything reachable"
}

// promise is the claim a copy opens with, which is weaker where the copy could
// not reach the whole of the value.
//
// Weaker rather than silent. A reader who is told a copy shares nothing will
// write code that relies on it, and a copy that shares one field is exactly as
// dangerous as no copy at all — so the sentence says which of the two this is
// before it says anything else.
func promise(held *plan) string {
	if held.carried {
		return "that shares nothing with it except the fields this package cannot name"
	}
	return "that shares nothing with it"
}

// value writes the statements that copy one value into a target.
//
// into is an assignable expression and from is the value being copied. depth
// distinguishes the variables a nested copy binds, so that a slice of slices
// does not shadow its own.
func (w *writer) value(into, from string, of *form, depth int) {
	if held, one := expression(from, of); one {
		w.line("%s = %s", into, held)
		return
	}

	switch of.how {
	case howPointer:
		w.pointer(into, from, of, depth)

	case howSlice:
		w.slice(into, from, of, depth)

	case howArray:
		w.array(into, from, of, depth)

	case howMap:
		w.mapping(into, from, of, depth)

	case howAssign, howShare, howMethod, howThrough, howOpaque:
		// Every one of these is an expression, so the branch above wrote it —
		// except the opaque, which is refused while planning and never reaches
		// a writer at all.
	}
}

// expression returns a copy of a value written as one expression, and whether
// there is one.
//
// Most copies are: assigning, calling a method, cloning a slice or a map whose
// elements need nothing. Writing them as expressions is what keeps the output
// readable — a map of strings copied by a loop that binds a variable to assign
// it is three lines saying what maps.Clone says in one, and generated code is
// read.
func expression(from string, of *form) (string, bool) {
	switch of.how {
	case howAssign, howShare:
		return from, true

	case howMethod:
		return from + "." + method + "()", true

	case howThrough:
		return of.call + "(" + from + ")", true

	case howSlice:
		if !needs(of.elem) {
			return "slices.Clone(" + from + ")", true
		}

	case howMap:
		if !needs(of.elem) {
			return "maps.Clone(" + from + ")", true
		}

	case howArray:
		// An array is a value, so assigning it copies its elements. Only where
		// they each need copying is there anything to write.
		if !needs(of.elem) {
			return from, true
		}

	case howPointer, howOpaque:
	}

	return "", false
}

// needs reports whether copying a value takes any statement at all.
//
// An array is the one shape whose answer is its element's: assigning an array
// copies what it holds, so an array of numbers is copied by the assignment the
// method opens with and an array of slices is not. Everything else answers for
// itself — a slice and a map and a pointer are references whatever they refer
// to, so a copy always has to build them again.
func needs(of *form) bool {
	if of == nil {
		return false
	}

	switch of.how {
	case howAssign, howShare:
		return false
	case howArray:
		return needs(of.elem)
	default:
		return true
	}
}

// pointer writes a new allocation holding a copy of what was pointed at.
//
// Nil stays nil, which the assignment the method opened with already did — so
// only the case where there is something to copy is written.
func (w *writer) pointer(into, from string, of *form, depth int) {
	// Parenthesised, because a dereference binds looser than a selector: what
	// is wanted from *p is a copy of what p points at, and *p.Clone() is a
	// dereference of what p.Clone() answered with.
	held := "(*" + from + ")"

	w.line("if %s != nil {", from)
	if single, one := expression(held, of.elem); one {
		w.line("%s := %s", w.block.Nested(depth, heldVar), single)
	} else {
		w.line("var %s %s", w.block.Nested(depth, heldVar), of.elem.spelled.Text)
		w.value(w.block.Nested(depth, heldVar), held, of.elem, depth+1)
	}
	w.line("%s = &%s", into, w.block.Nested(depth, heldVar))
	w.line("}")
}

// slice writes a new slice holding copies of the elements.
//
// A nil slice stays nil and an empty one stays empty and not nil, because the
// two are different values and a copy that turned one into the other would have
// changed something. Reached only where the elements each need copying: where
// they do not, the expression above writes slices.Clone, which says the same in
// one line and answers nil the same way.
func (w *writer) slice(into, from string, of *form, depth int) {
	index, one := w.block.Nested(depth, "i"), w.block.Nested(depth, "one")

	w.line("if %s != nil {", from)
	w.line("%s = make(%s, len(%s))", into, of.spelled.Text, from)
	w.line("for %s, %s := range %s {", index, one, from)
	w.value(into+"["+index+"]", one, of.elem, depth+1)
	w.line("}")
	w.line("}")
}

// array writes copies of the elements over the ones the assignment left.
//
// An array is a value, so the copy already holds one of each element; what is
// written here is the copying those elements each need.
func (w *writer) array(into, from string, of *form, depth int) {
	index, one := w.block.Nested(depth, "i"), w.block.Nested(depth, "one")

	w.line("for %s, %s := range %s {", index, one, from)
	w.value(into+"["+index+"]", one, of.elem, depth+1)
	w.line("}")
}

// mapping writes a new map holding copies of the values.
//
// The keys are assigned rather than copied: a map key is comparable, and a
// comparable type holds nothing a copy could share. Reached only where the
// values each need copying, for the reason the slice above gives.
func (w *writer) mapping(into, from string, of *form, depth int) {
	key, one, made := w.block.Nested(depth, "key"), w.block.Nested(depth, "one"), w.block.Nested(depth, "value")

	w.line("if %s != nil {", from)
	w.line("%s = make(%s, len(%s))", into, of.spelled.Text, from)
	w.line("for %s, %s := range %s {", key, one, from)

	if held, single := expression(one, of.elem); single {
		w.line("%s[%s] = %s", into, key, held)
	} else {
		w.line("var %s %s", made, of.elem.spelled.Text)
		w.value(made, one, of.elem, depth+1)
		w.line("%s[%s] = %s", into, key, made)
	}

	w.line("}")
	w.line("}")
}
