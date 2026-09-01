// Package jsoncodec generates a streaming JSON codec for a subject.
//
// Named for what it does rather than for the marker it claims, because a
// package called json beside a file that imports encoding/json/v2 is two
// things under one name in the same line of source.
//
// What it writes is a pair of halves per type — MarshalJSONTo and
// UnmarshalJSONFrom, the interfaces encoding/json/v2 dispatches to — driven by
// the fields the subject declares and the json tags written on them. The
// subject's own fields are walked without reflection: a member's name is a
// string constant in the output and a field's type is decided when the code is
// written, which is what makes the result both faster than the reflective path
// and readable as the thing it does.
//
// Three things still go through the standard library's encoder, and all three
// are visible in the output. A field marked as a reflective boundary, which is
// described below and is the author's own decision. A slice or array of bytes,
// which JSON carries as a base64 string rather than as an array — that is the
// standard library's rule rather than a choice available here, and its encoder
// is what implements it. And a member tagged omitempty whose emptiness only its
// own output can settle, which is written into a buffer and dropped if what came
// back was empty; the buffer carries the options of the encoder it will go into,
// so a delegated member is written exactly as the standard library would have
// written it. What that does and does not extend to is below.
//
// # What a field may be
//
// The codec is written against types it can see through. A basic type, a named
// type over one, a pointer, a slice, an array, a map with string keys, and a
// struct whose own codec this layer also writes are all seen through. So is a
// type that declares both halves of a codec itself, which is delegated to rather
// than duplicated — a hand-written codec stays authoritative. A type declaring
// one half and not the other is refused instead, since generating the pair
// would redeclare the half that is already there.
//
// Everything else is refused rather than guessed at. An interface field holds a
// type nobody knows until run time; a struct from another module holds
// unexported fields generated code cannot read. So is a tag option this layer
// does not generate for — a format, a loose name match, a number asked to be
// written as a string — because an option quietly ignored puts the document in
// a shape nobody asked for. Each is a diagnostic naming the field, and most have
// the same way out: write
//
//	//forge:json fallback=stdlib
//
// above the field. That hands the field to the reflective encoder for the
// length of that one value, and says so at the place a reader would ask.
//
// The refusal is the point. A codec that quietly skipped what it could not see
// through would produce JSON missing a member, which no test that round-trips
// through the same codec would ever catch.
//
// # What a caller's options reach
//
// A caller passes options to Marshal and Unmarshal, and a generated codec
// honours some of them everywhere and some of them only in places.
//
// The options that decide how JSON is written — indentation, spacing, HTML
// escaping — are honoured throughout, because they belong to the encoder and a
// generated codec writes every token through it. So does anything a caller
// passes that only takes effect where the standard library is doing the writing
// anyway.
//
// The options that decide what a Go value becomes — StringifyNumbers,
// FormatNilSliceAsNull, Deterministic, a marshaler registered with
// WithMarshalers — reach only the members this layer delegates: a byte slice, a
// field marked as a reflective boundary, a member buffered for omitempty. A
// member written as tokens was decided when the code was written, and there is
// nothing left at run time for an option to change. A caller who sets
// FormatNilSliceAsNull will see null for a delegated member and [] for a
// generated one, in the same object.
//
// That is the cost of writing the code in advance, and it is stated here rather
// than discovered: a codec that consulted the options per field would be doing
// at run time exactly the work this exists to avoid. A subject whose encoding
// must follow such an option throughout is one to hand to the reflective
// encoder, either per field or by not declaring this layer over it at all.
//
// # Order
//
// Members are written in the order the fields are declared — an embedded
// struct's where the embedded field is, and a field that takes a name from a
// shallower one in its own place rather than the excluded one's, which is the
// order the standard library writes too.
//
// A map this layer writes has its members in the order its keys sort, which the
// standard library does only when asked. It is a choice in favour of output that
// does not change between runs: generated code is committed, and a diff that
// appears because a map iterated differently is a diff nobody can review.
//
// A map inside a member this layer delegates follows the caller instead, whose
// default is to sort nothing — so an object can hold one map sorted and another
// not. Deterministic is what settles it, and settles it the same way for both.
// It is the sharpest instance of the split described above, because the two
// halves are the same kind of value written two ways in one object.
package jsoncodec
