package clone

import (
	"fmt"
	"strconv"
	"strings"
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

// String returns the assembled source.
func (w *writer) String() string { return w.out.String() }

// through writes the function everything generated calls, which forwards to the
// method where the type can carry one.
func (w *writer) through(held *plan, name string) {
	spelled := held.spelled.Text

	w.line("// %s returns a copy of a %s that shares nothing with it.", name, spelled)
	w.line("//")
	w.line("// The value's own method holds the body; this is what generated code")
	w.line("// calls, so that a caller names one function whether or not the type")
	w.line("// is one a method could be declared on.")
	w.line("func %s(%s %s) %s {", name, valueVar, spelled, spelled)
	w.line("return %s.%s()", valueVar, method)
	w.line("}")
	w.blank()
}

// copy writes one type's whole copy.
func (w *writer) copy(held *plan) {
	spelled := held.spelled.Text

	w.line("// %s returns a copy of the %s that shares nothing with it.", method, spelled)
	w.line("//")
	w.line("// Everything reachable is copied, so what is done to the copy is invisible")
	w.line("// in the original and the other way round. The copy starts as an assignment,")
	w.line("// which is already the whole of it for a field holding a number, a string or")
	w.line("// anything else made only of those; what follows is the fields for which an")
	w.line("// assignment would have copied a reference rather than what it refers to.")
	w.line("func (%s %s) %s() %s {", valueVar, spelled, method, spelled)
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

	case howAssign, howShare, howMethod, howOpaque:
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
		w.line("%s := %s", local(heldVar, depth), single)
	} else {
		w.line("var %s %s", local(heldVar, depth), of.elem.spelled.Text)
		w.value(local(heldVar, depth), held, of.elem, depth+1)
	}
	w.line("%s = &%s", into, local(heldVar, depth))
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
	index, one := local("i", depth), local("one", depth)

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
	index, one := local("i", depth), local("one", depth)

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
	key, one, made := local("key", depth), local("one", depth), local("value", depth)

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

// local names a variable a nested copy binds, so that a slice of slices does
// not shadow its own.
func local(name string, depth int) string {
	if depth == 0 {
		return name
	}
	return name + strconv.Itoa(depth)
}
