package csv

import (
	"fmt"
	"strconv"
	"strings"
)

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
// Looked up rather than built, because two fields can want one name: Camel
// lowers a whole word, so ID and Id are both id, and a body declaring idCell
// twice is a file this layer wrote and the compiler refused. [naming] allocates
// one per column, which also keeps them clear of a package of the same name.
func (c column) local(names locals) string { return names.cells[c.field] }

// held returns the field as the codec writes it.
func (c column) held(names locals) string { return names.value + "." + c.field }

// out returns the value the cell's text is written from, converted to the type
// strconv works in where the field is not already of that type.
func (c column) out(names locals) string {
	if !c.converts() {
		return c.held(names)
	}

	return c.form.native() + "(" + c.held(names) + ")"
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

	w.encoder(of, held.names)
	w.decoder(of, held)

	return w.String()
}

// encoder writes the function that turns one element into a record.
func (w *writer) encoder(of table, names locals) {
	w.doc(
		fmt.Sprintf("%s writes one %s into the record it is given, and returns the record "+
			"it wrote.", of.encode, of.elem),
		"The record is handed in and handed back rather than allocated here, so that one "+
			"buffer serves a whole document: a writer walking a million elements pays for "+
			"the row once.")
	w.line("func %s(%s []string, %s %s) ([]string, error) {", of.encode, names.record, names.value, of.elem)
	w.line("%s = %s[:0]", names.record, names.record)
	w.blank()

	for _, one := range of.columns {
		w.cellOut(of, one, names)
	}

	w.blank()
	w.line("return %s, nil", names.record)
	w.line("}")
	w.blank()
}

// cellOut writes one field into the record.
//
// A cell with a text codec behind it takes four lines and its own blank ones,
// because it can fail; every other form is one call and they read as the list
// of columns they are.
func (w *writer) cellOut(of table, one column, names locals) {
	if one.form == formText {
		w.blank()
		w.line("%s, %s := %s.%s()",
			one.local(names), names.err, one.held(names), textMarshalMethod)
		w.line("if %s != nil {", names.err)
		w.line("return %s, fmt.Errorf(%s, %s)", names.record, unwritable(of, one), names.err)
		w.line("}")
		w.line("%s = append(%s, string(%s))", names.record, names.record, one.local(names))
		w.blank()

		return
	}

	w.line("%s = append(%s, %s)", names.record, names.record, formatted(one, names))
}

// formatted returns the call that renders one cell's value as text.
func formatted(one column, names locals) string {
	switch one.form {
	case formString:
		return one.out(names)
	case formBool:
		return "strconv.FormatBool(" + one.out(names) + ")"
	case formInt:
		return "strconv.FormatInt(" + one.out(names) + ", 10)"
	case formUint:
		return "strconv.FormatUint(" + one.out(names) + ", 10)"
	case formFloat:
		// 'g' with a precision of -1 is the shortest text that reads back as
		// the same value, which is the only rendering a document can round-trip
		// through. A fixed precision would either lose the last digits of a
		// large number or pad a small one out with zeros nobody wrote.
		return "strconv.FormatFloat(" + one.out(names) + ", 'g', -1, " + strconv.Itoa(one.bits) + ")"
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
	names := held.names

	w.doc(
		fmt.Sprintf("%s reads one record into %s.", of.decode, of.article()),
		"Every cell is copied out, so the element does not hold on to the record: a reader "+
			"is free to hand the same one back on the next call, and one does.")
	w.line("func %s(%s []string) (%s, error) {", of.decode, names.record, of.elem)
	w.line("var %s %s", names.value, of.elem)
	w.blank()

	w.note("Checked rather than assumed, so that a caller who assembled the record itself " +
		"is answered rather than panicked at.")
	w.line("if len(%s) != %d {", names.record, held.columns)
	w.line("return %s, fmt.Errorf(%s, len(%s))", names.value,
		strconv.Quote(of.article()+" has "+plural(held.columns)+
			" and this record has %d"), names.record)
	w.line("}")
	w.blank()

	for at, one := range of.columns {
		w.cellIn(of, one, at, held.names)
	}

	w.blank()
	w.line("return %s, nil", names.value)
	w.line("}")
	w.blank()
}

// cellIn reads one cell of the record into its field.
func (w *writer) cellIn(of table, one column, at int, names locals) {
	cell := fmt.Sprintf("%s[%d]", names.record, at)

	switch one.form {
	case formString:
		w.line("%s = %s", one.held(names), one.in(cell))

	case formText:
		w.line("if %s := %s.%s([]byte(%s)); %s != nil {",
			names.err, one.held(names), textUnmarshalMethod, cell, names.err)
		w.line("return %s, fmt.Errorf(%s, %s)", names.value, unreadable(of, one), names.err)
		w.line("}")

	default:
		w.line("%s, %s := %s", one.local(names), names.err, scanned(one, cell))
		w.line("if %s != nil {", names.err)
		w.line("return %s, fmt.Errorf(%s, %s)", names.value, unreadable(of, one), names.err)
		w.line("}")
		w.line("%s = %s", one.held(names), one.in(one.local(names)))
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
