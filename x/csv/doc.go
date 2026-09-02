// Package csv writes a forge stack out as a CSV document and reads one back.
//
// It is a layer, which means it is not a library: nothing here is called at run
// time. Registering it into a catalog and running the result over a package
// makes a declaration like
//
//	//forge:csv
//	type Rows forge.Csv[forge.Collection[Person]]
//
// generate WriteCSVTo, ReadCSVFrom and CSVHeader on Rows, over the subject's
// own fields, importing nothing but the standard library.
//
// # Running it
//
// The layer is code, so the binary that knows about it is the binary somebody
// linked it into. [github.com/okian/forge/driver] is the dozen lines that takes:
//
//	package main
//
//	import (
//		"github.com/okian/forge/driver"
//		"github.com/okian/forge/x/csv"
//	)
//
//	func main() {
//		catalog := driver.Builtins()
//		catalog.MustRegister(csv.New())
//
//		driver.Main(catalog)
//	}
//
// That is cmd/forge-csv, which is here and buildable. It takes the same
// command line as forge's own binary, walks the same packages and writes the
// same files.
//
// The marker it claims is forge's own [github.com/okian/forge.Csv]. Forge
// publishes that type without a generator behind it, so a declaration naming it
// type-checks against plain forge today and is reported as pending work by a
// binary that has not linked this layer. Registering this one takes the marker
// over — see [github.com/okian/forge/plugin.Registry] — so the declaration an
// author writes does not change when the layer arrives.
//
// # What a stack has to offer
//
// Structured and Streamable, which together mean a container of a subject with
// fields. A transport terminates a stack, so it is written outermost and only
// one of them may appear.
//
// The two halves are decided separately, from the methods the stack beneath
// turned out to expose. A container that walks gets WriteCSVTo; one that can be
// emptied and appended to gets ReadCSVFrom; one that does both gets both.
// Neither is invented: a decorator that withdrew the walk withdrew it because
// walking is no longer safe, and a document written out of a lock by going
// round it would be worse than no method at all.
//
// # What a column can hold
//
// One row per element and one column per field, which is the whole of what CSV
// is: a flat table with a header. So a field goes into a column when its value
// has a text form, and a subject holding one that has not cannot be written as
// CSV at all.
//
// A field has a text form when it is a string, a boolean, an integer or a
// float — or a type defined over one of those — or when it carries a text codec:
// MarshalText and UnmarshalText, whether its author wrote them or a layer of the
// same run is about to. That last is what lets a closed set written with
// //forge:enum go into a column as its member name rather than as the number
// behind it.
//
// A field with no text form is named in a refusal, along with what to do about
// it. Nothing is skipped silently and nothing is written through reflection: a
// column whose values came out as a Go struct literal would be a document
// nothing else can read.
//
// Unexported fields are left out, without a word. Generated code could read one
// from inside the subject's own package and could not from anywhere else, and a
// document whose columns depended on where the code was generated is worse than
// one that leaves the field out everywhere.
//
// # Naming the columns
//
// A field's own name, unless a csv struct tag renames it:
//
//	type Person struct {
//		ID      int    `csv:"id"`
//		Name    string `csv:"full_name"`
//		Comment string `csv:"-"`
//	}
//
// A dash omits the field, exactly as it does under encoding/json. The header is
// what ReadCSVFrom checks a document against before it reads a single element,
// so a document whose columns arrived in another order is refused rather than
// read into the wrong fields.
//
// # Why the methods are called what they are
//
// WriteCSVTo rather than WriteTo. A stack may hold more than one thing that
// turns into bytes — Csv over a container whose elements carry a JSON codec is
// an ordinary stack — and the plain name belongs to whichever of them the
// author designated. Nobody has designated one, so this layer takes the
// qualified name always: two names for one method depending on what else is in
// the stack would be worse than one name that is always right.
//
// The signatures are io.WriterTo's and io.ReaderFrom's all the same, so a
// caller who wants the interface wraps the method in three lines and gets it.
//
// # What it refuses, and under what number
//
// Every code here is in the range a layer forge does not ship takes, so
// Code.Ours tells one of these from one of forge's — and this is the
// documentation the number sends a reader to.
//
//   - FRG6100 — the delimiter is not one character, or is one a CSV document
//     cannot be delimited by: a quote, half a line ending, or the rune invalid
//     UTF-8 decodes to.
//   - FRG6101 — a field has no text form. The refusal names every such field
//     of the subject at once, because they are one decision rather than
//     several.
//   - FRG6102 — the subject has no columns: every field is unexported or
//     tagged out.
//   - FRG6103 — two fields want one column, which would leave every value in
//     the second reachable only by counting.
//   - FRG6104 — the layer beneath does not offer the walk or the sink a
//     document is written over. The one refusal here that is nobody's fault
//     but the layer beneath's, and the hint says so.
//
// # What a document does not carry back
//
// Three things a document loses, none of them recoverable and all of them the
// reader's own rules rather than this layer's. They are written here because a
// silent difference nobody wrote down is worse than one everybody knows.
//
// A table of exactly one column, holding a string or a text form, cannot write
// an empty value. A record of one empty cell is a blank line, and a reader
// discards a blank line before it counts fields — so the row would go out and
// not come back. The writer refuses the value rather than losing the row, and
// the declaration is left alone, since a one-column table round-trips perfectly
// until one of its values is empty. Nothing can be done about a document
// written elsewhere: the row is gone before there is anything to inspect.
//
// A CRLF inside a quoted cell comes back as an LF, wherever it appears. A bare
// CR and a bare LF survive; only the pair collapses. That is the reader's
// newline handling and it cannot be turned off, so a value holding Windows line
// endings is not one a document round-trips.
//
// And what a failed read leaves in the container is not one answer. Nothing is
// dropped until the first row is handed over, so a document that fails before
// then leaves the container as it was, and one that fails part way through
// leaves the rows before the failure in it. The generated method says as much;
// read a non-nil error as leaving the container in no particular state.
package csv
