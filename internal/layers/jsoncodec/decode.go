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
	v := w.n("v")

	if of.attach {
		w.line("// %s reads one JSON value into the %s.", unmarshalMethod, spelled)
		w.line("//")
		w.line("// A member the document does not mention keeps the value the destination")
		w.line("// held, and on any error the destination holds what it held before the")
		w.line("// call. Everything read out of data is copied, so data is the caller's")
		w.line("// again the moment this returns.")
		w.line("func (%s *%s) %s(%s []byte) error {", v, spelled, unmarshalMethod, w.n("data"))
		w.readEntry(name, false)
		w.line("}")
		w.blank()

		w.line("// %s fills %s with strings that point into data rather than", borrowedMethod, v)
		w.line("// copies of it. It is the quickest way in and the sharpest: data must")
		w.line("// outlive %s and must not be modified, or %s changes underneath its", v, v)
		w.line("// holder. Where that cannot be promised, %s copies.", unmarshalMethod)
		w.line("func (%s *%s) %s(%s []byte) error {", v, spelled, borrowedMethod, w.n("data"))
		w.readEntry(name, true)
		w.line("}")
		w.blank()
	}

	w.line("// %s reads one %s from %s at %s, and returns where the next value", name, spelled, w.n("b"), w.n("i"))
	w.line("// begins.")
	w.line("//")
	w.line("// The scanning half of the codec, which the entry points wrap: it holds")
	w.line("// the document to the grammar the standard library holds it to — syntax,")
	w.line("// UTF-8, no duplicate member names — and stops at the first thing wrong")
	w.line("// with it. %s is how many values this one is already inside, and", w.n("depth"))
	w.line("// %s decides whether strings point into %s or copy out of it.", w.n("borrow"), w.n("b"))
	w.line("func %s(%s []byte, %s, %s int, %s *%s, %s bool) (int, error) {",
		name, w.n("b"), w.n("i"), w.n("depth"), v, spelled, w.n("borrow"))
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
	held, next, err := w.n("held"), w.n("next"), w.n("err")

	w.line("%s := *%s", held, w.n("v"))
	w.line("%s, %s := %s(%s, 0, 0, &%s, %v)", next, err, name, w.n("data"), held, borrow)
	w.line("if %s != nil {", err)
	w.line("return %s", err)
	w.line("}")
	w.line("if %s := jsonAtEnd(%s, %s); %s != nil {", err, w.n("data"), next, err)
	w.line("return %s", err)
	w.line("}")
	w.line("*%s = %s", w.n("v"), held)
	w.line("return nil")
}

// readBody writes what one scanning function does.
func (w *writer) readBody(of *form) {
	b, i, err := w.n("b"), w.n("i"), w.n("err")
	names, first := w.n("names"), w.n("first")
	lo, hi, at, esc := w.n("lo"), w.n("hi"), w.n("at"), w.n("esc")
	next, done := w.n("next"), w.n("done")

	w.readPrologue(of.spelled.Text)

	w.line("var %s jsonNames", names)
	if len(of.members) > 0 {
		w.line("var %s []byte", w.n("scratch"))
	}
	w.line("for %s := true; ; %s = false {", first, first)
	w.line("%s, %s, %s := jsonMemberNext(%s, %s, %s)", next, done, err, b, i, first)
	w.line("if %s != nil {", err)
	w.line("return 0, %s", err)
	w.line("}")
	w.line("if %s {", done)
	w.line("return %s, nil", next)
	w.line("}")
	w.line("%s, %s, %s, %s, %s := jsonMemberName(%s, %s)", lo, hi, at, esc, err, b, next)
	w.line("if %s != nil {", err)
	w.line("return 0, %s", err)
	w.line("}")
	w.line("%s = %s", i, at)

	if len(of.members) == 0 {
		w.readUnknown()
		w.line("}")
		return
	}

	w.line("switch string(jsonName(%s, %s, %s, %s, &%s)) {", b, lo, hi, esc, w.n("scratch"))
	for index, one := range of.members {
		w.readMember(one, index)
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
	b, i, depth := w.n("b"), w.n("i"), w.n("depth")

	w.line("%s = jsonSkipSpace(%s, %s)", i, b, i)
	w.line("if %s, %s := jsonScanNull(%s, %s); %s {", w.n("next"), w.n("ok"), b, i, w.n("ok"))
	w.line("*%s = %s{}", w.n("v"), spelled)
	w.line("return %s, nil", w.n("next"))
	w.line("}")

	w.line("if %s >= len(%s) || %s[%s] != '{' {", i, b, b, i)
	w.line("return 0, jsonCannotRead(%s, %s, %s)", strconv.Quote(spelled), b, i)
	w.line("}")
	w.line("%s++", depth)
	w.line("if %s > jsonMaxDepth {", depth)
	w.line("return 0, errJSONDeep")
	w.line("}")
	w.line("%s++", i)
}

// readUnknown writes what happens to a member the type does not declare: its
// name is held against the ones already seen, and its value is stepped over at
// full grammar.
func (w *writer) readUnknown() {
	b, i, err := w.n("b"), w.n("i"), w.n("err")

	w.line("if %s.unknown(%s, %s, %s, %s) {", w.n("names"), b, w.n("lo"), w.n("hi"), w.n("esc"))
	w.line("return 0, errJSONDuplicate")
	w.line("}")
	w.line("if %s, %s = jsonSkipValue(%s, %s, %s); %s != nil {", i, err, b, i, w.n("depth"), err)
	w.line("return 0, %s", err)
	w.line("}")
}

// readMember writes the case that reads one member of an object.
func (w *writer) readMember(one member, index int) {
	v := w.n("v")

	w.line("case %s:", strconv.Quote(one.name))
	w.line("if %s.declare(%d) {", w.n("names"), index)
	w.line("return 0, errJSONDuplicate")
	w.line("}")

	// An embedded pointer is allocated on the way in. A member arriving for a
	// struct that is not there is what asks for it to be there, and the
	// allocation is per guard so that a member two pointers deep works.
	for _, held := range one.guards {
		w.line("if %s.%s == nil {", v, held.path)
		w.line("%s.%s = new(%s)", v, held.path, held.elem)
		w.line("}")
	}

	w.readValue(v+"."+one.path, &one.of, 0, 0)
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
		i, err := w.n("i"), w.n("err")
		w.line("if %s, %s = %s(%s, %s, %s%s, &%s, %s); %s != nil {",
			i, err, decoderFor(of.typ), w.n("b"), i, w.n("depth"), plus(nested), held, w.n("borrow"), err)
		w.line("return 0, %s", err)
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

// opens returns the condition that the byte at the scan position does not open
// a value of this kind, which is what turns a wrong-kind document into an
// error naming both sides.
func (w *writer) opens(kind scalarKind) string {
	held := w.n("b") + "[" + w.n("i") + "]"

	switch kind {
	case scalarBool:
		return held + " != 't' && " + held + " != 'f'"
	case scalarString:
		return held + ` != '"'`
	default:
		return "(" + held + " < '0' || " + held + " > '9') && " + held + " != '-'"
	}
}

// readScalar reads one scalar into a target.
//
// A null is not a disagreement. It sets the zero value and reads on, which is
// what the standard library does with a null into anything that cannot itself
// be null. Anything of the wrong kind is refused by name, and a number is held
// to the width of what it is going into rather than truncated to it.
func (w *writer) readScalar(held string, of *form, depth int, kind scalarKind) {
	b, i, err := w.n("b"), w.n("i"), w.n("err")
	one := w.at("held", depth)
	next := w.at("next", depth+1)
	spelled := of.spelled.Text

	w.line("if %s, %s := jsonScanNull(%s, %s); %s {", next, w.n("ok"), b, i, w.n("ok"))
	w.line("%s = %s", held, zeroLiteral(kind))
	w.line("%s = %s", i, next)
	w.line("} else if %s >= len(%s) || %s {", i, b, w.opens(kind))
	w.line("return 0, jsonCannotRead(%s, %s, %s)", strconv.Quote(spelled), b, i)
	w.line("} else {")

	switch kind {
	case scalarBool:
		w.line("%s, %s, %s := jsonScanBool(%s, %s)", one, next, err, b, i)

	case scalarString:
		w.line("%s, %s, %s, %s, %s := jsonScanString(%s, %s)",
			w.n("lo"), w.n("hi"), next, w.n("esc"), err, b, i)

	case scalarInt:
		w.line("%s, %s, %s := jsonScanInt(%s, %s, %s)", one, next, err, b, i, bitsOf(of))

	case scalarUint:
		w.line("%s, %s, %s := jsonScanUint(%s, %s, %s)", one, next, err, b, i, bitsOf(of))

	case scalarFloat:
		w.line("%s, %s, %s := jsonScanFloat(%s, %s, %s)", one, next, err, b, i, bitsOf(of))
	}

	w.line("if %s != nil {", err)
	w.line("return 0, %s", err)
	w.line("}")

	if kind == scalarString {
		w.line("%s = %s(jsonString(%s, %s, %s, %s, %s))",
			held, spelled, b, w.n("lo"), w.n("hi"), w.n("esc"), w.n("borrow"))
	} else {
		w.line("%s = %s(%s)", held, spelled, one)
	}
	w.line("%s = %s", i, next)
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
	b, i, err := w.n("b"), w.n("i"), w.n("err")
	lo, hi, esc := w.n("lo"), w.n("hi"), w.n("esc")
	next := w.at("next", depth+1)
	one := w.at("held", depth)
	spelled := of.spelled.Text
	_, fixed := of.typ.Underlying().(*types.Array)

	w.line("if %s, %s := jsonScanNull(%s, %s); %s {", next, w.n("ok"), b, i, w.n("ok"))
	if fixed {
		w.line("%s = %s{}", held, spelled)
	} else {
		w.line("%s = nil", held)
	}
	w.line("%s = %s", i, next)
	w.line(`} else if %s >= len(%s) || %s[%s] != '"' {`, i, b, b, i)
	w.line("return 0, jsonCannotRead(%s, %s, %s)", strconv.Quote(spelled), b, i)
	w.line("} else {")
	w.line("%s, %s, %s, %s, %s := jsonScanString(%s, %s)", lo, hi, next, esc, err, b, i)
	w.line("if %s != nil {", err)
	w.line("return 0, %s", err)
	w.line("}")

	if fixed {
		w.line("%s, %s := jsonScanBytes(%s, %s, %s, %s, nil)", one, err, b, lo, hi, esc)
		w.line("if %s != nil {", err)
		w.line("return 0, %s", err)
		w.line("}")
		w.line("if len(%s) != len(%s) {", one, held)
		w.line("return 0, errors.New(%s)", strconv.Quote("base64 of the wrong length for "+spelled))
		w.line("}")
		w.line("copy(%s[:], %s)", held, one)
	} else {
		w.line("%s, %s := jsonScanBytes(%s, %s, %s, %s, %s[:0])", one, err, b, lo, hi, esc, held)
		w.line("if %s != nil {", err)
		w.line("return 0, %s", err)
		w.line("}")
		w.line("if %s == nil {", one)
		w.line("%s = %s{}", one, spelled)
		w.line("}")
		w.line("%s = %s", held, one)
	}
	w.line("%s = %s", i, next)
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
	b, i, err := w.n("b"), w.n("i"), w.n("err")
	start := w.at("start", depth)
	next := w.at("next", depth)

	w.line("%s := %s", start, i)
	w.line("%s, %s := jsonSkipValue(%s, %s, %s%s)", next, err, b, i, w.n("depth"), plus(nested))
	w.line("if %s != nil {", err)
	w.line("return 0, %s", err)
	w.line("}")

	switch {
	case of.how == writtenFallback || of.reads != unmarshalMethod:
		w.line("if %s := json.Unmarshal(%s[%s:%s], &%s); %s != nil {", err, b, start, next, held, err)
		w.line("return 0, %s", err)
		w.line("}")

	case of.borrows:
		read := w.at("read", depth)
		w.line("%s := %s.%s", read, held, unmarshalMethod)
		w.line("if %s {", w.n("borrow"))
		w.line("%s = %s.%s", read, held, borrowedMethod)
		w.line("}")
		w.line("if %s := %s(%s[%s:%s]); %s != nil {", err, read, b, start, next, err)
		w.line("return 0, %s", err)
		w.line("}")

	default:
		w.line("if %s := %s.%s(%s[%s:%s]); %s != nil {", err, held, unmarshalMethod, b, start, next, err)
		w.line("return 0, %s", err)
		w.line("}")
	}

	w.line("%s = %s", i, next)
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
	b, i, err := w.n("b"), w.n("i"), w.n("err")
	lo, hi, esc := w.n("lo"), w.n("hi"), w.n("esc")
	next := w.at("next", depth+1)
	zero := w.at("zero", depth)
	spelled := of.spelled.Text

	w.line("if %s, %s := jsonScanNull(%s, %s); %s {", next, w.n("ok"), b, i, w.n("ok"))
	w.line("var %s %s", zero, spelled)
	w.line("%s = %s", held, zero)
	w.line("%s = %s", i, next)
	w.line(`} else if %s >= len(%s) || %s[%s] != '"' {`, i, b, b, i)
	w.line("return 0, jsonCannotRead(%s, %s, %s)", strconv.Quote(spelled), b, i)
	w.line("} else {")
	w.line("%s, %s, %s, %s, %s := jsonScanString(%s, %s)", lo, hi, next, esc, err, b, i)
	w.line("if %s != nil {", err)
	w.line("return 0, %s", err)
	w.line("}")
	w.line("if %s := %s.%s(jsonName(%s, %s, %s, %s, &%s)); %s != nil {",
		err, held, textUnmarshalMethod, b, lo, hi, esc, w.n("scratch"), err)
	w.line("return 0, %s", err)
	w.line("}")
	w.line("%s = %s", i, next)
	w.line("}")
}

// readPointer allocates for a value and leaves nil for a null.
func (w *writer) readPointer(held string, of *form, depth, nested int) {
	one := w.at("held", depth)
	next := w.at("next", depth+1)

	w.line("if %s, %s := jsonScanNull(%s, %s); %s {", next, w.n("ok"), w.n("b"), w.n("i"), w.n("ok"))
	w.line("%s = nil", held)
	w.line("%s = %s", w.n("i"), next)
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
	b, i, err := w.n("b"), w.n("i"), w.n("err")
	out := w.at("out", depth)
	one := w.at("one", depth)
	next := w.at("next", depth+1)
	first := w.at("first", depth+1)
	done := w.at("done", depth+1)

	w.line("if %s, %s := jsonScanNull(%s, %s); %s {", next, w.n("ok"), b, i, w.n("ok"))
	w.line("%s = nil", held)
	w.line("%s = %s", i, next)
	w.line("} else if %s >= len(%s) || %s[%s] != '[' {", i, b, b, i)
	w.line("return 0, jsonCannotRead(%s, %s, %s)", strconv.Quote(of.spelled.Text), b, i)
	w.line("} else {")
	w.openComposite(nested)
	w.line("%s := %s[:0]", out, held)
	w.line("for %s := true; ; %s = false {", first, first)
	w.line("%s, %s, %s := jsonElementNext(%s, %s, %s)", next, done, err, b, i, first)
	w.line("if %s != nil {", err)
	w.line("return 0, %s", err)
	w.line("}")
	w.line("if %s {", done)
	w.line("%s = %s", i, next)
	w.line("break")
	w.line("}")
	w.line("%s = %s", i, next)
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
	b, i, err := w.n("b"), w.n("i"), w.n("err")
	index := w.at("at", depth+1)
	one := w.at("one", depth)
	next := w.at("next", depth+1)
	first := w.at("first", depth+1)
	done := w.at("done", depth+1)
	spelled := of.spelled.Text

	w.line("if %s, %s := jsonScanNull(%s, %s); %s {", next, w.n("ok"), b, i, w.n("ok"))
	w.line("%s = %s{}", held, spelled)
	w.line("%s = %s", i, next)
	w.line("} else if %s >= len(%s) || %s[%s] != '[' {", i, b, b, i)
	w.line("return 0, jsonCannotRead(%s, %s, %s)", strconv.Quote(spelled), b, i)
	w.line("} else {")
	w.openComposite(nested)
	w.line("%s := 0", index)
	w.line("for %s := true; ; %s = false {", first, first)
	w.line("%s, %s, %s := jsonElementNext(%s, %s, %s)", next, done, err, b, i, first)
	w.line("if %s != nil {", err)
	w.line("return 0, %s", err)
	w.line("}")
	w.line("if %s {", done)
	w.line("%s = %s", i, next)
	w.line("break")
	w.line("}")
	w.line("if %s >= len(%s) {", index, held)
	w.line("return 0, errors.New(%s)", strconv.Quote("too many array elements for "+spelled))
	w.line("}")
	w.line("%s = %s", i, next)
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
	b, i := w.n("b"), w.n("i")
	built := w.at("into", depth)
	names := w.at("names", depth+1)
	next := w.at("next", depth+1)

	w.line("if %s, %s := jsonScanNull(%s, %s); %s {", next, w.n("ok"), b, i, w.n("ok"))
	w.line("%s = nil", held)
	w.line("%s = %s", i, next)
	w.line("} else if %s >= len(%s) || %s[%s] != '{' {", i, b, b, i)
	w.line("return 0, jsonCannotRead(%s, %s, %s)", strconv.Quote(of.spelled.Text), b, i)
	w.line("} else {")
	w.openComposite(nested)
	w.line("%s := make(%s)", built, of.spelled.Text)
	w.line("var %s jsonNames", names)
	w.readMapMembers(of, depth, nested, built)
	w.line("%s = %s", held, built)
	w.line("}")
}

// readMapMembers writes the loop that fills a map, one member at a time.
func (w *writer) readMapMembers(of *form, depth, nested int, built string) {
	b, i, err := w.n("b"), w.n("i"), w.n("err")
	names := w.at("names", depth+1)
	key := w.at("key", depth)
	value := w.at("value", depth)
	next := w.at("next", depth+1)
	first := w.at("first", depth+1)
	done := w.at("done", depth+1)
	lo, hi := w.at("lo", depth+1), w.at("hi", depth+1)
	at, esc := w.at("at", depth+1), w.at("esc", depth+1)

	w.line("for %s := true; ; %s = false {", first, first)
	w.line("%s, %s, %s := jsonMemberNext(%s, %s, %s)", next, done, err, b, i, first)
	w.line("if %s != nil {", err)
	w.line("return 0, %s", err)
	w.line("}")
	w.line("if %s {", done)
	w.line("%s = %s", i, next)
	w.line("break")
	w.line("}")
	w.line("%s, %s, %s, %s, %s := jsonMemberName(%s, %s)", lo, hi, at, esc, err, b, next)
	w.line("if %s != nil {", err)
	w.line("return 0, %s", err)
	w.line("}")
	w.line("if %s.unknown(%s, %s, %s, %s) {", names, b, lo, hi, esc)
	w.line("return 0, errJSONDuplicate")
	w.line("}")
	w.line("%s = %s", i, at)
	w.line("%s := %s(jsonString(%s, %s, %s, %s, %s))", key, of.key.spelled.Text, b, lo, hi, esc, w.n("borrow"))
	w.line("var %s %s", value, of.elem.spelled.Text)
	w.readValue(value, of.elem, depth+1, nested+1)
	w.line("%s[%s] = %s", built, key, value)
	w.line("}")
}

// openComposite steps over the byte that opens a composite and holds the
// document to the nesting bound, which one more level has just been added to.
func (w *writer) openComposite(nested int) {
	w.line("if %s%s > jsonMaxDepth {", w.n("depth"), plus(nested+1))
	w.line("return 0, errJSONDeep")
	w.line("}")
	w.line("%s++", w.n("i"))
}
