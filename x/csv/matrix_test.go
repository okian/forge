package csv_test

import (
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// The matrix is every stack this layer can be written into, generated and
// compiled.
//
// It exists because the rules are stated one at a time and apply together. What
// this layer refuses is tested where it is written, over shapes assembled by
// hand; what nothing else asks is whether a stack that satisfies every rule
// produces a package that builds — and that answer is a product of the layers
// rather than a property of any one of them, so it is found by trying the
// combinations rather than by reasoning about them.
//
// Only the arrangements holding this layer. The rest are forge's own matrix's
// business, and duplicating them here would be re-running somebody else's tests
// against the same catalog.
//
// It is bounded by the markers below rather than by how deep it goes. Five, and
// they are the ones a transport meets: two containers, the storage under them,
// an element codec that also puts methods on the declared type, and a decorator
// that withdraws the walk this layer needs. A marker forge stages and has no
// generator for would contribute a refusal that says so and nothing else.

// depth is how many layers a matrix stack is built from.
//
// Three, which is what it takes to reach every rule about where a transport may
// sit: over a container, under one, twice in one stack, and over a decorator
// that has taken away what it needs.
const depth = 3

// markers are the layers the matrix arranges, spelled as a declaration names
// them.
//
// Forge's own, all of them, because this layer claims one of forge's markers
// rather than declaring its own — so every stack here is one an author writes
// with a single import.
var markers = []string{"Csv", "Collection", "Ring", "Json", "Guarded"}

// The name of the layer under test, which every stack in the matrix holds at
// least one of.
const under = "Csv"

// layerCodeFloor is the lowest code a layer forge does not ship may take.
//
// Written down here because it is the one number in the published contract this
// module has no constant for: a code below it is forge's, and one above it is
// somebody's layer. Every refusal the matrix reaches is on the forge side —
// arranging real layers wrongly is a composition fault — so what this guards
// against is a code from neither range, which would be a number nobody can look
// up.
const layerCodeFloor = 6000

// Every stack this layer can appear in either builds or is refused in a way an
// author can act on.
//
// The two halves are one test because the interesting thing is the line between
// them. A stack is either something the layer will build or something forge will
// explain, and what must not happen is either a refusal nobody can act on or a
// file that does not compile — so both outcomes are checked, whichever a given
// stack turns out to be.
//
// One generation run and one build for the whole matrix rather than one each.
// Each stack is a package of the same module, so the loader walks them together
// and the compiler builds them together; running them one at a time would cost
// a go/packages session per stack and would find nothing extra.
func TestTheCompositionMatrix(t *testing.T) {
	held := arrangements()
	if len(held) == 0 {
		t.Fatal("the matrix has nothing to arrange")
	}

	root := module(t, packages(held)...)

	out, status := running(t, catalog(t), "-C", root, "generate", "./...")

	built, refused := 0, 0

	for at, stack := range held {
		dir := filepath.Join(root, named(at))

		switch files := generatedIn(t, dir); {
		case len(files) == 0:
			refused++

			// A stack that produced nothing has to have been told why, at its
			// own declaration.
			if !strings.Contains(out, filepath.Join(named(at), "spec.go")) {
				t.Errorf("%s generated nothing and was not reported:\n%s", spelled(stack), out)
			}

		default:
			built++

			if strings.Contains(out, filepath.Join(named(at), "spec.go")) {
				t.Errorf("%s was reported and generated anyway:\n%s", spelled(stack), out)
			}
			write(t, filepath.Join(dir, "using.go"),
				callSite(named(at), stack, declarationIn(t, dir)))
		}
	}

	// A run where nothing built, or where nothing was refused, would pass every
	// assertion inside and mean the matrix had stopped exercising one of the two
	// things it is for.
	if built == 0 || refused == 0 {
		t.Fatalf("the matrix built %d stacks and refused %d, so it is only exercising one of the two",
			built, refused)
	}
	t.Logf("%d stacks built, %d refused", built, refused)

	// Every refusal says what to do about it, and every code it carries is one
	// somebody registered.
	//
	// Not which refusal, because the matrix does not know: what is wrong with a
	// given stack is what the rules say, and a test that decided that for itself
	// would be a second implementation of them that agreed until one changed.
	//
	// More codes than refused stacks, and that is right rather than a
	// miscount: a stack can break several rules at once, and a report that
	// stopped at the first would send somebody back for the next after each
	// fix. What is asserted is that every stack that was refused is in the
	// report — which the loop above does, at its own declaration — and that
	// every entry in the report is actionable.
	codes := codesIn(t, out)
	if len(codes) < refused {
		t.Errorf("%d stacks were refused and the report carries %d codes:\n%s", refused, len(codes), out)
	}
	for _, one := range codes {
		if !one.Ours() && int(one) < layerCodeFloor {
			t.Errorf("FRG%d is in neither forge's range nor a layer's:\n%s", int(one), out)
		}
	}
	if hints := strings.Count(out, "hint:"); hints != len(codes) {
		t.Errorf("the report carries %d codes and %d hints:\n%s", len(codes), hints, out)
	}
	if status == 0 && refused > 0 {
		t.Errorf("the run reported %d refusals and exited 0", refused)
	}

	compiles(t, root)
}

// compiles builds everything the matrix generated, in both of the builds a
// spec-form declaration is written for.
//
// Both ways round, because the declaration's own type is forge's marker in one
// of them and forge's output in the other: the build that excludes the
// generated file has to hold a matching API, and the build that includes it has
// to not collide with the marker. A call site in each package is what makes
// either mean anything — without one, a package whose methods are all missing
// still type-checks, since nothing asks for them.
func compiles(t *testing.T, root string) {
	t.Helper()

	for _, tags := range [][]string{nil, {"-tags", "forgespec"}} {
		args := append([]string{"build"}, tags...)

		cmd := exec.CommandContext(t.Context(), "go", append(args, "./...")...)
		cmd.Dir = root

		if out, err := cmd.CombinedOutput(); err != nil {
			t.Errorf("the matrix generated packages that do not build with %v: %v\n%s", tags, err, out)
		}
	}
}

// arrangements returns every ordered arrangement of the markers, up to depth,
// that holds the layer under test.
//
// With repetition on purpose. A stack naming one layer twice is a stack
// somebody can write, and the rule that refuses it is one the matrix should be
// exercising rather than assuming.
func arrangements() [][]string {
	var out [][]string

	// Each round extends the round before it rather than everything so far,
	// which is the difference between the arrangements of each length and the
	// arrangements of each length once per length that follows it.
	level := [][]string{{}}

	for range depth {
		var longer [][]string
		for _, held := range level {
			for _, one := range markers {
				longer = append(longer, append(slices.Clone(held), one))
			}
		}
		for _, one := range longer {
			if slices.Contains(one, under) {
				out = append(out, one)
			}
		}
		level = longer
	}

	return out
}

// packages returns one fixture package per arrangement.
func packages(held [][]string) []fixture {
	out := make([]fixture, 0, len(held))

	for at, stack := range held {
		name := named(at)

		out = append(out, fixture{
			name:    name,
			dir:     name,
			subject: matrixSubject,
			spec: markerImport +
				"// " + spelled(stack) + "\n//\n" +
				directives(stack) +
				"type " + declaredIn(name) + " " + instantiated(stack) + "\n",
		})
	}

	return out
}

// matrixSubject is the subject every stack in the matrix is over.
//
// Two columns and no more. What varies across the matrix is the arrangement of
// the layers, and a subject with a column of every form would multiply the cost
// of every case by the same constant without reaching anything new — the worked
// example is where the forms are covered.
const matrixSubject = `// Person is the subject every arrangement is over.
type Person struct {
	ID   int    ` + "`csv:\"id\"`" + `
	Name string ` + "`csv:\"name\"`" + `
}
`

// named returns the package one arrangement goes in.
//
// Numbered rather than spelled, because a package name has to be an identifier
// and a stack's spelling has brackets in it. The comment above each declaration
// carries the spelling, so a failure that names a directory is one step from
// the stack it was about.
func named(at int) string { return "s" + strconv.Itoa(at) }

// declaredIn returns the type name a package's declaration is written under.
func declaredIn(pkg string) string { return strings.ToUpper(pkg[:1]) + pkg[1:] + "Rows" }

// spelled names a stack the way an author would read it, outermost first.
func spelled(stack []string) string {
	return strings.Join(stack, "[") + strings.Repeat("]", len(stack)-1)
}

// instantiated spells a stack as the declaration's underlying type.
func instantiated(stack []string) string {
	var b strings.Builder

	for _, one := range stack {
		b.WriteString("forge." + one + "[")
	}
	b.WriteString("Person")
	b.WriteString(strings.Repeat("]", len(stack)))

	return b.String()
}

// directives returns the //forge: comments a stack needs to be generated for.
//
// One per distinct layer, and the two that cannot be generated without an
// option carry it. A directive for a layer named twice is written once, since
// what a second one means is the layer's own question and none of these has
// answered it.
func directives(stack []string) string {
	var (
		out  strings.Builder
		seen []string
	)

	for _, one := range stack {
		if slices.Contains(seen, one) {
			continue
		}
		seen = append(seen, one)

		out.WriteString("//forge:" + strings.ToLower(one))
		if options, needs := needed[one]; needs {
			out.WriteString(" " + options)
		}
		out.WriteString("\n")
	}

	return out.String()
}

// needed holds the options a layer cannot be generated without.
var needed = map[string]string{"Ring": "cap=4"}

// declarationIn returns what forge wrote for the package, as source.
//
// Which of the transport's methods a stack got is decided by what the layers
// beneath it turned out to expose, so the only way to write a call site that
// exercises all of them is to read what was written.
//
// One file to read, which it did not used to be: a package's output was spread
// over a file per declaration and this had to pick the right one out. A
// constant name is one of the things that arrangement cost.
func declarationIn(t *testing.T, dir string) string {
	t.Helper()

	return read(t, filepath.Join(dir, generated))
}

// The three methods the transport puts on the declared type.
//
// Written down here rather than read from the layer's own constants, which are
// unexported and out of reach from a test outside the package. That is the
// right way round: these are the names a caller of the generated code writes,
// so a test that took them from the layer would agree with it about a rename
// nobody else had heard of.
const (
	headerMethod = "CSVHeader"
	writeMethod  = "WriteCSVTo"
	readMethod   = "ReadCSVFrom"
)

// callSite returns a file that reads the generated API, so that both builds of
// a spec-form declaration have to hold it.
//
// Every method the transport wrote, and not just one. Which of the three a
// stack gets depends on what is beneath it — a container that walks and cannot
// be filled gets the writing half alone — so the call site is assembled from
// what was generated rather than written down. A regression that silently
// stopped emitting the reading half for a whole class of stacks would otherwise
// pass: the file would compile, because nothing asked for the method.
//
// Through a pointer, because a declaration is not required to be copyable: a
// stack holding a lock is a struct nothing may copy, and a call site taking one
// by value would report that as a fault in the generated code when it is a
// fault in the fixture. A pointer's method set holds the value methods too, so
// nothing is given up by asking this way.
func callSite(pkg string, stack []string, written string) string {
	var out strings.Builder

	out.WriteString("package " + pkg + "\n\nimport (\n\t\"io\"\n)\n\n")
	out.WriteString("// The generated API for " + spelled(stack) + ", read so that both builds\n" +
		"// of the declaration have to hold it.\n")

	declared := declaredIn(pkg)

	if strings.Contains(written, ") "+headerMethod+"()") {
		out.WriteString("func columns(c *" + declared + ") []string { return c." +
			headerMethod + "() }\n\n")
	}
	if strings.Contains(written, ") "+writeMethod+"(") {
		out.WriteString("func out(c *" + declared + ", w io.Writer) (int64, error) { return c." +
			writeMethod + "(w) }\n\n")
	}
	if strings.Contains(written, ") "+readMethod+"(") {
		out.WriteString("func in(c *" + declared + ", r io.Reader) (int64, error) { return c." +
			readMethod + "(r) }\n")
	}

	return out.String()
}
