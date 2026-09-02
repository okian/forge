// Package ledger is the worked example: a subject, three declarations over it,
// and the files this layer wrote from them, committed beside the source the way
// they are meant to be.
//
// Read [Entry] first. It is an ordinary struct written without regard for what
// a layer will make of it, and every column of the documents this package
// writes comes from one of its fields.
//
// The declarations are in a file under the forgespec build tag, because the
// type each of them is written as is a marker rather than a representation.
// What they ask for is:
//
//   - Entries — the whole ledger as a table over a plain slice, with the
//     collection's query surface beneath it. The ordinary case.
//   - Recent — the last four entries and no more, over a ring buffer, and
//     delimited by a pipe rather than a comma. The bounded case, whose methods
//     take a pointer because a ring's do.
//   - Bare — a table with no header row, for the two ends of a pipe that have
//     already agreed on the columns. The case that shows what the header buys
//     by leaving it out.
//
// The tests beside them read as usage: a document written, read back, and
// compared against what went in.
//
// Regenerate with the module's own binary, which is what an author of a layer
// runs and what forge's own would report as pending:
//
//	go run ./cmd/forge-csv generate ./ledger
package ledger

import "time"

// Entry is one line of a ledger.
//
// Flat, which is what a CSV row is. A field holding a struct of its own would
// have no cell to go in, and this layer refuses that rather than flattening it
// on the author's behalf — a column named amount.value is a column somebody
// decided on, and it is not the generator's decision to make.
type Entry struct {
	// ID identifies the entry. An int rather than an int64, so that the
	// generated reader is held to what an int can carry on the machine it was
	// built for rather than to what this machine could.
	ID int `csv:"id"`

	// Payee is who the entry was to. A plain string, which is the one form a
	// cell needs no conversion for.
	Payee string `csv:"payee"`

	// Amount is what the entry moved, in the smallest unit of its currency.
	// Its type is defined over an integer, so the cell is the number and the
	// field is converted on the way in and out.
	Amount Cents `csv:"amount"`

	// Currency is what the amount is in. Defined over a string, which is the
	// case a conversion is written for even though no parsing is.
	Currency Currency `csv:"currency"`

	// Rate is what the amount was converted at. A float, written as the
	// shortest text that reads back as the same value.
	Rate float64 `csv:"rate"`

	// Revision counts the corrections. An unsigned integer, and a narrow one,
	// so the generated reader refuses a document claiming a revision no uint8
	// holds.
	Revision uint8 `csv:"revision"`

	// Settled records whether the money arrived.
	Settled bool `csv:"settled"`

	// Posted is when the entry was made. It has no form of its own that a cell
	// could hold, and it carries a text codec — so the cell is what that codec
	// writes, which for a time is RFC 3339.
	Posted time.Time `csv:"posted"`

	// Note is for a person and not for a document, so it is left out of both.
	Note string `csv:"-"`

	// audited records whether somebody checked the entry, and is unexported on
	// purpose: a column is written from outside this package as readily as from
	// inside it, so a field only this package can read is one no document could
	// carry. It is left out without a word rather than refused.
	audited bool
}

// Cents is an amount in the smallest unit of its currency.
type Cents int64

// Currency is an ISO 4217 code.
type Currency string

// Audited reports whether somebody checked the entry.
func (e Entry) Audited() bool { return e.audited }

// WithAudit returns the entry with somebody's check recorded.
//
// A copy rather than a mutation, so that both of the methods here take a value.
// A type with one of each is a type whose method set depends on how a caller
// happens to be holding it — the pointer's holds both and the value's holds
// only its own — and a subject a generator puts methods on has no business
// carrying that question.
func (e Entry) WithAudit() Entry {
	e.audited = true
	return e
}
