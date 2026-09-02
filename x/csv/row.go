package csv

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/okian/forge/plugin"
)

// recordVar is what the row codec calls the record it works on.
const recordVar = "record"

// headings returns the columns' names, in the order they are written.
func (t table) headings() []string {
	out := make([]string, len(t.columns))
	for i, one := range t.columns {
		out[i] = one.name
	}

	return out
}

// local names the variable one cell's text is read into or written out of.
//
// After the field, so that a reader of the generated code can see which column
// went wrong without counting them. Suffixed, so that it cannot be the name the
// codec already uses for the record, the element or the error — and so that a
// field named Range or Type does not produce a variable named after a keyword.
func (c column) local() string { return plugin.Camel(c.field) + "Cell" }

// held returns the field as the codec writes it.
func (c column) held() string { return valueVar + "." + c.field }

// out returns the value the cell's text is written from, converted to the type
// strconv works in where the field is not already of that type.
func (c column) out() string {
	if !c.converts() {
		return c.held()
	}

	return c.form.native() + "(" + c.held() + ")"
}

// in returns the text's value as the field takes it, converted back where the
// field is not of the type strconv answered with.
func (c column) in(from string) string {
	if !c.converts() {
		return from
	}

	return c.typ + "(" + from + ")"
}

// rowCodec assembles the two functions one row goes through.
//
// They are the subject's rather than the declaration's: which columns a subject
// has and how each becomes text is decided by its fields, so two declarations
// over one subject want the same pair and the package holds it once.
func rowCodec(of table, held stack) string {
	var w writer

	w.encoder(of)
	w.decoder(of, held)

	return w.String()
}

// encoder writes the function that turns one element into a record.
func (w *writer) encoder(of table) {
	w.doc(
		fmt.Sprintf("%s writes one %s into the record it is given, and returns the record "+
			"it wrote.", of.encode, of.elem),
		"The record is handed in and handed back rather than allocated here, so that one "+
			"buffer serves a whole document: a writer walking a million elements pays for "+
			"the row once.")
	w.line("func %s(%s []string, %s %s) ([]string, error) {", of.encode, recordVar, valueVar, of.elem)
	w.line("%s = %s[:0]", recordVar, recordVar)
	w.blank()

	for _, one := range of.columns {
		w.cellOut(of, one)
	}

	w.blank()
	w.line("return %s, nil", recordVar)
	w.line("}")
	w.blank()
}

// cellOut writes one field into the record.
//
// A cell with a text codec behind it takes four lines and its own blank ones,
// because it can fail; every other form is one call and they read as the list
// of columns they are.
func (w *writer) cellOut(of table, one column) {
	if one.form == formText {
		w.blank()
		w.line("%s, err := %s.%s()", one.local(), one.held(), textMarshalMethod)
		w.line("if err != nil {")
		w.line("return %s, fmt.Errorf(%s, err)", recordVar, unwritable(of, one))
		w.line("}")
		w.line("%s = append(%s, string(%s))", recordVar, recordVar, one.local())
		w.blank()

		return
	}

	w.line("%s = append(%s, %s)", recordVar, recordVar, formatted(one))
}

// formatted returns the call that renders one cell's value as text.
func formatted(one column) string {
	switch one.form {
	case formString:
		return one.out()
	case formBool:
		return "strconv.FormatBool(" + one.out() + ")"
	case formInt:
		return "strconv.FormatInt(" + one.out() + ", 10)"
	case formUint:
		return "strconv.FormatUint(" + one.out() + ", 10)"
	case formFloat:
		// 'g' with a precision of -1 is the shortest text that reads back as
		// the same value, which is the only rendering a document can round-trip
		// through. A fixed precision would either lose the last digits of a
		// large number or pad a small one out with zeros nobody wrote.
		return "strconv.FormatFloat(" + one.out() + ", 'g', -1, " + strconv.Itoa(one.bits) + ")"
	default:
		// Every form is above, and a column with none was refused before the
		// table was built. Reaching here is this file having drifted from the
		// one that decides forms, so it produces something that does not
		// compile rather than something that compiles and is wrong.
		return "nil"
	}
}

// decoder writes the function that turns one record into an element.
func (w *writer) decoder(of table, held stack) {
	w.doc(
		fmt.Sprintf("%s reads one record into %s.", of.decode, of.article()),
		"Every cell is copied out, so the element does not hold on to the record: a reader "+
			"is free to hand the same one back on the next call, and one does.")
	w.line("func %s(%s []string) (%s, error) {", of.decode, recordVar, of.elem)
	w.line("var %s %s", valueVar, of.elem)
	w.blank()

	w.note("Checked rather than assumed, so that a caller who assembled the record itself " +
		"is answered rather than panicked at.")
	w.line("if len(%s) != %d {", recordVar, held.columns)
	w.line("return %s, fmt.Errorf(%s, len(%s))", valueVar,
		strconv.Quote(of.article()+" has "+plural(held.columns)+
			" and this record has %d"), recordVar)
	w.line("}")
	w.blank()

	for at, one := range of.columns {
		w.cellIn(of, one, at)
	}

	w.blank()
	w.line("return %s, nil", valueVar)
	w.line("}")
	w.blank()
}

// cellIn reads one cell of the record into its field.
func (w *writer) cellIn(of table, one column, at int) {
	cell := fmt.Sprintf("%s[%d]", recordVar, at)

	switch one.form {
	case formString:
		w.line("%s = %s", one.held(), one.in(cell))

	case formText:
		w.line("if err := %s.%s([]byte(%s)); err != nil {", one.held(), textUnmarshalMethod, cell)
		w.line("return %s, fmt.Errorf(%s, err)", valueVar, unreadable(of, one))
		w.line("}")

	default:
		w.line("%s, err := %s", one.local(), scanned(one, cell))
		w.line("if err != nil {")
		w.line("return %s, fmt.Errorf(%s, err)", valueVar, unreadable(of, one))
		w.line("}")
		w.line("%s = %s", one.held(), one.in(one.local()))
	}

	w.blank()
}

// unreadable quotes the sentence a cell that could not be read is reported
// with, and unwritable the one a cell that could not be written is.
//
// Both name the column rather than its position, because the position is the
// one thing a reader of the message already has: what they do not have is which
// of the subject's fields it was.
func unreadable(of table, one column) string {
	return strconv.Quote("reading the " + one.name + " column of " + of.article() + ": %w")
}

func unwritable(of table, one column) string {
	return strconv.Quote("writing the " + one.name + " column of " + of.article() + ": %w")
}

// article names the element the way a sentence introduces one.
//
// The messages here are read far more often than they are written, and "a
// Entry" is the kind of thing that makes a reader wonder what else was not
// looked at. The rule is the written one rather than the spoken one — it goes
// by the letter and not by the sound — so a subject called Hour is introduced
// as "a Hour". Getting that right would mean a pronunciation dictionary, and
// the letter rule is right for every subject anybody has named.
//
// The subject's own name rather than its spelling, which is what makes the
// letter the right one to look at: a subject declared elsewhere is spelled
// other.Person, whose first letter belongs to the package. Reading that one
// would have the article agree with an import.
func (t table) article() string {
	if t.subject == "" {
		return "a value"
	}

	if strings.ContainsRune("AEIOUaeiou", rune(t.subject[0])) {
		return "an " + t.subject
	}

	return "a " + t.subject
}

// plural names a number of columns, in the plural it deserves.
//
// A one-column table is a real table and its messages are read by whoever
// wrote it. "has 1 columns" is the same class of blemish [table.article]
// exists to avoid, arrived at from the other side.
func plural(n int) string {
	if n == 1 {
		return "1 column"
	}

	return strconv.Itoa(n) + " columns"
}

// scanned returns the call that reads one cell's text back as a value.
func scanned(one column, cell string) string {
	switch one.form {
	case formBool:
		return "strconv.ParseBool(" + cell + ")"
	case formInt:
		return "strconv.ParseInt(" + cell + ", 10, " + strconv.Itoa(one.bits) + ")"
	case formUint:
		return "strconv.ParseUint(" + cell + ", 10, " + strconv.Itoa(one.bits) + ")"
	case formFloat:
		return "strconv.ParseFloat(" + cell + ", " + strconv.Itoa(one.bits) + ")"
	default:
		// As in [formatted]: a form with no parse is one this file has stopped
		// agreeing with the one that decides forms about.
		return "nil, nil"
	}
}
