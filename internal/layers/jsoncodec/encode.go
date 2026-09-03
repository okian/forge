package jsoncodec

import (
	"go/types"
	"strconv"
	"strings"
)

// encoder writes the function that puts one type on the wire.
//
// Every type gets a function, including the ones that also get a method. The
// method is what makes the type satisfy the standard library's interface, and
// the function is what everything generated calls — so a subject that moves
// into a package where no method can be declared changes which of the two holds
// the body, and changes nothing that calls it.
func (w *writer) encoder(of *form) {
	name := encoderFor(of.typ)
	spelled := of.spelled.Text

	w.line("// %s writes a %s as JSON.", name, spelled)
	if of.attach {
		w.line("//")
		w.line("// The value's own method holds the body; this is what generated code")
		w.line("// calls, so that a caller names one function whether or not the type")
		w.line("// is one a method could be declared on.")
		w.line("func %s(%s *jsontext.Encoder, %s %s) error {", name, w.names.encoder, w.names.value, spelled)
		w.line("return %s.%s(%s)", w.names.value, marshalMethod, w.names.encoder)
		w.line("}")
		w.blank()

		w.line("// %s writes the %s as a JSON object.", marshalMethod, spelled)
		w.line("//")
		w.line("// Members are written in the order the fields are declared, an embedded")
		w.line("// struct's where the embedded field is. A field that takes a name from a")
		w.line("// shallower one keeps its own place rather than the excluded one's.")
		w.line("func (%s %s) %s(%s *jsontext.Encoder) error {", w.names.value, spelled, marshalMethod, w.names.encoder)
		w.body(of)
		w.line("}")
		w.blank()
		return
	}

	w.line("func %s(%s *jsontext.Encoder, %s %s) error {", name, w.names.encoder, w.names.value, spelled)
	w.body(of)
	w.line("}")
	w.blank()
}

// body writes what one encoder does, which depends only on the form.
func (w *writer) body(of *form) {
	switch of.how {
	case writtenStruct:
		w.line("if err := %s.WriteToken(jsontext.BeginObject); err != nil {", w.names.encoder)
		w.line("return err")
		w.line("}")
		for _, one := range of.members {
			w.writeMember(one)
		}
		w.line("return %s.WriteToken(jsontext.EndObject)", w.names.encoder)

	default:
		w.writeValue(w.names.value, of, 0)
		w.line("return nil")
	}
}

// writeMember writes one member of an object: its name, then its value, under
// whatever conditions leave it out.
func (w *writer) writeMember(one member) {
	closing := 0

	// An embedded pointer contributes nothing when it is nil, which is the rule
	// for a promoted member rather than a choice. The guards nest, because an
	// inner one cannot be read before the outer one is known to be there.
	for _, held := range one.guards {
		w.line("if %s.%s != nil {", w.names.value, held.path)
		closing++
	}

	held := w.omitted(one, w.names.value+"."+one.path)
	switch {
	case !held.can && one.omitEmpty:
		// Empty is a question about what was written, and this is a member
		// whose writing is somebody else's — so it is written first and asked
		// about afterwards, which is how the standard library answers it too.
		w.buffered(one)
		for range closing {
			w.line("}")
		}
		return

	case !held.can:
		w.refused = append(w.refused, one)

	case held.never:
		// Nothing to write, and no branch to write it in.
		for range closing {
			w.line("}")
		}
		return

	case held.cond != "":
		w.line("if %s {", held.cond)
		closing++
	}

	w.line("if err := %s; err != nil {", w.member(one.name))
	w.line("return err")
	w.line("}")
	w.writeValue(w.names.value+"."+one.path, &one.of, 0)

	for range closing {
		w.line("}")
	}
}

// buffered writes a member by encoding it first and leaving it out if what came
// back was empty.
//
// The last resort for omitempty, and the only correct one where what a member
// writes is decided elsewhere: by a codec its author wrote, or by the reflective
// encoder. Neither can be asked in advance what it will produce, and the
// standard library does not ask — it writes, looks, and retracts. This does the
// same, at the cost of one buffer for that member.
//
// Written through an encoder carrying the options of the one it will go into,
// rather than through a bare Marshal. Options are what a caller uses to say how
// their JSON should look — sort a map's keys, register a marshaler for a type,
// stringify numbers — and a member encoded without them is a hole in every one
// of those: a caller asking for deterministic output would get it everywhere
// except here, which is worse than not offering it, because the exception is
// invisible.
//
// The trailing newline goes because an encoder writes one after a top-level
// value and a member is not one.
//
// The four empty values are JSON's, not Go's, and the comparison is exact
// because what it compares against is what the standard library produced: a
// compact encoding with no room for a space between the braces.
func (w *writer) buffered(one member) {
	w.line("{")
	w.line("var into bytes.Buffer")
	w.line("if err := json.MarshalEncode(jsontext.NewEncoder(&into, %s.Options()), %s.%s); err != nil {",
		w.names.encoder, w.names.value, one.path)
	w.line("return err")
	w.line("}")
	w.line("buffered := bytes.TrimRight(into.Bytes(), \"\\n\")")

	w.line("switch string(buffered) {")
	w.line(`case "null", "\"\"", "[]", "{}":`)
	w.line("// Empty, so the member is not written at all.")
	w.line("default:")
	w.checked("%s", w.member(one.name))
	w.checked("%s.WriteValue(buffered)", w.names.encoder)
	w.line("}")
	w.line("}")
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
func (w *writer) omitted(one member, held string) when {
	switch {
	case one.omitZero:
		cond, can := w.nonZero(held, &one.of)
		return when{cond: cond, can: can}
	case one.omitEmpty:
		return w.nonEmpty(held, &one.of)
	default:
		return always
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
		// standard library can ask because it looks at the bytes afterwards;
		// this cannot, and answering "never empty" would write a member the
		// author asked to leave out.
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
		case !held.can:
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

// writeValue writes the statements that put one value on the wire.
//
// depth distinguishes the variables a nested composite binds, so that a slice
// of slices does not shadow its own loop variable.
func (w *writer) writeValue(held string, of *form, depth int) {
	if w.writeScalar(held, of) {
		return
	}

	switch of.how {
	case writtenBytes, writtenFallback:
		// Both go through the reflective encoder, for different reasons that
		// produce the same line. A byte slice is a base64 string rather than an
		// array, which the token layer does not build; a fallback field is one
		// nothing here can see through.
		w.checked("json.MarshalEncode(%s, %s)", w.names.encoder, held)

	case writtenDelegate:
		w.delegate(held, of, depth)

	case writtenText:
		w.text(held, of, depth)

	case writtenStruct:
		w.checked("%s(%s, %s)", encoderFor(of.typ), w.names.encoder, held)

	case writtenPointer:
		w.writePointer(held, of, depth)

	case writtenSlice, writtenArray:
		w.writeArray(held, of, depth)

	case writtenMap:
		w.writeMap(held, of, depth)

	case writtenBool, writtenString, writtenInt, writtenUint, writtenFloat, writtenInvalid:
		// Nothing here for either. The scalars were answered above, and are
		// named so that a form added to the plan and forgotten here is a
		// compiler's complaint rather than a value nothing writes; a refused
		// form has its refusal recorded already, and the run stops before any
		// of this is emitted.
	}
}

// writeScalar writes the forms that are one jsontext token, and reports whether
// it wrote one.
//
// Split from the rest because they are the forms with nothing to decide: each
// is a constructor and a conversion, and holding them beside the composites
// made one function whose length said more about how many token kinds JSON has
// than about anything a reader needs to follow.
func (w *writer) writeScalar(held string, of *form) bool {
	switch of.how {
	case writtenBool:
		w.token(held, "Bool", "bool", of)
	case writtenString:
		w.token(held, "String", "string", of)
	case writtenInt:
		w.token(held, "Int", "int64", of)
	case writtenUint:
		w.token(held, "Uint", "uint64", of)

	case writtenFloat:
		// A float32 has a shortest form of its own, and it is not the shortest
		// form of the float64 it widens to: 1.1 as a float32 widens to
		// 1.100000023841858, which is the number but not what anybody else
		// writes for it. jsontext has a constructor per width for exactly this.
		if basic, ok := of.typ.Underlying().(*types.Basic); ok && basic.Kind() == types.Float32 {
			w.token(held, "Float32", "float32", of)
			return true
		}
		w.token(held, "Float", "float64", of)

	default:
		return false
	}

	return true
}

// delegate calls the codec a type declares for itself.
//
// Through a local where the method is declared on the pointer, because Go will
// not call one on something it cannot take the address of. A field of the
// receiver is addressable and would not need this; a map's element is not, and
// a map's element is one of the things this is handed — so the local is what
// makes the one case work without the caller having to know which case it is.
// It is also what a person writing this by hand would do.
func (w *writer) delegate(held string, of *form, depth int) {
	if valueMethod(of.typ, marshalMethod) {
		w.checked("%s.%s(%s)", held, marshalMethod, w.names.encoder)
		return
	}

	one := loopVar("held", depth)
	w.line("{")
	w.line("%s := %s", one, held)
	w.checked("%s.%s(%s)", one, marshalMethod, w.names.encoder)
	w.line("}")
}

// text writes a value as the JSON string its own text codec produces.
//
// What the standard library does with such a type, and for the same reason: the
// text form is what its author said the value means, and the form underneath is
// an implementation detail a document has no way to explain. A closed set is
// the case that makes it matter — a member written as the number behind it says
// nothing to a reader and moves the day somebody inserts a member above it —
// and it is the codec's refusal of an undeclared value that comes with it.
//
// Always through a local, where [writer.delegate] asks first whether it needs
// one. Asking means reading the package for the method's receiver, and the
// method may be one this run has not written yet: on a clean checkout there is
// no receiver to read and on the next run there is, so the answer would differ
// between two runs of one unchanged declaration and the file would rewrite
// itself on alternate builds. A local is right for either receiver — a method
// on the pointer needs something addressable and a method on the value will
// take one — so the question is not worth the way it has to be asked.
func (w *writer) text(held string, of *form, depth int) {
	one := loopVar("held", depth)
	text := loopVar("text", depth)

	w.line("{")
	w.line("%s := %s", one, held)
	w.line("%s, err := %s.%s(%s)", text, one, of.text, appended(of.text))
	w.line("if err != nil {")
	w.line("return err")
	w.line("}")
	w.checked("%s.WriteToken(jsontext.String(string(%s)))", w.names.encoder, text)
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

// token writes a scalar as one jsontext token, converted to the type the token
// constructor takes.
//
// The conversion is written even where it is a no-op, because a named type over
// a string is not a string as far as a function signature is concerned, and
// leaving it out would produce output that compiles for some subjects and not
// for others.
func (w *writer) token(held, constructor, as string, of *form) {
	value := as + "(" + held + ")"
	if of.how == writtenBool {
		value = held
		if of.spelled.Text != "bool" {
			value = "bool(" + held + ")"
		}
	}

	w.checked("%s.WriteToken(jsontext.%s(%s))", w.names.encoder, constructor, value)
}

// writePointer writes null for a nil pointer and the value it points at for
// anything else.
func (w *writer) writePointer(held string, of *form, depth int) {
	w.line("if %s == nil {", held)
	w.checked("%s.WriteToken(jsontext.Null)", w.names.encoder)
	w.line("} else {")
	w.writeValue("(*"+held+")", of.elem, depth+1)
	w.line("}")
}

// writeArray writes a slice or an array as a JSON array.
func (w *writer) writeArray(held string, of *form, depth int) {
	one := loopVar("one", depth)

	w.checked("%s.WriteToken(jsontext.BeginArray)", w.names.encoder)
	w.line("for _, %s := range %s {", one, held)
	w.writeValue(one, of.elem, depth+1)
	w.line("}")
	w.checked("%s.WriteToken(jsontext.EndArray)", w.names.encoder)
}

// writeMap writes a map as a JSON object, its members in the order its keys
// sort.
//
// Sorted rather than in range order, which is where this codec differs from what
// the standard library does by default — it sorts only when asked, through its
// Deterministic option. Generated output is committed and reviewed, and a member
// order that changed between runs would produce a diff nobody could account for;
// a caller who needs the other behaviour has the reflective encoder.
func (w *writer) writeMap(held string, of *form, depth int) {
	key := loopVar("key", depth)

	w.checked("%s.WriteToken(jsontext.BeginObject)", w.names.encoder)
	w.line("for _, %s := range slices.Sorted(maps.Keys(%s)) {", key, held)
	w.writeValue(key, of.key, depth+1)
	w.writeValue(held+"["+key+"]", of.elem, depth+1)
	w.line("}")
	w.checked("%s.WriteToken(jsontext.EndObject)", w.names.encoder)
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
