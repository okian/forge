package explain

import (
	"io"
	"strconv"
	"strings"
	"text/tabwriter"
)

// nothing is what a column shows when a step contributes none of what it names.
// An em dash rather than a blank, so that an empty cell reads as an answer
// rather than as a column somebody forgot to fill.
const nothing = "—"

// Text writes the resolution as a table.
//
// Three tables and not one. What a layer does, what it leaves for the layer
// above it, and what it will emit are three questions, and the answers are
// different lengths — a single row carrying all of them wraps on any terminal
// and stops being a table at all.
func (r Resolution) Text(w io.Writer) error {
	var b strings.Builder

	b.WriteString(r.Name)
	if r.Declaration != "" {
		b.WriteString(" ")
		b.WriteString(r.Declaration)
	}
	b.WriteString("\n")

	if where := r.where(); where != "" {
		b.WriteString("  ")
		b.WriteString(where)
		b.WriteString("\n")
	}

	r.resolution(&b)
	r.shapes(&b)
	r.methods(&b)

	_, err := io.WriteString(w, b.String())
	return err
}

// where says which file the declaration was written in and in what form.
func (r Resolution) where() string {
	var parts []string
	if r.Position != "" {
		parts = append(parts, r.Position)
	}
	if r.Package != "" {
		parts = append(parts, "package "+r.Package)
	}
	// Only a form the declaration really has. The zero value spells itself
	// "invalid", which is a true answer to a question nobody asked and reads as
	// a complaint about a declaration that is fine.
	if r.Form.Valid() {
		parts = append(parts, r.Form.String()+" form")
	}
	return strings.Join(parts, " · ")
}

// resolution writes what each step contributes, in the order resolution
// reaches them.
func (r Resolution) resolution(b *strings.Builder) {
	b.WriteString("\n")

	table(b, []string{"Step", "Layer", "Kind", "Effect"}, func(rows func(...string)) {
		for _, step := range r.Steps {
			rows(strconv.Itoa(step.Number), step.Name, kindOf(step), step.Effect)
		}
	})
}

// shapes writes what each step leaves for the step above it.
func (r Resolution) shapes(b *strings.Builder) {
	b.WriteString("\nShape\n")

	table(b, []string{"Step", "Layer", "Adds", "Masks", "Exposes"}, func(rows func(...string)) {
		for _, step := range r.Steps {
			rows(strconv.Itoa(step.Number), step.Name,
				list(step.Adds), list(step.Masks), list(step.Shape))
		}
	})
}

// methods writes what each step will emit and what it takes away.
//
// Every step gets a row, including the ones that emit nothing. Leaving those
// out would read as a stack whose other steps are the only ones there are, and
// it would hide the difference this table exists to show: a step that emits
// nothing and a step whose methods are not written yet are not the same claim.
//
// Headed with the declared type's name, because a method on the subject is not
// in it. An element layer attaches to the subject rather than to the container,
// where the layer above cannot build on what it wrote, so a log value and a
// codec for the subject's own fields land on a type this table is not about.
// Without the name a reader takes those rows for layers that write nothing,
// which is the opposite of true.
//
// A row can be empty for a second reason, and the heading does not tell them
// apart. What a layer contributes here is what it puts on its surface, and a
// layer is asked for one while the stack is still being composed — so one whose
// methods depend on what ends up above it cannot answer yet, and says nothing
// rather than promising a method a decorator may take away. The codec's methods
// on a container are the case: they exist and they are not listed. The layer
// says so where it declines to describe them.
//
// So the heading is worth what it says and no more. It stops the table being
// read as everything the stack writes; it does not make the table complete, and
// completing it needs composition to settle a shape rather than build one in a
// single pass.
func (r Resolution) methods(b *strings.Builder) {
	b.WriteString("\nMethods on " + r.Name + "\n")

	table(b, []string{"Step", "Layer", "Emits", "Withdraws"}, func(rows func(...string)) {
		for _, step := range r.Steps {
			rows(strconv.Itoa(step.Number), step.Name, emits(step), list(step.Withdraws))
		}
	})
}

// emits says what a step will contribute, telling nothing from not yet.
func emits(step Step) string {
	if len(step.Methods) > 0 {
		return strings.Join(step.Methods, ", ")
	}
	if step.Pending {
		return pending
	}
	return nothing
}

// kindOf names a step's kind, saying so when the layer is one this release does
// not ship.
func kindOf(step Step) string {
	kind := step.Kind.String()
	if step.Staged {
		kind += " (staged)"
	}
	return kind
}

// oneLine folds a cell onto one line.
//
// A layer's summary is its own text, and a third-party layer may write anything
// in it. A tab in one would open a column nobody declared and shift every row
// after it; a newline would end the row halfway through. Neither is worth
// letting a layer do to the table that describes it.
func oneLine(cell string) string {
	if !strings.ContainsAny(cell, "\t\n\r\v\f") {
		return cell
	}
	return strings.Join(strings.FieldsFunc(cell, func(r rune) bool {
		return r == '\t' || r == '\n' || r == '\r' || r == '\v' || r == '\f'
	}), " ")
}

// list renders a set of names, or the mark that says there were none.
func list(of []string) string {
	if len(of) == 0 {
		return nothing
	}
	return strings.Join(of, ", ")
}

// table writes aligned columns under a heading row.
//
// Aligned by the widest cell rather than to a fixed width, since a layer's name
// and a capability list are both as long as somebody made them, and a column
// sized for today's catalog is one that misaligns the first time a layer is
// added.
func table(b *strings.Builder, heading []string, body func(row func(...string))) {
	w := tabwriter.NewWriter(b, 0, 0, 2, ' ', 0)

	// Writes to a tabwriter over a strings.Builder, neither of which can fail:
	// the builder's Write never returns an error, and the writer holds
	// everything until Flush. The errors are dropped here rather than carried
	// through a report that has nowhere to put them.
	row := func(cells ...string) {
		flat := make([]string, len(cells))
		for i, cell := range cells {
			flat[i] = oneLine(cell)
		}
		_, _ = io.WriteString(w, strings.Join(flat, "\t")+"\n")
	}

	row(heading...)
	body(row)

	// The writer holds every row until it can measure them, so a report that
	// did not flush would be a report with nothing in it.
	_ = w.Flush()
}
