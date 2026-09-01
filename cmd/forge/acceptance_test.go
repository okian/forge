package main

import (
	"bytes"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/okian/forge/internal/emit"
	generated "github.com/okian/forge/internal/generate"
	"github.com/okian/forge/internal/layers"
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

// ours returns the paths of the generated files committed in the example.
//
// By the name forge writes rather than by the header inside, because what this
// is for is finding a file forge would have written and no longer does — and
// the test that reads the headers has to find such a file before it can say the
// header is missing.
func ours() ([]string, error) {
	return filepath.Glob(filepath.Join(example, "zz_forge_*.go"))
}

// regenerating runs the real pipeline over the example and returns its files.
func regenerating(t *testing.T) map[string][]byte {
	t.Helper()

	env := &environment{stdout: os.Stdout, stderr: os.Stderr, pipeline: stages()}

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
	env := &environment{stdout: &out, stderr: &errs, pipeline: stages()}

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
