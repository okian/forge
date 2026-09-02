package csv

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
	"strings"

	"github.com/okian/forge/plugin"
)

// writer assembles the source of one file's worth of declarations.
//
// Text rather than syntax. What is written here is a pair of methods with loops
// and branches in them, and a tree for one is many times its own size — the
// sentences this builds are what a person would write, and reading them back is
// how they are checked. What that costs is the possibility of assembling
// something that is not Go, which is why the result is parsed before it leaves
// this package: a failure to parse is reported against the declaration rather
// than discovered in a file on disk.
type writer struct{ out strings.Builder }

// line writes one line of the body.
//
// Indentation is left to gofmt, which forge runs over everything it emits, so
// that the assembly here reads as the sentences it is rather than as a column
// of tabs.
func (w *writer) line(format string, args ...any) {
	if len(args) == 0 {
		w.out.WriteString(format)
	} else {
		fmt.Fprintf(&w.out, format, args...)
	}
	w.out.WriteByte('\n')
}

// blank separates two declarations.
func (w *writer) blank() { w.out.WriteByte('\n') }

// doc writes a comment, one argument per paragraph, wrapped.
//
// Prose in and lines out, rather than lines in. A generated file is read in
// review far more often than it is produced, and a comment broken where the
// assembling source happened to break is a comment that reads as though nobody
// looked at it — so the breaking is done here, at the width forge wraps its own
// comments to, and the sentences above are written as sentences.
func (w *writer) doc(paragraphs ...string) { w.wrapped(plugin.CommentWidth, paragraphs) }

// note writes a comment inside a function body.
//
// Narrower than [writer.doc] by the tab gofmt will put in front of it, so that
// a comment explaining a loop comes to the same column as the doc comment above
// the function holding it. A body two blocks deep would want narrower again;
// nothing here is, so the one step is the whole rule.
func (w *writer) note(paragraphs ...string) { w.wrapped(plugin.CommentWidth-tabWidth, paragraphs) }

// tabWidth is how many columns the tab gofmt indents a body by is taken to be.
//
// Four, which is what gofmt's own output is read at and what go doc renders.
// It is a convention rather than a fact — a tab is as wide as whoever is
// looking has set it — so what this buys is that the common setting looks
// right rather than that every setting does.
const tabWidth = 4

// wrapped writes each paragraph as its own run of comment lines, separated by a
// bare marker.
func (w *writer) wrapped(width int, paragraphs []string) {
	for at, one := range paragraphs {
		if at > 0 {
			w.line("//")
		}
		for _, held := range plugin.Wrapped(one, width) {
			w.line("// %s", held)
		}
	}
}

// String returns the assembled source, as the file it has to parse as.
func (w *writer) String() string { return "package p\n\n" + w.out.String() }

// parse reads assembled source back as the declarations to emit.
func parse(src string) (*ast.File, *token.FileSet, error) {
	fset := token.NewFileSet()

	file, err := parser.ParseFile(fset, "csv.go", src,
		parser.ParseComments|parser.SkipObjectResolution)
	if err != nil {
		return nil, nil, err
	}

	return file, fset, nil
}

// container assembles the methods that go on the declared type.
func container(held stack) string {
	var w writer

	w.header(held)
	if held.writes {
		w.writeCounter(held)
		w.writeTo(held)
	}
	if held.reads {
		w.readCounter(held)
		w.readFrom(held)
	}

	return w.String()
}

// header writes the method that names the columns.
//
// It is emitted whether or not the document carries a header row, because the
// columns have an order either way and that order is what a reader of the
// generated API has to know to make sense of a record. What the option decides
// is whether the order is also written into the document.
//
// On the same receiver as everything else the layer writes, though it reads
// nothing. A type whose method set depends on how a caller happens to be
// holding it is a type nobody can reason about — and a container whose other
// methods take a pointer usually takes one because it must, so the one method
// that took a value would be the one a vet check complained about.
func (w *writer) header(held stack) {
	w.doc(
		fmt.Sprintf("%s returns the columns %s writes, in the order it writes them.",
			headerMethod, held.declared),
		"A fresh slice on every call, so that a caller sorting or rewriting what it was "+
			"given cannot change what the next document is checked against.")
	w.line("func (%s %s) %s() []string {", held.names.receiver, held.receiver(), headerMethod)
	w.line("return []string{%s}", held.literal)
	w.line("}")
	w.blank()
}

// quoted renders names as the comma-separated literal that declares them.
func quotedNames(names []string) string {
	held := make([]string, len(names))
	for i, one := range names {
		held[i] = strconv.Quote(one)
	}

	return strings.Join(held, ", ")
}

// writeCounter writes the stream that counts what reached the caller's writer.
//
// The CSV writer buffers, so what it has been given and what the writer beneath
// it has taken are two numbers, and a method reporting a count is reporting the
// second: a write that fails part way through has to say what got out rather
// than what was composed.
func (w *writer) writeCounter(held stack) {
	name := held.writing()

	w.doc(
		fmt.Sprintf("%s counts the bytes a writer accepted.", name),
		"The CSV writer buffers, so what it has written and what the writer beneath it "+
			"has taken are two numbers. This is the second, which is the one a caller "+
			"copying the container out is entitled to.")
	w.line("type %s struct {", name)
	w.line("to io.Writer")
	w.line("n  int64")
	w.line("}")
	w.blank()

	w.doc("Write passes the bytes on and counts the ones that were taken.")
	w.line("func (%[1]s *%[2]s) Write(p []byte) (int, error) {", held.names.sink, name)
	w.line("n, %[1]s := %[2]s.to.Write(p)", held.names.err, held.names.sink)
	w.line("%s.n += int64(n)", held.names.sink)
	w.line("return n, %s", held.names.err)
	w.line("}")
	w.blank()
}

// writeTo writes the method that sends the container out as a document.
func (w *writer) writeTo(held stack) {
	w.doc(
		fmt.Sprintf("%s writes the container to w as a CSV document, and reports how many "+
			"bytes reached w.", writeMethod),
		"One pass over the elements and one record buffer for the whole document, so what "+
			"it costs to write a container is what it costs to write its rows and nothing "+
			"besides.")
	names := held.names

	w.line("func (%s %s) %s(%s io.Writer) (int64, error) {",
		names.receiver, held.receiver(), writeMethod, names.sink)
	w.line("%s := %s{to: %s}", names.counted, held.writing(), names.sink)
	w.blank()
	w.line("%s := csv.NewWriter(&%s)", names.out, names.counted)
	w.line("%s.Comma = %s", names.out, held.comma)
	w.blank()
	w.line("%s := make([]string, 0, %d)", names.record, held.columns)
	w.line("var %s error", names.err)
	w.blank()

	if held.header {
		w.line("if %s = %s.Write(%s.%s()); %s != nil {",
			names.err, names.out, names.receiver, headerMethod, names.err)
		w.line("return %s.n, %s", names.counted, names.err)
		w.line("}")
	}

	w.line("for %s := range %s.%s() {", names.value, names.receiver, walkMethod)
	w.line("if %s, %s = %s(%s, %s); %s != nil {",
		names.record, names.err, held.encode, names.record, names.value, names.err)
	w.line("return %s.n, %s", names.counted, names.err)
	w.line("}")

	w.blankLine(held)

	w.line("if %s = %s.Write(%s); %s != nil {", names.err, names.out, names.record, names.err)
	w.line("return %s.n, %s", names.counted, names.err)
	w.line("}")
	w.line("}")
	w.blank()

	w.note("Flushed here rather than by the caller, because the caller was handed a count " +
		"and a count of buffered bytes is not one.")
	w.line("%s.Flush()", names.out)
	w.blank()
	w.line("return %s.n, %s.Error()", names.counted, names.out)
	w.line("}")
	w.blank()
}

// blankLine writes the check that keeps a row from being written and not read
// back.
//
// One shape of table needs it, and [table.blank] says which and why: a record
// of one empty cell is a blank line, and a reader discards a blank line before
// it counts fields. So the row would go out and not come back, with nothing on
// either side reporting it.
//
// Refused rather than written, because a document that has quietly lost a row
// is worse than a write that stopped and said so. Nothing is emitted for the
// tables that cannot produce one, which is nearly all of them.
func (w *writer) blankLine(held stack) {
	if held.blank == "" {
		return
	}

	w.note("A record of one empty cell is a blank line, and a reader discards a blank " +
		"line before it counts fields — so this row would be written and never read " +
		"back. Refused here rather than lost there.")
	w.line("if %s[0] == \"\" {", held.names.record)
	w.line("return %s.n, errors.New(%s)", held.names.counted, strconv.Quote(held.declared+
		" cannot write an empty "+held.blank+": it is the only column, so the record "+
		"would be a blank line and a reader would skip it"))
	w.line("}")
}

// readCounter writes the stream that counts what was taken from the caller's
// reader.
func (w *writer) readCounter(held stack) {
	name := held.reading()

	w.doc(
		fmt.Sprintf("%s counts the bytes a reader gave up.", name),
		"The CSV reader buffers, so it takes more from the reader beneath it than the "+
			"records it has handed back account for. What is counted here is what was taken, "+
			"which is what a caller reading into the container is entitled to know.")
	w.line("type %s struct {", name)
	w.line("from io.Reader")
	w.line("n    int64")
	w.line("}")
	w.blank()

	w.doc("Read passes the bytes on and counts them.")
	w.line("func (%[1]s *%[2]s) Read(p []byte) (int, error) {", held.names.source, name)
	w.line("n, %[1]s := %[2]s.from.Read(p)", held.names.err, held.names.source)
	w.line("%s.n += int64(n)", held.names.source)
	w.line("return n, %s", held.names.err)
	w.line("}")
	w.blank()
}

// readFrom writes the method that fills the container from a document.
func (w *writer) readFrom(held stack) {
	w.doc(append([]string{
		fmt.Sprintf("%s reads a CSV document from r into the container, and reports how many "+
			"bytes were taken from r.", readMethod),
		"What the container held is dropped before the first row is read into it, so " +
			"reading into one twice leaves the second document rather than both — which is " +
			"what reading a document into a value means everywhere else. An empty reader " +
			"empties it and reads nothing else.",
		"The rows are handed over one at a time as they are read, so the document is never " +
			"held in memory beside the container being filled from it.",
		"What a failure leaves behind follows from that, and it is not one answer. Nothing " +
			"is dropped until the first row arrives, so a document that fails before then " +
			"leaves the container exactly as it was — whatever the reason, and there are " +
			"several. One that fails once the rows have started leaves the rows before the " +
			"failure in it, since each was handed over before the next was read. Read a " +
			"non-nil error as leaving the container in no particular state rather than as " +
			"leaving it alone.",
		"More bytes are usually taken from r than the document occupies, because the " +
			"reader buffers: what is reported is what was read rather than what was used. " +
			"So this cannot pull one document out of a stream holding several — give it a " +
			"reader over the one document.",
	}, w.silences(held)...)...)
	names := held.names

	w.line("func (%s *%s) %s(%s io.Reader) (int64, error) {",
		names.receiver, held.declared, readMethod, names.source)

	w.room(held)

	w.line("%s := %s{from: %s}", names.counted, held.reading(), names.source)
	w.blank()
	w.line("%s := csv.NewReader(&%s)", names.in, names.counted)
	w.line("%s.Comma = %s", names.in, held.comma)
	w.line("%s.FieldsPerRecord = %d", names.in, held.columns)

	w.note("The record is read into again on every call, which the row codec is written for: " +
		"it copies every cell out into the element before the next call arrives.")
	w.line("%s.ReuseRecord = true", names.in)
	w.blank()

	w.opening(held)
	w.rows(held)
	w.ending(held)

	w.line("}")
	w.blank()
}

// silences says what this method reads differently from what was written, and
// does not report.
//
// Two of them, both the reader's own rules rather than anything a layer written
// over it decides, and both invisible: what comes back is a document, just not
// the one that went out. Neither can be fixed here, so each is a sentence — and
// a sentence is worth more than a fix that does not exist, because it is the
// difference between a caller who knows and a caller who finds out from a
// customer.
//
// Only where the table can reach them. A document of numbers holds no line
// ending and cannot have a blank row, so its reader says nothing about either.
func (w *writer) silences(held stack) []string {
	var out []string

	if held.blank != "" {
		out = append(out, "A blank line is not a record, which is the reader's rule: so a "+
			"document written elsewhere, holding an empty "+held.blank+" on some row, "+
			"comes back one row short and says nothing. Nothing here can recover it — "+
			"the line is discarded before the fields are counted. What this package does "+
			"is refuse to write one.")
	}

	if held.text {
		out = append(out, "A CRLF inside a quoted cell comes back as an LF, every time it "+
			"appears. That is the reader's own newline handling and it cannot be turned "+
			"off. A bare CR and a bare LF both survive; it is only the pair that "+
			"collapses.")
	}

	return out
}

// room writes the check that a bounded container can hold anything at all,
// which is what keeps a document from deciding whether the program stops.
//
// Asked before a byte is read, so the answer does not depend on what arrived. A
// container that was never constructed refuses an empty document exactly as it
// refuses a full one, which is the difference between a mistake somebody's
// tests find and one their traffic finds.
func (w *writer) room(held stack) {
	if !held.bounded {
		return
	}

	w.line("if %s.%s() == 0 {", held.names.receiver, capMethod)
	w.line("return 0, errors.New(%s)", strconv.Quote(held.declared+
		" holds nothing until it is constructed, so nothing can be read into it"))
	w.line("}")
	w.blank()
}

// opening writes what happens to the document's first record.
//
// Read and compared rather than skipped. A document whose columns arrived in
// another order would otherwise fill every element from the wrong fields and
// report nothing at all — which is the one failure a header exists to prevent.
func (w *writer) opening(held stack) {
	if !held.header {
		w.note("Nothing is read before the rows: this document carries no header, so the " +
			"first record is an element.")
		w.blank()

		return
	}

	w.note("The header before anything else, and compared rather than skipped: a document " +
		"whose columns arrived in another order would otherwise fill every element from " +
		"the wrong fields and report nothing.")
	names := held.names

	w.line("%s, %s := %s.Read()", names.header, names.err, names.in)
	w.line("if errors.Is(%s, io.EOF) {", names.err)
	w.line("%s.%s()", names.receiver, resetMethod)
	w.line("return %s.n, nil", names.counted)
	w.line("}")
	w.line("if %s != nil {", names.err)
	w.line("return %s.n, %s", names.counted, names.err)
	w.line("}")
	w.line("if %s := %s.%s(); !slices.Equal(%s, %s) {",
		names.want, names.receiver, headerMethod, names.header, names.want)
	w.line("return %s.n, fmt.Errorf(", names.counted)
	w.line("%s, %s, %s)",
		strconv.Quote("cannot read "+held.declared+": the document is headed %q, not %q"),
		names.header, names.want)
	w.line("}")
	w.blank()
}

// rows writes the sequence the elements reach the container through.
//
// A sequence rather than a call per element, because a sequence is what the
// contract's sink takes — and it is what lets the container decide how to take
// them: a bounded one drops or refuses, and neither answer is the reader's to
// make.
func (w *writer) rows(held stack) {
	w.note("What went wrong inside the walk, which a sequence has no way to answer with. " +
		"The container is handed elements one at a time and the failure is carried out here.")
	names := held.names

	w.line("var %s error", names.failed)
	w.blank()

	w.line("%s.%s()", names.receiver, resetMethod)
	w.line("%s%s.%s(func(yield func(%s) bool) {",
		held.binding(names), names.receiver, appendMethod, held.elem)
	w.line("for {")
	w.line("%s, %s := %s.Read()", names.record, names.err, names.in)
	w.line("if errors.Is(%s, io.EOF) {", names.err)
	w.line("return")
	w.line("}")
	w.line("if %s != nil {", names.err)
	w.line("%s = %s", names.failed, names.err)
	w.line("return")
	w.line("}")
	w.blank()
	w.line("%s, %s := %s(%s)", names.held, names.err, held.decode, names.record)
	w.line("if %s != nil {", names.err)
	w.line("%s = %s", names.failed, names.err)
	w.line("return")
	w.line("}")
	w.line("if !yield(%s) {", names.held)
	w.line("return")
	w.line("}")
	w.line("}")
	w.line("})")
	w.blank()
}

// ending writes the two ways reading can have gone wrong.
//
// The reading failure first: a container that refused an element refused it
// because the element arrived, and the reason it arrived wrongly is the one
// worth reporting.
func (w *writer) ending(held stack) {
	names := held.names

	if !held.refuses {
		w.line("return %s.n, %s", names.counted, names.failed)

		return
	}

	w.line("if %s != nil {", names.failed)
	w.line("return %s.n, %s", names.counted, names.failed)
	w.line("}")
	w.blank()
	w.line("return %s.n, %s", names.counted, names.refused)
}
