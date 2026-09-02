package csv_test

import (
	"fmt"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"

	"github.com/okian/forge/plugin"
)

// A subject that is not a table is refused, and the refusal names every field
// that made it one.
//
// One report rather than one per field, because it is one decision: the subject
// cannot be written as CSV. Three reports about one decision read as three
// problems to solve separately, and solving any one of them changes nothing.
func TestASubjectThatIsNotATable(t *testing.T) {
	held := catalog(t)
	root := module(t, fixture{
		name: "fixture",
		subject: `// Person holds two fields no cell can carry.
type Person struct {
	ID   int
	Tags []string
	Meta map[string]string
}
`,
		spec: markerImport + `//forge:csv
type Rows forge.Csv[forge.Collection[Person]]
`,
	})

	out, status := running(t, held, "-C", root, "generate", ".")
	if status == 0 {
		t.Fatalf("a subject with no text form for two fields was generated for:\n%s", out)
	}

	for _, want := range []string{
		"FRG6101",
		"Tags ([]string)",
		"Meta (map[string]string)",
		"MarshalText",
		"hint:",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the refusal does not carry %q:\n%s", want, out)
		}
	}

	if got := len(codesIn(t, out)); got != 1 {
		t.Errorf("two unwritable fields produced %d reports, want one:\n%s", got, out)
	}
	if len(generatedIn(t, root)) != 0 {
		t.Error("a refused declaration left files behind")
	}
}

// A subject with nothing to tabulate is refused rather than written as a
// document of empty records.
func TestASubjectWithNoColumns(t *testing.T) {
	held := catalog(t)
	root := module(t, fixture{
		name: "fixture",
		subject: `// Person has nothing a document may carry.
type Person struct {
	Note    string ` + "`csv:\"-\"`" + `
	audited bool
}

// Audited is here so the unexported field is used.
func (p Person) Audited() bool { return p.audited }
`,
		spec: markerImport + `//forge:csv
type Rows forge.Csv[forge.Collection[Person]]
`,
	})

	out, status := running(t, held, "-C", root, "generate", ".")
	if status == 0 {
		t.Fatalf("a subject with no columns was generated for:\n%s", out)
	}

	for _, want := range []string{"FRG6102", "no columns", "unexported", "hint:"} {
		if !strings.Contains(out, want) {
			t.Errorf("the refusal does not carry %q:\n%s", want, out)
		}
	}
}

// Two fields that would write one column are refused rather than one of them
// being renamed on the author's behalf.
//
// A header is what the far end matches a document against, so two columns of
// one name leave every value in the second reachable only by counting — which
// is exactly what a header exists to avoid.
func TestTwoFieldsThatWantOneColumn(t *testing.T) {
	held := catalog(t)
	root := module(t, fixture{
		name: "fixture",
		subject: `// Person names one column twice.
type Person struct {
	Given  string ` + "`csv:\"name\"`" + `
	Family string ` + "`csv:\"name\"`" + `
}
`,
		spec: markerImport + `//forge:csv
type Rows forge.Csv[forge.Collection[Person]]
`,
	})

	out, status := running(t, held, "-C", root, "generate", ".")
	if status == 0 {
		t.Fatalf("two fields wrote one column:\n%s", out)
	}

	for _, want := range []string{"FRG6103", "Given", "Family", `"name"`, "hint:"} {
		if !strings.Contains(out, want) {
			t.Errorf("the refusal does not carry %q:\n%s", want, out)
		}
	}
}

// A stack whose elements cannot be walked is refused by composition, with what
// the layer said in the message.
//
// The refusal is forge's — the code and the position are about the stack rather
// than about anything inside the layer — and the sentence is the layer's, which
// is the division the plugin surface draws.
func TestAStackTheLayerCannotSitOn(t *testing.T) {
	held := catalog(t)
	root := module(t, fixture{
		name:    "fixture",
		subject: subject,
		spec: markerImport + `//forge:csv
type Rows forge.Csv[Person]
`,
	})

	out, status := running(t, held, "-C", root, "generate", ".")
	if status == 0 {
		t.Fatalf("a transport over a subject with no container was generated for:\n%s", out)
	}
	if !strings.Contains(out, "cannot be walked") {
		t.Errorf("the refusal does not carry what the layer said:\n%s", out)
	}

	// Forge's own code, because what is wrong is the stack.
	code := reported(t, out)
	if !code.Ours() {
		t.Errorf("FRG%d is the layer's, and a refused stack is forge's to report", int(code))
	}
}

// Nothing may be written over a transport, and only one of them may appear.
//
// Both are forge's rules rather than this layer's, and both are here because
// they are the rules that make a transport what it is: the reason it writes a
// whole document is that there is nothing above it.
func TestWhereATransportMaySit(t *testing.T) {
	cases := map[string]string{
		"wrapped in a container": `//forge:csv
type Rows forge.Collection[forge.Csv[Person]]
`,
		"written twice": `//forge:csv
type Rows forge.Csv[forge.Csv[forge.Collection[Person]]]
`,
	}

	for name, spec := range cases {
		t.Run(name, func(t *testing.T) {
			held := catalog(t)
			root := module(t, fixture{name: "fixture", subject: subject, spec: markerImport + spec})

			out, status := running(t, held, "-C", root, "generate", ".")
			if status == 0 {
				t.Fatalf("the stack was generated for:\n%s", out)
			}
			if !strings.Contains(out, "FRG1008") {
				t.Errorf("the refusal is not the one about where a transport sits:\n%s", out)
			}
		})
	}
}

// A layer beneath that does not offer the streaming contract is reported rather
// than generated against.
//
// It is the one refusal here an author cannot cause and cannot fix, so it says
// so: the layer beneath is forge's or somebody else's, and the declaration is
// innocent. Reaching it needs a layer written to break the contract, which is
// what [contrary] is — and which is also this test's own guard, since a harness
// that could not tell a broken layer from a working one would pass either way.
func TestALayerBeneathThatIsNotTheContract(t *testing.T) {
	cases := map[string]plugin.Method{
		"a walk that hands over no sequence": {Name: "All", Signature: "() int"},
		"a walk that takes something":        {Name: "All", Signature: "(n int) iter.Seq[E]"},
		"a capacity that is not a count":     {Name: "Cap", Signature: "() string"},
		"a reset that answers":               {Name: "Reset", Signature: "() error"},
		"a sink that takes no sequence":      {Name: "AppendSeq", Signature: "(v E)"},
		"a sink that answers with something else": {
			Name: "AppendSeq", Signature: "(seq iter.Seq[E]) int",
		},
	}

	for name, broken := range cases {
		t.Run(name, func(t *testing.T) {
			held := catalog(t)
			held.MustRegister(contrary{broken: broken})

			root := module(t,
				fixture{
					name: "broken",
					dir:  "broken",
					subject: `// Broken is a marker whose layer breaks the contract on purpose.
type Broken[T any] struct{ _ [0]T }
`,
				},
				fixture{
					name:    "fixture",
					subject: subject,
					spec: brokenImports +
						"//forge:csv\ntype Rows forge.Csv[broken.Broken[Person]]\n",
				},
			)

			out, status := running(t, held, "-C", root, "generate", ".")
			if status == 0 {
				t.Fatalf("a document was written over a layer that is not the contract:\n%s", out)
			}
			if !strings.Contains(out, "FRG6104") {
				t.Errorf("the refusal is not the one about the layer beneath:\n%s", out)
			}
			if !strings.Contains(out, "fault in the layer beneath") {
				t.Errorf("the refusal does not say whose fault it is:\n%s", out)
			}
		})
	}
}

// A layer beneath that does offer the contract generates, which is what says
// the test above is failing for the reason it claims.
func TestALayerBeneathThatIsTheContract(t *testing.T) {
	held := catalog(t)
	held.MustRegister(contrary{})

	root := module(t,
		fixture{
			name: "broken",
			dir:  "broken",
			subject: `// Broken is a marker whose layer keeps the contract when asked to.
type Broken[T any] struct{ _ [0]T }
`,
		},
		fixture{
			name:    "fixture",
			subject: subject,
			spec: brokenImports +
				"//forge:csv\ntype Rows forge.Csv[broken.Broken[Person]]\n",
		},
	)

	if out, status := running(t, held, "-C", root, "generate", "."); status != 0 {
		t.Fatalf("a layer that keeps the contract was refused, exiting %d:\n%s", status, out)
	}
}

// brokenImports opens a spec file naming forge's transport over a marker of
// the fixture's own, which is the arrangement the contract tests need: a layer
// nobody wrote carefully, with a real transport written over it.
const brokenImports = `import (
	"github.com/okian/forge"

	"example.com/fixture/broken"
)

`

// contrary is a storage layer that offers the streaming contract, with one
// method replaced by something that is not it.
//
// Storage rather than refining, so that it declares the type and nothing is
// filled in beneath it: what is being tested is what this layer does with the
// surface it is handed, and a slice quietly appearing underneath would hand it
// a working walk as well as a broken one.
type contrary struct {
	broken plugin.Method

	// refuses makes its sink answer with an error, which is what a bounded
	// container asked to say so rather than to drop its oldest element does.
	//
	// No layer forge ships has one — a slice cannot be full — so without this
	// the two lines the reader writes for such a container would be compiled by
	// nothing. A branch nothing compiles is a branch that is correct until
	// somebody edits it.
	refuses bool
}

func (contrary) Origin() plugin.TypeRef {
	return plugin.TypeRef{Pkg: "example.com/fixture/broken", Name: "Broken"}
}

func (contrary) Kind() plugin.Kind                { return plugin.KindStorage }
func (contrary) OptionSchema() []plugin.OptionDef { return nil }
func (contrary) Accepts(plugin.Shape) error       { return nil }
func (contrary) Writes() []string                 { return nil }

// Binds names what its own output imports, which the CSV layer's set does not
// cover: a walk is a sequence and a slice is filled through the standard
// library.
func (contrary) Binds() []plugin.Import {
	return []plugin.Import{{Path: "iter", Name: "iter"}, {Path: "slices", Name: "slices"}}
}

// Shape offers the contract, with whichever method the case broke put in place
// of the working one.
func (c contrary) Shape(_ *plugin.Context, below plugin.Shape) plugin.Shape {
	below.Caps = below.Caps.With(plugin.Sized, plugin.Ordered, plugin.Streamable)

	sink := "(seq iter.Seq[E])"
	if c.refuses {
		sink += " error"
	}

	held := []plugin.Method{
		{Name: "All", Signature: "() iter.Seq[E]", Owner: c.Origin()},
		{Name: "Cap", Signature: "() int", Owner: c.Origin()},
		{Name: "Reset", Signature: "()", Owner: c.Origin(), Pointer: true},
		{Name: "AppendSeq", Signature: sink, Owner: c.Origin(), Pointer: true},
	}

	if c.broken.Name != "" {
		for i := range held {
			if held[i].Name == c.broken.Name {
				held[i].Signature = c.broken.Signature
			}
		}
	}

	return below.WithMethods(held...)
}

// Generate declares the type and the four methods it said it had, so that what
// the CSV layer writes over them compiles when it is written at all.
//
// A storage layer owes the declared type as well as its methods: forge writes
// what a stack asks for and does not invent the type they are on. The bodies
// are the shortest thing that type-checks, since what is under test is the
// layer above rather than this one.
func (c contrary) Generate(ctx *plugin.Context, _ plugin.Shape) (plugin.Unit, error) {
	elem := ctx.Model.SubjectSpelling(ctx.Bound())
	declared := ctx.Declared()

	src := fmt.Sprintf(`package p

// %[1]s holds elements in a slice.
type %[1]s []%[2]s

// All walks the elements.
func (s %[1]s) All() iter.Seq[%[2]s] { return slices.Values(s) }

// Cap reports how much it can hold, which for a slice is one more than it has
// room for — a slice is not bounded, and the reader written over this refuses a
// container reporting nothing at all.
func (s %[1]s) Cap() int { return cap(s) + 1 }

// Reset empties it.
func (s *%[1]s) Reset() { *s = (*s)[:0] }

// AppendSeq adds every element a sequence yields.
func (s *%[1]s) AppendSeq(seq iter.Seq[%[2]s]) %[3]s { *s = slices.AppendSeq(*s, seq)%[4]s }
`, declared, elem.Text, c.answers(), c.answered())

	fset := token.NewFileSet()

	file, err := parser.ParseFile(fset, "broken.go", src,
		parser.ParseComments|parser.SkipObjectResolution)
	if err != nil {
		return plugin.Unit{}, err
	}

	return plugin.Unit{
		Decls:    file.Decls,
		Comments: file.Comments,
		Fset:     fset,
		Imports:  plugin.Reaching(file.Decls, append(c.Binds(), elem.Imports...)),
	}, nil
}

// answers and answered spell the result of its sink, in a signature and in the
// body that has to satisfy it.
//
// A container that can refuse says so and answers with nothing gone wrong,
// which is enough: what is under test is the reader written over it rather than
// anything this decides.
func (c contrary) answers() string {
	if c.refuses {
		return "error"
	}

	return ""
}

func (c contrary) answered() string {
	if c.refuses {
		return "; return nil"
	}

	return ""
}

// A container whose sink can refuse gets a reader written for one, and it
// compiles and runs.
//
// The two lines this reaches — binding the sink's result, and reporting the
// reading failure before the refusal — are written for a bounded container
// that was asked to say so rather than to drop its oldest element. No layer
// forge ships has one, so without a layer written to have one they would be
// generated and compiled by nothing.
func TestALayerBeneathWhoseSinkCanRefuse(t *testing.T) {
	held := catalog(t)
	held.MustRegister(contrary{refuses: true})

	root := module(t,
		fixture{
			name: "broken",
			dir:  "broken",
			subject: `// Broken is a marker whose layer has a sink that can refuse.
type Broken[T any] struct{ _ [0]T }
`,
		},
		fixture{
			name:    "fixture",
			subject: subject,
			spec: brokenImports +
				"//forge:csv\ntype Rows forge.Csv[broken.Broken[Person]]\n",
		},
	)

	if out, status := running(t, held, "-C", root, "generate", "."); status != 0 {
		t.Fatalf("generating over a sink that can refuse exited %d:\n%s", status, out)
	}

	written := read(t, filepath.Join(root, generated))
	for _, want := range []string{
		"refused := c.AppendSeq(",
		"if failed != nil {",
		"return counted.n, refused",
	} {
		if !strings.Contains(written, want) {
			t.Errorf("the reader does not carry %q:\n%s", want, written)
		}
	}

	write(t, filepath.Join(root, "rows_test.go"), refusingRoundTrip)
	behaves(t, root)
}

// refusingRoundTrip is the test the fixture runs against the reader written for
// a sink that can refuse.
const refusingRoundTrip = `package fixture

import (
	"bytes"
	"testing"
)

// A document round-trips through a container whose sink answers.
func TestARefusingSinkStillReads(t *testing.T) {
	held := Rows{{ID: 1, Name: "one"}, {ID: 2, Name: "two"}}

	var out bytes.Buffer
	if _, err := held.WriteCSVTo(&out); err != nil {
		t.Fatalf("writing: %v", err)
	}

	var read Rows
	if _, err := read.ReadCSVFrom(&out); err != nil {
		t.Fatalf("reading: %v", err)
	}
	if len(read) != 2 {
		t.Errorf("two rows went out and %d came back", len(read))
	}
}
`
