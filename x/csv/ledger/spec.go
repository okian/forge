//go:build forgespec

package ledger

import "github.com/okian/forge"

// Entries is the whole ledger as a CSV table.
//
// The ordinary arrangement: a transport over a query surface over a slice. The
// storage is not written, because a refining layer with none beneath it has one
// filled in — so the underlying type of Entries is a real slice of Entry, and
// everything the language does to a slice can be done to it.
//
//forge:csv
//forge:collection sort=Payee index=ID
type Entries forge.Csv[forge.Collection[Entry]]

// Recent is the last four entries, delimited by a pipe.
//
// The bounded arrangement. A ring's methods take a pointer, because keeping the
// wrap-around right is not something a copy can do, so the document is written
// through a pointer as well. Reading a document with more than four rows leaves
// the last four, which is what a ring is for — and reading into one that was
// never constructed is refused rather than silently dropping every row.
//
//forge:csv comma=|
//forge:ring cap=4
type Recent forge.Csv[forge.Ring[Entry]]

// Bare is the ledger with no header row, over storage and nothing else.
//
// Two things at once, and both are about what a stack does not have to hold.
// The storage is named rather than inferred, so what is beneath the transport
// is a plain slice with no query surface over it — a document is written and
// read exactly as well without one. And the header is left out, for the two
// ends of a pipe that have already agreed on the columns.
//
// What leaving the header out gives up is the check: a document whose columns
// arrived in another order is read into the wrong fields and nothing says so,
// which is exactly what the header exists to prevent. CSVHeader is generated
// all the same, because the columns have an order whether or not it is written
// down.
//
//forge:csv header=false
type Bare forge.Csv[forge.Slice[Entry]]
