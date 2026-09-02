package ledger_test

import (
	"bytes"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/okian/forge/x/csv/ledger"
)

// Everything here goes through the generated API rather than through the
// underlying slice, and that is a rule rather than a style.
//
// A declaration over a transport is written in a file under the marker build
// tag, so the type is forge's marker in one build and forge's output in the
// other. Indexing it or ranging over it directly compiles in one of those and
// not in the other, and a test that did would only ever be run one way round.
// Through the methods it compiles both ways, which is the whole point of the
// stub file — and reading as usage is what an example is for anyway.

// posted is a fixed instant, so that a document written twice is the same
// document.
var posted = time.Date(2024, time.March, 17, 9, 30, 0, 0, time.UTC)

// entries returns three entries covering every column form the subject has: a
// plain string, a defined integer, a defined string, a float, a narrow
// unsigned, a boolean and a time.
//
// The awkward values are deliberate. A payee holding the delimiter and one
// holding the quote are what the CSV escaping rules exist for, and a codec that
// wrote them plainly would produce a document that reads back as a different
// number of columns.
func entries() []ledger.Entry {
	return []ledger.Entry{
		{
			ID: 1, Payee: "Hydro", Amount: -4250, Currency: "CAD",
			Rate: 1, Revision: 0, Settled: true, Posted: posted,
		},
		{
			ID: 2, Payee: "Café, Bakery", Amount: -1899, Currency: "CAD",
			Rate: 0.7321, Revision: 2, Settled: false, Posted: posted.Add(time.Hour),
			Note: "the note is not a column",
		},
		{
			ID: 3, Payee: `He said "yes"`, Amount: 120000, Currency: "USD",
			Rate: 1.3654, Revision: 255, Settled: true, Posted: posted.Add(48 * time.Hour),
		},
	}
}

// columns returns the header the ledger's declarations write.
func columns() []string {
	var zero ledger.Entries
	return zero.CSVHeader()
}

// document returns the whole ledger as the document Entries writes.
func document(t *testing.T) string {
	t.Helper()

	var out bytes.Buffer

	held := ledger.NewEntries(entries()...)
	if _, err := held.WriteCSVTo(&out); err != nil {
		t.Fatalf("writing the ledger: %v", err)
	}

	return out.String()
}

// same reports whether two entries are the same, comparing the time by the
// instant it names rather than by its representation.
//
// A time carries a monotonic reading and a location, and neither survives a
// document: what RFC 3339 records is an instant and an offset. So the values
// that go out and the values that come back are equal in the sense that
// matters and not in the sense == asks about.
func same(a, b ledger.Entry) bool {
	if !a.Posted.Equal(b.Posted) {
		return false
	}

	a.Posted, b.Posted = time.Time{}, time.Time{}

	return a == b
}

// A document written out reads back as what went into it.
//
// The one claim a codec exists to make, and it is made over values rather than
// over bytes: what a caller cares about is that the ledger they wrote is the
// ledger they get, not that the document looks a particular way.
func TestADocumentReadsBackAsWhatWentIntoIt(t *testing.T) {
	held := ledger.NewEntries(entries()...)

	var out bytes.Buffer

	n, err := held.WriteCSVTo(&out)
	if err != nil {
		t.Fatalf("writing the ledger: %v", err)
	}
	if int(n) != out.Len() {
		t.Errorf("the writer reported %d bytes and wrote %d", n, out.Len())
	}

	document := out.String()

	var read ledger.Entries

	taken, err := read.ReadCSVFrom(&out)
	if err != nil {
		t.Fatalf("reading the ledger back: %v", err)
	}

	// What was taken from the reader rather than what the document occupies.
	// The reader buffers, so the two are only equal when the document is the
	// whole of what r had — which is what a caller is told to give it.
	if int(taken) != len(document) {
		t.Errorf("the reader reported %d bytes taken from a %d-byte document", taken, len(document))
	}

	got := slices.Collect(read.All())
	want := entries()

	if len(got) != len(want) {
		t.Fatalf("read %d entries, wrote %d", len(got), len(want))
	}

	for i := range want {
		// The note is not a column, so it comes back as the zero value however
		// it went out. Everything else has to survive.
		want[i].Note = ""

		if !same(got[i], want[i]) {
			t.Errorf("entry %d came back as %+v, want %+v", i, got[i], want[i])
		}
	}
}

// The document opens with the columns the type says it has.
func TestTheDocumentIsHeadedByItsColumns(t *testing.T) {
	want := strings.Join(columns(), ",")

	if got, _, _ := strings.Cut(document(t), "\n"); got != want {
		t.Errorf("the document opens %q, want %q", got, want)
	}
}

// The columns are the exported fields a tag did not rename or remove.
//
// Written out rather than derived, because deriving it would be
// re-implementing the rule the layer applies and the two would agree until one
// of them changed.
func TestWhichFieldsAreColumns(t *testing.T) {
	want := []string{"id", "payee", "amount", "currency", "rate", "revision", "settled", "posted"}

	if got := columns(); !slices.Equal(got, want) {
		t.Errorf("the columns are %q, want %q", got, want)
	}

	// A fresh slice on every call, so a caller cannot rewrite what the next one
	// is checked against.
	first := columns()
	first[0] = "rewritten"

	if columns()[0] == "rewritten" {
		t.Error("the header handed a caller the one every other caller reads")
	}
}

// A document whose columns arrived in another order is refused rather than read
// into the wrong fields.
//
// The one failure a header exists to prevent, and the reason it is compared
// rather than skipped: every value in the document would otherwise land in a
// field of the right type and the wrong meaning, and nothing would say so.
func TestADocumentWithTheWrongColumnsIsRefused(t *testing.T) {
	swapped := "payee,id,amount,currency,rate,revision,settled,posted\n" +
		"Hydro,1,-4250,CAD,1,0,true," + posted.Format(time.RFC3339) + "\n"

	var read ledger.Entries

	_, err := read.ReadCSVFrom(strings.NewReader(swapped))
	if err == nil {
		t.Fatal("a document with the columns swapped was read")
	}
	for _, want := range []string{"headed", "Entries"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal %q does not carry %q", err, want)
		}
	}
}

// A cell that is not the type its column holds is refused, and the refusal
// names the column.
//
// One case per form the reader has to parse, and one that is the interesting
// half: a revision of 256 is a number, and it is not a uint8. It is why the
// width is spelled out rather than parsed at sixty-four bits and converted — a
// reader that truncated would put 0 in the field and report nothing.
func TestACellOfTheWrongKindIsRefused(t *testing.T) {
	header := strings.Join(columns(), ",") + "\n"

	cases := map[string]struct {
		row    string
		column string
	}{
		"a number that is not one": {
			row:    "one,Hydro,-4250,CAD,1,0,true," + posted.Format(time.RFC3339),
			column: "id",
		},
		"a number too wide for its field": {
			row:    "1,Hydro,-4250,CAD,1,256,true," + posted.Format(time.RFC3339),
			column: "revision",
		},
		"a defined integer that is not a number": {
			row:    "1,Hydro,tuppence,CAD,1,0,true," + posted.Format(time.RFC3339),
			column: "amount",
		},
		"a boolean that is not one": {
			row:    "1,Hydro,-4250,CAD,1,0,perhaps," + posted.Format(time.RFC3339),
			column: "settled",
		},
		"a float that is not one": {
			row:    "1,Hydro,-4250,CAD,par,0,true," + posted.Format(time.RFC3339),
			column: "rate",
		},
		"a time that is not one": {
			row:    "1,Hydro,-4250,CAD,1,0,true,the seventeenth",
			column: "posted",
		},
	}

	for name, held := range cases {
		t.Run(name, func(t *testing.T) {
			var read ledger.Entries

			_, err := read.ReadCSVFrom(strings.NewReader(header + held.row + "\n"))
			if err == nil {
				t.Fatal("the row was read")
			}
			if !strings.Contains(err.Error(), held.column) {
				t.Errorf("the refusal does not name the %s column: %v", held.column, err)
			}
		})
	}
}

// A record with the wrong number of cells is refused.
//
// By the reader rather than by the row codec, which is what setting the count
// on the reader buys: the document is refused where it went wrong rather than
// where the missing cell was reached for.
func TestARecordOfTheWrongLengthIsRefused(t *testing.T) {
	header := strings.Join(columns(), ",") + "\n"

	var read ledger.Entries

	if _, err := read.ReadCSVFrom(strings.NewReader(header + "1,Hydro\n")); err == nil {
		t.Fatal("a record with two cells was read into a subject with eight columns")
	}
}

// Reading into a container replaces what was there, and an empty document
// empties it.
func TestReadingReplacesWhatWasThere(t *testing.T) {
	held := document(t)
	read := ledger.NewEntries(entries()...)

	for range 2 {
		if _, err := read.ReadCSVFrom(strings.NewReader(held)); err != nil {
			t.Fatalf("reading the ledger: %v", err)
		}
		if got, want := read.Len(), len(entries()); got != want {
			t.Fatalf("the container holds %d entries after a read, want %d", got, want)
		}
	}

	if _, err := read.ReadCSVFrom(strings.NewReader("")); err != nil {
		t.Fatalf("reading an empty document: %v", err)
	}
	if got := read.Len(); got != 0 {
		t.Errorf("an empty document left %d entries behind", got)
	}
}

// A document with no header is written and read by the declaration that asked
// for none.
func TestADocumentWithNoHeader(t *testing.T) {
	held := ledger.NewBare(entries()...)

	var out bytes.Buffer
	if _, err := held.WriteCSVTo(&out); err != nil {
		t.Fatalf("writing the ledger: %v", err)
	}

	// The first record is an element rather than the columns.
	if opening, _, _ := strings.Cut(out.String(), ","); opening != "1" {
		t.Errorf("the document opens with %q, want the first element's id", opening)
	}

	var read ledger.Bare
	if _, err := read.ReadCSVFrom(&out); err != nil {
		t.Fatalf("reading the ledger back: %v", err)
	}

	if got, want := read.Len(), held.Len(); got != want {
		t.Errorf("read %d entries, wrote %d", got, want)
	}

	// The columns still have an order, and the type still says what it is.
	if got := len(read.CSVHeader()); got != len(columns()) {
		t.Errorf("the type reports %d columns, want %d", got, len(columns()))
	}
}

// Two declarations over one subject read each other's rows.
//
// Which is the visible half of a claim about the output: the row codec belongs
// to the subject, so the package holds one of it however many declarations
// asked. What it would look like written twice is a package that does not
// compile, so this test running at all is the other half.
func TestTwoDeclarationsOverOneSubjectAgree(t *testing.T) {
	// Bare reads no header, so the document Entries wrote is offered to it
	// without its first line.
	_, rows, _ := strings.Cut(document(t), "\n")

	var read ledger.Bare
	if _, err := read.ReadCSVFrom(strings.NewReader(rows)); err != nil {
		t.Fatalf("one declaration could not read the other's rows: %v", err)
	}

	got := slices.Collect(read.All())
	want := entries()

	if len(got) != len(want) {
		t.Fatalf("read %d entries, wrote %d", len(got), len(want))
	}

	want[1].Note = ""
	if !same(got[1], want[1]) {
		t.Errorf("the second entry came back as %+v, want %+v", got[1], want[1])
	}
}

// A field only this package can read is no document's business, and the
// unexported one is left out of both directions.
func TestAnUnexportedFieldIsNoColumn(t *testing.T) {
	one := entries()[0].WithAudit()

	if !one.Audited() {
		t.Fatal("the fixture did not record what it was asked to")
	}

	var out bytes.Buffer
	if _, err := ledger.NewEntries(one).WriteCSVTo(&out); err != nil {
		t.Fatalf("writing the ledger: %v", err)
	}

	var read ledger.Entries
	if _, err := read.ReadCSVFrom(&out); err != nil {
		t.Fatalf("reading the ledger back: %v", err)
	}

	for held := range read.All() {
		if held.Audited() {
			t.Error("a document carried a field only its own package can read")
		}
	}
}

// A bounded container reads a document of its own format and refuses one it has
// no room for.
func TestABoundedContainer(t *testing.T) {
	room := ledger.NewRecent()
	room.AppendSeq(ledger.NewEntries(entries()...).All())

	if got, want := room.Cap(), 4; got != want {
		t.Errorf("the ring holds %d elements, want the %d it was declared with", got, want)
	}

	var piped bytes.Buffer
	if _, err := room.WriteCSVTo(&piped); err != nil {
		t.Fatalf("writing the ring: %v", err)
	}

	// The ring asked for a pipe, so its documents are not the comma-delimited
	// ones the other two declarations write. Two declarations, two formats,
	// which is what the option is for.
	if !strings.Contains(piped.String(), "|") {
		t.Errorf("the document is not delimited by a pipe:\n%s", piped.String())
	}

	read := ledger.NewRecent()
	if _, err := read.ReadCSVFrom(&piped); err != nil {
		t.Fatalf("reading the ring: %v", err)
	}
	if got, want := read.Len(), len(entries()); got != want {
		t.Errorf("the ring holds %d entries, want %d", got, want)
	}

	// A ring that was never constructed holds nothing, and is refused before a
	// byte is read rather than after the first row does not fit.
	var unbuilt ledger.Recent

	n, err := unbuilt.ReadCSVFrom(strings.NewReader("anything"))
	if err == nil {
		t.Fatal("a ring with no room read a document")
	}
	if n != 0 {
		t.Errorf("a refused read took %d bytes from the reader", n)
	}
	if !strings.Contains(err.Error(), "constructed") {
		t.Errorf("the refusal does not say what is missing: %v", err)
	}
}

// A ring given more rows than it holds keeps the last of them.
//
// Which is the ring's decision and not the reader's: the rows are handed over
// one at a time and what the container does with them is its own business, so a
// reader that had counted first would be second-guessing it.
func TestABoundedContainerDropsWhatItCannotHold(t *testing.T) {
	long := ledger.NewRecent()

	// Six entries into a ring of four, numbered so the survivors can be named.
	long.AppendSeq(func(yield func(ledger.Entry) bool) {
		held := append(entries(), entries()...)
		for i := range held {
			held[i].ID = i + 1
			if !yield(held[i]) {
				return
			}
		}
	})

	var out bytes.Buffer
	if _, err := long.WriteCSVTo(&out); err != nil {
		t.Fatalf("writing the ring: %v", err)
	}

	read := ledger.NewRecent()
	if _, err := read.ReadCSVFrom(&out); err != nil {
		t.Fatalf("reading the ring: %v", err)
	}
	if got := read.Len(); got != 4 {
		t.Fatalf("the ring holds %d entries, want its capacity of 4", got)
	}

	var ids []int
	for one := range read.All() {
		ids = append(ids, one.ID)
	}

	if want := []int{3, 4, 5, 6}; !slices.Equal(ids, want) {
		t.Errorf("the ring holds %v, want the last four written, %v", ids, want)
	}
}

// Writing to a sink that refuses reports it rather than losing it.
func TestAWriterThatRefusesIsReported(t *testing.T) {
	if _, err := ledger.NewEntries(entries()...).WriteCSVTo(refusing{}); err == nil {
		t.Fatal("writing to a sink that refuses everything succeeded")
	}
}

// refusing is a writer that takes nothing, which is what a full disk and a
// closed socket both look like from here.
type refusing struct{}

func (refusing) Write([]byte) (int, error) { return 0, errRefused }

// errRefused is what the sink above answers with, as a string that is an error
// so the fixture needs no dependency to refuse with.
var errRefused = refused("the sink took nothing")

type refused string

func (e refused) Error() string { return string(e) }
