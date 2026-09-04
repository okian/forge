// Package jsoncodec generates an append-based JSON codec for a subject.
//
// Named for what it does rather than for the marker it claims, because a
// package called json beside a file that imports encoding/json/v2 is two
// things under one name in the same line of source.
//
// What it writes is four entry points per type. AppendJSON is the
// implementation: it appends the value to a caller's buffer, allocating
// nothing beyond the buffer's growth, with every member's name and its
// punctuation baked as one string constant. MarshalJSON and UnmarshalJSON are
// the doors the standard library dispatches through, in either of its
// generations, so a subject reached by json.Marshal is written by this codec
// wherever it appears. UnmarshalJSONBorrowed is the sharp variant a caller
// asks for by name: the strings it fills the value with point into the
// document rather than copying out of it, exact for as long as the document
// outlives the value and is not written over.
//
// The subject's own fields are walked without reflection: a member's name is a
// string constant in the output and a field's type is decided when the code is
// written. The scanning half reads the document bytes directly — the grammar,
// the escapes, UTF-8, duplicate member names, the nesting bound — and holds it
// to the same verdicts the standard library reaches, which the differential
// tests beside the fixtures are what enforce.
//
// # The container as well as the subject
//
// A declaration puts this layer under a container, and the container gets a
// codec too: the same four, plus WriteTo and ReadFrom, written on the declared
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
// WriteTo appends into a borrowed window and hands it to the writer a flush at
// a time, so a container of a million elements costs one window to write.
// ReadFrom is the same in reverse: it holds the largest single element rather
// than the document, and the elements reach the container as they are read.
//
// # What it is worth, measured
//
// A thousand elements into a buffer the caller reuses, against the reflective
// path writing the same elements: this allocates nothing and takes under half
// the time. Reading is faster than reflection by a wider margin than before
// and borrowing widens it again, since a borrowed read allocates nothing per
// string. The figures are pinned in scripts/budget.txt and measured by the
// benchmarks beside the example, which is where to look rather than here.
//
// The shared machinery — the escaper, the number formats, the scanner, the
// pools — is emitted once per package as the wire runtime, and every helper in
// it is held to the standard library by differential test and fuzzing: the
// bytes it writes are the bytes encoding/json/v2 writes, and the documents it
// refuses are the ones that library refuses.
//
// Two things still go through the standard library, and both are visible in
// the output. A field marked as a reflective boundary, which is described
// below and is the author's own decision. And a type whose own codec speaks an
// interface this output cannot drive — MarshalJSONTo and its decoder twin need
// an encoder, and generated code carries none — which is handed to
// json.Marshal and its answer spliced in whole.
//
// # What a field may be
//
// The codec is written against types it can see through. A basic type, a named
// type over one, a pointer, a slice, an array, a map keyed by a string, an
// integer or a float, and a struct whose own codec this layer also writes are
// all seen through. A slice or array of bytes is base64 in a string, which is
// the standard library's rule rather than a choice available here, and is
// written and read by the wire runtime directly. A numeric key becomes the
// quoted number a member name has to be, read back with the verdicts a number
// of that width gets as a value — and a bool key is refused, because the
// standard library refuses to spell one as a name.
//
// A type that declares a codec of its own — AppendJSON, or MarshalJSON and
// UnmarshalJSON, or the streaming pair — is delegated to rather than
// duplicated: a hand-written codec stays authoritative. One that appends is
// called straight, since appending is what the caller is doing anyway;
// anything else is reached through the standard library, which knows how to
// call each shape and validates what comes back. A type declaring a writing
// half and no reading half, or the reverse, is refused instead, since
// generating the missing half would redeclare the one that is there under a
// reader that never consults it.
//
// One kind of delegate is not reached that way, and it is known by name
// rather than by shape. time.Time's MarshalJSON is its AppendText between
// quotes — one strict RFC 3339 formatter behind both — so a time is appended
// straight into the caller's buffer instead of being handed over and spliced,
// and is read back through its own UnmarshalJSON so the verdicts on the way in
// stay the method's own. The identity is what buys the shortcut, the same way
// it buys time.Duration its refusal: what is known here is known about that
// one type, and a type defined over it carries none of its methods and is
// decided like any other struct.
//
// A type carrying a text codec goes onto the wire as the string that codec
// writes, which is what the standard library does with one and for the same
// reason: the text is what its author said the value means, and the form
// underneath is a detail no document can explain. The appender is the half
// taken where the type carries both, which is the standard library's own
// preference and here for the same reason it is there: a buffer is waiting, so
// the text is appended where it will sit and settled after the fact — its
// closing quote where it was ordinary, one detour back through the escaper
// where it was not — and the ordinary value costs no allocation at all. A closed set is the case that
// makes it matter, and the declaration giving the type its text codec is
// usually a different declaration — so what the run will write is asked of the
// layers rather than read off the package, which would answer differently on a
// clean checkout than on the next one.
//
// One package is as wide as that goes. A declaration in a neighbouring package
// is not asked, so a field whose type is given a text codec over there is
// written as the form underneath it — the same way on every run and from every
// invocation, which is what matters more than reaching it. Declare a closed set
// beside the types that hold its members.
//
// Two things are not offered a text codec. A struct whose members this layer
// can read is written from them, because member by member is what the layer is
// for. And a map's key type is refused rather than written through it: what a
// key needs is a member name, and the members would come out in the order the
// Go keys sort rather than the order the names do.
//
// Everything else is refused rather than guessed at. An interface field holds a
// type nobody knows until run time; a struct from another module holds
// unexported fields generated code cannot read, unless it carries a codec of
// its own or a text codec to be written through. So is a tag option this layer
// does not generate for — a format, a loose name match, a number asked to be
// written as a string — because an option quietly ignored puts the document in
// a shape nobody asked for. Each is a diagnostic naming the field, and most
// have the same way out: write
//
//	//forge:json fallback=stdlib
//
// above the field. That hands the field to the reflective encoder for the
// length of that one value, and says so at the place a reader would ask.
//
// # A tag that is wrong about itself
//
// Six more refusals are about the tag rather than about the type, and they are
// the standard library's rather than this layer's: a tag it rejects is a tag
// whose field arrives on the wire under rules nobody agreed to. An option that
// is one of the known ones misspelled — omitEmpty for omitempty — is refused
// rather than ignored, because a reader seeing that word expects the member to
// be left out. An option written twice is refused rather than resolved to its
// first occurrence, because a tag saying a thing twice describes two wire
// formats and choosing between them quietly is not a generator's to make; the
// case that reads as a contradiction rather than a repetition, case:ignore
// beside case:strict, is refused as the repeat it is. An embed carrying a name
// or another option is refused, because promotion is what embed means and a
// promoted member carries its own name. A json tag on an unexported field is
// refused, since the field is left out either way and the tag would read as an
// instruction that was followed. A struct with fields and nothing to write is
// refused rather than written as an empty object, because {} read back into the
// same type gives the same value and no test comparing a codec with itself
// could ever see the loss. And a time.Duration with no format asked for is
// refused, because the standard library refuses to choose between a count of
// nanoseconds and a string like "1h30m" and neither does this.
//
// The refusal is the point. A codec that quietly skipped what it could not see
// through would produce JSON missing a member, which no test that round-trips
// through the same codec would ever catch.
//
// # What a caller's options reach
//
// AppendJSON takes no options, because the struct tag is the only thing that
// decides what a value becomes, and admitting a second source of truth is what
// the previous design spent most of its complexity on. What it writes is what
// json.Marshal writes under Deterministic(true): compact, members in
// declaration order, maps sorted.
//
// A caller who wants a document shaped differently — indented, HTML-safe,
// nulls for nil slices — asks the standard library for it: json.Marshal
// dispatches to this codec for the value and then formats what came back under
// the caller's options, so the whole-document options still land. What no
// option reaches is the inside of a single generated value on the direct path,
// and that is the cost of writing the code in advance: a codec that consulted
// options per field would be doing at run time exactly the work this exists to
// avoid.
//
// # Order
//
// Members are written in the order the fields are declared — an embedded
// struct's where the embedded field is, and a field that takes a name from a
// shallower one in its own place rather than the excluded one's, which is the
// order the standard library writes too.
//
// A map this layer writes has its members in the order their names sort, which
// the standard library does only when asked — and it is the names rather than
// the keys, because that is the order the library sorts into: "10" comes
// before "3", however the numbers behind them compare. It is a choice in
// favour of output that does not change between runs: generated code is
// committed, and a diff that appears because a map iterated differently is a
// diff nobody can review.
// A member this layer delegates to the standard library is written under
// Deterministic(true) for the same reason, so one object never holds one map
// sorted and another not.
package jsoncodec
