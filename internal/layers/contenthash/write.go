package contenthash

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/okian/forge/plugin"
)

// The names generated code binds, written once so that every hash and every
// nested loop agree on them.
const (
	valueVar = "v"
	hashVar  = "h"
	partVar  = "part"
	totalVar = "total"

	// seedFn, and the four below it, are the arithmetic emitted beside the
	// hashes. They are named here because the bodies call them and the shared
	// file declares them, and a name written twice is a name that can differ.
	seedFn   = "fnvSeed"
	wholeFn  = "fnvUint64"
	boolFn   = "fnvBool"
	stringFn = "fnvString"
	floatFn  = "fnvFloat"
)

// writer assembles the source of a hash.
//
// Text rather than syntax, for the reason the other layers' writers give: what
// is assembled is a function of loops and branches, which is many times its own
// size as a tree. The cost is the possibility of writing something that is not
// Go, and it is paid where the layer can still be stopped — the source is
// parsed before it leaves the package.
type writer struct {
	out strings.Builder

	// totals counts the maps written into one function, so that each gets an
	// accumulator of its own.
	//
	// Counted rather than named after the nesting, because two maps side by
	// side are at the same depth and in the same scope: an accumulator named
	// after the depth would be declared twice, which is a function that does
	// not compile. Everything else a map binds lives inside its own loop, where
	// two of one name are two variables.
	totals int
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

	w.line("// %s returns a content hash of %s.", name, valueVar)
	w.line("//")
	w.line("// The value's own method holds the body; this is what generated code")
	w.line("// calls, so that a caller names one function whether or not the type")
	w.line("// is one a method could be declared on.")
	w.line("func %s(%s %s) uint64 {", name, valueVar, spelled)
	w.line("return %s.%s()", valueVar, method)
	w.line("}")
	w.blank()
}

// hash writes one type's whole hash, as the method where the type can carry one
// and as the function everything calls where it cannot.
//
// The second is not a lesser form of the first. A struct the subject reaches in
// another package, and an instantiation of a generic anywhere, both have
// nowhere to put a method — and a hash written as one there is a file that does
// not compile rather than a hash that is missing something. Which of the two it
// was goes into the comment, because a reader looking at a function where they
// expected a method is asking exactly that.
func (w *writer) hash(held *plan, name string) {
	spelled := held.spelled.Text

	if held.attach {
		w.line("// %s returns a content hash of the %s.", method, spelled)
	} else {
		w.line("// %s returns a content hash of %s.", name, valueVar)
		w.line("//")
		w.wrapped("A function rather than a method, because " + held.why + ".")
	}

	w.line("//")
	w.line("// Two values that are the same all the way down hash to the same number,")
	w.line("// in this process and in every other, on any machine and in any build. Two")
	w.line("// that differ almost certainly do not: this is a hash rather than an")
	w.line("// identity, so a caller who cannot afford a collision compares as well.")
	w.line("//")
	w.line("// It allocates nothing, which is what makes it affordable to take on every")
	w.line("// lookup rather than once and cache.")

	if held.attach {
		w.line("func (%s %s) %s() uint64 {", valueVar, spelled, method)
	} else {
		w.line("func %s(%s %s) uint64 {", name, valueVar, spelled)
	}

	w.line("%s := %s", hashVar, seedFn)

	switch {
	case held.value != nil:
		w.value(hashVar, valueVar, held.value, 0)

	default:
		for _, one := range held.fields {
			w.value(hashVar, valueVar+"."+one.path, one.of, 0)
		}
	}

	w.line("return %s", hashVar)
	w.line("}")
	w.blank()
}

// value writes the statements that mix one value into an accumulator.
//
// into names the variable being accumulated into, from is the value being
// hashed, and depth distinguishes the variables a nested hash binds so that a
// slice of maps does not shadow its own.
func (w *writer) value(into, from string, of *form, depth int) {
	switch of.how {
	case howWhole:
		w.line("%s = %s(%s, uint64(%s))", into, wholeFn, into, from)

	case howBool:
		w.line("%s = %s(%s, bool(%s))", into, boolFn, into, from)

	case howString:
		w.line("%s = %s(%s, string(%s))", into, stringFn, into, from)

	case howFloat:
		w.line("%s = %s(%s, float64(%s))", into, floatFn, into, from)

	case howComplex:
		// Both halves, in that order. A complex number is a pair, and the pair
		// is the whole of what it is.
		held := "complex128(" + from + ")"
		w.line("%s = %s(%s, real(%s))", into, floatFn, into, held)
		w.line("%s = %s(%s, imag(%s))", into, floatFn, into, held)

	case howMethod:
		w.line("%s = %s(%s, %s.%s())", into, wholeFn, into, selecting(from), method)

	case howThrough:
		w.line("%s = %s(%s, %s(%s))", into, wholeFn, into, of.call, from)

	case howPointer:
		w.pointer(into, from, of, depth)

	case howSlice:
		w.slice(into, from, of, depth)

	case howArray:
		w.array(into, from, of, depth)

	case howMap:
		w.mapping(into, from, of, depth)

	case howStruct:
		for _, one := range of.members {
			w.value(into, selecting(from)+"."+one.name, one.of, depth)
		}

	case howInvalid, howOpaque:
		// Refused while planning, so nothing reaches a writer. The arm is here
		// because a switch over a closed set that leaves one out is a switch
		// somebody will forget to extend.
	}
}

// pointer writes whether there is anything there, and then what is.
//
// Whether, always: a nil pointer and a pointer to the zero value are different
// values, and a hash that mixed in nothing for the first would call them the
// same.
func (w *writer) pointer(into, from string, of *form, depth int) {
	w.line("%s = %s(%s, %s != nil)", into, boolFn, into, from)
	w.line("if %s != nil {", from)
	w.value(into, "*"+from, of.elem, depth+1)
	w.line("}")
}

// selecting returns an expression something may be written after, parenthesised
// where it has to be.
//
// A dereference binds looser than a selector, so what is wanted from *p is the
// value p points at, and *p.Hash() is a dereference of whatever p.Hash()
// answered with. Everywhere else the parentheses would be noise in a file
// somebody reads, so they are written where they carry the meaning and nowhere
// else.
func selecting(from string) string {
	if strings.HasPrefix(from, "*") {
		return "(" + from + ")"
	}
	return from
}

// slice writes whether the slice is there, how long it is, and then its
// elements in order.
//
// All three. A nil slice and an empty one are different values; two slices that
// run together the same way are not the same pair; and the order is part of
// what a slice is, unlike a map.
func (w *writer) slice(into, from string, of *form, depth int) {
	one := local("one", depth)

	w.line("%s = %s(%s, %s != nil)", into, boolFn, into, from)
	w.line("%s = %s(%s, uint64(len(%s)))", into, wholeFn, into, from)
	w.line("for _, %s := range %s {", one, from)
	w.value(into, one, of.elem, depth+1)
	w.line("}")
}

// array writes the elements in order.
//
// No length: an array's length is part of its type, so every value of the type
// has the same one and mixing it in would say the same thing every time.
func (w *writer) array(into, from string, of *form, depth int) {
	one := local("one", depth)

	w.line("for _, %s := range %s {", one, from)
	w.value(into, one, of.elem, depth+1)
	w.line("}")
}

// mapping writes a map's size and a total of its entries.
//
// Entry by entry into a fresh accumulator, added up rather than chained,
// because addition does not care what order it is done in and ranging over a Go
// map is deliberately not in one. Chaining them would give one map as many
// hashes as it has orders to walk it in, which is the bug this shape exists to
// avoid rather than an inefficiency.
//
// The count as well as the total, so that a map is told from one that happens
// to add up the same way, and whether the map is there at all, so that a nil
// map is told from an empty one.
func (w *writer) mapping(into, from string, of *form, depth int) {
	key, one := local("key", depth), local("one", depth)
	part, total := local(partVar, depth), w.accumulator()

	w.line("var %s uint64", total)
	w.line("for %s, %s := range %s {", key, one, from)
	w.line("%s := %s", part, seedFn)
	w.value(part, key, of.key, depth+1)
	w.value(part, one, of.elem, depth+1)
	w.line("%s += %s", total, part)
	w.line("}")

	w.line("%s = %s(%s, %s != nil)", into, boolFn, into, from)
	w.line("%s = %s(%s, uint64(len(%s)))", into, wholeFn, into, from)
	w.line("%s = %s(%s, %s)", into, wholeFn, into, total)
}

// accumulator names the variable one map's entries are totalled into, which is
// a fresh one for every map in the function.
func (w *writer) accumulator() string {
	w.totals++
	if w.totals == 1 {
		return totalVar
	}
	return totalVar + strconv.Itoa(w.totals)
}

// local names a variable a nested hash binds, so that a slice of slices does
// not shadow its own.
func local(name string, depth int) string {
	if depth == 0 {
		return name
	}
	return name + strconv.Itoa(depth)
}
