package people_test

import (
	"bytes"
	"encoding/json/jsontext"
	json "encoding/json/v2"
	"strconv"
	"testing"

	"github.com/okian/forge/examples/people"
)

// recent returns a container holding n people with distinct fields.
//
// The same shape the other benchmarks build, in the container that carries a
// codec. Distinct values rather than one repeated, so that neither encoder can
// be measured writing the same bytes over and over — and so the standard
// library's string cache cannot flatter the reflective baseline.
func recent(n int) *people.Recent {
	held := people.NewRecent()
	for i := range n {
		held.Push(people.Person{
			ID:    i,
			Name:  "person-" + strconv.Itoa(i),
			Email: "person-" + strconv.Itoa(i) + "@example.com",
			Age:   i % 97,
		})
	}
	return held
}

// document returns what the container writes, for the readers to read.
func document(b *testing.B, held *people.Recent) []byte {
	b.Helper()

	out, err := json.Marshal(held)
	if err != nil {
		b.Fatalf("building the document: %v", err)
	}
	return out
}

// The generated codec appending a thousand elements into a buffer the caller
// owns.
//
// This is the claim the layer exists for, and it is why the buffer lives
// outside the loop: writing a container costs one pass over its elements and
// nothing besides, and a caller who reuses a buffer pays its growth once. A
// codec that gathered the elements first, or that allocated per element, would
// show here as a figure that grows with the fixture.
func BenchmarkJSONEncode(b *testing.B) {
	held := recent(elements)

	// Warmed before it is measured, here and in every benchmark below. The
	// buffer grows to fit the document, once — and a measurement that included
	// that would report the growth divided by however many iterations it
	// happened to run for, which is a different number at every -benchtime and
	// not a property of the code.
	var buf []byte
	write := func() {
		var err error
		if buf, err = held.AppendJSON(buf[:0]); err != nil {
			b.Fatalf("encoding: %v", err)
		}
	}
	write()

	b.ReportAllocs()

	for b.Loop() {
		write()
	}
}

// The same document, written by the reflective encoder over the same elements.
//
// The comparison the whole layer is worth measuring against. It writes into a
// reused buffer of its own, so what differs between the two figures is the
// encoding and nothing around it.
func BenchmarkJSONEncodeReflectively(b *testing.B) {
	held := twins(recent(elements))

	var out bytes.Buffer
	enc := jsontext.NewEncoder(&out)

	write := func() {
		out.Reset()
		enc.Reset(&out)

		if err := json.MarshalEncode(enc, held); err != nil {
			b.Fatalf("encoding: %v", err)
		}
	}
	write()

	b.ReportAllocs()

	for b.Loop() {
		write()
	}
}

// The generated codec reading a thousand elements back.
//
// Into a container that has already been filled once, which is the ordinary
// case and the one the memory question is about: the container is emptied and
// refilled, so a reader that allocated per member would show as a figure that
// grows with the fixture rather than one that stays where the strings are.
func BenchmarkJSONDecode(b *testing.B) {
	written := document(b, recent(elements))

	into := people.NewRecent()
	read := func() {
		if err := into.UnmarshalJSON(written); err != nil {
			b.Fatalf("decoding: %v", err)
		}
	}
	read()

	b.ReportAllocs()

	for b.Loop() {
		read()
	}
}

// The same read with the strings borrowed rather than copied, which is the
// sharp variant a caller with a stable document asks for by name.
//
// What it saves is the allocation and the copy behind every string member; what
// it costs is the promise that the document outlives the container and is not
// written over. The benchmark holds the document still, which is exactly the
// case the method is for.
func BenchmarkJSONDecodeBorrowed(b *testing.B) {
	written := document(b, recent(elements))

	into := people.NewRecent()
	read := func() {
		if err := into.UnmarshalJSONBorrowed(written); err != nil {
			b.Fatalf("decoding: %v", err)
		}
	}
	read()

	b.ReportAllocs()

	for b.Loop() {
		read()
	}
}

// The same document, read by the reflective decoder into a slice of the same
// elements.
func BenchmarkJSONDecodeReflectively(b *testing.B) {
	written := document(b, recent(elements))

	var into []twin
	reader := bytes.NewReader(written)
	dec := jsontext.NewDecoder(reader)

	read := func() {
		reader.Reset(written)
		dec.Reset(reader)

		if err := json.UnmarshalDecode(dec, &into); err != nil {
			b.Fatalf("decoding: %v", err)
		}
	}
	read()

	b.ReportAllocs()

	for b.Loop() {
		read()
	}
}

// The container written to a writer, which is the whole document as one call
// rather than as a buffer somebody else is holding.
//
// It costs what encoding costs plus a borrowed flush window, which is the
// price of not having to own a buffer. Measured because it is the call a
// caller writing to a file or a socket actually makes.
func BenchmarkJSONWriteTo(b *testing.B) {
	held := recent(elements)

	var out bytes.Buffer

	write := func() {
		out.Reset()

		if _, err := held.WriteTo(&out); err != nil {
			b.Fatalf("writing: %v", err)
		}
	}
	write()

	b.ReportAllocs()

	for b.Loop() {
		write()
	}
}
