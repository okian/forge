package jsoncodec

import (
	"go/types"
	"strconv"
)

// decoder writes the function that reads one type back off the wire.
//
// The same arrangement as the encoder: the type carries the method where it
// can, the function forwards to it, and generated code calls the function
// either way. A pointer receiver, because reading writes.
func (w *writer) decoder(of *form) {
	name := decoderFor(of.typ)
	spelled := of.spelled.Text

	w.line("// %s reads a %s from JSON.", name, spelled)
	if of.attach {
		w.line("//")
		w.line("// The value's own method holds the body; this is what generated code")
		w.line("// calls, so that a caller names one function whether or not the type")
		w.line("// is one a method could be declared on.")
		w.line("func %s(%s *jsontext.Decoder, %s *%s) error {", name, w.names.decoder, w.names.value, spelled)
		w.line("return %s.%s(%s)", w.names.value, unmarshalMethod, w.names.decoder)
		w.line("}")
		w.blank()

		w.line("// %s reads a JSON object into the %s.", unmarshalMethod, spelled)
		w.line("//")
		w.line("// A member the object holds and the type does not is skipped, which is")
		w.line("// what keeps a reader working against a writer that has since added one.")
		w.line("func (%s *%s) %s(%s *jsontext.Decoder) error {", w.names.value, spelled, unmarshalMethod, w.names.decoder)
		w.readBody(of)
		w.line("}")
		w.blank()
		return
	}

	w.line("func %s(%s *jsontext.Decoder, %s *%s) error {", name, w.names.decoder, w.names.value, spelled)
	w.readBody(of)
	w.line("}")
	w.blank()
}

// readBody writes what one decoder does.
func (w *writer) readBody(of *form) {
	if of.how != writtenStruct {
		w.readValue("(*"+w.names.value+")", of, 0)
		w.line("return nil")
		return
	}

	// A JSON null is a value the reader may legally be handed, and what it means
	// is that there is no object — so the value becomes its zero value rather
	// than keeping what it held. It matters because a target is often read into
	// twice: a decoder that left the old value in place would answer a null
	// with whatever the previous document happened to say.
	w.line("if %s.PeekKind() == 'n' {", w.names.decoder)
	w.line("if _, err := %s.ReadToken(); err != nil {", w.names.decoder)
	w.line("return err")
	w.line("}")
	w.line("var zero %s", of.spelled.Text)
	w.line("*%s = zero", w.names.value)
	w.line("return nil")
	w.line("}")

	// Checked rather than assumed. Reading a member name out of an array would
	// otherwise produce a value assembled from whatever the tokens happened to
	// be, and an error naming what arrived is what says where to look.
	//
	// A decoder that failed is the other thing a peek answers, and it answers
	// it with no kind at all — so a document that stops halfway would be
	// reported as an object of no kind, which says nothing about where it
	// stopped. Reading is what has the answer, and the read is only reached
	// when there is no object to read.
	w.line("if kind := %s.PeekKind(); kind != '{' {", w.names.decoder)
	w.line("if _, err := %s.ReadToken(); err != nil {", w.names.decoder)
	w.line("return err")
	w.line("}")
	w.line("return fmt.Errorf(%s, kind)", strconv.Quote("cannot read "+of.spelled.Text+" from a JSON %s"))
	w.line("}")
	w.line("if _, err := %s.ReadToken(); err != nil {", w.names.decoder)
	w.line("return err")
	w.line("}")
	w.line("for %s.PeekKind() != '}' {", w.names.decoder)
	w.line("name, err := %s.ReadToken()", w.names.decoder)
	w.line("if err != nil {")
	w.line("return err")
	w.line("}")
	w.line("switch name.String() {")

	for _, one := range of.members {
		w.readMember(one)
	}

	w.line("default:")
	w.line("if err := %s.SkipValue(); err != nil {", w.names.decoder)
	w.line("return err")
	w.line("}")
	w.line("}")
	w.line("}")
	w.line("_, err := %s.ReadToken()", w.names.decoder)
	w.line("return err")
}

// readMember writes the case that reads one member of an object.
func (w *writer) readMember(one member) {
	w.line("case %s:", strconv.Quote(one.name))

	// An embedded pointer is allocated on the way in. A member arriving for a
	// struct that is not there is what asks for it to be there, and the
	// allocation is per guard so that a member two pointers deep works.
	for _, held := range one.guards {
		w.line("if %s.%s == nil {", w.names.value, held.path)
		w.line("%s.%s = new(%s)", w.names.value, held.path, held.elem)
		w.line("}")
	}

	w.readValue(w.names.value+"."+one.path, &one.of, 0)
}

// readValue writes the statements that read one value into a target.
//
// held is an assignable expression. depth distinguishes the variables a nested
// composite binds, so that a slice of slices does not shadow its own.
func (w *writer) readValue(held string, of *form, depth int) {
	switch of.how {
	case writtenBool, writtenString, writtenInt, writtenUint, writtenFloat:
		w.readScalar(held, of, accessorFor(of), kindsFor(of), of.how != writtenBool && of.how != writtenString)

	case writtenBytes, writtenFallback:
		w.checked("json.UnmarshalDecode(%s, &%s)", w.names.decoder, held)

	case writtenDelegate:
		w.checked("%s.%s(%s)", held, unmarshalMethod, w.names.decoder)

	case writtenText:
		w.readText(held, of)

	case writtenStruct:
		w.checked("%s(%s, &%s)", decoderFor(of.typ), w.names.decoder, held)

	case writtenPointer:
		w.readPointer(held, of, depth)

	case writtenSlice:
		w.readSlice(held, of, depth)

	case writtenArray:
		w.readArray(held, of, depth)

	case writtenMap:
		w.readMap(held, of, depth)

	case writtenInvalid:
		// Refused already; nothing is emitted for it.
	}
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
func (w *writer) readText(held string, of *form) {
	w.line("{")
	w.line("raw, err := %s.ReadToken()", w.names.decoder)
	w.line("if err != nil {")
	w.line("return err")
	w.line("}")

	w.line("switch raw.Kind() {")
	w.line("case 'n':")
	w.line("var zero %s", of.spelled.Text)
	w.line("%s = zero", held)

	w.line("case '\"':")
	w.checked("%s.%s([]byte(raw.String()))", held, textUnmarshalMethod)

	w.line("default:")
	w.line("return fmt.Errorf(%s, raw.Kind())",
		strconv.Quote("cannot read "+of.spelled.Text+" from a JSON %s"))
	w.line("}")
	w.line("}")
}

// accessorFor names the jsontext accessor that reads a scalar of this form, and
// kindsFor the token kinds it accepts, written as a switch's case list.
//
// A float32 has an accessor of its own, because it has a shortest form of its
// own and because that accessor is what checks a float against the range of the
// width it is going into.
func accessorFor(of *form) string {
	switch of.how {
	case writtenBool:
		return "Bool"
	case writtenString:
		return "String"
	case writtenInt:
		return "Int"
	case writtenUint:
		return "Uint"
	case writtenFloat:
		if basic, ok := of.typ.Underlying().(*types.Basic); ok && basic.Kind() == types.Float32 {
			return "Float32"
		}
		return "Float"
	default:
		return ""
	}
}

func kindsFor(of *form) string {
	switch of.how {
	case writtenBool:
		return "'t', 'f'"
	case writtenString:
		return `'"'`
	default:
		return "'0'"
	}
}

// readScalar reads one token into a target, checking that the token is one the
// target could hold.
//
// The kind is checked rather than assumed. jsontext's accessors are documented
// as panicking when the token is not of the kind they read, so a decoder that
// called one straight off would crash on JSON it merely disagreed with — and
// what a codec is usually pointed at is JSON somebody else wrote. Reading a
// number into a string does not even panic: it returns the number's own text
// and stores it, which is worse, because nothing says anything at all.
//
// A null is not a disagreement. It sets the zero value and reads on, which is
// what the standard library does with a null into anything that cannot itself
// be null.
//
// kinds is the token kinds the target accepts, written as the case list of a
// switch. fallible says whether the accessor can fail for a token of the right
// kind, which the numeric ones can: a JSON number with a fraction is an error
// rather than a truncation. What jsontext checks is the range of the type it
// reads into — an int64, a uint64 or a float64 — so a narrower target is
// checked again by [writer.narrowed], without which the number would arrive
// intact and be wrapped by the conversion.
//
// The temporaries are named for what they are and are deliberately not named
// for anything a target can be. A target is a field selector or one of the
// variables a composite binds, and a temporary sharing a name with one of those
// shadows it — which the compiler accepts when the types agree, leaving a
// decoder that reads the value and assigns it to itself.
func (w *writer) readScalar(held string, of *form, accessor, kinds string, fallible bool) {
	w.line("{")
	w.line("raw, err := %s.ReadToken()", w.names.decoder)
	w.line("if err != nil {")
	w.line("return err")
	w.line("}")

	w.line("switch raw.Kind() {")
	w.line("case 'n':")
	w.line("var zero %s", of.spelled.Text)
	w.line("%s = zero", held)

	w.line("case %s:", kinds)
	if fallible {
		w.line("number, err := raw.%s()", accessor)
		w.line("if err != nil {")
		w.line("return err")
		w.line("}")
		w.narrowed(of)
		w.line("%s = %s(number)", held, of.spelled.Text)
	} else {
		w.line("%s = %s(raw.%s())", held, of.spelled.Text, accessor)
	}

	w.line("default:")
	w.line("return fmt.Errorf(%s, raw.Kind())",
		strconv.Quote("cannot read "+of.spelled.Text+" from a JSON %s"))
	w.line("}")
	w.line("}")
}

// narrowed refuses a number the target cannot hold.
//
// jsontext reads an integer as an int64 or a uint64 and checks it against that,
// so a value out of range for an int8 arrives intact and is wrapped by the
// conversion — a document silently becoming a different number. Every width
// narrower than what was read needs its own check, and int, uint and uintptr
// need one on every platform, because their width is the platform's.
//
// Written as a conversion and back rather than as a pair of bounds, so that the
// check is the same sentence for every width and names no constant that could be
// the wrong one.
//
// Floats are not here. A float32 is read by the accessor for its own width,
// which checks its own range — and narrowing a float loses precision by design,
// so a conversion and back would refuse every value that is merely inexact.
func (w *writer) narrowed(of *form) {
	basic, ok := of.typ.Underlying().(*types.Basic)
	if !ok {
		return
	}

	spelled := of.spelled.Text

	switch basic.Kind() {
	case types.Int, types.Int8, types.Int16, types.Int32:
		w.line("if int64(%s(number)) != number {", spelled)
		w.line("return fmt.Errorf(%s, number)", strconv.Quote("%d is out of range for "+spelled))
		w.line("}")

	case types.Uint, types.Uint8, types.Uint16, types.Uint32, types.Uintptr:
		w.line("if uint64(%s(number)) != number {", spelled)
		w.line("return fmt.Errorf(%s, number)", strconv.Quote("%d is out of range for "+spelled))
		w.line("}")

	default:
		// As wide as what was read, or not a number at all: nothing can be lost
		// on the way in, so there is nothing to check.
	}
}

// readPointer allocates for a value and leaves nil for a null.
func (w *writer) readPointer(held string, of *form, depth int) {
	one := loopVar("held", depth)

	w.line("if %s.PeekKind() == 'n' {", w.names.decoder)
	w.line("if _, err := %s.ReadToken(); err != nil {", w.names.decoder)
	w.line("return err")
	w.line("}")
	w.line("%s = nil", held)
	w.line("} else {")
	w.line("var %s %s", one, of.elem.spelled.Text)
	w.readValue(one, of.elem, depth+1)
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
func (w *writer) readSlice(held string, of *form, depth int) {
	out := loopVar("out", depth)
	one := loopVar("one", depth)

	w.line("if %s.PeekKind() == 'n' {", w.names.decoder)
	w.line("if _, err := %s.ReadToken(); err != nil {", w.names.decoder)
	w.line("return err")
	w.line("}")
	w.line("%s = nil", held)
	w.line("} else {")
	w.openArray()
	w.line("%s := %s[:0]", out, held)
	w.line("for %s.PeekKind() != ']' {", w.names.decoder)
	w.line("var %s %s", one, of.elem.spelled.Text)
	w.readValue(one, of.elem, depth+1)
	w.line("%s = append(%s, %s)", out, out, one)
	w.line("}")
	w.closeToken()
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
func (w *writer) readArray(held string, of *form, depth int) {
	index := loopVar("at", depth)
	one := loopVar("one", depth)
	spelled := of.spelled.Text

	w.line("if %s.PeekKind() == 'n' {", w.names.decoder)
	w.line("if _, err := %s.ReadToken(); err != nil {", w.names.decoder)
	w.line("return err")
	w.line("}")
	w.line("var zero %s", spelled)
	w.line("%s = zero", held)
	w.line("} else {")

	w.openArray()
	w.line("%s := 0", index)
	w.line("for %s.PeekKind() != ']' {", w.names.decoder)
	w.line("if %s >= len(%s) {", index, held)
	w.line("return fmt.Errorf(%s)", strconv.Quote("too many array elements for "+spelled))
	w.line("}")
	w.line("var %s %s", one, of.elem.spelled.Text)
	w.readValue(one, of.elem, depth+1)
	w.line("%s[%s] = %s", held, index, one)
	w.line("%s++", index)
	w.line("}")
	w.line("if %s < len(%s) {", index, held)
	w.line("return fmt.Errorf(%s)", strconv.Quote("too few array elements for "+spelled))
	w.line("}")
	w.closeToken()
	w.line("}")
}

// readMap reads a JSON object into a map.
func (w *writer) readMap(held string, of *form, depth int) {
	built := loopVar("into", depth)
	key := loopVar("key", depth)
	value := loopVar("value", depth)

	w.line("if %s.PeekKind() == 'n' {", w.names.decoder)
	w.line("if _, err := %s.ReadToken(); err != nil {", w.names.decoder)
	w.line("return err")
	w.line("}")
	w.line("%s = nil", held)
	w.line("} else {")
	w.line("if _, err := %s.ReadToken(); err != nil {", w.names.decoder)
	w.line("return err")
	w.line("}")
	w.line("%s := make(%s)", built, of.spelled.Text)
	w.line("for %s.PeekKind() != '}' {", w.names.decoder)
	w.line("var %s %s", key, of.key.spelled.Text)
	w.readValue(key, of.key, depth+1)
	w.line("var %s %s", value, of.elem.spelled.Text)
	w.readValue(value, of.elem, depth+1)
	w.line("%s[%s] = %s", built, key, value)
	w.line("}")
	w.closeToken()
	w.line("%s = %s", held, built)
	w.line("}")
}

// openArray reads the token that opens a JSON array, and closeToken the one
// that closes whatever was opened.
func (w *writer) openArray() {
	w.line("if _, err := %s.ReadToken(); err != nil {", w.names.decoder)
	w.line("return err")
	w.line("}")
}

func (w *writer) closeToken() {
	w.line("if _, err := %s.ReadToken(); err != nil {", w.names.decoder)
	w.line("return err")
	w.line("}")
}
