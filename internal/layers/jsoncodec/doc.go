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
// # The container as well as the subject
//
// A declaration puts this layer under a container, and the container gets a
// codec too: the same pair, plus WriteTo and ReadFrom, written on the declared
// type and carrying the whole stack as one JSON array. Neither half of that is
// available to either layer alone — the container knows nothing about what it
// holds, and the codec knows nothing about how many there are — which is the
// composition the two exist to make.
//
// It is written over the streaming contract rather than over any particular
// container: All to write, AppendSeq and Reset to read. So a container that can
// be walked and not filled gets the writing half and not the reading one, and a
// stack whose walk a decorator withdrew gets neither — the walk was withdrawn
// because walking is no longer safe, and whatever replaces it belongs to the
// layer that took it away.
//
// A container of a million elements costs no more to write than one element
// does, because the elements go into the encoder one at a time and nothing is
// assembled first. Reading is the same in reverse: elements reach the container
// as they are parsed, so a document is never held in memory beside the
// container being filled from it.
//
// # What it is worth, measured
//
// A thousand elements through an encoder the caller owns, against the
// reflective path writing the same elements through the same encoder: this
// allocates nothing at all where the reflective path allocates twice, and takes
// about a fifth longer. Reading is the other way round — the same allocations,
// since both make the same strings, and about a sixth quicker.
//
// The encoder is slower because encoding/json/v2 writes a struct straight into
// its own buffer, while generated code goes through the token API any caller
// could use. Going around that — assembling each object by hand and writing it
// as one value — was tried and is slower again, because a value written that
// way is validated on the way in.
//
// So the reason to declare this layer is not speed. It is output that costs no
// memory to produce, a codec whose behaviour is readable in the source rather
// than inferred from tags at run time, and a binary with no reflection in it. A
// subject whose encoding has to be the fastest thing in the program is one to
// measure both ways before deciding.
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
