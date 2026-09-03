package jsoncodec

import (
	"go/types"
	"strconv"
)

// decoder writes the functions that read one type back off the wire.
//
// The scanning body is always a package-level function, because it takes the
// document, an offset, a depth and the borrow flag — none of which belongs in
// a type's method set. Where the type can carry methods, the two the standard
// library dispatches to and the borrowing variant beside them wrap that
// function; a caller reads one value through whichever door fits.
func (w *writer) decoder(of *form) {
	name := decoderFor(of.typ)
	spelled := of.spelled.Text

	if of.attach {
		w.line("// %s reads one JSON value into the %s.", unmarshalMethod, spelled)
		w.line("//")
		w.line("// A member the document does not mention keeps the value the destination")
		w.line("// held, and on any error the destination holds what it held before the")
		w.line("// call. Everything read out of data is copied, so data is the caller's")
		w.line("// again the moment this returns.")
		w.line("func (%s *%s) %s(data []byte) error {", valueVar, spelled, unmarshalMethod)
		w.readEntry(name, false)
		w.line("}")
		w.blank()

		w.line("// %s fills %s with strings that point into data rather than", borrowedMethod, valueVar)
		w.line("// copies of it. It is the quickest way in and the sharpest: data must")
		w.line("// outlive %s and must not be modified, or %s changes underneath its", valueVar, valueVar)
		w.line("// holder. Where that cannot be promised, %s copies.", unmarshalMethod)
		w.line("func (%s *%s) %s(data []byte) error {", valueVar, spelled, borrowedMethod)
		w.readEntry(name, true)
		w.line("}")
		w.blank()
	}

	w.line("// %s reads one %s from b at i, and returns where the next value", name, spelled)
	w.line("// begins.")
	w.line("//")
	w.line("// The scanning half of the codec, which the entry points wrap: it holds")
	w.line("// the document to the grammar the standard library holds it to — syntax,")
	w.line("// UTF-8, no duplicate member names — and stops at the first thing wrong")
	w.line("// with it. depth is how many values this one is already inside, and")
	w.line("// borrow decides whether strings point into b or copy out of it.")
	w.line("func %s(b []byte, i, depth int, %s *%s, borrow bool) (int, error) {", name, valueVar, spelled)
	w.readBody(of)
	w.line("}")
	w.blank()
}

// readEntry writes the body of an entry-point method: seed from the
// destination, scan, hold the document to being one value, and assign on
// success.
//
// The local seed is what preserves merge semantics — a member the document
// does not mention keeps the value the destination held — and it is also what
// makes failure atomic: a document refused halfway leaves no value assembled
// from two documents behind it.
func (w *writer) readEntry(name string, borrow bool) {
	w.line("held := *%s", valueVar)
	w.line("next, err := %s(data, 0, 0, &held, %v)", name, borrow)
	w.line("if err != nil {")
	w.line("return err")
	w.line("}")
	w.line("if err := jsonAtEnd(data, next); err != nil {")
	w.line("return err")
	w.line("}")
	w.line("*%s = held", valueVar)
	w.line("return nil")
}

// readBody writes what one scanning function does.
func (w *writer) readBody(of *form) {
	w.readPrologue(of.spelled.Text)

	w.line("var names jsonNames")
	if len(of.members) > 0 {
		w.line("var scratch []byte")
	}
	w.line("for first := true; ; first = false {")
	w.line("next, done, err := jsonMemberNext(b, i, first)")
	w.line("if err != nil {")
	w.line("return 0, err")
	w.line("}")
	w.line("if done {")
	w.line("return next, nil")
	w.line("}")
	w.line("lo, hi, at, esc, err := jsonMemberName(b, next)")
	w.line("if err != nil {")
	w.line("return 0, err")
	w.line("}")
	w.line("i = at")

	if len(of.members) == 0 {
		w.readUnknown()
		w.line("}")
		return
	}

	w.line("switch string(jsonName(b, lo, hi, esc, &scratch)) {")
	for at, one := range of.members {
		w.readMember(one, at)
	}
	w.line("default:")
	w.readUnknown()
	w.line("}")
	w.line("}")
}

// readPrologue writes what happens before an object's members: the null that
// means there is no object, the wrong kind refused by name, and the nesting
// bound.
//
// A JSON null is a value the reader may legally be handed, and what it means
// is that there is no object — so the value becomes its zero value rather than
// keeping what it held. It matters because a target is often read into twice:
// a decoder that left the old value in place would answer a null with whatever
// the previous document happened to say.
func (w *writer) readPrologue(spelled string) {
	w.line("i = jsonSkipSpace(b, i)")
	w.line("if next, ok := jsonScanNull(b, i); ok {")
	w.line("*%s = %s{}", valueVar, spelled)
	w.line("return next, nil")
	w.line("}")

	w.line("if i >= len(b) || b[i] != '{' {")
	w.line("return 0, jsonCannotRead(%s, b, i)", strconv.Quote(spelled))
	w.line("}")
	w.line("depth++")
	w.line("if depth > jsonMaxDepth {")
	w.line("return 0, errJSONDeep")
	w.line("}")
	w.line("i++")
}

// readUnknown writes what happens to a member the type does not declare: its
// name is held against the ones already seen, and its value is stepped over at
// full grammar.
func (w *writer) readUnknown() {
	w.line("if names.unknown(b, lo, hi, esc) {")
	w.line("return 0, errJSONDuplicate")
	w.line("}")
	w.line("if i, err = jsonSkipValue(b, i, depth); err != nil {")
	w.line("return 0, err")
	w.line("}")
}

// readMember writes the case that reads one member of an object.
func (w *writer) readMember(one member, at int) {
	w.line("case %s:", strconv.Quote(one.name))
	w.line("if names.declare(%d) {", at)
	w.line("return 0, errJSONDuplicate")
	w.line("}")

	// An embedded pointer is allocated on the way in. A member arriving for a
	// struct that is not there is what asks for it to be there, and the
	// allocation is per guard so that a member two pointers deep works.
	for _, held := range one.guards {
		w.line("if %s.%s == nil {", valueVar, held.path)
		w.line("%s.%s = new(%s)", valueVar, held.path, held.elem)
		w.line("}")
	}

	w.readValue(valueVar+"."+one.path, &one.of, 0, 0)
}

// readValue writes the statements that read one value into a target.
//
// held is an assignable expression. depth distinguishes the variables a nested
// composite binds, so that a slice of slices does not shadow its own; nested
// counts the objects and arrays this function's own emission has already
// opened, which is what keeps the depth bound honest inside one function.
func (w *writer) readValue(held string, of *form, depth, nested int) {
	switch of.how {
	case writtenBool:
		w.readScalar(held, of, depth, scalarBool)

	case writtenString:
		w.readScalar(held, of, depth, scalarString)

	case writtenInt:
		w.readScalar(held, of, depth, scalarInt)

	case writtenUint:
		w.readScalar(held, of, depth, scalarUint)

	case writtenFloat:
		w.readScalar(held, of, depth, scalarFloat)

	case writtenBytes:
		w.readBytes(held, of, depth)

	case writtenDelegate, writtenFallback:
		w.readSpan(held, of, depth, nested)

	case writtenText:
		w.readText(held, of, depth)

	case writtenStruct:
		w.line("if i, err = %s(b, i, depth%s, &%s, borrow); err != nil {", decoderFor(of.typ), plus(nested), held)
		w.line("return 0, err")
		w.line("}")

	case writtenPointer:
		w.readPointer(held, of, depth, nested)

	case writtenSlice:
		w.readSlice(held, of, depth, nested)

	case writtenArray:
		w.readArray(held, of, depth, nested)

	case writtenMap:
		w.readMap(held, of, depth, nested)

	case writtenInvalid:
		// Refused already; nothing is emitted for it.
	}
}

// plus writes a static depth offset as it appears after the depth variable.
func plus(nested int) string {
	if nested == 0 {
		return ""
	}
	return "+" + strconv.Itoa(nested)
}

// scalarKind names the five scalar reads, which share their shape and differ
// in the scan they call and the check on the byte that opens the value.
type scalarKind int

const (
	scalarBool scalarKind = iota
	scalarString
	scalarInt
	scalarUint
	scalarFloat
)

// opens returns the condition that the byte at i does not open a value of this
// kind, which is what turns a wrong-kind document into an error naming both
// sides.
func (k scalarKind) opens() string {
	switch k {
	case scalarBool:
		return "b[i] != 't' && b[i] != 'f'"
	case scalarString:
		return `b[i] != '"'`
	default:
		return "(b[i] < '0' || b[i] > '9') && b[i] != '-'"
	}
}

// readScalar reads one scalar into a target.
//
// A null is not a disagreement. It sets the zero value and reads on, which is
// what the standard library does with a null into anything that cannot itself
// be null. Anything of the wrong kind is refused by name, and a number is held
// to the width of what it is going into rather than truncated to it.
func (w *writer) readScalar(held string, of *form, depth int, kind scalarKind) {
	one := loopVar("held", depth)
	next := loopVar("next", depth+1)
	spelled := of.spelled.Text

	w.line("if %s, ok := jsonScanNull(b, i); ok {", next)
	w.line("%s = %s", held, zeroLiteral(kind))
	w.line("i = %s", next)
	w.line("} else if i >= len(b) || %s {", kind.opens())
	w.line("return 0, jsonCannotRead(%s, b, i)", strconv.Quote(spelled))
	w.line("} else {")

	switch kind {
	case scalarBool:
		w.line("%s, %s, err := jsonScanBool(b, i)", one, next)

	case scalarString:
		w.line("lo, hi, %s, esc, err := jsonScanString(b, i)", next)

	case scalarInt:
		w.line("%s, %s, err := jsonScanInt(b, i, %s)", one, next, bitsOf(of))

	case scalarUint:
		w.line("%s, %s, err := jsonScanUint(b, i, %s)", one, next, bitsOf(of))

	case scalarFloat:
		w.line("%s, %s, err := jsonScanFloat(b, i, %s)", one, next, bitsOf(of))
	}

	w.line("if err != nil {")
	w.line("return 0, err")
	w.line("}")

	if kind == scalarString {
		w.line("%s = %s(jsonString(b, lo, hi, esc, borrow))", held, spelled)
	} else {
		w.line("%s = %s(%s)", held, spelled, one)
	}
	w.line("i = %s", next)
	w.line("}")
}

// zeroLiteral is what a null assigns: the untyped literal every basic kind
// accepts.
func zeroLiteral(kind scalarKind) string {
	switch kind {
	case scalarBool:
		return "false"
	case scalarString:
		return `""`
	default:
		return "0"
	}
}

// bitsOf returns the width a number is held to, written as the constant the
// generated file names.
//
// int, uint and uintptr take the platform's word, which strconv spells; every
// other width is part of the type. What jsonScanInt checks against the width
// is the value, so a document carrying 300 for an int8 is refused rather than
// wrapped.
func bitsOf(of *form) string {
	basic, ok := of.typ.Underlying().(*types.Basic)
	if !ok {
		return "64"
	}

	switch basic.Kind() {
	case types.Int, types.Uint, types.Uintptr:
		return "strconv.IntSize"
	case types.Int8, types.Uint8:
		return "8"
	case types.Int16, types.Uint16:
		return "16"
	case types.Int32, types.Uint32, types.Float32:
		return "32"
	default:
		return "64"
	}
}

// readBytes reads a base64 string into a byte slice or a byte array.
//
// A slice reuses the capacity the target already holds and comes back empty
// rather than nil for an empty encoding, which is the standard library's
// distinction between "" and null. An array takes exactly its own length and
// refuses anything else, because the length is part of the type.
func (w *writer) readBytes(held string, of *form, depth int) {
	next := loopVar("next", depth+1)
	one := loopVar("held", depth)
	spelled := of.spelled.Text
	_, fixed := of.typ.Underlying().(*types.Array)

	w.line("if %s, ok := jsonScanNull(b, i); ok {", next)
	if fixed {
		w.line("%s = %s{}", held, spelled)
	} else {
		w.line("%s = nil", held)
	}
	w.line("i = %s", next)
	w.line(`} else if i >= len(b) || b[i] != '"' {`)
	w.line("return 0, jsonCannotRead(%s, b, i)", strconv.Quote(spelled))
	w.line("} else {")
	w.line("lo, hi, %s, esc, err := jsonScanString(b, i)", next)
	w.line("if err != nil {")
	w.line("return 0, err")
	w.line("}")

	if fixed {
		w.line("%s, err := jsonScanBytes(b, lo, hi, esc, nil)", one)
		w.line("if err != nil {")
		w.line("return 0, err")
		w.line("}")
		w.line("if len(%s) != len(%s) {", one, held)
		w.line("return 0, errors.New(%s)", strconv.Quote("base64 of the wrong length for "+spelled))
		w.line("}")
		w.line("copy(%s[:], %s)", held, one)
	} else {
		w.line("%s, err := jsonScanBytes(b, lo, hi, esc, %s[:0])", one, held)
		w.line("if err != nil {")
		w.line("return 0, err")
		w.line("}")
		w.line("if %s == nil {", one)
		w.line("%s = %s{}", one, spelled)
		w.line("}")
		w.line("%s = %s", held, one)
	}
	w.line("i = %s", next)
	w.line("}")
}

// readSpan reads a value whose reader is somebody else's: the span of the next
// value is found and validated here, and its bytes are handed over whole.
//
// A delegate declaring the borrowing reader is offered the borrow it was
// declared for; anything else gets the copying half, which is the contract's
// default. A type whose reader speaks the streaming interface is reached
// through the standard library, which knows how to call it.
func (w *writer) readSpan(held string, of *form, depth, nested int) {
	start := loopVar("start", depth)
	next := loopVar("next", depth)

	w.line("%s := i", start)
	w.line("%s, err := jsonSkipValue(b, i, depth%s)", next, plus(nested))
	w.line("if err != nil {")
	w.line("return 0, err")
	w.line("}")

	switch {
	case of.how == writtenFallback || of.reads != unmarshalMethod:
		w.line("if err := json.Unmarshal(b[%s:%s], &%s); err != nil {", start, next, held)
		w.line("return 0, err")
		w.line("}")

	case of.borrows:
		w.line("if borrow {")
		w.line("if err := %s.%s(b[%s:%s]); err != nil {", held, borrowedMethod, start, next)
		w.line("return 0, err")
		w.line("}")
		w.line("} else if err := %s.%s(b[%s:%s]); err != nil {", held, unmarshalMethod, start, next)
		w.line("return 0, err")
		w.line("}")

	default:
		w.line("if err := %s.%s(b[%s:%s]); err != nil {", held, unmarshalMethod, start, next)
		w.line("return 0, err")
		w.line("}")
	}

	w.line("i = %s", next)
}

// readText reads a JSON string back through the type's own text codec.
//
// Null is the zero value, which is what every other form here does with it and
// what the standard library does with a text codec: a document saying a member
// is absent is not a document the reader should be asked to parse the absence
// of. Anything that is not a string is refused by name, so that a document
// carrying the number the value used to be written as says so rather than
// arriving as whatever the reader made of it.
//
// The value is addressable wherever this is reached. A member of the struct
// being read is a field of a local, and a slice's or map's element is read into
// a local of its own before being put in place — so the reader half, which is
// declared on the pointer, has something to take the address of.
func (w *writer) readText(held string, of *form, depth int) {
	next := loopVar("next", depth+1)
	zero := loopVar("zero", depth)
	spelled := of.spelled.Text

	w.line("if %s, ok := jsonScanNull(b, i); ok {", next)
	w.line("var %s %s", zero, spelled)
	w.line("%s = %s", held, zero)
	w.line("i = %s", next)
	w.line(`} else if i >= len(b) || b[i] != '"' {`)
	w.line("return 0, jsonCannotRead(%s, b, i)", strconv.Quote(spelled))
	w.line("} else {")
	w.line("lo, hi, %s, esc, err := jsonScanString(b, i)", next)
	w.line("if err != nil {")
	w.line("return 0, err")
	w.line("}")
	w.line("if err := %s.%s(jsonName(b, lo, hi, esc, &scratch)); err != nil {", held, textUnmarshalMethod)
	w.line("return 0, err")
	w.line("}")
	w.line("i = %s", next)
	w.line("}")
}

// readPointer allocates for a value and leaves nil for a null.
func (w *writer) readPointer(held string, of *form, depth, nested int) {
	one := loopVar("held", depth)
	next := loopVar("next", depth+1)

	w.line("if %s, ok := jsonScanNull(b, i); ok {", next)
	w.line("%s = nil", held)
	w.line("i = %s", next)
	w.line("} else {")
	w.line("var %s %s", one, of.elem.spelled.Text)
	w.readValue(one, of.elem, depth+1, nested)
	w.line("%s = &%s", held, one)
	w.line("}")
}

// readSlice reads a JSON array into a slice, reusing what the target already
// holds.
//
// Reused rather than allocated fresh: reading into a value that has been read
// into before is the ordinary case in a loop over a stream, and a slice whose
// capacity survives is the difference between one allocation and one per value.
//
// An empty array and a null are two different things and stay two. A null makes
// the slice nil; an empty array makes it an empty slice that is not nil, which
// takes an allocation the reuse would otherwise have saved — because reslicing
// a nil slice to nothing leaves it nil, and writing that back out again would
// turn every [] a reader saw into a null.
func (w *writer) readSlice(held string, of *form, depth, nested int) {
	out := loopVar("out", depth)
	one := loopVar("one", depth)
	next := loopVar("next", depth+1)
	first := loopVar("first", depth)

	w.line("if %s, ok := jsonScanNull(b, i); ok {", next)
	w.line("%s = nil", held)
	w.line("i = %s", next)
	w.line("} else if i >= len(b) || b[i] != '[' {")
	w.line("return 0, jsonCannotRead(%s, b, i)", strconv.Quote(of.spelled.Text))
	w.line("} else {")
	w.openArray(nested)
	w.line("%s := %s[:0]", out, held)
	w.line("for %s := true; ; %s = false {", first, first)
	w.line("%s, done, err := jsonElementNext(b, i, %s)", next, first)
	w.line("if err != nil {")
	w.line("return 0, err")
	w.line("}")
	w.line("if done {")
	w.line("i = %s", next)
	w.line("break")
	w.line("}")
	w.line("i = %s", next)
	w.line("var %s %s", one, of.elem.spelled.Text)
	w.readValue(one, of.elem, depth+1, nested+1)
	w.line("%s = append(%s, %s)", out, out, one)
	w.line("}")
	w.line("if %s == nil {", out)
	w.line("%s = %s{}", out, of.spelled.Text)
	w.line("}")
	w.line("%s = %s", held, out)
	w.line("}")
}

// readArray reads a JSON array into a fixed-length one.
//
// The length is part of the type, so an array of the wrong length is a document
// that does not fit the type it is being read into — and both directions are
// refused, because the standard library refuses both. A reader that quietly
// took the first three of four would drop data without saying so, and one that
// took one of three would leave two elements holding whatever was there before.
//
// A null is not a short array. It zeroes the whole of it, which is what a null
// does everywhere else.
func (w *writer) readArray(held string, of *form, depth, nested int) {
	index := loopVar("at", depth)
	one := loopVar("one", depth)
	next := loopVar("next", depth+1)
	first := loopVar("first", depth)
	spelled := of.spelled.Text

	w.line("if %s, ok := jsonScanNull(b, i); ok {", next)
	w.line("%s = %s{}", held, spelled)
	w.line("i = %s", next)
	w.line("} else if i >= len(b) || b[i] != '[' {")
	w.line("return 0, jsonCannotRead(%s, b, i)", strconv.Quote(spelled))
	w.line("} else {")
	w.openArray(nested)
	w.line("%s := 0", index)
	w.line("for %s := true; ; %s = false {", first, first)
	w.line("%s, done, err := jsonElementNext(b, i, %s)", next, first)
	w.line("if err != nil {")
	w.line("return 0, err")
	w.line("}")
	w.line("if done {")
	w.line("i = %s", next)
	w.line("break")
	w.line("}")
	w.line("if %s >= len(%s) {", index, held)
	w.line("return 0, errors.New(%s)", strconv.Quote("too many array elements for "+spelled))
	w.line("}")
	w.line("i = %s", next)
	w.line("var %s %s", one, of.elem.spelled.Text)
	w.readValue(one, of.elem, depth+1, nested+1)
	w.line("%s[%s] = %s", held, index, one)
	w.line("%s++", index)
	w.line("}")
	w.line("if %s < len(%s) {", index, held)
	w.line("return 0, errors.New(%s)", strconv.Quote("too few array elements for "+spelled))
	w.line("}")
	w.line("}")
}

// readMap reads a JSON object into a map.
//
// Its member names are held to the same no-duplicates rule as a struct's,
// because the standard library holds them to it: a document writing one key
// twice is describing two values for one place.
func (w *writer) readMap(held string, of *form, depth, nested int) {
	built := loopVar("into", depth)
	names := loopVar("names", depth)
	key := loopVar("key", depth)
	value := loopVar("value", depth)
	next := loopVar("next", depth+1)
	first := loopVar("first", depth)

	w.line("if %s, ok := jsonScanNull(b, i); ok {", next)
	w.line("%s = nil", held)
	w.line("i = %s", next)
	w.line("} else if i >= len(b) || b[i] != '{' {")
	w.line("return 0, jsonCannotRead(%s, b, i)", strconv.Quote(of.spelled.Text))
	w.line("} else {")
	w.openArray(nested)
	w.line("%s := make(%s)", built, of.spelled.Text)
	w.line("var %s jsonNames", names)
	w.line("for %s := true; ; %s = false {", first, first)
	w.line("%s, done, err := jsonMemberNext(b, i, %s)", next, first)
	w.line("if err != nil {")
	w.line("return 0, err")
	w.line("}")
	w.line("if done {")
	w.line("i = %s", next)
	w.line("break")
	w.line("}")
	w.line("lo, hi, at, esc, err := jsonMemberName(b, %s)", next)
	w.line("if err != nil {")
	w.line("return 0, err")
	w.line("}")
	w.line("if %s.unknown(b, lo, hi, esc) {", names)
	w.line("return 0, errJSONDuplicate")
	w.line("}")
	w.line("i = at")
	w.line("%s := %s(jsonString(b, lo, hi, esc, borrow))", key, of.key.spelled.Text)
	w.line("var %s %s", value, of.elem.spelled.Text)
	w.readValue(value, of.elem, depth+1, nested+1)
	w.line("%s[%s] = %s", built, key, value)
	w.line("}")
	w.line("%s = %s", held, built)
	w.line("}")
}

// openArray steps over the byte that opens a composite and holds the document
// to the nesting bound, which one more level has just been added to.
func (w *writer) openArray(nested int) {
	w.line("if depth%s > jsonMaxDepth {", plus(nested+1))
	w.line("return 0, errJSONDeep")
	w.line("}")
	w.line("i++")
}
