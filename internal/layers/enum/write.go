package enum

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/okian/forge/plugin"
)

// writer assembles one closed set's API as source.
//
// Text rather than syntax, for the reason the other layers' writers give: what
// is assembled is a run of switches with a case per member, which is many times
// its own size as a tree. The cost is the possibility of writing something that
// is not Go, and it is paid where the layer can still be stopped — the source
// is parsed before it leaves the package.
type writer struct{ out strings.Builder }

// String returns what has been written.
func (w *writer) String() string { return w.out.String() }

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

// wrapped writes a sentence over however many comment lines it takes.
func (w *writer) wrapped(text string) {
	for _, held := range plugin.Wrapped(text, plugin.CommentWidth) {
		w.line("// %s", held)
	}
}

// blank separates two declarations.
func (w *writer) blank() { w.line("") }

// set writes the whole of one closed set's API.
func (w *writer) set(held *plan) {
	w.rendering(held)

	// The author's own is theirs, and the two writers that ask a value whether
	// it is a member call whichever there is. A second one would not compile,
	// and the collision policy would drop the generated one silently — leaving
	// the text codec calling a method whose answer is somebody else's, which is
	// the right outcome arrived at by accident.
	if !held.of.HasMethod(validMethod) {
		w.blank()
		w.validity(held)
	}

	w.blank()
	w.listing(held)
	w.blank()
	w.parsing(held)
	w.blank()
	w.marshalling(held)
	w.blank()
	w.appending(held)
	w.blank()
	w.unmarshalling(held)
}

// rendering writes what a member is called.
func (w *writer) rendering(held *plan) {
	name := held.spelled.Text

	w.wrapped(displayMethod + " returns what this member is called.")
	w.line("//")
	w.wrapped("A value nobody declared renders as the type and the value it holds, " +
		"rather than as one of the members. It is not one, and a rendering that " +
		"said otherwise would let an undeclared value travel through a log and a " +
		"document with nothing saying where it came from.")

	w.line("func (v %s) %s() string {", name, displayMethod)
	w.line("\tswitch v {")

	for _, one := range held.distinct() {
		w.line("\tcase %s:", one.name)
		w.line("\t\treturn %s", strconv.Quote(one.text))
	}

	w.line("\t}")
	w.line("\treturn %q + %s + \")\"", name+"(", w.unknown(held))
	w.line("}")
}

// unknown returns the expression rendering a value that is not a member.
//
// Each width converted to what strconv takes for it. An unsigned type read
// through the signed function is not merely rendered oddly: every value above
// what a signed one holds comes out negative, so the two halves of a uint64 set
// would render as the same number with different signs.
func (w *writer) unknown(held *plan) string {
	switch {
	case held.text:
		return "strconv.Quote(string(v))"
	case held.unsigned:
		return "strconv.FormatUint(uint64(v), 10)"
	default:
		return "strconv.FormatInt(int64(v), 10)"
	}
}

// validity writes the test for whether a value is a member at all.
//
// A method rather than a condition written into each of the two that need it,
// because it is the question a caller has as well: a set counted from iota
// cannot say by comparing against its zero, since the zero is a member, and a
// caller left to work it out would write the switch again with one member
// missed.
func (w *writer) validity(held *plan) {
	name := held.spelled.Text

	w.wrapped(validMethod + " reports whether this is a member of the set.")
	w.line("//")
	w.wrapped("Worth asking, because a value of the type is not necessarily one of " +
		"them: the type is the whole range of whatever it is a name for, and the set " +
		"is the constants declared of it. A number off a wire, out of a database, or " +
		"cast from an integer is a value nobody declared, and there is no zero to " +
		"compare against — for a set counted from iota the zero is an ordinary " +
		"member.")

	// One case clause holding every member rather than a clause each. A Go
	// switch does not fall through, so a clause per member would answer for the
	// last one and report every other member as not being one.
	w.line("func (v %s) %s() bool {", name, validMethod)
	w.line("\tswitch v {")

	names := make([]string, 0, len(held.members))
	for _, one := range held.distinct() {
		names = append(names, one.name)
	}
	w.line("\tcase %s:", strings.Join(names, ",\n\t\t"))

	w.line("\t\treturn true")
	w.line("\t}")
	w.line("\treturn false")
	w.line("}")
}

// listing writes the members, in the order they were declared.
func (w *writer) listing(held *plan) {
	name := held.spelled.Text

	w.wrapped(valuesFunc + name + " returns every member, in the order they were declared.")
	w.line("//")
	w.wrapped("Declaration order rather than sorted, because that is the order the " +
		"constant block reads in and the order a run counted by iota means. A fresh " +
		"slice each call, so that a caller sorting or appending to what they were " +
		"given does not change what the next one is given.")

	w.line("func %s%s() []%s {", valuesFunc, name, name)
	w.line("\treturn []%s{", name)

	for _, one := range held.distinct() {
		w.line("\t\t%s,", one.name)
	}

	w.line("\t}")
	w.line("}")
}

// parsing writes the reader that takes a member's name back to the member.
func (w *writer) parsing(held *plan) {
	name := held.spelled.Text

	w.wrapped(parseFunc + name + " returns the member with this name.")
	w.line("//")
	w.wrapped("Every name a member has, which for a set with two names for one value " +
		"is both of them: aliasing is what a package does while a name is being " +
		"changed, and a reader that took only the new one would break every caller " +
		"the moment the old one was added.")

	w.line("func %s%s(s string) (%s, error) {", parseFunc, name, name)
	w.line("\tswitch s {")

	for _, one := range held.named() {
		w.line("\tcase %s:", strconv.Quote(one.text))
		w.line("\t\treturn %s, nil", one.name)
	}

	w.line("\t}")
	w.line("")
	w.line("\tvar zero %s", name)
	w.line("\treturn zero, errors.New(strconv.Quote(s) + %q)", " is not a member of "+name)
	w.line("}")
}

// marshalling writes the text form, which is the rendering.
func (w *writer) marshalling(held *plan) {
	name := held.spelled.Text

	w.wrapped(marshalMethod + " returns the member's name as text.")
	w.line("//")
	w.wrapped("The same text " + displayMethod + " renders, because a closed set has one " +
		"spelling and two would be a value that read one way and travelled another. " +
		"It is also what a JSON codec reaches for when the type has none of its own, " +
		"so the member goes over the wire under the name it is known by rather than " +
		"as the number behind it.")
	w.line("//")
	w.wrapped("A value nobody declared is refused rather than written. " + displayMethod +
		" renders one as the type and the value it holds, which is what a log wants " +
		"and is nothing a reader could take back — so letting it onto a wire would " +
		"be writing a document that cannot be read, and the zero of a set whose " +
		"members start elsewhere is exactly such a value.")

	w.line("func (v %s) %s() ([]byte, error) {", name, marshalMethod)
	w.line("\tif !v.%s() {", validMethod)
	w.line("\t\treturn nil, errors.New(v.%s() + %q)", displayMethod, " is not a member of "+name)
	w.line("\t}")
	w.line("")
	w.line("\treturn []byte(v.%s()), nil", displayMethod)
	w.line("}")
}

// appending writes the allocation-free half of the text form.
func (w *writer) appending(held *plan) {
	name := held.spelled.Text

	w.wrapped(appendMethod + " writes the member's name onto the end of a buffer.")
	w.line("//")
	w.wrapped("What " + marshalMethod + " does without the slice it has to allocate, for a " +
		"caller who owns the buffer already. encoding/json reaches for this in " +
		"preference where a type has it, so it refuses what that refuses: a value " +
		"nobody declared, which the two would otherwise disagree about depending on " +
		"which the codec happened to call.")

	w.line("func (v %s) %s(b []byte) ([]byte, error) {", name, appendMethod)
	w.line("\tif !v.%s() {", validMethod)
	w.line("\t\treturn nil, errors.New(v.%s() + %q)", displayMethod, " is not a member of "+name)
	w.line("\t}")
	w.line("")
	w.line("\treturn append(b, v.%s()...), nil", displayMethod)
	w.line("}")
}

// unmarshalling writes the reader the text form is read back through.
func (w *writer) unmarshalling(held *plan) {
	name := held.spelled.Text

	w.wrapped(unmarshalMethod + " reads a member back from its name.")
	w.line("//")
	w.wrapped("A name nobody declared is an error rather than a zero value. The zero of " +
		"a set counted from iota is an ordinary member, so decoding an unknown name " +
		"into it would turn a typo in a document into a value the receiver treats as " +
		"meant — which is the failure a closed set exists to prevent.")

	w.line("func (v *%s) %s(b []byte) error {", name, unmarshalMethod)
	w.line("\theld, err := %s%s(string(b))", parseFunc, name)
	w.line("\tif err != nil {")
	w.line("\t\treturn err")
	w.line("\t}")
	w.line("")
	w.line("\t*v = held")
	w.line("\treturn nil")
	w.line("}")
}
