package racetest

import (
	"fmt"
	"go/format"
	"slices"
	"strconv"
	"strings"

	"github.com/okian/forge/internal/model"
)

// How much work the written test does.
//
// Enough goroutines that they overlap on any machine anybody runs this on, and
// enough rounds that overlapping is likely rather than lucky. A race the
// detector sees once is a race; a race it never gets the chance to see is a
// test that passes for the wrong reason.
//
// The rounds are the readers'. Writers run until the readers have finished
// rather than for a count of their own, because the two are not the same size
// of work: a writing round adds one element and a reading round copies the
// whole container, walks it and writes a document, so matched counts would
// leave the writers done in the first few per cent of the run and the rest of
// it unaccompanied.
//
// The writers are bounded as well as gated, and the bound is a backstop rather
// than the plan. Over a container that does not grow — which is what the matrix
// puts every layer over — the readers finish first and the bound is never
// reached. Over one that does, the two feed each other: every element added
// makes every later reading round slower, which gives the writers longer to add
// more, and a run that would have taken milliseconds takes as long as anybody
// is willing to wait. That is a property of stressing an unbounded container
// with a reader that walks all of it, rather than of any layer, so what it gets
// is a ceiling and not a fix.
const (
	writers = 4
	readers = 4
	rounds  = 64

	// writes is the most any one writer does. Far above what a bounded
	// container reaches before the readers are done, and far below where the
	// run stops being one anybody waits for.
	writes = rounds * 256
)

// The names the written test gives itself.
const (
	valueName = "held"
	viewName  = "v"
	elemName  = "one"

	writingGroup = "writing"
	readingGroup = "reading"
	stopFlag     = "done"

	wroteCount  = "wrote"
	readCount   = "read"
	walkedCount = "walked"
)

// Asked is what writing the stress test needs to know.
//
// Names rather than a stack, because what this writes is Go source and every
// decision in it is a name the declaration already settled. Reading a composed
// stack here would be reading it a second time, in a package with no business
// knowing what a layer is.
type Asked struct {
	// Package is the package clause the file is written under, and Declared is
	// the type under test.
	Package  string
	Declared string

	// Make is the expression that makes one, and is empty where the zero value
	// is one.
	//
	// An expression rather than a name, because how a container is made is the
	// business of whatever declared it: some take a size and some take nothing,
	// and what reaches here is the call already written.
	Make string

	// Elem is how one element is written, since a writer needs something to
	// add and the only thing it can be sure of is the element's zero value.
	Elem string

	// View is what a scope hands over, and Scope and ReadScope are the two ways
	// in.
	View      string
	Scope     string
	ReadScope string

	// Walk is the view method a reader walks the container with, and Append the
	// one a writer adds elements with.
	//
	// Both are on the view rather than on the declared type, which is the whole
	// of what a concurrent layer does: what would race is reachable only from
	// inside a scope, and a stress test that could reach them directly would be
	// stressing something no caller can write.
	Walk   string
	Append string

	// Counts is the method that says how many elements there are, and is empty
	// where the container cannot say.
	//
	// Named apart from the rest of the reads because the test does two things
	// with it: it calls it like any other read, and it asks it afterwards
	// whether what the writers added is still there — which is what tells a
	// container that accepted the elements from one that quietly dropped them.
	Counts string

	// Copies is the method that hands back a copy of the elements, and is empty
	// where there is none.
	//
	// Also named apart, because a copy is read element by element rather than
	// discarded: a copy that aliased what it copied is the mistake this kind of
	// layer warns about in its own documentation, and a caller who never looks
	// at what came back cannot be racing anybody over it.
	Copies string

	// Reads are the other methods on the declared type that take nothing,
	// answer with something, and are safe to call from anywhere.
	//
	// They are called from the reading goroutines rather than from inside a
	// scope, because inside one they would deadlock on a lock that is not
	// reentrant.
	Reads []string

	// Encodes is the codec's entry point, and is empty where the elements have
	// no codec.
	Encodes string

	// Imports are what the file may name, narrowed to what it turns out to
	// write.
	Imports []model.Import
}

// Write returns the stress test for one declaration, formatted.
//
// It refuses a declaration that does not offer the contract rather than writing
// a shorter test: a concurrent layer with no stress test is what this package
// exists to prevent, and a test that quietly exercised half of one would be the
// same outcome with a green tick on it.
func Write(of Asked) ([]byte, error) {
	if err := offered(of); err != nil {
		return nil, err
	}

	w := &strings.Builder{}

	w.WriteString("// Code generated by forge's race harness. DO NOT EDIT.\n")
	w.WriteString("//\n")
	w.WriteString("// It is committed so that the ordinary test run covers it, and recorded so\n")
	w.WriteString("// that a change to what it exercises is a diff somebody reads.\n\n")
	w.WriteString("package " + of.Package + "\n\n")

	imports(w, of)
	declare(w, of)
	writing(w, of)
	reading(w, of)
	closing(w, of)
	sinking(w, of)

	out, err := format.Source([]byte(w.String()))
	if err != nil {
		return nil, fmt.Errorf("what was written for %s is not valid Go: %w", of.Declared, err)
	}
	return out, nil
}

// offered reports what the declaration is missing, if anything.
func offered(of Asked) error {
	for _, one := range []struct{ what, held string }{
		{"a package to be written into", of.Package},
		{"a type to be about", of.Declared},
		{"an element to add", of.Elem},
		{"a view to reach the container through", of.View},
		{"a write scope", of.Scope},
		{"a read scope", of.ReadScope},
		{"a walk to read the container with", of.Walk},
		{"a way to add elements", of.Append},
	} {
		if one.held == "" {
			return fmt.Errorf(
				"a stress test needs %s, and %q offers none — a concurrent layer that "+
					"does not offer scoped access needs a harness of its own rather than "+
					"this one written around it",
				one.what, of.Declared)
		}
	}
	return nil
}

// name is what the written test is called.
func name(of Asked) string { return "Test" + of.Declared + "UnderConcurrentUse" }

// sink names the function the test hands the elements it copies, so that
// reading them is something the compiler cannot decide not to do.
func sink(of Asked) string { return "sink" + of.Declared }

// routes names what a reading round does, in the order it does it.
//
// Read off the declaration rather than written down, because what a stack
// offers is the stack's business: a container that cannot be counted has no
// count to call, and a sentence promising one would be describing a test that
// is not there.
func routes(of Asked) string {
	held := make([]string, 0, len(of.Reads)+4)

	if of.Counts != "" {
		held = append(held, of.Counts)
	}
	held = append(held, of.Reads...)

	if of.Copies != "" {
		held = append(held, of.Copies+" read element by element")
	}
	held = append(held, "a walk inside "+of.ReadScope)

	if of.Encodes != "" {
		held = append(held, of.Encodes)
	}

	if len(held) == 1 {
		return held[0]
	}
	return strings.Join(held[:len(held)-1], ", ") + " and " + held[len(held)-1]
}

// imports writes the import block, in the two groups a formatter would put them
// in and with the name each one binds where the path does not give that name.
func imports(w *strings.Builder, of Asked) {
	held := []model.Import{
		{Path: "slices", Name: "slices"},
		{Path: "sync", Name: "sync"},
		{Path: "sync/atomic", Name: "atomic"},
		{Path: "testing", Name: "testing"},
	}
	held = append(held, of.Imports...)

	slices.SortFunc(held, func(a, b model.Import) int { return strings.Compare(a.Path, b.Path) })
	held = slices.CompactFunc(held, func(a, b model.Import) bool { return a.Path == b.Path })

	w.WriteString("import (\n")

	// In the three groups a formatter puts them in — the standard library,
	// everybody else's, and this module's — because a generated file a
	// formatter rewrites is one that stops matching the copy it is held to,
	// and the run that noticed would blame the harness.
	written := false
	for _, group := range grouped(held) {
		if len(group) == 0 {
			continue
		}
		if written {
			w.WriteString("\n")
		}
		written = true

		for _, one := range group {
			w.WriteString("\t" + bound(one) + "\n")
		}
	}

	w.WriteString(")\n\n")
}

// module is the import path prefix this repository's own packages sit under,
// which is the third group a formatter sorts imports into.
const module = "github.com/okian/forge"

// grouped splits the imports the way the formatter this repository runs does:
// the standard library, then everybody else's, then this module's.
//
// The first split is the go command's own rule — a path whose first element
// carries a dot is a module path, and everything else is under GOROOT — and the
// second is a prefix, which is how the grouping is configured.
func grouped(held []model.Import) [][]model.Import {
	out := make([][]model.Import, 3)

	for _, one := range held {
		out[where(one.Path)] = append(out[where(one.Path)], one)
	}
	return out
}

// where says which group a path belongs to.
func where(path string) int {
	switch first, _, _ := strings.Cut(path, "/"); {
	case !strings.Contains(first, "."):
		return 0
	case path == module || strings.HasPrefix(path, module+"/"):
		return 2
	default:
		return 1
	}
}

// bound writes one import, with the name it binds where the path does not give
// that name on its own.
//
// A path does not say what it binds: encoding/json/v2 binds json, and a layer
// that had to rename a package writes it under the name it chose. An import
// written without the name it was given is one the file names and does not
// have.
func bound(one model.Import) string {
	last := one.Path[strings.LastIndex(one.Path, "/")+1:]
	if one.Name != "" && (one.Aliased || one.Name != last) {
		return one.Name + " " + strconv.Quote(one.Path)
	}
	return strconv.Quote(one.Path)
}

// declare writes the test's own opening: what it is for, the value every
// goroutine below shares, and the counters that say the calls ran.
func declare(w *strings.Builder, of Asked) {
	w.WriteString("// " + name(of) + " runs writers and readers against one\n")
	w.WriteString("// " + of.Declared + " at once.\n")
	w.WriteString("//\n")
	w.WriteString("// The failure worth catching here is a race, which has no assertion and is\n")
	w.WriteString("// reported by the detector — so running this without the detector says\n")
	w.WriteString("// only that the calls work.\n")
	w.WriteString("//\n")
	w.WriteString("// What it does assert is that they ran at all. A scope that took the lock\n")
	w.WriteString("// and never called what it was given races nobody and passes every check\n")
	w.WriteString("// the detector makes, so the counters below are what tell a container that\n")
	w.WriteString("// is correctly locked from one that does nothing.\n")
	w.WriteString("//\n")
	w.WriteString("// The routes taken every round are " + routes(of) + ",\n")
	w.WriteString("// because each holds the value for a different length of time and a\n")
	w.WriteString("// concurrent layer that is right about one of them is not thereby right\n")
	w.WriteString("// about the rest. A method answering with nothing is not among them: what\n")
	w.WriteString("// one does cannot be read off its signature, and calling it because it\n")
	w.WriteString("// took no arguments is as likely to deadlock the run as to stress it.\n")
	w.WriteString("func " + name(of) + "(t *testing.T) {\n")
	w.WriteString("\tt.Parallel()\n\n")

	if of.Make == "" {
		w.WriteString("\tvar " + valueName + " " + of.Declared + "\n\n")
	} else {
		w.WriteString("\t" + valueName + " := " + of.Make + "\n\n")
	}

	w.WriteString("\tvar " + wroteCount + ", " + readCount + ", " + walkedCount + " atomic.Int64\n")
	w.WriteString("\tvar " + stopFlag + " atomic.Bool\n\n")
	w.WriteString("\tvar " + writingGroup + ", " + readingGroup + " sync.WaitGroup\n\n")
}

// writing writes the goroutines that change the container.
func writing(w *strings.Builder, of Asked) {
	w.WriteString("\t// The writers, which reach the container only through a write scope —\n")
	w.WriteString("\t// which is the whole of what a concurrent layer does, so a stress test\n")
	w.WriteString("\t// that got at it any other way would be stressing something no caller\n")
	w.WriteString("\t// can write.\n")
	w.WriteString("\t//\n")
	w.WriteString("\t// They run until the readers have finished rather than for a count of\n")
	w.WriteString("\t// their own: a writing round adds one element and a reading round reads\n")
	w.WriteString("\t// the whole container, so matched counts would leave the readers running\n")
	w.WriteString("\t// alone for most of the test.\n")
	w.WriteString("\t//\n")
	w.WriteString("\t// The ceiling is a backstop. A container that does not grow reaches the\n")
	w.WriteString("\t// readers' end long before it, and one that does would otherwise feed\n")
	w.WriteString("\t// itself — each element added makes every later reading round slower,\n")
	w.WriteString("\t// which gives the writers longer to add more.\n")
	w.WriteString("\tfor range " + count(writers) + " {\n")
	w.WriteString("\t\t" + writingGroup + ".Go(func() {\n")
	w.WriteString("\t\t\tvar " + elemName + " " + of.Elem + "\n\n")
	w.WriteString("\t\t\tfor range " + count(writes) + " {\n")
	w.WriteString("\t\t\t\tif " + stopFlag + ".Load() {\n")
	w.WriteString("\t\t\t\t\tbreak\n")
	w.WriteString("\t\t\t\t}\n\n")
	w.WriteString("\t\t\t\t" + valueName + "." + of.Scope + "(func(" + viewName + " " + of.View + ") {\n")
	w.WriteString("\t\t\t\t\t" + viewName + "." + of.Append +
		"(slices.Values([]" + of.Elem + "{" + elemName + "}))\n")
	w.WriteString("\t\t\t\t\t" + wroteCount + ".Add(1)\n")
	w.WriteString("\t\t\t\t})\n")
	w.WriteString("\t\t\t}\n")
	w.WriteString("\t\t})\n")
	w.WriteString("\t}\n\n")
}

// reading writes the goroutines that read it, by every route there is.
func reading(w *strings.Builder, of Asked) {
	w.WriteString("\t// The readers. Each round takes every route out of the container that\n")
	w.WriteString("\t// does not change it, so the detector sees them overlapping with the\n")
	w.WriteString("\t// writers above and with each other.\n")
	w.WriteString("\tfor range " + count(readers) + " {\n")
	w.WriteString("\t\t" + readingGroup + ".Go(func() {\n")
	w.WriteString("\t\t\tfor range " + count(rounds) + " {\n")

	for _, one := range plain(of) {
		w.WriteString("\t\t\t\t_ = " + valueName + "." + one + "()\n")
	}

	if of.Copies != "" {
		w.WriteString("\n")
		w.WriteString("\t\t\t\t// Read element by element rather than discarded, because a copy\n")
		w.WriteString("\t\t\t\t// that aliased what it copied is a race only somebody looking at\n")
		w.WriteString("\t\t\t\t// what came back can be part of.\n")
		w.WriteString("\t\t\t\tfor _, " + elemName + " := range " + valueName + "." + of.Copies + "() {\n")
		w.WriteString("\t\t\t\t\t" + sink(of) + "(" + elemName + ")\n")
		w.WriteString("\t\t\t\t}\n")
	}

	w.WriteString("\n")
	w.WriteString("\t\t\t\t" + valueName + "." + of.ReadScope + "(func(" + viewName + " " + of.View + ") {\n")
	w.WriteString("\t\t\t\t\t" + readCount + ".Add(1)\n\n")
	w.WriteString("\t\t\t\t\tfor range " + viewName + "." + of.Walk + "() {\n")
	w.WriteString("\t\t\t\t\t\t" + walkedCount + ".Add(1)\n")
	w.WriteString("\t\t\t\t\t}\n")
	w.WriteString("\t\t\t\t})\n")

	if of.Encodes != "" {
		w.WriteString("\n")
		w.WriteString("\t\t\t\tvar out bytes.Buffer\n")
		w.WriteString("\t\t\t\tif err := " + valueName + "." + of.Encodes +
			"(jsontext.NewEncoder(&out)); err != nil {\n")
		w.WriteString("\t\t\t\t\tt.Errorf(\"writing the container: %v\", err)\n")
		w.WriteString("\t\t\t\t\treturn\n")
		w.WriteString("\t\t\t\t}\n")
	}

	w.WriteString("\t\t\t}\n")
	w.WriteString("\t\t})\n")
	w.WriteString("\t}\n\n")
}

// plain returns the reads whose result the test does nothing with, which is the
// count and everything else that is neither a copy nor a codec.
func plain(of Asked) []string {
	out := make([]string, 0, len(of.Reads)+1)
	if of.Counts != "" {
		out = append(out, of.Counts)
	}

	return append(out, of.Reads...)
}

// closing writes the wait, and the assertions that say the calls ran.
func closing(w *strings.Builder, of Asked) {
	w.WriteString("\t" + readingGroup + ".Wait()\n")
	w.WriteString("\t" + stopFlag + ".Store(true)\n")
	w.WriteString("\t" + writingGroup + ".Wait()\n\n")

	w.WriteString("\t// What a green run has to mean. Each of these is zero for a container\n")
	w.WriteString("\t// that took the lock and then did nothing with it — which races nobody,\n")
	w.WriteString("\t// passes the detector, and is not what any of this is for.\n")
	w.WriteString("\tfor _, one := range []struct {\n")
	w.WriteString("\t\twrong string\n")
	w.WriteString("\t\tran   int64\n")
	w.WriteString("\t}{\n")
	w.WriteString("\t\t{\"the write scope never ran what it was given\", " + wroteCount + ".Load()},\n")
	w.WriteString("\t\t{\"the read scope never ran what it was given\", " + readCount + ".Load()},\n")
	w.WriteString("\t\t{\"nothing was ever walked inside a read scope\", " + walkedCount + ".Load()},\n")
	w.WriteString("\t} {\n")
	w.WriteString("\t\tif one.ran == 0 {\n")
	w.WriteString("\t\t\tt.Error(one.wrong)\n")
	w.WriteString("\t\t}\n")
	w.WriteString("\t}\n")

	if of.Counts != "" {
		w.WriteString("\n")
		w.WriteString("\t// And that what the writers added is still there. How much of it is the\n")
		w.WriteString("\t// storage layer's business — a bounded container drops the oldest and\n")
		w.WriteString("\t// an unbounded one keeps everything — but none of them empties itself,\n")
		w.WriteString("\t// so a container that ends the run holding nothing accepted nothing.\n")
		w.WriteString("\tif " + valueName + "." + of.Counts + "() == 0 {\n")
		w.WriteString("\t\tt.Error(\"the writers filled it and it ended the run empty\")\n")
		w.WriteString("\t}\n")
	}

	w.WriteString("}\n")
}

// sinking writes the function a reader hands the elements it copied.
func sinking(w *strings.Builder, of Asked) {
	if of.Copies == "" {
		return
	}

	w.WriteString("\n")
	w.WriteString("// " + sink(of) + " is where an element a reader copied goes.\n")
	w.WriteString("//\n")
	w.WriteString("// Reading a copy is the whole point of taking one, and a read whose result\n")
	w.WriteString("// the compiler can see is unused is a read it need not perform — so the\n")
	w.WriteString("// elements go to a function it cannot see into. That makes loading them\n")
	w.WriteString("// something that definitely happens, and so something the detector can\n")
	w.WriteString("// report a race over.\n")
	w.WriteString("//\n")
	w.WriteString("//go:noinline\n")
	w.WriteString("func " + sink(of) + "(" + viewName + " " + of.Elem + ") {}\n")
}

// count writes a number the way the source does.
func count(n int) string { return strconv.Itoa(n) }
