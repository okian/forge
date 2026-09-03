package jsoncodec

import (
	"encoding/json/jsontext"
	"go/types"
	"strconv"
	"strings"
)

// encoder writes the functions that put one type on the wire.
//
// Every type gets an appending function, including the ones that also get
// methods. The methods are what make the type satisfy the standard library's
// interface, and the function is what everything generated calls — so a
// subject that moves into a package where no method can be declared changes
// which of the two holds the body, and changes nothing that calls it.
func (w *writer) encoder(of *form) {
	name := encoderFor(of.typ)
	spelled := of.spelled.Text
	dst, v := w.n("dst"), w.n("v")

	w.line("// %s appends a %s as JSON.", name, spelled)
	if of.attach {
		w.line("//")
		w.line("// The value's own method holds the body; this is what generated code")
		w.line("// calls, so that a caller names one function whether or not the type")
		w.line("// is one a method could be declared on.")
		w.line("func %s(%s []byte, %s %s) ([]byte, error) {", name, dst, v, spelled)
		w.line("return %s.%s(%s)", v, appendJSONMethod, dst)
		w.line("}")
		w.blank()

		w.line("// %s appends the %s to %s as a JSON object, and returns the", appendJSONMethod, spelled, dst)
		w.line("// extended buffer.")
		w.line("//")
		w.line("// Members are written in the order the fields are declared, an embedded")
		w.line("// struct's where the embedded field is. Nothing is allocated beyond the")
		w.line("// growth of %s, which is what makes this the implementation the other", dst)
		w.line("// entry points reach.")
		w.line("func (%s %s) %s(%s []byte) ([]byte, error) {", v, spelled, appendJSONMethod, dst)
		w.appendBody(of)
		w.line("}")
		w.blank()

		w.marshaller(spelled)
		return
	}

	w.line("func %s(%s []byte, %s %s) ([]byte, error) {", name, dst, v, spelled)
	w.appendBody(of)
	w.line("}")
	w.blank()
}

// marshaller writes the entry point the standard library dispatches to.
func (w *writer) marshaller(spelled string) {
	v := w.n("v")

	w.line("// %s writes the %s as a compact JSON document.", marshalMethod, spelled)
	w.line("//")
	w.line("// The document is assembled in a borrowed buffer and copied out, so the")
	w.line("// cost over [%s.%s] is one exactly-sized allocation — the", spelled, appendJSONMethod)
	w.line("// slice being returned. It is also what makes the standard library")
	w.line("// dispatch to this codec wherever a %s appears.", spelled)
	w.line("func (%s %s) %s() ([]byte, error) {", v, spelled, marshalMethod)
	w.line("%s := jsonTakeScratch()", w.n("scratch"))
	w.line("%s, %s := %s.%s((*%s)[:0])", w.n("held"), w.n("err"), v, appendJSONMethod, w.n("scratch"))
	w.line("return jsonFinish(%s, %s, %s)", w.n("scratch"), w.n("held"), w.n("err"))
	w.line("}")
	w.blank()
}

// appendBody writes what one appender does: the object, member by member.
//
// The opening brace is the interesting byte. Where the first member is always
// written it is fused into that member's name, and every later member leads
// with a comma; where the first member may be left out, every member leads
// with a comma and the first one written has its comma overwritten with the
// brace at the end — which is what lets a member's presence be decided without
// any member knowing whether another came before it.
func (w *writer) appendBody(of *form) {
	dst := w.n("dst")

	plans := make([]when, len(of.members))
	needErr := false
	for i, one := range of.members {
		plans[i] = w.omitted(one, w.n("v")+"."+one.path)
		held := one.of
		if fallible(&held, make(map[*form]bool)) {
			needErr = true
		}
	}

	if needErr {
		w.line("var %s error", w.n("err"))
	}

	fused := w.fusedOpen(of, plans)
	if !fused {
		w.line("%s := len(%s)", w.n("open"), dst)
	}

	opened := false
	for i, one := range of.members {
		if plans[i].can && plans[i].never {
			continue
		}
		lead := ","
		if fused && !opened {
			lead = "{"
		}
		w.writeMember(one, plans[i], lead)
		opened = true
	}

	switch {
	case !opened:
		w.line("return append(%s, '{', '}'), nil", dst)
	case fused:
		w.line("return append(%s, '}'), nil", dst)
	default:
		w.line("if len(%s) == %s {", dst, w.n("open"))
		w.line("return append(%s, '{', '}'), nil", dst)
		w.line("}")
		w.line("%s[%s] = '{'", dst, w.n("open"))
		w.line("return append(%s, '}'), nil", dst)
	}
}

// fusedOpen reports whether the opening brace can be baked into the first
// member's name, which it can exactly when that member is always written.
func (w *writer) fusedOpen(of *form, plans []when) bool {
	for i, one := range of.members {
		if plans[i].can && plans[i].never {
			continue
		}
		return len(one.guards) == 0 && plans[i].can && plans[i].cond == "" && !plans[i].retract
	}
	return false
}

// writeMember writes one member of an object: its name fused with the byte
// before it, then its value, under whatever conditions leave it out.
func (w *writer) writeMember(one member, held when, lead string) {
	dst, v := w.n("dst"), w.n("v")
	closing := 0

	// An embedded pointer contributes nothing when it is nil, which is the rule
	// for a promoted member rather than a choice. The guards nest, because an
	// inner one cannot be read before the outer one is known to be there.
	for _, guard := range one.guards {
		w.line("if %s.%s != nil {", v, guard.path)
		closing++
	}

	if !held.can {
		w.refused = append(w.refused, one)
	}
	if held.cond != "" {
		w.line("if %s {", held.cond)
		closing++
	}

	prefix := lead + quotedName(one.name) + ":"
	if held.retract {
		mark := w.at("kept", w.marks)
		w.marks++
		w.line("%s := len(%s)", mark, dst)
		w.line("%s = append(%s, %s...)", dst, dst, goString(prefix))
		w.appendValue(v+"."+one.path, &one.of, 0)
		w.line("if jsonWroteEmpty(%s, %s+%d) {", dst, mark, len(prefix))
		w.line("%s = %s[:%s]", dst, dst, mark)
		w.line("}")
	} else {
		w.line("%s = append(%s, %s...)", dst, dst, goString(prefix))
		w.appendValue(v+"."+one.path, &one.of, 0)
	}

	for range closing {
		w.line("}")
	}
}

// quotedName is a member's name as the document carries it, escaped once here
// rather than on every call.
//
// The canonical form is the standard library's, so a name that needs an escape
// is baked already escaped: there is no caller whose options could ask for a
// different escaping.
func quotedName(name string) string {
	quoted, err := jsontext.AppendQuote(nil, name)
	if err != nil {
		// A name the standard library cannot quote — invalid UTF-8 in a struct
		// tag. Go's own quoting keeps the output compiling, and the agreement
		// tests are what would surface the divergence, on source no gofmt run
		// would have let through.
		return strconv.Quote(name)
	}
	return string(quoted)
}

// goString writes a string as a Go literal a person can read: backquoted where
// the content allows it, escaped where it does not.
func goString(s string) string {
	if strconv.CanBackquote(s) {
		return "`" + s + "`"
	}
	return strconv.Quote(s)
}

// fallible reports whether the statements appending a value of this form bind
// the enclosing function's error, which is what decides whether that error is
// declared at all.
//
// The question is about the emission rather than about the value: a nested
// struct is written by a call that answers with an error whatever the struct
// holds, and a delegate reached through the standard library fails through a
// local of its own. Everything a composite holds is asked through its element,
// with a set of what is already being asked so a type that reaches itself
// terminates.
func fallible(of *form, asking map[*form]bool) bool {
	if of == nil || asking[of] {
		return false
	}
	asking[of] = true
	defer delete(asking, of)

	switch of.how {
	case writtenString, writtenFloat, writtenText, writtenStruct, writtenMap:
		return true

	case writtenDelegate:
		return of.writes == appendJSONMethod

	case writtenPointer, writtenSlice, writtenArray:
		return fallible(of.elem, asking)

	case writtenBool, writtenInt, writtenUint, writtenBytes, writtenFallback, writtenInvalid:
		return false
	}

	return false
}

// omitted returns the condition under which a member is written at all, or the
// empty string when it always is.
//
// The two options differ in what they are defined against, and both are here
// because the standard library has both. omitzero asks a Go question — is this
// the type's zero value — and omitempty asks a JSON one: is what this becomes
// an empty string, array or object. A number that is zero is omitted by the
// first and written by the second, which is the distinction and the reason
// neither can stand in for the other.
//
// omitempty can always be answered, because the append form makes the
// standard library's own method nearly free: where no condition can be written
// in advance, the member is written and taken back off the end if what was
// written was empty. omitzero has no such recourse — zero is a question about
// the Go value, and a value whose parts cannot be compared has no answer —
// so it alone can make a member unwritable.
func (w *writer) omitted(one member, held string) when {
	switch {
	case one.omitZero && one.omitEmpty:
		// Not alternatives, and the standard library does not read them as
		// such: omitzero is asked before the value is written and omitempty
		// after, so a member carrying both is left out when either says to
		// leave it out. They agree about a nil slice and disagree about an
		// allocated empty one, and answering with the first of the two writes
		// "s":[] where the standard library writes no member at all.
		cond, can := w.nonZero(held, &one.of)
		zero := when{cond: cond, can: can}
		empty := w.nonEmpty(held, &one.of)
		if !empty.can {
			if !zero.can {
				return when{}
			}
			return when{cond: zero.cond, retract: true, can: true}
		}
		return survives(zero, empty)

	case one.omitZero:
		cond, can := w.nonZero(held, &one.of)
		return when{cond: cond, can: can}

	case one.omitEmpty:
		empty := w.nonEmpty(held, &one.of)
		if !empty.can {
			// Empty is a question about what was written, and this is a member
			// whose writing is somebody else's — so it is written first and
			// asked about afterwards, which is how the standard library
			// answers it too.
			return when{retract: true, can: true}
		}
		return empty

	default:
		return always
	}
}

// survives returns the condition that a member passes both tests, which is
// what a member carrying both options has to do to be written at all.
//
// A test that says never wins outright, because a member left out under one
// option is left out.
func survives(zero, empty when) when {
	switch {
	case !zero.can || !empty.can:
		return when{}
	case zero.never || empty.never:
		return when{never: true, can: true}
	case zero.cond == "":
		return empty
	case empty.cond == "":
		return zero
	case zero.cond == empty.cond:
		// A string is zero exactly when it is empty, and so are several other
		// shapes. Writing the same test twice is not wrong, but go vet refuses
		// a redundant and, and a reader would ask what the difference was
		// meant to be.
		return zero
	default:
		return when{cond: "(" + zero.cond + ") && (" + empty.cond + ")", can: true}
	}
}

// nonZero returns the condition that a value is not its type's zero value, and
// whether one could be written at all.
//
// Zero is a Go question, and Go answers it with == only for a comparable type.
// A struct holding a slice is not comparable, and a codec that gave up there
// would write a member the author asked to omit — silently, and differently
// from the standard library, which reaches the same answer through reflection.
// So the test is built out of the parts instead: a value is zero exactly when
// every part of it is.
//
// That works only where the parts are all reachable. A struct with a field this
// codec does not write — an unexported one, or one tagged off the wire — has
// parts that are still part of the value, so a condition built from the rest
// answers a narrower question than the one asked. A type that brought its own
// codec and cannot be compared has no reachable parts at all. Both are reported
// rather than written either way, because both ways are wrong and only one of
// them says so.
func (w *writer) nonZero(held string, of *form) (string, bool) {
	// Asked before anything else, because a type that says what empty means
	// for itself has the last word on it — which is the rule the standard
	// library follows and the reason a type bothers to declare the method.
	if zeroable(of) {
		return "!" + held + ".IsZero()", true
	}

	if said, done := w.nonZeroScalar(held, of); done {
		return said, true
	}
	return w.nonZeroComposite(held, of)
}

// nonZeroScalar answers for the shapes whose zero value is one comparison, and
// reports whether it answered.
func (w *writer) nonZeroScalar(held string, of *form) (string, bool) {
	switch of.how {
	case writtenSlice, writtenMap, writtenPointer:
		// The zero value of each of these is nil, and comparing a slice or a
		// map with == is legal only against nil anyway.
		return held + " != nil", true

	case writtenBytes:
		// Bytes arrive as a slice or as an array, whose zero values are not the
		// same thing: a slice's is nil, and an array's is an array of zeroes,
		// which nil cannot be compared against at all.
		if _, fixed := of.typ.Underlying().(*types.Array); fixed {
			return held + " != (" + of.spelled.Text + "{})", true
		}
		return held + " != nil", true

	case writtenString:
		return held + ` != ""`, true

	case writtenBool:
		return held, true

	case writtenInt, writtenUint, writtenFloat:
		return held + " != 0", true

	case writtenStruct, writtenArray, writtenFallback, writtenDelegate, writtenText, writtenInvalid:
		return "", false
	}

	return "", false
}

// nonZeroComposite answers for the shapes whose zero value is either one
// comparison or none, depending on what they are made of.
func (w *writer) nonZeroComposite(held string, of *form) (string, bool) {
	if zero, ok := zeroValue(of); ok {
		return held + " != " + zero, true
	}

	switch of.how {
	case writtenStruct:
		// Judged from the members, which is right only when the members are the
		// whole struct. A field this codec does not write is still part of what
		// zero means, and a condition built from the rest would call a struct
		// zero while an unexported slice inside it held something.
		if !of.whole {
			return "", false
		}
		return w.someMember(held, of)

	case writtenArray:
		return w.someElement(held, of)

	case writtenDelegate, writtenFallback, writtenText:
		// A type that writes itself, one handed to the reflective encoder, or
		// one written through its text codec. None is comparable — the case
		// above would have taken it — and none has parts this may look at.
		return "", false

	case writtenInvalid, writtenBool, writtenString, writtenInt, writtenUint,
		writtenFloat, writtenBytes, writtenSlice, writtenMap, writtenPointer:
		return "", true
	}

	return "", true
}

// zeroValue returns how a type's zero value is written, for the types that can
// be compared against one at all.
//
// Comparability is not the question on its own. A pointer and an interface are
// comparable and have no composite literal; a named integer that brought its own
// codec is comparable and is written 0 rather than T{}. Writing T{} for either
// produces source that is not Go, which a codec assembled as text finds out
// only when it is parsed — or, for a named scalar, does not find out at all,
// because the literal is syntactically fine and lands in the committed file.
func zeroValue(of *form) (string, bool) {
	if !equatable(of) || of.how == writtenInvalid {
		return "", false
	}

	switch under := of.typ.Underlying().(type) {
	case *types.Basic:
		switch {
		case under.Info()&types.IsBoolean != 0:
			return "false", true
		case under.Info()&types.IsString != 0:
			return `""`, true
		case under.Info()&types.IsNumeric != 0:
			return "0", true
		default:
			return "", false
		}

	case *types.Struct, *types.Array:
		return "(" + of.spelled.Text + "{})", true

	case *types.Pointer, *types.Interface, *types.Chan, *types.Signature:
		return "nil", true

	default:
		return "", false
	}
}

// someMember returns the condition that some member of a struct is not zero.
//
// A struct is zero when every member of it is, so the test is the members'
// tests joined. A member reached through an embedded pointer is not read at
// all: the pointer being there is already enough to say the struct is not zero,
// and reading through a nil one would panic before the answer was reached.
func (w *writer) someMember(held string, of *form) (string, bool) {
	// No guard against reaching this struct again, because it cannot be
	// reached: Go forbids a struct containing itself by value, and a member
	// that reaches it indirectly is a pointer, a slice or a map, each of which
	// is answered by a comparison against nil rather than by looking inside.
	var (
		parts  []string
		guards = make(map[string]bool)
	)

	for _, one := range of.members {
		if len(one.guards) > 0 {
			outermost := one.guards[0].path
			if !guards[outermost] {
				guards[outermost] = true
				parts = append(parts, held+"."+outermost+" != nil")
			}
			continue
		}

		said, can := w.nonZero(held+"."+one.path, &one.of)
		if !can {
			return "", false
		}
		if said != "" {
			parts = append(parts, said)
		}
	}

	return strings.Join(parts, " || "), true
}

// someElement returns the condition that some element of an array is not zero.
//
// Written out per index rather than as a loop, because the length is part of
// the type: three comparisons joined is what a person would write, and it is
// what the compiler can see through.
func (w *writer) someElement(held string, of *form) (string, bool) {
	length, ok := of.typ.Underlying().(*types.Array)
	if !ok {
		return "", false
	}

	var parts []string
	for i := range length.Len() {
		said, can := w.nonZero(held+"["+strconv.FormatInt(i, 10)+"]", of.elem)
		if !can {
			return "", false
		}
		if said != "" {
			parts = append(parts, said)
		}
	}

	return strings.Join(parts, " || "), true
}

// when says under what condition a member is written.
type when struct {
	// cond is the condition, empty when the member is written unconditionally.
	cond string

	// never records that the member is never written, which no condition can
	// say: a condition that is always false is a branch the compiler will warn
	// about and a reader will puzzle over, and leaving the member out entirely
	// is what it means.
	never bool

	// retract records that whether the member stays cannot be known until it
	// is written: it is appended, looked at, and taken back off the end if
	// what was written was one of JSON's empties. What omitempty means for a
	// member whose bytes somebody else decides.
	retract bool

	// can records that a condition could be written at all. What cannot is a
	// type that brought its own codec and cannot be compared, whose parts are
	// neither reachable from here nor this layer's to reason about.
	can bool
}

// always is what a member with nothing to omit it says.
var always = when{can: true}

// nonEmpty returns the condition that a value is not empty on the wire.
//
// Empty is a JSON question rather than a Go one: the empty string, the empty
// array, the empty object, and null. A number is never empty however small it
// is, and neither is a boolean, so a member of either is always written.
//
// A struct is the interesting case, and the one the standard library answers by
// looking at what it produced. It renders as an empty object exactly when every
// one of its members is left out, and every one of those is a condition this
// already knows how to write — so the answer is those conditions joined rather
// than the output measured.
func (w *writer) nonEmpty(held string, of *form) when {
	switch of.how {
	case writtenString:
		return when{cond: held + ` != ""`, can: true}

	case writtenSlice, writtenMap, writtenBytes:
		return when{cond: "len(" + held + ") != 0", can: true}

	case writtenPointer:
		// A pointer to something that renders empty is empty. The standard
		// library omits it, and a codec that only asked whether the pointer was
		// there would write "p":{} where the standard library writes nothing.
		//
		// A type reached through itself has no condition to write. Whether such
		// a value is empty depends on how deep the chain runs, which is a
		// question about the value rather than about the type — the standard
		// library answers it by looking at what it wrote, and this would have to
		// expand the condition once per link, forever.
		if w.asking[of] {
			return when{}
		}
		w.asking[of] = true
		defer delete(w.asking, of)

		inner := w.nonEmpty("(*"+held+")", of.elem)
		switch {
		case !inner.can:
			return when{}
		case inner.never:
			return when{never: true, can: true}
		case inner.cond == "":
			return when{cond: held + " != nil", can: true}
		}
		return when{cond: held + " != nil && (" + inner.cond + ")", can: true}

	case writtenStruct:
		return w.someWritten(held, of)

	case writtenArray:
		// A fixed-length array is an array of that length, whatever it holds.
		// Only a zero-length one is ever empty, and then it always is.
		if length, ok := of.typ.Underlying().(*types.Array); ok && length.Len() == 0 {
			return when{never: true, can: true}
		}
		return always

	case writtenBool, writtenInt, writtenUint, writtenFloat, writtenInvalid:
		return always

	case writtenDelegate, writtenFallback, writtenText:
		// Empty is a question about what was written, and what these write is
		// decided elsewhere — by a codec somebody else wrote, by the reflective
		// encoder, or by a text codec whose output nothing here has seen. The
		// caller answers by writing the member and looking, which the retract
		// path is for.
		return when{}
	}

	return always
}

// someWritten returns the condition that a struct writes any member at all.
//
// A member with nothing to omit it is written whatever the struct holds, and one
// such member is enough: the object is never empty, so the struct is never
// omitted and no condition is needed.
func (w *writer) someWritten(held string, of *form) when {
	if w.asking[of] {
		return when{}
	}
	w.asking[of] = true
	defer delete(w.asking, of)

	var (
		parts  []string
		guards = make(map[string]bool)
	)

	for _, one := range of.members {
		if len(one.guards) > 0 {
			// A member behind an embedded pointer is written when the pointer
			// is there, and reading past a nil one to ask anything finer would
			// panic before the answer was reached.
			outermost := one.guards[0].path
			if !guards[outermost] {
				guards[outermost] = true
				parts = append(parts, held+"."+outermost+" != nil")
			}
			continue
		}

		held := w.omitted(one, held+"."+one.path)
		switch {
		case !held.can || held.retract:
			// A member whose presence is decided by looking at what it wrote
			// is one whose presence cannot be written as a condition here.
			return when{}
		case held.never:
			continue
		case held.cond == "":
			return always
		}
		parts = append(parts, held.cond)
	}

	if len(parts) == 0 {
		// Every member is always left out, so the object is always empty.
		return when{never: true, can: true}
	}
	return when{cond: strings.Join(parts, " || "), can: true}
}

// appendValue writes the statements that put one value on the wire, appended
// to dst.
//
// depth distinguishes the variables a nested composite binds, so that a slice
// of slices does not shadow its own loop variable.
func (w *writer) appendValue(held string, of *form, depth int) {
	if w.appendScalar(held, of, depth) {
		return
	}

	dst, err := w.n("dst"), w.n("err")

	switch of.how {
	case writtenFallback:
		w.appendSpliced(held, depth)

	case writtenDelegate:
		w.appendDelegate(held, of, depth)

	case writtenText:
		w.appendText(held, of, depth)

	case writtenStruct:
		w.line("if %s, %s = %s(%s, %s); %s != nil {", dst, err, encoderFor(of.typ), dst, held, err)
		w.line("return %s, %s", dst, err)
		w.line("}")

	case writtenPointer:
		w.line("if %s == nil {", held)
		w.line(`%s = append(%s, "null"...)`, dst, dst)
		w.line("} else {")
		w.appendValue("(*"+held+")", of.elem, depth+1)
		w.line("}")

	case writtenSlice, writtenArray:
		w.appendArray(held, of, depth)

	case writtenMap:
		w.appendMap(held, of, depth)

	case writtenBool, writtenString, writtenInt, writtenUint, writtenFloat, writtenBytes, writtenInvalid:
		// Nothing here for any of them. The scalars were answered above, and
		// are named so that a form added to the plan and forgotten here is a
		// compiler's complaint rather than a value nothing writes; a refused
		// form has its refusal recorded already, and the run stops before any
		// of this is emitted.
	}
}

// appendScalar writes the forms that are one append, and reports whether it
// wrote one.
func (w *writer) appendScalar(held string, of *form, depth int) bool {
	dst, err := w.n("dst"), w.n("err")

	switch of.how {
	case writtenBool:
		w.line("%s = strconv.AppendBool(%s, bool(%s))", dst, dst, held)

	case writtenInt:
		w.line("%s = strconv.AppendInt(%s, int64(%s), 10)", dst, dst, held)

	case writtenUint:
		w.line("%s = strconv.AppendUint(%s, uint64(%s), 10)", dst, dst, held)

	case writtenFloat:
		// A float32 has a shortest form of its own, and it is not the shortest
		// form of the float64 it widens to: 1.1 as a float32 widens to
		// 1.100000023841858, which is the number but not what anybody else
		// writes for it. The width is passed so the helper chooses against the
		// right one.
		bits := 64
		if basic, ok := of.typ.Underlying().(*types.Basic); ok && basic.Kind() == types.Float32 {
			bits = 32
		}
		w.line("if %s, %s = jsonAppendFinite(%s, float64(%s), %d); %s != nil {", dst, err, dst, held, bits, err)
		w.line("return %s, %s", dst, err)
		w.line("}")

	case writtenString:
		w.line("if %s, %s = jsonAppendString(%s, string(%s)); %s != nil {", dst, err, dst, held, err)
		w.line("return %s, %s", dst, err)
		w.line("}")

	case writtenBytes:
		// A byte slice is base64 in a string rather than an array, which is the
		// standard library's rule rather than a choice available here. An array
		// of bytes is the same string, sliced through a local because what is
		// held may not be addressable.
		if _, fixed := of.typ.Underlying().(*types.Array); fixed {
			one := w.at("held", depth)
			w.line("{")
			w.line("%s := %s", one, held)
			w.line("%s = jsonAppendBytes(%s, %s[:])", dst, dst, one)
			w.line("}")
			return true
		}
		w.line("%s = jsonAppendBytes(%s, %s)", dst, dst, held)

	default:
		return false
	}

	return true
}

// appendSpliced writes a value through the standard library and splices the
// document it returns.
//
// The boundary for a field nothing here can see through, and for a type whose
// codec speaks an interface this output cannot carry an encoder for. What
// comes back is valid compact JSON by the library's own contract, so it is
// appended whole.
//
// Deterministically, because everything this codec writes is: a map the
// library reaches through this boundary would otherwise come out in an order
// that differs between two runs over one value, in a document whose every
// other member is reproducible.
func (w *writer) appendSpliced(held string, depth int) {
	dst := w.n("dst")
	spliced := w.at("spliced", depth)
	failed := w.at("failed", depth)

	w.line("{")
	w.line("%s, %s := json.Marshal(%s, json.Deterministic(true))", spliced, failed, held)
	w.line("if %s != nil {", failed)
	w.line("return %s, %s", dst, failed)
	w.line("}")
	w.line("%s = append(%s, %s...)", dst, dst, spliced)
	w.line("}")
}

// appendDelegate calls the codec a type declares for itself.
//
// A type whose codec appends is called straight, because appending is what
// this caller is doing anyway. Any other shape of codec is reached through the
// standard library, which knows how to call each generation of the contract
// and validates what the method answers — this codec's own output must stay
// valid whatever somebody's method returns.
func (w *writer) appendDelegate(held string, of *form, depth int) {
	if of.writes != appendJSONMethod {
		w.appendSpliced(held, depth)
		return
	}

	dst, err := w.n("dst"), w.n("err")

	// Through a local where the method is declared on the pointer, because Go
	// will not call one on something it cannot take the address of. A field of
	// the receiver is addressable and would not need this; a map's element is
	// not, and a map's element is one of the things this is handed — so the
	// local is what makes the one case work without the caller having to know
	// which case it is.
	if valueMethod(of.typ, appendJSONMethod) {
		w.line("if %s, %s = %s.%s(%s); %s != nil {", dst, err, held, appendJSONMethod, dst, err)
		w.line("return %s, %s", dst, err)
		w.line("}")
		return
	}

	one := w.at("held", depth)
	w.line("{")
	w.line("%s := %s", one, held)
	w.line("if %s, %s = %s.%s(%s); %s != nil {", dst, err, one, appendJSONMethod, dst, err)
	w.line("return %s, %s", dst, err)
	w.line("}")
	w.line("}")
}

// appendText writes a value as the JSON string its own text codec produces.
//
// What the standard library does with such a type, and for the same reason: the
// text form is what its author said the value means, and the form underneath is
// an implementation detail a document has no way to explain. A closed set is
// the case that makes it matter — a member written as the number behind it says
// nothing to a reader and moves the day somebody inserts a member above it —
// and it is the codec's refusal of an undeclared value that comes with it.
//
// Always through a local, where a delegate asks first whether it needs one.
// Asking means reading the package for the method's receiver, and the method
// may be one this run has not written yet: on a clean checkout there is no
// receiver to read and on the next run there is, so the answer would differ
// between two runs of one unchanged declaration and the file would rewrite
// itself on alternate builds. A local is right for either receiver — a method
// on the pointer needs something addressable and a method on the value will
// take one — so the question is not worth the way it has to be asked.
func (w *writer) appendText(held string, of *form, depth int) {
	dst, err := w.n("dst"), w.n("err")
	one := w.at("held", depth)
	text := w.at("text", depth)
	failed := w.at("failed", depth)

	w.line("{")
	w.line("%s := %s", one, held)
	w.line("%s, %s := %s.%s(%s)", text, failed, one, of.text, appended(of.text))
	w.line("if %s != nil {", failed)
	w.line("return %s, %s", dst, failed)
	w.line("}")
	w.line("if %s, %s = jsonAppendString(%s, string(%s)); %s != nil {", dst, err, dst, text, err)
	w.line("return %s, %s", dst, err)
	w.line("}")
	w.line("}")
}

// appended is the argument a text writer takes: nothing for MarshalText, and
// the buffer to append to for AppendText, which here is none.
func appended(method string) string {
	if method == textAppendMethod {
		return "nil"
	}
	return ""
}

// appendArray writes a slice or an array as a JSON array.
//
// Every element leads with a comma and the first one's comma is overwritten
// with the bracket at the end, which is the same bargain the object writer
// makes: no element needs to know whether it is first.
func (w *writer) appendArray(held string, of *form, depth int) {
	dst := w.n("dst")
	mark := w.at("mark", depth)
	one := w.at("one", depth)

	w.line("{")
	w.line("%s := len(%s)", mark, dst)
	w.line("for _, %s := range %s {", one, held)
	w.line("%s = append(%s, ',')", dst, dst)
	w.appendValue(one, of.elem, depth+1)
	w.line("}")
	w.line("if len(%s) == %s {", dst, mark)
	w.line("%s = append(%s, '[', ']')", dst, dst)
	w.line("} else {")
	w.line("%s[%s] = '['", dst, mark)
	w.line("%s = append(%s, ']')", dst, dst)
	w.line("}")
	w.line("}")
}

// appendMap writes a map as a JSON object, its members in the order its keys
// sort.
//
// Sorted rather than in range order, which is where this codec differs from
// what the standard library does by default — it sorts only when asked,
// through its Deterministic option. Generated output is committed and
// reviewed, and a member order that changed between runs would produce a diff
// nobody could account for; sorted is also the one order that can be tested.
//
// The keys are gathered into a borrowed slice, which is the entire allocation
// profile of writing a map. A failure inside the loop returns without handing
// the slice back, which costs the pool one slice and nothing else.
func (w *writer) appendMap(held string, of *form, depth int) {
	dst, err := w.n("dst"), w.n("err")
	keys := w.at("keys", depth)
	key := w.at("key", depth)
	mark := w.at("mark", depth)

	w.line("{")
	w.line("%s := jsonSortedKeys(%s)", keys, held)
	w.line("%s := len(%s)", mark, dst)
	w.line("for _, %s := range *%s {", key, keys)
	w.line("%s = append(%s, ',')", dst, dst)
	w.line("if %s, %s = jsonAppendString(%s, %s); %s != nil {", dst, err, dst, key, err)
	w.line("return %s, %s", dst, err)
	w.line("}")
	w.line("%s = append(%s, ':')", dst, dst)
	w.appendValue(held+"["+of.key.spelled.Text+"("+key+")]", of.elem, depth+1)
	w.line("}")
	w.line("jsonDropKeys(%s)", keys)
	w.line("if len(%s) == %s {", dst, mark)
	w.line("%s = append(%s, '{', '}')", dst, dst)
	w.line("} else {")
	w.line("%s[%s] = '{'", dst, mark)
	w.line("%s = append(%s, '}')", dst, dst)
	w.line("}")
	w.line("}")
}

// loopVar names a variable a nested composite binds, so that the inner one does
// not shadow the outer.
func loopVar(name string, depth int) string {
	if depth == 0 {
		return name
	}
	return name + strconv.Itoa(depth)
}

// equatable reports whether a value of this type may be compared with ==, which
// is what decides whether a zero test can be written as one comparison.
//
// Asked of the type rather than of the members this codec writes. A struct's
// unexported fields and the ones tagged out of the wire are not written and are
// still part of it, so a codec that judged comparability by what it writes
// would call a struct holding an unexported slice comparable — and emit a
// comparison the compiler refuses, in a file the author did not write.
func equatable(of *form) bool {
	return of != nil && of.typ != nil && types.Comparable(of.typ)
}

// zeroable reports whether a type answers for itself whether it is zero.
//
// The standard library asks a type that declares IsZero rather than comparing
// it, and a codec that compared instead would disagree with it about exactly
// the types whose author went to the trouble of saying what empty means.
// Declared on the type or on a pointer to it, since either is reachable from a
// value the codec holds.
func zeroable(of *form) bool {
	if of == nil || of.typ == nil {
		return false
	}

	for _, held := range []types.Type{of.typ, types.NewPointer(of.typ)} {
		found := types.NewMethodSet(held).Lookup(nil, "IsZero")
		if found == nil {
			continue
		}

		signature, ok := found.Obj().Type().(*types.Signature)
		if !ok || signature.Params().Len() != 0 || signature.Results().Len() != 1 {
			continue
		}
		if basic, is := signature.Results().At(0).Type().(*types.Basic); is && basic.Kind() == types.Bool {
			return true
		}
	}
	return false
}
