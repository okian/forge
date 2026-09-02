package csv

import (
	"strings"
	"unicode"

	"github.com/okian/forge/plugin"
)

// locals are the identifiers the generated bodies bind.
//
// Allocated rather than written down, and the reason is shadowing rather than
// uniqueness. A body that binds `record` and also has to spell a subject called
// record writes `record.Person`, or `var v record`, inside a scope where record
// is a variable — which this layer generated without complaint and the compiler
// then refused, in a file the author cannot edit.
//
// What a layer cannot know in advance is everything the subject brings with it:
// the package it is declared in where that is somewhere else, and the type
// names themselves where it is not. So these are asked for against those names
// rather than chosen. See [naming] for what the set is seeded with, which is
// the half that decides whether this works.
//
// Only the names bound in a body that also spells a type need this. A parameter
// of the counting streams, or the yield a sequence is handed, is bound too and
// is not allocated: those bodies name no type, and a parameter's scope excludes
// its own signature, so `func(yield func(yield.Person) bool)` is legal Go.
//
// One set for the whole unit rather than one per function body, which is what
// [plugin.Locals] describes. It over-constrains slightly — a name taken in one
// body is avoided in all of them — and what it buys is one spelling per idea
// across the file, which is what a reader would want anyway.
type locals struct {
	// receiver is what a method calls the container, and value one of its
	// elements.
	receiver string
	value    string

	// record is the slice of cells one row goes through.
	record string

	// cells are the per-column variables a cell's text is read into, keyed by
	// the field each belongs to. Allocated like the rest rather than built from
	// the field name, because two fields whose camel forms are one word — ID
	// and Id are both id — would otherwise be declared twice in one scope.
	cells map[string]string

	// err is the error a checked call binds, and failed what the walk could not
	// report for itself.
	err    string
	failed string

	// sink and source are the caller's own streams, counted the counting one
	// wrapped around them, and out and in the CSV writer and reader themselves.
	sink    string
	source  string
	counted string
	out     string
	in      string

	// header is the row read off a document and want the one it is compared
	// against.
	header string
	want   string

	// refused is what a container that can say no answers with, and held one
	// element read out of a record.
	refused string
	held    string
}

// naming allocates the identifiers a unit's bodies bind, out of the way of
// every name those bodies also have to spell.
//
// Seeded with two sets, and the second is the one that is easy to forget. The
// packages the file imports cover a subject declared somewhere else. A subject
// declared in the package being generated into imports nothing, so its name
// arrives only through the spellings — which is why every identifier in the
// element's spelling and in each column's type is taken as well, type arguments
// included: `Box[record]` needs record reserved as much as a bare record does.
//
// Split on everything that cannot be part of an identifier, so a qualified
// spelling contributes both halves. Reserving the package half of other.Kind
// costs nothing and reserving the type half is the point.
//
// The names asked for are the ones a reader would have written. What comes back
// is those, unless something else already holds one — in which case the one
// that moves is this layer's local, numbered from two, and the subject keeps
// the spelling its author gave it. The other direction would be worse: it would
// leave the author's own code naming something forge had renamed underneath it.
func naming(bound []plugin.Import, of table) locals {
	block := plugin.Locals(taken(bound, of)...)

	held := locals{
		receiver: block.Declare("c"),
		value:    block.Declare("v"),
		record:   block.Declare("record"),
		err:      block.Declare("err"),
		failed:   block.Declare("failed"),
		sink:     block.Declare("w"),
		source:   block.Declare("r"),
		counted:  block.Declare("counted"),
		out:      block.Declare("out"),
		in:       block.Declare("in"),
		header:   block.Declare("header"),
		want:     block.Declare("want"),
		refused:  block.Declare("refused"),
		held:     block.Declare("held"),
		cells:    make(map[string]string, len(of.columns)),
	}

	// In column order, so that the same table allocates the same names on every
	// run. A map walked here would write a different file each time.
	//
	// After the field, so that a reader of the generated code can see which
	// column went wrong without counting them. Suffixed, so that a field named
	// Range or Type does not produce a variable named after a keyword.
	for _, one := range of.columns {
		held.cells[one.field] = block.Declare(plugin.Camel(one.field) + "Cell")
	}

	return held
}

// taken returns every name the unit's bodies have to spell, which is the set
// the locals are allocated around.
func taken(bound []plugin.Import, of table) []string {
	held := make([]string, 0, len(bound)+len(of.columns)+4)

	for _, one := range bound {
		held = append(held, one.Name)
	}

	// The two helper names as well. Nothing binds them, so a collision would be
	// a call to a variable rather than a shadowed type, but the fix is the same
	// and it is one line.
	held = append(held, of.encode, of.decode)

	held = append(held, identifiers(of.elem)...)
	for _, one := range of.columns {
		held = append(held, identifiers(one.typ)...)
	}

	return held
}

// identifiers returns the words in a type's spelling that could be a name.
//
// A spelling is not parsed, only split: what is wanted is every identifier it
// mentions, and a package qualifier, a type argument and the type itself all
// read the same way once the punctuation is gone. Over-reserving is free here
// and under-reserving is a file that does not compile.
func identifiers(spelling string) []string {
	return strings.FieldsFunc(spelling, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_'
	})
}
