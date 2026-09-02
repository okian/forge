package cli_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/okian/forge/internal/diag"
	"github.com/okian/forge/internal/goldentest"
)

// index is where the published list of diagnostics lives, relative to this
// package.
//
// The list is a hundred lines of table nobody should be maintaining by hand,
// and the registry already holds every entry: registration happens at package
// initialisation, so linking this test is enough to know the whole set. So the
// document is generated and the test below is the gate.
//
// Regeneration goes through [goldentest.Updating] rather than a flag of this
// package's own, and that is not a preference. This package's tests already
// import goldentest, whose init registers -update — so a flag.Bool of that name
// here would run second and panic, every time, taking every test in the package
// down with it. It also keeps -update meaning one thing however the binary was
// assembled.
const index = "../../docs/diagnostics.md"

// The published index of diagnostics is every code this build registers.
//
// Both directions, and they fail for different reasons. A code in the registry
// and not in the document is a failure an author can be handed with nowhere to
// look it up. A code in the document and not in the registry is a line
// describing something that cannot happen any more, which is worse than a
// missing one: somebody reading it believes it.
//
// This package rather than a document generator of its own, because this is the
// package that reaches every other one. Registration is a side effect of
// linking, so the set is only complete where the whole tree is imported — and
// the command line is what imports it.
func TestTheDiagnosticsIndexIsTheRegistry(t *testing.T) {
	held := rendered()

	if goldentest.Updating() {
		// The mode generated output takes in this tree: it is read by people
		// as well as by the test above, and git records only the execute bit
		// anyway.
		if err := os.WriteFile(index, []byte(held), 0o644); err != nil {
			t.Fatalf("writing %s: %v", filepath.Base(index), err)
		}
		t.Logf("%s rewritten from the registry", filepath.Base(index))

		return
	}

	found, err := os.ReadFile(index)
	if err != nil {
		t.Fatalf("reading %s: %v", filepath.Base(index), err)
	}

	if string(found) == held {
		return
	}

	// Which codes disagree rather than which line differs. What produces a
	// difference is a code added or removed, and a line number in a generated
	// document tells the reader nothing about which one.
	t.Errorf("%s is not what the registry holds; regenerate it with\n"+
		"\tmake diagnostics\n\n%s",
		filepath.Base(index), missing(string(found), held))
}

// missing describes what the two lists disagree about, as the codes one holds
// and the other does not.
func missing(found, want string) string {
	var out strings.Builder

	for _, line := range difference(want, found) {
		out.WriteString("registered and not written down: " + line + "\n")
	}
	for _, line := range difference(found, want) {
		out.WriteString("written down and not registered: " + line + "\n")
	}

	if out.Len() == 0 {
		// The same codes with different wording, which is what happens when a
		// summary is reworded. Nothing to enumerate, and the fix is the same.
		return "the codes agree and their summaries do not\n"
	}

	return out.String()
}

// difference returns the code lines of one document that the other has not.
func difference(from, against string) []string {
	var out []string

	for _, line := range strings.Split(from, "\n") {
		if !strings.HasPrefix(line, "| `FRG") {
			continue
		}
		if !strings.Contains(against, code(line)) {
			out = append(out, strings.TrimSpace(line))
		}
	}

	return out
}

// code returns the identifier a table row opens with.
func code(line string) string {
	held, _, _ := strings.Cut(strings.TrimPrefix(line, "| "), " ")
	return held
}

// rendered returns the index as the registry describes it.
//
// A section per range, in the order the ranges run, and a table under each. The
// layer range gets its prose whether or not this build holds any of its codes,
// because the reader placing a failure by its first digit needs the sixes
// explained most of all: they are the ones forge's own documentation cannot
// list, and saying nothing would read as saying there are none.
func rendered() string {
	var out strings.Builder

	out.WriteString(preamble)

	category := diag.CategoryInvalid

	for _, one := range diag.Registered() {
		if held := one.Code.Category(); held != category {
			category = held
			out.WriteString(heading(category))
			out.WriteString("| Code | What it reports |\n| ---- | --------------- |\n")
		}

		fmt.Fprintf(&out, "| `%s` | %s |\n", one.Code, one.Summary)
	}

	if category != diag.CategoryLayer {
		out.WriteString(heading(diag.CategoryLayer))
	}

	return out.String()
}

// heading opens one range's section: its title and the sentence under it.
func heading(category diag.Category) string {
	held := section[category]

	return "\n## " + strings.ToUpper(held[:1]) + held[1:] + "\n\n" + ranges[category] + "\n\n"
}

// section names each range the way a heading does, and ranges is the sentence
// under it.
//
// Written here rather than taken from [diag.Category.String], which answers with
// the noun a diagnostic's own prose uses. A heading wants a phrase and a
// sentence wants a clause, and one string cannot be both without reading badly
// somewhere.
var (
	section = map[diag.Category]string{
		diag.CategoryComposition: "composition — `FRG1xxx`",
		diag.CategorySubject:     "the subject — `FRG2xxx`",
		diag.CategoryOptions:     "directives and options — `FRG3xxx`",
		diag.CategoryEmission:    "emission — `FRG4xxx`",
		diag.CategoryToolchain:   "input, output and the toolchain — `FRG5xxx`",
		diag.CategoryLayer:       "layers — `FRG6xxx` and above",
	}

	ranges = map[diag.Category]string{
		diag.CategoryComposition: "The shape of the stack: which layers may sit where, how many of each, " +
			"and whether each can sit on what is beneath it.",
		diag.CategorySubject: "The type a stack is specialised to, and what a layer can and cannot " +
			"make of its fields.",
		diag.CategoryOptions: "What was written on a `//forge:` directive or in a struct tag, judged " +
			"against what the layer said it accepts.",
		diag.CategoryEmission: "Found while deciding what to write. These are about the output rather " +
			"than about the declaration: an author has usually done nothing wrong, and the " +
			"hint says so.",
		diag.CategoryToolchain: "Loading the packages, reading the tree, and writing the files.",
		diag.CategoryLayer: "Reported by a layer forge does not ship, and not listed here: forge " +
			"cannot document a code it has never seen. The number belongs to whoever " +
			"wrote the layer and so does the explanation, and the message names which " +
			"layer raised it. [`x/csv`](../x/csv) is the one this repository ships " +
			"beside forge; anything else came from a binary somebody linked a layer " +
			"into.",
	}
)

// preamble opens the generated document.
//
// Held here rather than in the file so that the file is generated whole: a
// document half written by hand is one where an edit to the hand-written half
// survives until somebody regenerates and loses it.
const preamble = `<!-- Generated from the diagnostic registry. Do not edit.
     Regenerate with: make diagnostics -->

# Diagnostics

Every failure ` + "`forge`" + ` reports carries a code, and the code is permanent.
Messages get reworded and the rule behind one may be reimplemented, but the
number keeps its meaning — so a suppression, a runbook or a search against one
does not rot.

A report is one line saying what is wrong, the declaration underlined where the
fault is, and a hint saying what to do about it:

` + "```" + `
model/spec.go:12:6: FRG1008: Csv is a transport with Ring written over it
  Ring[Csv[Person]]
       ^^^
  hint: a transport terminates a stack, so write it outermost and write only one
` + "```" + `

The position is the **declaration** rather than the generated file. An author
cannot edit generated code, so a report pointing there would say where the
consequence landed rather than where the cause is.

The first digit places a failure without looking anything up.
`
