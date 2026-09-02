package csv

import (
	"fmt"
	"go/types"
	"slices"
	"strconv"
	"strings"

	"github.com/okian/forge/plugin"
)

// tagKey is the struct tag key a column's name is read from.
const tagKey = "csv"

// The tags a hint tells an author to write, quoted the way they would write
// them.
//
// Assembled here rather than at each hint, because a struct tag inside a Go
// string is three quoting characters deep and the assembly is what a reader of
// the hint has to see through. Named once, the hint reads as the sentence it is.
const (
	tagOmit   = "`" + tagKey + `:"-"` + "`"
	tagRename = "`" + tagKey + `:"<column>"` + "`"
)

// The halves of the text codec a field with no form of its own is written
// through.
const (
	textMarshalMethod   = "MarshalText"
	textUnmarshalMethod = "UnmarshalText"
)

// form says how one field's value becomes a cell of text and comes back.
type form uint8

const (
	// formInvalid is a field with no text form, which is a refusal rather than
	// a column.
	formInvalid form = iota

	formString
	formBool
	formInt
	formUint
	formFloat

	// formText is a field whose type carries MarshalText and UnmarshalText,
	// whether its author wrote them or a layer of this run is about to.
	formText
)

// native names the type strconv answers with for a form, which is what decides
// whether a cell has to be converted on the way in and on the way out.
//
// A field spelled as its form's native type needs no conversion, and one
// spelled as anything else needs one in both directions: int64(v.ID) going out
// and int(held) coming back. Writing the conversion unconditionally would
// compile and would put string(v.Name) in front of every string in the file.
func (f form) native() string {
	switch f {
	case formString:
		return "string"
	case formBool:
		return "bool"
	case formInt:
		return "int64"
	case formUint:
		return "uint64"
	case formFloat:
		return "float64"
	default:
		return ""
	}
}

// column is one field of the subject as one cell of a record.
type column struct {
	// name is the cell the header carries, and field is the Go field the value
	// is read from and written to.
	name  string
	field string

	// form says how the value becomes text, and typ is how the field's own type
	// is spelled in the file being generated into.
	form form
	typ  string

	// bits is the width strconv is told to work in: 0 for a type whose width is
	// the platform's, 8 through 64 for one that says.
	bits int
}

// converts reports whether the cell's value has to be converted between the
// field's type and the one strconv works in.
func (c column) converts() bool { return c.typ != c.form.native() }

// blank reports whether the cell holds text somebody wrote rather than a
// number strconv rendered, and so could come out as nothing.
//
// Only two forms can. A number and a boolean are rendered by strconv, which
// never answers with nothing; a string is whatever the field holds, and a text
// codec answers with whatever it likes.
//
// Two callers ask it and they are asking different questions. [table.blank]
// wants to know whether a cell can be empty, and [table.text] whether one can
// hold a line ending. The answer is the same today because the same two forms
// carry arbitrary text; a form that could do one and not the other would want
// this split in two, and there is no such form.
func (c column) blank() bool { return c.form == formString || c.form == formText }

// table is the subject as a CSV table.
type table struct {
	// key is what the row codec is about, which is what keeps two declarations
	// over one subject from writing it into the package twice.
	key string

	// elem is how one element is spelled in the file being generated into, and
	// imports are what that spelling and every column's binds.
	elem    string
	imports []plugin.Import

	// subject is the element's own name, without the package the spelling above
	// may have to qualify it with.
	//
	// Two names for one type, because they are read in two places. A signature
	// needs the spelling, which is qualified wherever the subject is declared
	// somewhere else; a sentence needs the name, since "an other.Person" is a
	// phrase about a package rather than about a type — and the article in
	// front of it has to agree with the type either way.
	subject string

	// encode and decode name the two functions one row goes through.
	encode string
	decode string

	// columns are the cells of a record, in the order they are written.
	columns []column
}

// blank returns the column whose emptiness would cost a row, and whether the
// table has one.
//
// One shape of table has this problem and it is worth spelling out, because it
// is silent. A record of one empty cell is written as a blank line — there is
// no delimiter to write, and a lone empty field is not quoted — and a reader
// discards a blank line before it counts fields against a record. So the row
// goes out and does not come back, and nothing on either side reports it.
//
// A record of two cells cannot do this: two empty cells are written as a comma,
// which is a record. Neither can a single column that strconv renders, since a
// number and a boolean are never nothing. What is left is a table of exactly
// one column holding a string or a text form, and the writer refuses the value
// rather than losing the row.
//
// Refused at the value rather than at the declaration, because the declaration
// is fine: a one-column table of names round-trips perfectly until one of the
// names is empty. Refusing the shape outright would take a working table away
// from everyone who never stores an empty one.
func (t table) blank() (column, bool) {
	if len(t.columns) != 1 || !t.columns[0].blank() {
		return column{}, false
	}

	return t.columns[0], true
}

// text reports whether any cell holds text a person wrote rather than a number
// strconv rendered.
//
// What it decides is a sentence rather than any code. Such a cell can hold a
// line ending, and a CRLF inside one comes back as an LF — the reader's own
// rule and not something a layer written over it can change — so the generated
// method says so where a table has a cell that could contain one, and says
// nothing where every cell is a number.
func (t table) text() bool {
	return slices.ContainsFunc(t.columns, column.blank)
}

// codeNoTextForm reports a field a cell cannot hold.
var codeNoTextForm = plugin.Register(6101, "a field has no text form, and a CSV cell is text")

// codeNoColumns reports a subject with nothing to tabulate.
var codeNoColumns = plugin.Register(6102, "a CSV document needs at least one column")

// codeTwoColumnsOneName reports two fields asking for one column.
var codeTwoColumnsOneName = plugin.Register(6103, "two fields want one CSV column")

// tabulated works out the table a subject makes, or refuses the subject.
//
// Every field is looked at before anything is reported, so an author with three
// fields that cannot be tabulated learns all three in one run. They arrive as
// one diagnostic rather than three, because they are one decision — the subject
// is not a table — and three reports about one decision read as three problems
// to solve separately.
func tabulated(ctx *plugin.Context) (table, error) {
	held := ctx.Model.Subject

	if why, cannot := plugin.Unnameable(held.Type(), ctx.Model.Pkg.PkgPath); cannot {
		return table{}, fmt.Errorf("csv: %s cannot be tabulated: %s", ctx.Model.Name, why)
	}

	spelled := plugin.Spell(held.Type(), ctx.Model.Pkg.PkgPath, ctx.Bound())
	out := table{
		key:     markerName + " " + plugin.TypeIdentity(held.Type()),
		elem:    spelled.Text,
		imports: spelled.Imports,
		subject: plugin.TypeString(held.Type()),
		encode:  "encode" + identifier(held.Type()) + "CSVInto",
		decode:  "decode" + identifier(held.Type()) + "CSVFrom",
	}

	var refused []string

	for _, field := range held.Fields {
		name, wanted := columnName(field)
		if !wanted {
			continue
		}

		one, err := tabulate(ctx, field, name)
		if err != nil {
			refused = append(refused, err.Error())
			continue
		}

		out.columns = append(out.columns, one)
		out.imports = append(out.imports, one.binds(ctx, field)...)
	}

	if len(refused) > 0 {
		return table{}, plugin.New(codeNoTextForm, ctx.Model.Pos,
			"%s cannot be written as CSV: %s", ctx.Model.Name, listed(refused)).
			WithHint("%s", hint(
				"give the type a "+textMarshalMethod+" and an "+textUnmarshalMethod,
				"or leave the field out with "+tagOmit))
	}

	if err := unique(ctx, out.columns); err != nil {
		return table{}, err
	}

	if len(out.columns) == 0 {
		return table{}, plugin.New(codeNoColumns, ctx.Model.Pos,
			"%s has no columns: every field of %s is unexported or left out",
			ctx.Model.Name, plugin.TypeString(held.Type())).
			WithHint("%s", hint("export a field", "or drop a "+tagOmit+" from one"))
	}

	return out, nil
}

// binds returns what a column's own spelling asks the file to import.
//
// A named type from another package is the case: a field written as time.Time
// is converted to nothing and called on directly, and a field written as
// somebody's Celsius is converted through its own name — which the file has to
// be able to write.
func (c column) binds(ctx *plugin.Context, field plugin.Field) []plugin.Import {
	if !c.converts() && c.form != formText {
		return nil
	}
	return plugin.Spell(field.Type.Type, ctx.Model.Pkg.PkgPath, ctx.Bound()).Imports
}

// tabulate returns the column one field makes, or says why it makes none.
//
// The error is a sentence rather than a diagnostic, because one field is not
// the whole answer: what is reported is that the subject is not a table, and
// the fields that made it one are the list inside that report.
func tabulate(ctx *plugin.Context, field plugin.Field, name string) (column, error) {
	held := column{name: name, field: field.Name}

	if carries(ctx, field.Type.Type) {
		held.form = formText
		held.typ = spelling(ctx, field)

		return held, nil
	}

	basic, is := types.Unalias(field.Type.Type).Underlying().(*types.Basic)
	if !is {
		return column{}, fmt.Errorf("%s (%s)", field.Name, field.Type)
	}

	held.form, held.bits = formOf(basic.Kind())
	if held.form == formInvalid {
		return column{}, fmt.Errorf("%s (%s)", field.Name, field.Type)
	}
	held.typ = spelling(ctx, field)

	return held, nil
}

// spelling returns how a field's own type is written in the file being
// generated into.
func spelling(ctx *plugin.Context, field plugin.Field) string {
	return plugin.Spell(field.Type.Type, ctx.Model.Pkg.PkgPath, ctx.Bound()).Text
}

// forms names the form and the width each basic kind a cell can hold is read
// at.
//
// A width of zero means the platform's, which is what strconv reads a bare int
// or uint as. Spelling it that way rather than as 64 is what keeps a generated
// file honest on a 32-bit build: a document holding a number no int can carry
// is refused there and accepted here, which is what the author's own code would
// do with it.
//
// A table rather than a switch, because it is a table: fourteen kinds, one
// answer each, and nothing to branch on. Nothing walks it, so its being a map
// leaks no order into anything.
//
// Five kinds are absent, each for a reason a column shows. A complex number has
// no agreed text form. A uintptr is an address, which means nothing in a file
// that outlives the process. An unsafe pointer is the same only more so. The
// untyped kinds cannot be a field's type at all.
var forms = map[types.BasicKind]struct {
	form form
	bits int
}{
	types.String: {form: formString},
	types.Bool:   {form: formBool},

	types.Int:   {form: formInt},
	types.Int8:  {form: formInt, bits: 8},
	types.Int16: {form: formInt, bits: 16},
	types.Int32: {form: formInt, bits: 32},
	types.Int64: {form: formInt, bits: 64},

	types.Uint:   {form: formUint},
	types.Uint8:  {form: formUint, bits: 8},
	types.Uint16: {form: formUint, bits: 16},
	types.Uint32: {form: formUint, bits: 32},
	types.Uint64: {form: formUint, bits: 64},

	types.Float32: {form: formFloat, bits: 32},
	types.Float64: {form: formFloat, bits: 64},
}

// formOf returns the form a basic kind has, and the width strconv works in.
//
// A kind with no row is [formInvalid] at a width of zero, which is the zero
// value of the row a missing key answers with — so a kind nobody wrote down is
// a field this layer refuses rather than one it guesses at.
func formOf(kind types.BasicKind) (form, int) {
	held := forms[kind]
	return held.form, held.bits
}

// carries reports whether a type has both halves of a text codec, so that its
// value goes into a cell as the text that codec writes.
//
// Both halves, because a cell written through one and read through nothing is a
// document that cannot be loaded back. A type with one half is left to whatever
// its underlying form is, which is what happened before the question was asked.
//
// Asked of what this run will write as well as of what the author wrote. What a
// neighbouring declaration's layers will write is in neither the package nor
// the model on a clean checkout and is in the package on every run after — so
// believing the package would put the number in the cell the first time and the
// member's name every time after, from one unchanged declaration.
func carries(ctx *plugin.Context, t types.Type) bool {
	return has(ctx, t, textMarshalMethod) && has(ctx, t, textUnmarshalMethod)
}

// has reports whether a type will carry a method: one its author declared, or
// one this run is about to write.
func has(ctx *plugin.Context, t types.Type, method string) bool {
	return ctx.Writes(t, method) || ctx.Authored(t, method)
}

// columnName returns the cell a field is written under, and whether the field
// is written at all.
//
// The tag wins where there is one, because a tag is what the rest of the
// ecosystem reads: a column named differently by forge than by the reader on
// the other end is a column that goes out under one name and is looked for
// under another. A dash omits the field, exactly as it does under
// encoding/json.
//
// An unexported field is not written. Generated code could read one from inside
// the subject's own package and could not from anywhere else, and a document
// whose columns depended on where the code was generated is worse than one that
// leaves the field out everywhere.
func columnName(field plugin.Field) (string, bool) {
	if !field.Exported {
		return "", false
	}

	if tag, ok := field.Tag(tagKey); ok {
		if tag.Ignored {
			return "", false
		}
		if tag.Name != "" {
			return tag.Name, true
		}
	}

	return field.Name, true
}

// unique refuses two fields that would write one column.
//
// A header is what a reader matches a document against, so two columns of one
// name leave every value in the second reachable only by counting — which is
// exactly what a header exists to avoid. Renaming one of them here would be
// choosing on the author's behalf between two names they wrote deliberately.
func unique(ctx *plugin.Context, columns []column) error {
	seen := make(map[string]string, len(columns))

	for _, one := range columns {
		held, twice := seen[one.name]
		if !twice {
			seen[one.name] = one.field
			continue
		}

		return plugin.New(codeTwoColumnsOneName, ctx.Model.Pos,
			"%s writes %s and %s into one column named %q",
			ctx.Model.Name, held, one.field, one.name).
			WithHint("%s", hint(
				"rename one of them with "+tagRename,
				"or leave one out with "+tagOmit))
	}

	return nil
}

// listed joins names into the phrase a sentence can hold.
//
// An Oxford-free "a, b and c", because it reads as one clause in the middle of
// a diagnostic rather than as a list somebody has to parse.
func listed(names []string) string {
	switch len(names) {
	case 1:
		return names[0]
	case 2:
		return names[0] + " and " + names[1]
	default:
		return strings.Join(names[:len(names)-1], ", ") + " and " + names[len(names)-1]
	}
}

// identifier returns the name a type's pair of row functions is built from.
//
// Two types must never share one. The name is what the functions are declared
// as, and two types given one name is a package that does not compile — so a
// named type is spelled with its package, since two packages may each declare a
// Person, and an instantiation spells out its arguments, since Box[int] and
// Box[string] are two types and each wants a codec of its own.
//
// The argument spelling is what stops a supported subject from being
// unusable rather than what stops a wrong document. Forge catches the clash
// either way — two generated functions of one name is a diagnostic — but the
// hint it can offer is to rename one of them, and nobody can rename Box[int]
// and Box[string] apart.
//
// It reads as something a person would have written, because it appears in the
// output: a reader who wants to know what encodeDomainPersonCSVInto encodes
// should not have to look it up.
func identifier(t types.Type) string {
	switch held := types.Unalias(t).(type) {
	case *types.Basic:
		return plugin.Upper(held.Name())

	case *types.Named:
		return qualified(held)

	case *types.Pointer:
		return "PointerTo" + identifier(held.Elem())

	case *types.Slice:
		return "SliceOf" + identifier(held.Elem())

	case *types.Array:
		return "Array" + strconv.FormatInt(held.Len(), 10) + "Of" + identifier(held.Elem())

	case *types.Map:
		return "MapOf" + identifier(held.Key()) + "To" + identifier(held.Elem())

	default:
		// A subject is a named, non-pointer type — the rule that says so is
		// forge's, and it is checked before a layer is asked anything. The
		// classes above are reachable only as a subject's type arguments. A
		// spelling is still owed, because this is reached while assembling
		// names rather than while deciding whether to.
		return "Unnamed"
	}
}

// qualified names a defined type by its package, its own name, and whatever it
// was instantiated with.
//
// The package's name rather than its path: a path holds slashes and dots and
// would have to be folded into an identifier anyway, and the fold is what
// introduces collisions. Two of them are left: two packages of one name, and a
// generic whose arguments spell out as a name somebody else's type already has
// — Box[int] and a plain BoxInt beside it. Both are rare and neither is silent,
// because what they produce is two declarations of one name, which forge
// refuses and the compiler would refuse after it.
func qualified(named *types.Named) string {
	var out strings.Builder

	if obj := named.Obj(); obj != nil && obj.Pkg() != nil {
		out.WriteString(plugin.Upper(obj.Pkg().Name()))
	}
	out.WriteString(plugin.Upper(plugin.RefOf(named).Name))

	if args := named.TypeArgs(); args != nil {
		for one := range args.Types() {
			out.WriteString(identifier(one))
		}
	}

	return out.String()
}
