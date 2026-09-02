package cli

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/token"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/okian/forge/internal/diag"
	"github.com/okian/forge/internal/emit"
	generated "github.com/okian/forge/internal/generate"
	"github.com/okian/forge/internal/layers"
	"github.com/okian/forge/internal/model"
	"github.com/okian/forge/internal/scalars"
)

// example is the worked example, from this package's directory.
const example = "../../examples/people"

// The committed example is what forge generates from the declaration in it.
//
// This is the end-to-end claim, and the only one made against real source on
// disk rather than a declaration built in a test: a package a person could have
// written, loaded by the go command, walked by every stage, and compared
// against the files that are checked in beside it. A change anywhere in the
// pipeline that alters what a collection looks like shows up here as a diff in
// a file a reader can read, which is the whole point of committing generated
// code.
//
// The header is compared separately and by shape rather than by bytes, for the
// reason [TestTheExampleCarriesForgesHeader] gives.
func TestTheExampleIsWhatForgeGeneratesToday(t *testing.T) {
	produced := regenerating(t)

	for name, got := range produced {
		want, err := os.ReadFile(filepath.Join(example, name))
		if err != nil {
			t.Errorf("reading the committed %s: %v — run `make example`", name, err)
			continue
		}

		if !bytes.Equal(body(got), body(want)) {
			t.Errorf("the committed %s is not what forge generates:\n%s\nrun `make example`",
				name, changes(lines(body(want)), lines(body(got))))
		}
	}

	// A file forge no longer writes is one nothing above compares, and it would
	// sit in the example looking like output. Compared by name rather than by
	// count, so that a declaration renamed — which leaves the count alone and
	// the old file behind — is reported as the leftover it is.
	found, err := ours()
	if err != nil {
		t.Fatalf("looking for generated files: %v", err)
	}

	committed := make([]string, len(found))
	for i, path := range found {
		committed[i] = filepath.Base(path)
	}
	slices.Sort(committed)

	written := slices.Sorted(maps.Keys(produced))
	if !slices.Equal(committed, written) {
		t.Errorf("the example holds %v and forge writes %v", committed, written)
	}
}

// Generating twice from one input produces one answer, byte for byte.
//
// Nothing in the pipeline is allowed to depend on the order a map was walked
// in, on a clock, or on anything else that differs between two runs of the same
// binary over the same source. It matters more here than in most generators
// because the output is committed: output that varied would make every run a
// diff, and a repository whose generated files change without anybody changing
// anything is one where nobody reads them.
//
// Compared whole rather than by body, since both halves came from this run and
// the header is therefore as fixed as everything else in them.
func TestGeneratingTwiceGivesOneAnswer(t *testing.T) {
	first, second := regenerating(t), regenerating(t)

	if len(first) != len(second) {
		t.Fatalf("one run wrote %d files and the next wrote %d", len(first), len(second))
	}

	for name, once := range first {
		twice, ok := second[name]
		if !ok {
			t.Errorf("%s was written by one run and not the next", name)
			continue
		}
		if !bytes.Equal(once, twice) {
			t.Errorf("%s differs between two runs over the same source:\n%s",
				name, changes(lines(once), lines(twice)))
		}
	}
}

// Every method the layers put on a subject is one they said they would.
//
// [layer.Layer.Writes] is what lets one declaration be generated knowing what a
// neighbour's layers will have made of a type it holds as a field, and it is
// answered before anything is generated — so nothing about the output can hold
// it honest except a test that reads the output afterwards. A layer that began
// writing a method and did not name it here would have a neighbour's codec go
// on writing the form underneath a type that had learned to say what it means,
// and nothing would fail.
//
// A superset is allowed and a subset is not. The claim is answered wide on
// purpose — a layer names what it may write, and withholds what the author has
// already written — so a name claimed and not found says nothing. A name found
// and not claimed is the fault this exists for.
//
// Against the example rather than a fixture, because the example is where every
// layer forge ships is exercised over real source by the real pipeline. What it
// cannot cover is a layer no declaration in it names; the catalog test is what
// holds those to their word.
func TestEveryMethodOnASubjectWasClaimed(t *testing.T) {
	env := &environment{stdout: os.Stdout, stderr: os.Stderr, pipeline: stages(layers.Builtins())}

	found, err := env.pipeline.follow(env, env.loadConfig(example))
	if err != nil {
		t.Fatalf("walking the example: %v", err)
	}

	// What the layers said they would write, by the subject each is over. The
	// same union generation builds, reached the same way so that a test cannot
	// pass by agreeing with itself about a different question.
	claimed := make(map[string]map[string]bool)

	for _, req := range found.Requests {
		if req.Model == nil {
			continue
		}

		held := req.Model.Ref().Name
		if claimed[held] == nil {
			claimed[held] = make(map[string]bool)
		}
		for _, ref := range req.Declaration.Stack {
			one, claims := layers.Builtins().Lookup(ref.Origin)
			if !claims {
				continue
			}
			for _, method := range one.Writes() {
				claimed[held][method] = true
			}
		}
	}

	if len(claimed) == 0 {
		t.Fatal("the example resolved to no subjects, so nothing was compared")
	}

	earned := earning(t, found)

	// And what they wrote, read off the file the package shares — which is
	// where a method on a subject lands, whichever declaration caused it.
	shared, ok := regenerating(t)["forge.gen.go"]
	if !ok {
		t.Fatal("the example has no shared file to read")
	}

	for _, one := range methodsOn(t, shared) {
		// A method on a type no declaration is over is one a layer wrote for
		// something the subject reaches. Not what this is about: the claim is
		// per subject, and a neighbour asking about such a type asks the type.
		if _, over := claimed[one.receiver]; !over {
			continue
		}
		if !claimed[one.receiver][one.method] && !earned[one.receiver][one.method] {
			t.Errorf("%s.%s was generated and no layer over %s claims to write it",
				one.receiver, one.method, one.receiver)
		}
	}
}

// earning returns the methods each subject gets from its own tags rather than
// from a layer, by the subject's name.
//
// Those need no claiming and are not the layers' to claim. What earns them is
// written on the subject's fields, so it is in the package on every run
// including the first — and a neighbour reading such a type finds the method by
// looking, which is exactly what a method no run has written yet cannot be
// found by.
//
// Asked as though no layer had written anything, which is the widest the answer
// gets and the only version of it that is not circular: what the tags actually
// earn depends on what the layers wrote, and what the layers wrote is the thing
// under test. Wide is the safe direction for an exemption whose job is to keep
// a true failure legible.
func earning(t *testing.T, found resolved) map[string]map[string]bool {
	t.Helper()

	out := make(map[string]map[string]bool)

	for _, req := range found.Requests {
		if req.Model == nil {
			continue
		}

		held := req.Model.Ref().Name
		if _, done := out[held]; done {
			continue
		}
		out[held] = make(map[string]bool)

		var problems diag.Set
		written, err := scalars.For(scalars.Asked{
			Subject:   req.Model,
			Local:     req.Declaration.Candidate.Pkg.PkgPath,
			At:        req.Model.Pos,
			Earning:   map[string]bool{model.TypeIdentity(req.Model.Type()): true},
			Generated: found.Session.Generated(),
		}, &problems)
		if err != nil || !problems.Empty() {
			t.Fatalf("asking what %s earns from its tags: %v\n%s", held, err, problems.Render())
		}

		for _, unit := range written {
			for _, one := range methodsIn(unit.Decls) {
				out[held][one] = true
			}
		}
	}

	return out
}

// emitted is one generated method and the type it was declared on.
type emitted struct {
	receiver string
	method   string
}

// methodsIn names the methods a set of declarations declares, whatever they are
// declared on.
//
// The receiver is not read, because these came from one subject's own emitters
// and are all on it or on nothing.
func methodsIn(decls []ast.Decl) []string {
	var out []string
	for _, decl := range decls {
		if fn, is := decl.(*ast.FuncDecl); is && fn.Recv != nil {
			out = append(out, fn.Name.Name)
		}
	}
	return out
}

// methodsOn reads the methods a generated file declares, by receiver.
//
// Parsed rather than matched, because a receiver is a type expression and a
// pointer one is spelled with a star that a pattern would have to know to
// strip — and what is wanted is the type's name either way.
func methodsOn(t *testing.T, source []byte) []emitted {
	t.Helper()

	file, err := parser.ParseFile(token.NewFileSet(), "shared.go", source, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("reading the generated file: %v", err)
	}

	var out []emitted
	for _, decl := range file.Decls {
		fn, is := decl.(*ast.FuncDecl)
		if !is || fn.Recv == nil || len(fn.Recv.List) != 1 {
			continue
		}

		held := fn.Recv.List[0].Type
		if star, indirect := held.(*ast.StarExpr); indirect {
			held = star.X
		}
		if name, is := held.(*ast.Ident); is {
			out = append(out, emitted{receiver: name.Name, method: fn.Name.Name})
		}
	}
	return out
}

// A clean checkout generates what a generated one does, byte for byte.
//
// The other half of [TestGeneratingTwiceGivesOneAnswer], and the half that
// catches a different mistake. That one runs twice over a tree that already
// holds forge's output; this one runs once over a tree that does not. What it
// is for is the questions a layer answers about a type a neighbour declaration
// writes methods on: those methods are in the package by the end of a run and
// absent at the start of the first, so a layer that read the package rather
// than asking the layers would answer differently here — and the file would
// rewrite itself on alternate builds, from declarations nobody had touched.
//
// The example is copied rather than emptied in place, because the committed
// files are what every other test in this package compares against.
func TestACleanCheckoutGeneratesTheSame(t *testing.T) {
	warm := regenerating(t)

	dir := unwritten(t)

	cold := walking(t, dir)

	// Compared by body, because the two runs stamped their own headers and one
	// of them recorded a different directory.
	for name, once := range warm {
		twice, ok := cold[name]
		if !ok {
			t.Errorf("%s was written from a generated tree and not from a clean one", name)
			continue
		}
		if !bytes.Equal(body(once), body(twice)) {
			t.Errorf("%s differs between a clean tree and a generated one:\n%s",
				name, changes(lines(body(once)), lines(body(twice))))
		}
	}

	for name := range cold {
		if _, ok := warm[name]; !ok {
			t.Errorf("%s was written from a clean tree and not from a generated one", name)
		}
	}
}

// unwritten copies the example's hand-written half into a module of its own and
// returns the package's directory.
//
// A module rather than a bare directory, because the go command will not load a
// package outside one — and one with a replace onto this checkout, because the
// declarations name forge's markers and nothing else may supply them.
//
// Copied rather than emptied in place, because the committed files are what
// every other test in this package compares against.
func unwritten(t *testing.T) string {
	t.Helper()

	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatalf("resolving this checkout: %v", err)
	}

	held := t.TempDir()
	mod := "module cleanexample\n\ngo 1.27.0\n\nrequire github.com/okian/forge v0.0.0\n\n" +
		"replace github.com/okian/forge => " + root + "\n"
	if err := os.WriteFile(filepath.Join(held, "go.mod"), []byte(mod), 0o600); err != nil {
		t.Fatalf("writing the copy's go.mod: %v", err)
	}

	dir := filepath.Join(held, "people")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatalf("making the copy: %v", err)
	}

	// The hand-written half only, which is what a checkout with no generated
	// code in it holds. The tests go too: they name what has not been written
	// yet, so a package holding them would not load.
	sources, err := filepath.Glob(filepath.Join(example, "*.go"))
	if err != nil {
		t.Fatalf("looking for the example's files: %v", err)
	}

	kept := 0
	for _, path := range sources {
		name := filepath.Base(path)
		if strings.HasSuffix(name, ".gen.go") || strings.HasSuffix(name, "_test.go") {
			continue
		}

		one, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("reading %s: %v", name, err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), one, 0o600); err != nil {
			t.Fatalf("writing %s: %v", name, err)
		}
		kept++
	}

	if kept == 0 {
		t.Fatal("the copy holds no source, so nothing would be generated from it")
	}

	return dir
}

// walking runs the real pipeline over a directory and returns its files.
//
// Apart from [regenerating] because the copy is not the example: it is loaded
// from somewhere else, so the declarations resolve to another import path and
// the two cannot share a session.
func walking(t *testing.T, dir string) map[string][]byte {
	t.Helper()

	// From inside the copy, because a pattern is resolved against the working
	// directory and an absolute path into another module is not a pattern the
	// go command will load.
	env := &environment{stdout: os.Stdout, stderr: os.Stderr, pipeline: stages(layers.Builtins()), dir: dir}

	found, err := env.pipeline.follow(env, env.loadConfig("."))
	if err != nil {
		t.Fatalf("walking the copy: %v", err)
	}
	if problems := found.All(); !problems.Empty() {
		t.Fatalf("the copy did not resolve cleanly:\n%s", problems.Render())
	}

	packages := grouped(found.Requests)
	if len(packages) != 1 {
		t.Fatalf("the copy is %d packages, want 1", len(packages))
	}

	files, problems := generated.Package(packages[0].path, packages[0].name, packages[0].requests,
		against(layers.Builtins(), found.Session))
	if !problems.Empty() {
		t.Fatalf("generating the copy was refused:\n%s", problems.Render())
	}

	out := make(map[string][]byte, len(files))
	for _, one := range files {
		out[one.Name] = one.Content
	}
	return out
}

// ours returns the paths of the generated files committed in the example.
//
// By the name forge writes rather than by the header inside, because what this
// is for is finding a file forge would have written and no longer does — and
// the test that reads the headers has to find such a file before it can say the
// header is missing.
func ours() ([]string, error) {
	return filepath.Glob(filepath.Join(example, "*.gen.go"))
}

// regenerating runs the real pipeline over the example and returns its files.
func regenerating(t *testing.T) map[string][]byte {
	t.Helper()

	env := &environment{stdout: os.Stdout, stderr: os.Stderr, pipeline: stages(layers.Builtins())}

	found, err := env.pipeline.follow(env, env.loadConfig(example))
	if err != nil {
		t.Fatalf("walking the example: %v", err)
	}
	if problems := found.All(); !problems.Empty() {
		t.Fatalf("the example did not resolve cleanly:\n%s", problems.Render())
	}

	packages := grouped(found.Requests)
	if len(packages) != 1 {
		t.Fatalf("the example is %d packages, want 1", len(packages))
	}

	files, problems := generated.Package(packages[0].path, packages[0].name, packages[0].requests,
		against(layers.Builtins(), found.Session))
	if !problems.Empty() {
		t.Fatalf("generating the example was refused:\n%s", problems.Render())
	}
	if len(files) == 0 {
		t.Fatal("generating the example produced no files")
	}

	out := make(map[string][]byte, len(files))
	for _, file := range files {
		out[file.Name] = file.Content
	}
	return out
}

// body returns everything after a generated file's header.
//
// The header is the one part of the file this comparison cannot use, and the
// three fields in it are excluded for two different reasons.
//
// The fingerprint moves on its own. It is derived from the Go version as well
// as from the two the header prints, so regenerating on a runner that has just
// picked up a patch release rewrites that line and nothing else.
//
// The two version lines are stable between the producers this test involves and
// are excluded anyway. Neither `go run`, which is what regenerates the example,
// nor `go test`, which is what runs this, records a VCS stamp, so both write
// (devel) and comparing them would pass — until somebody regenerates with a
// binary they built, which stamps a pseudo-version derived from their checkout
// and changes with every commit. Excluding the lines is what keeps a
// contributor's habit from being a failure.
//
// None of that weakens the check. What a header records is which run produced
// the file; the body is what the file does, and a change to what forge
// generates is a change to the body every time.
func body(src []byte) []byte {
	_, ok := emit.ReadHeader(src)
	if !ok {
		return src
	}

	// Every comment line from the top, which is the header and nothing else
	// because the emitter writes a blank line between it and whatever comes
	// next. That blank line is the invariant this leans on: an emitter that
	// wrote a doc comment directly beneath the header would have it dropped
	// here and compared against nothing.
	for rest := src; len(rest) > 0; {
		line, after, _ := bytes.Cut(rest, []byte("\n"))
		if !bytes.HasPrefix(bytes.TrimSpace(line), []byte("//")) {
			return rest
		}
		rest = after
	}

	return nil
}

// The committed example carries forge's own header, filled in.
//
// [TestTheExampleIsWhatForgeGeneratesToday] compares everything below it and so
// would pass on a file whose header had been edited away — at which point forge
// would no longer recognise the file as its own, would refuse to refresh it,
// and would report it as somebody's leftover. That is a failure the body
// comparison cannot see, so it is checked here by shape: forge's marker, and
// the three fields it records.
//
// By shape and not by value. What the fingerprint should be depends on the Go
// version that generated the file, so a committed one is right for the
// toolchain it was written on and stale for the next — which is the answer
// every user gets after an upgrade, and is what `make example` is for. This
// says the field is there and readable, and deliberately says nothing about
// what is in it.
func TestTheExampleCarriesForgesHeader(t *testing.T) {
	found, err := ours()
	if err != nil {
		t.Fatalf("looking for generated files: %v", err)
	}
	if len(found) == 0 {
		t.Fatal("the example holds no generated files")
	}

	for _, path := range found {
		src, err := os.ReadFile(path)
		if err != nil {
			t.Errorf("reading %s: %v", path, err)
			continue
		}

		header, ok := emit.ReadHeader(src)
		switch {
		case !ok:
			t.Errorf("%s does not carry forge's marker, so forge would not recognise it as its own", path)
		case header.Forge == "":
			t.Errorf("%s records no forge version", path)
		case header.Markers == "":
			t.Errorf("%s records no marker version", path)
		case header.Inputs == "":
			t.Errorf("%s records no fingerprint, so a staleness check would have nothing to compare", path)
		}
	}
}

// Explaining a package that has already been generated says nothing about
// collisions.
//
// The example is committed with its generated files beside it, which is the
// arrangement forge is built for and the one this verb is most often run in:
// somebody generates, reads the file, and asks what produced it. Every name in
// that file is one that generating the declaration again would write, so a
// collision check unable to tell a previous run's work from the author's
// reports a diagnostic for every name it wrote last time — above a report that
// was correct all along, about a package with nothing wrong with it.
//
// Against the real example rather than a declaration built here, because the
// fault needs a package whose generated files are on disk and loaded, and a
// stand-in session has neither.
func TestExplainingAPackageThatIsAlreadyGenerated(t *testing.T) {
	var out, errs bytes.Buffer
	env := &environment{stdout: &out, stderr: &errs, pipeline: stages(layers.Builtins())}

	cmd, _ := lookup("explain")
	if err := cmd.run(env, cmd, []string{"-t", "Persons", example}); err != nil {
		t.Fatalf("explaining the generated example: %v\n%s", err, errs.String())
	}

	if errs.Len() != 0 {
		t.Errorf("explaining a generated package complained:\n%s", errs.String())
	}
	if !strings.Contains(out.String(), "Persons") {
		t.Errorf("the question was not answered:\n%s", out.String())
	}
}
