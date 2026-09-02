package csv

import (
	"go/token"
	"go/types"
	"strings"
	"testing"

	"github.com/okian/forge/plugin"
)

// asking builds the context one option is read from.
//
// The two fields a layer reading an option touches, and no more: what forge
// hands a layer holds a loaded package and a composed stack, and neither is
// needed to answer a question about a word somebody wrote on a comment.
func asking(options ...plugin.DirectiveOption) *plugin.Context {
	return &plugin.Context{
		Model: &plugin.Model{
			Name: "Rows",
			Pos:  token.Position{Filename: "spec.go", Line: 7, Column: 6},
		},
		Options: plugin.Options{Layer: markerName, Entries: options},
	}
}

// option builds one option as a directive carries it.
func option(key, value string) plugin.DirectiveOption {
	return plugin.DirectiveOption{Key: key, Value: value}
}

// The delimiter is one character, and everything else is refused where the
// author can still change it.
//
// encoding/csv takes a rune and refuses four of them at run time. A generated
// file the author cannot edit is the worst place to find that out, so the same
// four are refused here — against the declaration, with the option to change
// named in the hint.
func TestTheDelimiterIsOneCharacter(t *testing.T) {
	kept := map[string]string{
		"a comma by default":    "",
		"a semicolon":           ";",
		"a pipe":                "|",
		"a tab":                 "\t",
		"one that is not ASCII": "·",
	}

	for name, held := range kept {
		t.Run(name, func(t *testing.T) {
			var options []plugin.DirectiveOption
			if held != "" {
				options = append(options, option(optionComma, held))
			}

			got, err := delimiter(asking(options...))
			if err != nil {
				t.Fatalf("%q was refused as a delimiter: %v", held, err)
			}
			if !strings.HasPrefix(got, "'") || !strings.HasSuffix(got, "'") {
				t.Errorf("the delimiter is written as %s, want a rune literal", got)
			}
		})
	}

	refused := map[string]string{
		"two characters":       "ab",
		"none at all":          " ", // one character, and not the one written
		"a quote":              `"`,
		"a carriage return":    "\r",
		"a newline":            "\n",
		"the replacement rune": "�",
	}

	for name, held := range refused {
		t.Run(name, func(t *testing.T) {
			_, err := delimiter(asking(option(optionComma, held)))
			if name == "none at all" {
				// A space is a legal delimiter, oddly enough, and is here to
				// keep the table honest about which of these is refused for
				// being reserved and which for being the wrong length.
				if err != nil {
					t.Fatalf("a space was refused as a delimiter: %v", err)
				}

				return
			}
			if err == nil {
				t.Fatalf("%q was accepted as a delimiter", held)
			}

			reported, is := plugin.From(err)
			if !is {
				t.Fatalf("the refusal is not a diagnostic: %v", err)
			}
			if reported.Code != codeDelimiter {
				t.Errorf("the refusal is %v, want %v", reported.Code, codeDelimiter)
			}
			if reported.Hint == "" {
				t.Error("the refusal says nothing to do about it")
			}
			if reported.Pos.Filename != "spec.go" {
				t.Errorf("the refusal points at %s rather than at the declaration", reported.Pos)
			}
		})
	}
}

// An escapable delimiter is written as a literal that compiles.
//
// An apostrophe and a backslash are the two characters a rune literal cannot
// hold plainly. Neither is a delimiter anybody wants and both are legal, so
// what is owed is a literal rather than a refusal.
func TestADelimiterThatHasToBeEscaped(t *testing.T) {
	for held, want := range map[string]string{"'": `'\''`, `\`: `'\\'`} {
		got, err := delimiter(asking(option(optionComma, held)))
		if err != nil {
			t.Fatalf("%q was refused as a delimiter: %v", held, err)
		}
		if got != want {
			t.Errorf("%q is written as %s, want %s", held, got, want)
		}
	}
}

// The header is written unless the declaration says otherwise, and the default
// is applied by the layer rather than assumed to have arrived.
func TestWhetherTheDocumentIsHeaded(t *testing.T) {
	cases := map[string]struct {
		options []plugin.DirectiveOption
		want    bool
	}{
		"unwritten":     {want: true},
		"written true":  {options: []plugin.DirectiveOption{option(optionHeader, "true")}, want: true},
		"written false": {options: []plugin.DirectiveOption{option(optionHeader, "false")}},
	}

	for name, held := range cases {
		t.Run(name, func(t *testing.T) {
			if got := heading(asking(held.options...)); got != held.want {
				t.Errorf("the document is headed %v, want %v", got, held.want)
			}
		})
	}
}

// Every basic kind a cell can hold has a form, and the ones that cannot have
// none.
//
// The widths are the interesting half. A bare int is read at the platform's
// width rather than at sixty-four bits, so a document holding a number no int
// can carry is refused on a 32-bit build — which is what the author's own code
// would do with it.
func TestTheFormOfEachKind(t *testing.T) {
	cases := map[types.BasicKind]struct {
		form form
		bits int
	}{
		types.String:  {form: formString},
		types.Bool:    {form: formBool},
		types.Int:     {form: formInt},
		types.Int8:    {form: formInt, bits: 8},
		types.Int16:   {form: formInt, bits: 16},
		types.Int32:   {form: formInt, bits: 32},
		types.Int64:   {form: formInt, bits: 64},
		types.Uint:    {form: formUint},
		types.Uint8:   {form: formUint, bits: 8},
		types.Uint16:  {form: formUint, bits: 16},
		types.Uint32:  {form: formUint, bits: 32},
		types.Uint64:  {form: formUint, bits: 64},
		types.Float32: {form: formFloat, bits: 32},
		types.Float64: {form: formFloat, bits: 64},

		types.Complex64:     {},
		types.Complex128:    {},
		types.Uintptr:       {},
		types.UnsafePointer: {},
		types.Invalid:       {},
		types.UntypedNil:    {},
	}

	for kind, want := range cases {
		held, bits := formOf(kind)
		if held != want.form {
			t.Errorf("%v has the form %d, want %d", kind, held, want.form)
		}
		if bits != want.bits {
			t.Errorf("%v is read at %d bits, want %d", kind, bits, want.bits)
		}
	}
}

// Every form strconv answers for names the type it answers with, and the one
// that goes through a text codec names none.
//
// It is what decides whether a cell is converted on the way in and out, so a
// form that named the wrong type would produce a file that does not compile
// and one that named none would produce a conversion in front of every string.
func TestWhatEachFormConvertsThrough(t *testing.T) {
	want := map[form]string{
		formString:  "string",
		formBool:    "bool",
		formInt:     "int64",
		formUint:    "uint64",
		formFloat:   "float64",
		formText:    "",
		formInvalid: "",
	}

	for held, native := range want {
		if got := held.native(); got != native {
			t.Errorf("form %d converts through %q, want %q", held, got, native)
		}
	}
}

// A column is converted only where the field is not already of the type
// strconv works in.
func TestWhichColumnsConvert(t *testing.T) {
	cases := map[string]struct {
		held     column
		converts bool
		out      string
		in       string
	}{
		"a plain string": {
			held: column{field: "Payee", form: formString, typ: "string"},
			out:  "v.Payee", in: "record[0]",
		},
		"a defined string": {
			held:     column{field: "Currency", form: formString, typ: "Currency"},
			converts: true, out: "string(v.Currency)", in: "Currency(record[0])",
		},
		"a plain int64": {
			held: column{field: "Amount", form: formInt, typ: "int64", bits: 64},
			out:  "v.Amount", in: "record[0]",
		},
		"a narrower int": {
			held:     column{field: "ID", form: formInt, typ: "int"},
			converts: true, out: "int64(v.ID)", in: "int(record[0])",
		},
	}

	for name, held := range cases {
		t.Run(name, func(t *testing.T) {
			if got := held.held.converts(); got != held.converts {
				t.Errorf("the column converts: %v, want %v", got, held.converts)
			}
			if got := held.held.out(); got != held.out {
				t.Errorf("the value goes out as %s, want %s", got, held.out)
			}
			if got := held.held.in("record[0]"); got != held.in {
				t.Errorf("the cell comes in as %s, want %s", got, held.in)
			}
		})
	}
}

// A cell's local is named after its field and cannot be the name the codec
// already uses.
func TestWhatACellsLocalIsCalled(t *testing.T) {
	for field, want := range map[string]string{
		"ID":       "idCell",
		"Payee":    "payeeCell",
		"JSONBlob": "jsonBlobCell",
		"Range":    "rangeCell",
		"Record":   "recordCell",
	} {
		if got := (column{field: field}).local(); got != want {
			t.Errorf("%s reads into %s, want %s", field, got, want)
		}
	}
}

// A field is a column under its tag, under its own name, or not at all.
func TestWhichFieldsAreColumns(t *testing.T) {
	cases := map[string]struct {
		field  plugin.Field
		name   string
		wanted bool
	}{
		"an exported field with no tag": {
			field:  plugin.Field{Name: "Payee", Exported: true},
			name:   "Payee",
			wanted: true,
		},
		"a field its tag renames": {
			field:  tagged("Payee", true, `csv:"payee"`),
			name:   "payee",
			wanted: true,
		},
		"a field its tag removes": {
			field: tagged("Note", true, `csv:"-"`),
		},
		"a field whose tag carries options only": {
			field:  tagged("Payee", true, `csv:",omitempty"`),
			name:   "Payee",
			wanted: true,
		},
		"a field tagged for somebody else": {
			field:  tagged("Payee", true, `json:"payee"`),
			name:   "Payee",
			wanted: true,
		},
		"an unexported field": {
			field: plugin.Field{Name: "audited"},
		},
	}

	for name, held := range cases {
		t.Run(name, func(t *testing.T) {
			got, wanted := columnName(held.field)
			if wanted != held.wanted {
				t.Fatalf("the field is a column: %v, want %v", wanted, held.wanted)
			}
			if got != held.name {
				t.Errorf("the column is named %q, want %q", got, held.name)
			}
		})
	}
}

// tagged builds a field carrying one struct tag.
func tagged(name string, exported bool, raw string) plugin.Field {
	parsed, problems := plugin.ParseTag(raw)
	if len(problems) > 0 {
		panic("csv: the test's own tag is malformed: " + raw)
	}

	return plugin.Field{Name: name, Exported: exported, Tags: parsed}
}

// A list of names reads as one clause however many there are.
func TestHowNamesAreListed(t *testing.T) {
	for want, held := range map[string][]string{
		"one":                      {"one"},
		"one and two":              {"one", "two"},
		"one, two and three":       {"one", "two", "three"},
		"one, two, three and four": {"one", "two", "three", "four"},
	} {
		if got := listed(held); got != want {
			t.Errorf("%q reads as %q, want %q", held, got, want)
		}
	}
}

// The element is introduced the way a sentence introduces one.
//
// From the subject's own name rather than from its spelling, which is the half
// worth pinning: a subject declared elsewhere is spelled other.Person, and an
// article agreeing with that agrees with an import.
func TestHowTheElementIsIntroduced(t *testing.T) {
	for subject, want := range map[string]string{
		"Entry":  "an Entry",
		"Person": "a Person",
		"Order":  "an Order",
		"":       "a value",
	} {
		if got := (table{subject: subject}).article(); got != want {
			t.Errorf("%q is introduced as %q, want %q", subject, got, want)
		}
	}

	// The spelling is not what is read, whatever it happens to start with.
	held := table{subject: "Person", elem: "other.Person"}
	if got := held.article(); got != "a Person" {
		t.Errorf("a subject from another package is introduced as %q, want %q", got, "a Person")
	}
}

// A number of columns reads as the plural it deserves.
func TestHowColumnsArePluralised(t *testing.T) {
	for n, want := range map[int]string{0: "0 columns", 1: "1 column", 8: "8 columns"} {
		if got := plural(n); got != want {
			t.Errorf("%d reads as %q, want %q", n, got, want)
		}
	}
}

// The header renders as the literal that declares it.
func TestHowTheHeaderIsRendered(t *testing.T) {
	got := quotedNames([]string{"id", `a "quoted" name`})
	want := `"id", "a \"quoted\" name"`

	if got != want {
		t.Errorf("the header renders as %s, want %s", got, want)
	}
}

// A form this file has stopped agreeing with produces something that does not
// compile rather than something that compiles and is wrong.
//
// It is unreachable: a column with no form was refused before the table was
// built. What is asserted is the direction of the failure, because the
// alternative — a plausible default — would put a wrong value in a cell and
// report nothing.
func TestAColumnWithNoFormProducesNothingThatCompiles(t *testing.T) {
	held := column{field: "Whatever", form: formInvalid}

	if got := formatted(held); got != "nil" {
		t.Errorf("a column with no form is written as %s, want something that does not compile", got)
	}
	if got := scanned(held, "record[0]"); got != "nil, nil" {
		t.Errorf("a column with no form is read as %s, want something that does not compile", got)
	}
}

// Two types never reach one pair of row functions.
//
// The name is what the functions are declared as, so two types given one name
// is a package that does not compile — and the case that used to produce one is
// two instantiations of a generic subject, whose arguments a name that stopped
// at the type would have dropped.
//
// Assembled from go/types rather than from a loaded package, because what is
// being checked is the naming and not the loading. A subject is always a named
// type; the composites here are reachable as its arguments.
func TestTwoTypesNeverReachOnePairOfFunctions(t *testing.T) {
	pkg := types.NewPackage("example.com/gen", "gen")

	box := func(args ...types.Type) *types.Named {
		param := types.NewTypeParam(types.NewTypeName(token.NoPos, pkg, "T", nil), types.Universe.Lookup("any").Type())
		named := types.NewNamed(types.NewTypeName(token.NoPos, pkg, "Box", nil), types.Typ[types.Int], nil)
		named.SetTypeParams([]*types.TypeParam{param})

		held, err := types.Instantiate(nil, named, args, false)
		if err != nil {
			t.Fatalf("instantiating Box: %v", err)
		}

		return held.(*types.Named)
	}

	held := map[string]types.Type{
		"an int":                 types.Typ[types.Int],
		"a string":               types.Typ[types.String],
		"Box[int]":               box(types.Typ[types.Int]),
		"Box[string]":            box(types.Typ[types.String]),
		"Box[[]int]":             box(types.NewSlice(types.Typ[types.Int])),
		"Box[*int]":              box(types.NewPointer(types.Typ[types.Int])),
		"Box[[2]int]":            box(types.NewArray(types.Typ[types.Int], 2)),
		"Box[[3]int]":            box(types.NewArray(types.Typ[types.Int], 3)),
		"Box[map[string]int]":    box(types.NewMap(types.Typ[types.String], types.Typ[types.Int])),
		"Box[map[int]string]":    box(types.NewMap(types.Typ[types.Int], types.Typ[types.String])),
		"Box[Box[int]]":          box(box(types.Typ[types.Int])),
		"a channel, which is no": types.NewChan(types.SendOnly, types.Typ[types.Int]),
	}

	seen := make(map[string]string, len(held))

	for name, one := range held {
		got := identifier(one)

		if before, twice := seen[got]; twice {
			t.Errorf("%s and %s both reach %s", before, name, got)
		}
		seen[got] = name
	}

	// A class with no spelling of its own is still owed one, because names are
	// assembled before anything decides whether to write them.
	if got := identifier(types.NewChan(types.SendOnly, types.Typ[types.Int])); got != "Unnamed" {
		t.Errorf("a type with no spelling is called %q, want Unnamed", got)
	}
}

// Source that is not Go is reported against the declaration rather than
// discovered in a file on disk.
func TestAssembledSourceThatDoesNotParse(t *testing.T) {
	_, err := parsed("package p\n\nfunc (", nil)
	if err == nil {
		t.Fatal("source that is not Go was accepted")
	}
	if !strings.Contains(err.Error(), "does not parse") {
		t.Errorf("the failure does not say what is wrong: %v", err)
	}
}
