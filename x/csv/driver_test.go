package csv_test

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/okian/forge/driver"
	"github.com/okian/forge/plugin"
	"github.com/okian/forge/x/csv"
)

// The subject and the declaration the end-to-end tests are written over.
//
// One of each, and small: what these check is that the stages agree, not what a
// column can hold — that is the worked example's job, and it does it over a
// subject with a column of every form.
const (
	subject = `// Person is the subject.
type Person struct {
	ID   int    ` + "`csv:\"id\"`" + `
	Name string ` + "`csv:\"name\"`" + `
}
`

	spec = markerImport + `// Rows is the subject as a CSV table.
//
//forge:csv
type Rows forge.Csv[forge.Collection[Person]]
`

	// markerImport opens a spec file that names one of forge's own markers,
	// which every declaration over this layer does: it claims forge's Csv
	// rather than declaring a marker of its own.
	markerImport = "import \"github.com/okian/forge\"\n\n"
)

// A layer forge does not ship is registered, listed, resolved, composed,
// generated for, and what it wrote compiles.
//
// The whole claim the published surface exists to make, and it is walked in one
// test rather than six because what is being checked is that the stages agree.
// A layer that listed and did not resolve, or resolved and generated nothing,
// would pass five tests out of six and be useless — and the failure worth
// catching is the seam, not the stage.
func TestTheLayerEndToEnd(t *testing.T) {
	held := catalog(t)
	root := module(t, fixture{name: "fixture", subject: subject, spec: spec})

	// Listed, with what it says about itself, and as a layer that generates
	// rather than as the placeholder it took the marker over from.
	out, status := running(t, held, "list")
	if status != 0 {
		t.Fatalf("listing the catalog exited %d:\n%s", status, out)
	}
	for _, want := range []string{"Csv", "transport", "header row"} {
		if !strings.Contains(out, want) {
			t.Errorf("the catalog does not carry %q:\n%s", want, out)
		}
	}
	if line := lineWith(out, "Csv"); strings.Contains(line, "staged") {
		t.Errorf("the marker is still listed as staged: %s", line)
	}

	// Resolved, and composed with the storage filled in beneath the collection.
	out, status = running(t, held, "-C", root, "explain", "-t", "Rows", ".")
	if status != 0 {
		t.Fatalf("explaining the declaration exited %d:\n%s", status, out)
	}
	for _, want := range []string{"Csv", "Collection", "Slice", "Encodable", "header row"} {
		if !strings.Contains(out, want) {
			t.Errorf("the explanation does not carry %q:\n%s", want, out)
		}
	}

	// Generated, and what was generated is what the layer wrote.
	if out, status := running(t, held, "-C", root, "generate", "."); status != 0 {
		t.Fatalf("generating exited %d:\n%s", status, out)
	}

	// Everything the package asked for is in the one file forge writes for it:
	// the declared type's methods, the row codec the subject earns, and what
	// the storage beneath contributed. It used to be three files, and what a
	// layer author has to know now is that there is one.
	written := read(t, filepath.Join(root, generated))
	for _, want := range []string{
		"func (c Rows) CSVHeader() []string",
		"func (c Rows) WriteCSVTo(w io.Writer) (int64, error)",
		"func (c *Rows) ReadCSVFrom(r io.Reader) (int64, error)",
		`return []string{"id", "name"}`,

		// The row codec, which is the subject's rather than this
		// declaration's — one copy of it however many declarations ask.
		"func encodeFixturePersonCSVInto(record []string, v Person) ([]string, error)",
		"func decodeFixturePersonCSVFrom(record []string) (Person, error)",

		// And forge's own storage layer, which is what composing with a layer
		// somebody added means.
		"func (s Rows) Len() int",
	} {
		if !strings.Contains(written, want) {
			t.Errorf("the generated file does not carry %q:\n%s", want, written)
		}
	}

	// Both halves of a spec-form declaration, so that a caller compiles whether
	// or not the spec file is in the build.
	if !strings.Contains(read(t, filepath.Join(root, stubs)), "WriteCSVTo") {
		t.Error("the build the declaration is absent from holds no matching API")
	}

	// Generating twice writes the same bytes, which is what a committed output
	// has to do or every run is a diff.
	first := written
	if out, status := running(t, held, "-C", root, "generate", "."); status != 0 {
		t.Fatalf("generating a second time exited %d:\n%s", status, out)
	}
	if again := read(t, filepath.Join(root, generated)); again != first {
		t.Error("generating twice from one declaration produced two files")
	}

	// And the freshness check agrees that what is on disk is what the
	// declaration asks for.
	if out, status := running(t, held, "-C", root, "check", "."); status != 0 {
		t.Errorf("the check verb refused what generation had just written, exiting %d:\n%s", status, out)
	}
}

// A binary that has not linked the layer says the work is pending rather than
// that the author erred.
//
// The other half of taking a published marker over, and the reason the marker
// is forge's rather than this layer's: the declaration in the fixture is the
// same one either way, so an author writes it against plain forge today and
// nothing in their tree changes when the layer arrives.
func TestWithoutTheLayerTheMarkerIsPending(t *testing.T) {
	root := module(t, fixture{name: "fixture", subject: subject, spec: spec})

	out, status := running(t, driver.Builtins(), "-C", root, "generate", ".")
	if status == 0 {
		t.Fatalf("a catalog with no CSV layer generated for a CSV declaration:\n%s", out)
	}
	if !strings.Contains(out, "FRG4900") {
		t.Errorf("the report does not say the layer's work is pending:\n%s", out)
	}
}

// A layer that generates may not take a marker over from another that does.
//
// Registering twice is the case, and it is what a binary linking two CSV layers
// would look like. Which of them the author meant is not a question the order
// of two calls should answer, so it is refused where somebody can still read
// the refusal.
func TestTheLayerCannotBeRegisteredTwice(t *testing.T) {
	held := catalog(t)

	if err := held.Register(csv.New()); err == nil {
		t.Fatal("the layer was registered over itself")
	}
}

// The layer's marker is one of forge's, so a report can tell the two apart by
// the code alone.
//
// Every code this layer reports is above forge's own range, which is what
// [plugin.Code.Ours] answers on and what says whose documentation to look in.
func TestTheLayersCodesAreItsOwn(t *testing.T) {
	held := catalog(t)
	root := module(t, fixture{
		name:    "fixture",
		subject: subject,
		spec: markerImport + `// Rows is delimited by something a document cannot be delimited by.
//
//forge:csv comma=;;
type Rows forge.Csv[forge.Collection[Person]]
`,
	})

	out, status := running(t, held, "-C", root, "generate", ".")
	if status == 0 {
		t.Fatalf("a two-character delimiter was accepted:\n%s", out)
	}

	code := reported(t, out)
	if code.Ours() {
		t.Errorf("FRG%d is in forge's own range, and it is this layer's", int(code))
	}
	if !strings.Contains(out, "hint:") {
		t.Errorf("the refusal says nothing to do about it:\n%s", out)
	}
}

// reported returns the one FRG code a report carries.
func reported(t *testing.T, out string) plugin.Code {
	t.Helper()

	held := codesIn(t, out)
	if len(held) != 1 {
		t.Fatalf("the report carries %d codes, want one:\n%s", len(held), out)
	}

	return held[0]
}

// codesIn returns every FRG code a report carries, in the order they appear.
//
// Read out of the rendered report rather than off a set of diagnostics, because
// the rendered report is what a caller of the driver gets: the surface a layer
// author is written against ends at the command line, and a test that reached
// past it would be checking something no author can see.
func codesIn(t *testing.T, out string) []plugin.Code {
	t.Helper()

	var held []plugin.Code

	for _, one := range strings.Fields(out) {
		digits, is := strings.CutPrefix(strings.TrimSuffix(one, ":"), "FRG")
		if !is {
			continue
		}

		number, err := strconv.Atoi(digits)
		if err != nil {
			t.Fatalf("the report carries %q, which is not a code: %v", one, err)
		}
		held = append(held, plugin.Code(number))
	}

	return held
}

// lineWith returns the line of a report holding a word, or the empty string.
func lineWith(out, word string) string {
	for _, one := range strings.Split(out, "\n") {
		if strings.Contains(one, word) {
			return one
		}
	}

	return ""
}

// A subject in another package is spelled with its package, and the file that
// spells it imports it.
//
// The case that catches the one mistake a layer assembling source is most
// likely to make. What a layer imports of its own it writes down in Binds; what
// the subject's own types come from is found by the spelling instead, and a
// layer that offered the file only the first set would write `other.Person`
// into a file importing nothing — a package that does not build, from a run
// that reported nothing wrong.
//
// The row codec is where it shows, because that is the half whose signature
// names the element. The declaration's own methods name it too, inside the
// sequence handed to the sink.
func TestASubjectInAnotherPackage(t *testing.T) {
	held := catalog(t)
	root := module(t,
		fixture{
			name: "other",
			dir:  "other",
			subject: `// Person is the subject, declared somewhere the declaration is not.
type Person struct {
	ID   int    ` + "`csv:\"id\"`" + `
	Kind Kind   ` + "`csv:\"kind\"`" + `
}

// Kind is a named string, so a cell of it is converted through its own name —
// which is the spelling that needs the import.
type Kind string
`,
		},
		fixture{
			name:    "fixture",
			subject: "// The declaration below is over a subject from elsewhere.\n",
			spec: `import (
	"github.com/okian/forge"

	"example.com/fixture/other"
)

//forge:csv
type Rows forge.Csv[forge.Collection[other.Person]]
`,
		},
	)

	if out, status := running(t, held, "-C", root, "generate", "./..."); status != 0 {
		t.Fatalf("generating for a subject in another package exited %d:\n%s", status, out)
	}

	written := read(t, filepath.Join(root, generated))

	for _, want := range []string{
		`"example.com/fixture/other"`,
		"v other.Person",
		"other.Kind(",
	} {
		if !strings.Contains(written, want) {
			t.Errorf("the generated file does not carry %q:\n%s", want, written)
		}
	}

	// And what was written compiles, which is the assertion the strings above
	// are only evidence for.
	building(t, root)
}

// A one-column table refuses the one value it could not read back.
//
// A record of one empty cell is written as a blank line, and a reader discards
// a blank line before it counts fields — so the row would go out and not come
// back, with nothing on either side reporting it. That is the shape of failure
// this repository is built not to have, so the writer refuses the value.
//
// The declaration is not refused, which is the other half. A one-column table
// of names round-trips perfectly until one of the names is empty, and taking
// the shape away from everybody would be answering a value's problem with a
// design's.
func TestAOneColumnTableRefusesAnEmptyCell(t *testing.T) {
	root := module(t, fixture{
		name: "fixture",
		subject: `// Tag is a subject with one column, which is the shape that cannot
// round-trip an empty value.
type Tag struct {
	Name string ` + "`csv:\"name\"`" + `
}
`,
		spec: markerImport + `//forge:csv
type Tags forge.Csv[forge.Collection[Tag]]
`,
	})

	if out, status := running(t, catalog(t), "-C", root, "generate", "."); status != 0 {
		t.Fatalf("a one-column table was refused at the declaration, exiting %d:\n%s", status, out)
	}

	written := read(t, filepath.Join(root, generated))
	for _, want := range []string{
		`if record[0] == ""`,
		"cannot write an empty name",
		"a reader would skip it",
	} {
		if !strings.Contains(written, want) {
			t.Errorf("the writer does not carry %q:\n%s", want, written)
		}
	}

	// And a table of two columns pays nothing for it, because two empty cells
	// are written as a delimiter and a delimiter is a record.
	pair := module(t, fixture{name: "fixture", subject: subject, spec: spec})
	if out, status := running(t, catalog(t), "-C", pair, "generate", "."); status != 0 {
		t.Fatalf("generating exited %d:\n%s", status, out)
	}
	if held := read(t, filepath.Join(pair, generated)); strings.Contains(held, `record[0] == ""`) {
		t.Error("a two-column table carries a check only a one-column table needs")
	}

	// What the generated code does rather than what it says. The assertions
	// above read the source, which would pass just as well if the check were
	// written and the rows lost anyway — so the fixture runs, writes both
	// shapes of document, and counts what comes back.
	write(t, filepath.Join(root, "tags_test.go"), oneColumn)
	behaves(t, root)
}

// oneColumn is the test the one-column fixture runs against its own generated
// code.
//
// Written into the fixture rather than here, because what is being checked is
// the behaviour of code this test only just produced: there is nothing in this
// module to call.
const oneColumn = `package fixture

import (
	"bytes"
	"strings"
	"testing"
)

// Every row a document holds comes back, and the row that could not have does
// not go out.
func TestEveryRowComesBack(t *testing.T) {
	held := NewTags(Tag{Name: "a"}, Tag{Name: "b"}, Tag{Name: "c"})

	var out bytes.Buffer
	if _, err := held.WriteCSVTo(&out); err != nil {
		t.Fatalf("writing three tags: %v", err)
	}

	var read Tags
	if _, err := read.ReadCSVFrom(&out); err != nil {
		t.Fatalf("reading them back: %v", err)
	}
	if got := read.Len(); got != 3 {
		t.Errorf("three rows went out and %d came back", got)
	}

	// The value that would otherwise be written as a blank line and skipped.
	empty := NewTags(Tag{Name: "a"}, Tag{Name: ""}, Tag{Name: "c"})

	var lost bytes.Buffer

	_, err := empty.WriteCSVTo(&lost)
	if err == nil {
		var back Tags
		if _, err := back.ReadCSVFrom(&lost); err != nil {
			t.Fatalf("reading back what was written: %v", err)
		}

		t.Fatalf("an empty cell was written; three rows went out and %d came back", back.Len())
	}
	if !strings.Contains(err.Error(), "name") {
		t.Errorf("the refusal does not name the column: %v", err)
	}
}
`

// behaves runs the fixture's own tests, which is what says generated code does
// what its source appears to say.
func behaves(t *testing.T, root string) {
	t.Helper()

	cmd := exec.CommandContext(t.Context(), "go", "test", "./...")
	cmd.Dir = root

	if out, err := cmd.CombinedOutput(); err != nil {
		t.Errorf("the generated code does not behave: %v\n%s", err, out)
	}
}

// A boolean option is read the way it was validated.
//
// Forge checks a boolean option with strconv.ParseBool, which takes 1, t, T,
// TRUE, 0, f, F and FALSE as well as the two words anybody writes. So a layer
// comparing against the word "false" would accept header=0 and then write a
// header into the one document that asked for none.
func TestEverySpellingOfABooleanOption(t *testing.T) {
	off := []string{"false", "False", "FALSE", "0", "f", "F"}
	on := []string{"true", "True", "TRUE", "1", "t", "T"}

	for _, held := range append(append([]string{}, off...), on...) {
		t.Run(held, func(t *testing.T) {
			root := module(t, fixture{
				name:    "fixture",
				subject: subject,
				spec: markerImport + "//forge:csv header=" + held + "\n" +
					"type Rows forge.Csv[forge.Collection[Person]]\n",
			})

			if out, status := running(t, catalog(t), "-C", root, "generate", "."); status != 0 {
				t.Fatalf("header=%s was refused, exiting %d:\n%s", held, status, out)
			}

			written := read(t, filepath.Join(root, generated))
			headed := strings.Contains(written, "out.Write(c.CSVHeader())")

			if want := slices.Contains(on, held); headed != want {
				t.Errorf("header=%s writes a header: %v, want %v", held, headed, want)
			}
		})
	}
}

// Two instantiations of one generic subject get a codec each.
//
// The row codec is named after the subject, so a name that stopped at the type
// would give Box[int] and Box[string] one pair of functions between them —
// which forge catches as two declarations wanting one name, and offers a hint
// nobody can act on: there is no way to rename Box[int] and Box[string] apart.
func TestTwoInstantiationsOfOneGenericSubject(t *testing.T) {
	root := module(t, fixture{
		name: "fixture",
		subject: `// Box is a generic subject, so two instantiations of it are two subjects.
type Box[T any] struct {
	Held  T      ` + "`csv:\"held\"`" + `
	Label string ` + "`csv:\"label\"`" + `
}
`,
		spec: markerImport + `//forge:csv
type Ints forge.Csv[forge.Collection[Box[int]]]

//forge:csv
type Strs forge.Csv[forge.Collection[Box[string]]]
`,
	})

	if out, status := running(t, catalog(t), "-C", root, "generate", "."); status != 0 {
		t.Fatalf("two instantiations of one generic subject exited %d:\n%s", status, out)
	}

	written := read(t, filepath.Join(root, generated))
	for _, want := range []string{
		"func encodeFixtureBoxIntCSVInto(",
		"func encodeFixtureBoxStringCSVInto(",
		"func decodeFixtureBoxIntCSVFrom(",
		"func decodeFixtureBoxStringCSVFrom(",
	} {
		if !strings.Contains(written, want) {
			t.Errorf("the generated file does not carry %q:\n%s", want, written)
		}
	}

	building(t, root)
}

// building compiles what was generated, in both of the builds a spec-form
// declaration is written for.
func building(t *testing.T, root string) {
	t.Helper()

	for _, tags := range [][]string{nil, {"-tags", "forgespec"}} {
		args := append([]string{"build"}, tags...)

		cmd := exec.CommandContext(t.Context(), "go", append(args, "./...")...)
		cmd.Dir = root

		if out, err := cmd.CombinedOutput(); err != nil {
			t.Errorf("what was generated does not build with %v: %v\n%s", tags, err, out)
		}
	}
}

// theBound are names the generated bodies bind, which is the set a subject's
// own package must be allowed to collide with. Not all of them — a body also
// binds the element, the receiver and one variable per column — but every one
// of these is bound somewhere a type is also spelled, which is what makes a
// collision a compile failure rather than a curiosity.
var theBound = []string{"record", "counted", "err", "held", "out", "in", "header", "failed", "want"}

// A subject from a package named after something the bodies bind still
// generates code that compiles.
//
// The one name a layer cannot know in advance is the subject's package, and the
// bodies bind a dozen of their own. A body that binds record and also has to
// spell record.Person writes that inside a scope where record is a variable —
// a file this layer generated without complaint and the compiler then refused,
// in a file the author cannot edit.
//
// Every local is asked for against what the file binds now, so the one that
// moves is this layer's. One case per bound name, because what is under test is
// the allocation rather than any one collision.
//
// Six of them are live today: record, counted, err, in, header and failed are
// each bound in a scope that also spells the element. The other three are
// insurance — want closes with its if statement, held is bound after the
// closure's own signature has resolved, and out lives in a body that names no
// element — and they become live the moment somebody hoists one of those out of
// its scope.
func TestASubjectFromAPackageTheBodiesBind(t *testing.T) {
	for _, one := range theBound {
		t.Run(one, func(t *testing.T) {
			root := module(t,
				fixture{
					name: one,
					dir:  one,
					subject: `// Person is the subject, in a package named after a local.
type Person struct {
	ID   int  ` + "`csv:\"id\"`" + `
	Kind Kind ` + "`csv:\"kind\"`" + `
}

// Kind is a named string, so a cell of it is converted through its own name —
// which is the spelling that needs the package.
type Kind string
`,
				},
				fixture{
					name:    "fixture",
					subject: "// The declaration below is over a subject from elsewhere.\n",
					spec: fmt.Sprintf(`import (
	"github.com/okian/forge"

	%q
)

//forge:csv
type Rows forge.Csv[forge.Collection[%s.Person]]
`, "example.com/fixture/"+one, one),
				},
			)

			if out, status := running(t, catalog(t), "-C", root, "generate", "./..."); status != 0 {
				t.Fatalf("generating over a subject from package %s exited %d:\n%s", one, status, out)
			}

			// The package keeps the spelling its author gave it, which is the
			// half a rename could get backwards: moving the import instead
			// would leave the author's own code naming something forge had
			// renamed underneath it.
			written := read(t, filepath.Join(root, generated))
			if want := one + ".Person"; !strings.Contains(written, want) {
				t.Errorf("the generated file does not spell %s:\n%s", want, written)
			}

			building(t, root)
		})
	}
}

// A subject declared in the package being generated into still generates code
// that compiles.
//
// The half that arrives through no import. A subject from elsewhere is found by
// its package name, which the file imports and the allocation can therefore
// see; one declared beside the declaration is spelled bare, so the only place
// its name appears is the spelling itself. A layer seeded from imports alone
// would reserve nothing for it and write `var v record` into a body whose
// parameter is already called record.
func TestASubjectDeclaredInThePackageGeneratedInto(t *testing.T) {
	cases := map[string]struct{ declared, subject string }{
		// The element itself, which is what the row codec's signature spells.
		"the element": {"record", `// record is the subject, declared right here.
type record struct {
	ID   int    ` + "`csv:\"id\"`" + `
	Name string ` + "`csv:\"name\"`" + `
}
`},

		// A field's own type, which a converted cell spells to convert through.
		// Named record rather than anything else because the row codec binds
		// that one: the decoder would otherwise write record(record[1]) with
		// the parameter in scope, and call a []string.
		"a field's type": {"Holder", `// Holder is the subject.
type Holder struct {
	ID   int    ` + "`csv:\"id\"`" + `
	Kind record ` + "`csv:\"kind\"`" + `
}

// record is a named string, so a cell of it converts through its own name.
type record string
`},
	}

	for name, held := range cases {
		t.Run(name, func(t *testing.T) {
			root := module(t, fixture{
				name:    "fixture",
				subject: held.subject,
				spec: fmt.Sprintf(`import "github.com/okian/forge"

//forge:csv
type Rows forge.Csv[forge.Collection[%s]]
`, held.declared),
			})

			if out, status := running(t, catalog(t), "-C", root, "generate", "./..."); status != 0 {
				t.Fatalf("generating over a same-package subject exited %d:\n%s", status, out)
			}

			building(t, root)
		})
	}
}

// A subject whose type argument is declared in the same package compiles too.
//
// Separately, because the hazard hides inside the argument rather than at the
// front of the spelling: a fix that reserved only the leading identifier of
// Box[record] would reserve Box and leave record to collide.
func TestASubjectWhoseTypeArgumentIsDeclaredHere(t *testing.T) {
	root := module(t, fixture{
		name: "fixture",
		subject: `// Box holds one of something.
type Box[T any] struct {
	Held  T      ` + "`csv:\"held\"`" + `
	Label string ` + "`csv:\"label\"`" + `
}

// record is a named string, reached only as a type argument below.
type record string
`,
		spec: `import "github.com/okian/forge"

//forge:csv
type Rows forge.Csv[forge.Collection[Box[record]]]
`,
	})

	if out, status := running(t, catalog(t), "-C", root, "generate", "./..."); status != 0 {
		t.Fatalf("generating over Box[record] exited %d:\n%s", status, out)
	}

	building(t, root)
}

// A subject with two fields of one camel form gets a local for each.
//
// Camel lowers a whole word, so ID and Id both come back id. A codec that built
// the per-column name from the field would declare idCell twice in one scope,
// which is a file this layer wrote and the compiler refused. The header keeps
// the two columns apart on its own, so nothing upstream catches this.
func TestASubjectWithTwoFieldsOfOneCamelForm(t *testing.T) {
	root := module(t, fixture{
		name: "fixture",
		subject: `// Person has two fields whose camel forms are the same word.
type Person struct {
	ID int ` + "`csv:\"id\"`" + `
	Id int ` + "`csv:\"other\"`" + `
}
`,
		spec: `import "github.com/okian/forge"

//forge:csv
type Rows forge.Csv[forge.Collection[Person]]
`,
	})

	if out, status := running(t, catalog(t), "-C", root, "generate", "./..."); status != 0 {
		t.Fatalf("generating over two fields with one camel form exited %d:\n%s", status, out)
	}

	building(t, root)
}
