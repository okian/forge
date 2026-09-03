package people_test

import (
	"bytes"
	json "encoding/json/v2"
	"errors"
	"io"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/okian/forge/examples/people"
)

// filled returns a container holding the fixture directory, in order.
func filled(t *testing.T) *people.Recent {
	t.Helper()

	held := people.NewRecent()
	for _, one := range directory() {
		held.Push(one)
	}
	return held
}

// The container's codec agrees with the standard library reading and writing
// the same elements as a plain slice.
//
// That is the whole claim a generated codec makes. Anything else — a name, an
// order, a number's precision — is a value that leaves one program and arrives
// wrong in another, and a round trip through the generated codec alone would
// agree with itself about every one of them.
func TestTheContainerWritesWhatASliceWould(t *testing.T) {
	held := filled(t)

	got, err := json.Marshal(held)
	if err != nil {
		t.Fatalf("marshaling the container: %v", err)
	}

	want, err := json.Marshal(slices.Collect(held.All()))
	if err != nil {
		t.Fatalf("marshaling the elements: %v", err)
	}

	if !bytes.Equal(got, want) {
		t.Errorf("the container wrote\n\t%s\nand a slice of the same elements wrote\n\t%s", got, want)
	}
}

// What the container wrote reads back as what it held.
func TestTheContainerReadsBackWhatItWrote(t *testing.T) {
	held := filled(t)

	written, err := json.Marshal(held)
	if err != nil {
		t.Fatalf("marshaling: %v", err)
	}

	read := people.NewRecent()
	if err := json.Unmarshal(written, read); err != nil {
		t.Fatalf("unmarshaling: %v", err)
	}

	if got, want := slices.Collect(read.All()), slices.Collect(held.All()); !slices.EqualFunc(got, want, survives) {
		t.Errorf("read back %v, want %v", got, want)
	}
}

// Reading into a container that already holds elements leaves the document,
// rather than the document appended to what was there.
//
// It is what reading a document into a value means everywhere else in Go, and
// the only reason it needs saying is that the container's own sink appends: the
// codec empties first, and nothing but this says so.
func TestReadingReplacesRatherThanAppends(t *testing.T) {
	held := filled(t)

	if err := json.Unmarshal([]byte(`[{"ID":9,"Name":"Edsger","Email":"e@example.com","Age":31}]`), held); err != nil {
		t.Fatalf("unmarshaling: %v", err)
	}

	if got, want := held.Names(), []string{"Edsger"}; !slices.Equal(got, want) {
		t.Errorf("after reading one element the container holds %v, want %v", got, want)
	}
}

// A null and an empty array are read alike, because a container is empty or
// holds elements and has no third state to keep them apart in.
func TestNullAndAnEmptyArrayBothEmptyIt(t *testing.T) {
	for _, document := range []string{"null", "[]"} {
		t.Run(document, func(t *testing.T) {
			held := filled(t)

			if err := json.Unmarshal([]byte(document), held); err != nil {
				t.Fatalf("unmarshaling %s: %v", document, err)
			}
			if got := held.Len(); got != 0 {
				t.Errorf("the container holds %d elements after %s, want none", got, document)
			}

			written, err := json.Marshal(held)
			if err != nil {
				t.Fatalf("marshaling: %v", err)
			}
			if string(written) != "[]" {
				t.Errorf("an empty container wrote %s, want []", written)
			}
		})
	}
}

// A document longer than the container holds leaves the last of it, which is
// what pushing the elements one at a time would leave.
//
// The document is read whole either way: the elements reach the container as
// they are parsed, and dropping the older ones is the container's answer rather
// than the reader's.
func TestADocumentLongerThanTheContainerLeavesTheLastOfIt(t *testing.T) {
	held := people.NewRecent()

	var document strings.Builder
	document.WriteByte('[')
	for i := range held.Cap() + 3 {
		if i > 0 {
			document.WriteByte(',')
		}
		document.WriteString(`{"ID":`)
		document.WriteString(strconv.Itoa(i))
		document.WriteString(`}`)
	}
	document.WriteByte(']')

	if err := json.Unmarshal([]byte(document.String()), held); err != nil {
		t.Fatalf("unmarshaling: %v", err)
	}

	if got, want := held.Len(), held.Cap(); got != want {
		t.Fatalf("the container holds %d elements, want its capacity of %d", got, want)
	}
	if got, want := held.IDs()[0], 3; got != want {
		t.Errorf("the oldest element kept is %d, want %d", got, want)
	}
}

// A container that was never constructed refuses to be read into, and refuses
// the same way whatever the document says.
//
// The refusal matters more than which one it is. The container's own rule is
// that a zero value has no buffer and adding to one is a mistake — but
// unmarshaling is reached from a document rather than from code somebody wrote,
// so a container that found out one element in would fail for a document with
// elements and not for a document without. That is a program whose tests pass
// and whose users see a panic.
func TestAContainerThatWasNeverConstructedRefusesEveryDocument(t *testing.T) {
	for _, document := range []string{"[]", "null", `[{"ID":1}]`} {
		t.Run(document, func(t *testing.T) {
			var held people.Recent

			err := json.Unmarshal([]byte(document), &held)
			if err == nil {
				t.Fatalf("reading %s into an unconstructed container was allowed", document)
			}
			if !strings.Contains(err.Error(), "until it is constructed") {
				t.Errorf("reading %s was refused with %v, which does not say why", document, err)
			}
		})
	}
}

// A document that stops in the middle is reported as the truncation it is,
// rather than as a value of no kind.
//
// The reader names the kind of value it refuses from the value's first byte,
// and a truncated document has no byte to name a kind from — so a reader that
// reached for the name anyway would report every truncated document as a JSON
// value of no kind at all: true, useless, and identical for a missing comma, a
// half-written object and an empty file.
func TestATruncatedDocumentSaysWhereItStopped(t *testing.T) {
	cases := map[string]string{
		"an array that never closes":          `[`,
		"an element that never closes":        `[{"ID":1`,
		"an array with nothing after a comma": `[{"ID":1},`,
		"two elements with no comma":          `[{"ID":1} {"ID":2}]`,
	}

	for name, document := range cases {
		t.Run(name, func(t *testing.T) {
			held := people.NewRecent()

			err := json.Unmarshal([]byte(document), held)
			if err == nil {
				t.Fatalf("%s was read without complaint", document)
			}
			if strings.Contains(err.Error(), "JSON invalid") {
				t.Errorf("%s is reported as a value of no kind: %v", document, err)
			}
		})
	}
}

// A document of the wrong shape is refused by name, since that is the one thing
// the reader knows and the author does not.
func TestADocumentOfTheWrongShapeIsRefusedByName(t *testing.T) {
	held := people.NewRecent()

	err := json.Unmarshal([]byte(`{"ID":1}`), held)
	if err == nil {
		t.Fatal("an object was read into a container of arrays")
	}
	if !strings.Contains(err.Error(), "Recent") {
		t.Errorf("the refusal does not name the type: %v", err)
	}
}

// Writing to a writer reports how many bytes reached it, and reading from a
// reader reports how many the document took.
func TestWhatWritingAndReadingReport(t *testing.T) {
	held := filled(t)

	var out bytes.Buffer
	n, err := held.WriteTo(&out)
	if err != nil {
		t.Fatalf("writing: %v", err)
	}
	if got := int64(out.Len()); n != got {
		t.Errorf("WriteTo reported %d bytes and wrote %d", n, got)
	}

	read := people.NewRecent()
	back, err := read.ReadFrom(bytes.NewReader(out.Bytes()))
	if err != nil {
		t.Fatalf("reading: %v", err)
	}

	// One less than what was written: WriteTo ends a document with a newline
	// so that a stream of them can be read, and the newline is not part of the
	// array.
	if want := n - 1; back != want {
		t.Errorf("ReadFrom reported %d bytes and the array was %d", back, want)
	}
	if got, want := slices.Collect(read.All()), slices.Collect(held.All()); !slices.EqualFunc(got, want, survives) {
		t.Errorf("read back %v, want %v", got, want)
	}
}

// A write that fails part way reports what got out, not what was composed.
//
// io.WriterTo is a contract about the writer, and io.Copy hands the number
// straight back to a caller who may be counting or resuming from it. The
// document is composed in a window before it is flushed, so the two numbers
// differ exactly on the path where the difference is read.
func TestWritingReportsWhatTheWriterTook(t *testing.T) {
	held := filled(t)

	stops := &shortWriter{after: 20}
	n, err := held.WriteTo(stops)

	if err == nil {
		t.Fatal("a writer that refused the bytes reported no error")
	}
	if n != int64(stops.written) {
		t.Errorf("WriteTo reported %d bytes and the writer took %d", n, stops.written)
	}
}

// shortWriter accepts a fixed number of bytes and then refuses.
type shortWriter struct {
	after   int
	written int
}

// Write takes what is left of the allowance and refuses the rest.
func (w *shortWriter) Write(p []byte) (int, error) {
	if room := w.after - w.written; room < len(p) {
		w.written += max(room, 0)
		return max(room, 0), errors.New("no room")
	}

	w.written += len(p)
	return len(p), nil
}

// A reader holding nothing reports that it is at the end, rather than reporting
// a document of some unrecognisable shape.
func TestReadingFromAnEmptyReaderReportsTheEnd(t *testing.T) {
	held := people.NewRecent()

	_, err := held.ReadFrom(strings.NewReader(""))
	if !errors.Is(err, io.EOF) {
		t.Errorf("reading from an empty reader gave %v, want something that is io.EOF", err)
	}
}

// The container's codec is what the standard library dispatches to, whether it
// is handed the container or a pointer to it.
//
// The methods take a pointer, because the ring's own walk does. json/v2 makes a
// value addressable before it looks for them, so both work — and this is what
// says so, since the alternative is a codec that silently falls back to
// reflecting over a struct of unexported fields.
func TestTheStandardLibraryFindsTheGeneratedCodec(t *testing.T) {
	held := filled(t)

	byValue, err := json.Marshal(*held)
	if err != nil {
		t.Fatalf("marshaling a value: %v", err)
	}
	byPointer, err := json.Marshal(held)
	if err != nil {
		t.Fatalf("marshaling a pointer: %v", err)
	}

	if !bytes.Equal(byValue, byPointer) {
		t.Errorf("a value wrote %s and a pointer wrote %s", byValue, byPointer)
	}
	if !bytes.HasPrefix(byValue, []byte(`[{"ID":`)) {
		t.Errorf("the container was written as %s, which is not what its codec writes", byValue)
	}
}

// The elements go into the buffer the caller passes, so the buffer they hand
// over is the one extended.
//
// It is what "append" means here and it is not visible from the bytes: a codec
// that composed the document somewhere of its own and copied it in would
// produce the same array. What says otherwise is that whatever the buffer
// already held is still in front of the document, and that handing the buffer
// back reset to its start — the way a caller reusing one does — yields the
// same document again.
func TestTheCallersBufferIsTheOneAppendedInto(t *testing.T) {
	held := filled(t)

	dst := []byte("prefix: ")
	out, err := held.AppendJSON(dst)
	if err != nil {
		t.Fatalf("appending to a caller's buffer: %v", err)
	}
	if !bytes.HasPrefix(out, []byte("prefix: [")) {
		t.Errorf("the buffer's own bytes did not survive the append:\n%s", out)
	}

	document := append([]byte(nil), out[len("prefix: "):]...)
	again, err := held.AppendJSON(out[:0])
	if err != nil {
		t.Fatalf("appending into the reused buffer: %v", err)
	}
	if !bytes.Equal(again, document) {
		t.Errorf("the reused buffer wrote\n\t%s\nand the first append wrote\n\t%s", again, document)
	}
}

// The encoder this writes through does not check for repeated names, and the
// decoder does.
//
// The asymmetry is the point. Writing, the names are the ones the codec was
// generated from: a subject with two members under one JSON name is refused
// when the codec is written, so the encoder would be re-establishing something
// already settled — and it costs about a quarter of what writing a document
// takes, because it records every name it writes and unquotes each one back out
// to compare. Reading, the names arrive from outside, so refusing a repeated
// one is the decoder protecting a caller from their input rather than from this
// codec.
//
// Written as one test because the two halves are one decision, and a change to
// either that did not think about the other would pass a test that only knew
// about its own half.
func TestNamesAreCheckedReadingAndNotWriting(t *testing.T) {
	held := filled(t)

	// The document this writes is one the standard library reads, which is the
	// only thing the option could have put at risk: an encoder that stopped
	// tracking names would otherwise be free to write a document nothing else
	// accepts.
	var out bytes.Buffer
	if _, err := held.WriteTo(&out); err != nil {
		t.Fatalf("writing: %v", err)
	}

	var back []people.Person
	if err := json.Unmarshal(out.Bytes(), &back); err != nil {
		t.Fatalf("the standard library cannot read what WriteTo wrote: %v\n%s", err, out.String())
	}
	if len(back) != len(directory()) {
		t.Errorf("the library read %d people, %d were written", len(back), len(directory()))
	}

	// And a document that repeats a name is still refused, which is the half
	// the option must not have touched.
	repeated := `[{"id":1,"id":2,"name":"a","email":"a@example.com","age":1}]`

	into := people.NewRecent()
	if _, err := into.ReadFrom(strings.NewReader(repeated)); err == nil {
		t.Error("a document repeating a member name was read without complaint")
	}
}
