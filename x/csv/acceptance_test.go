package csv_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// example names the worked example's directory and the two files of it that a
// person wrote.
//
// Everything else in there is output, and this test is what says so: the two
// named here go into a module of their own, the layer is run over them, and
// what comes out is compared against what is committed.
const example = "ledger"

var authored = []string{"ledger.go", "spec.go"}

// The worked example is what this layer would generate for it today.
//
// A committed output is a promise, and the promise is not that the files exist:
// it is that they are what the declarations beside them ask for. A change to
// what the layer writes leaves them stale, and stale generated code is worse
// than none — it compiles, so nothing fails until somebody reads it and
// believes it.
//
// Regenerated into a module of its own rather than in place. A test that wrote
// into the tree would be a test that changed what it was checking, and one that
// ran second would compare the output against itself.
func TestTheWorkedExampleIsWhatTheLayerWritesNow(t *testing.T) {
	root := module(t, fixture{
		name:    example,
		subject: afterPackage(t, filepath.Join(example, "ledger.go")),
		spec:    afterPackage(t, filepath.Join(example, "spec.go")),
	})

	if out, status := running(t, catalog(t), "-C", root, "generate", "."); status != 0 {
		t.Fatalf("regenerating the worked example exited %d:\n%s", status, out)
	}

	made := generatedIn(t, root)
	committed := generatedIn(t, example)

	slices.Sort(made)
	slices.Sort(committed)

	if !slices.Equal(made, committed) {
		t.Fatalf("the layer writes %q for the example and %q are committed", made, committed)
	}

	for _, one := range made {
		want := read(t, filepath.Join(example, one))
		got := read(t, filepath.Join(root, one))

		// The fingerprint is what a staleness check compares, so a file
		// without one cannot be checked at all.
		if !strings.Contains(want, fingerprint) {
			t.Errorf("%s records no fingerprint", one)
		}

		if body(got) != body(want) {
			t.Errorf("%s is not what the layer writes now; regenerate the example", one)
		}
	}
}

// fingerprint opens the header line a staleness check reads.
const fingerprint = "// inputs "

// body returns everything after a generated file's header.
//
// The header is the one part of the file this comparison cannot use. The
// fingerprint is derived from the Go version as well as from the declarations,
// so regenerating on a runner that has just picked up a patch release rewrites
// that line and nothing else; and the two version lines say (devel) from `go
// run` and a pseudo-version from a binary somebody built, so comparing them
// would turn a contributor's habit into a failure.
//
// None of that weakens the check. What a header records is which run produced
// the file; the body is what the file does, and a change to what the layer
// generates is a change to the body every time.
func body(src string) string {
	_, after, found := strings.Cut(src, fingerprint)
	if !found {
		return src
	}

	_, out, _ := strings.Cut(after, "\n")

	return out
}

// afterPackage returns a file's contents from its package clause onward,
// without the clause.
//
// The harness writes the package clause and the build tag, so a fixture hands
// it the interesting half. The example's own files carry both, being real
// source rather than a fixture, so this is what takes them off again.
func afterPackage(t *testing.T, path string) string {
	t.Helper()

	held := read(t, path)

	at := strings.Index(held, "\npackage ")
	if at < 0 {
		t.Fatalf("%s has no package clause", path)
	}

	_, out, found := strings.Cut(held[at+1:], "\n\n")
	if !found {
		t.Fatalf("%s has nothing after its package clause", path)
	}

	return out
}

// The example holds nothing forge did not write and nobody edited.
//
// A generated file carries a line saying so, and this is what keeps a helpful
// edit to one from surviving a review: the file would be overwritten by the
// next run, so the edit is a change that disappears.
func TestTheWorkedExampleHoldsOnlyWhatItShould(t *testing.T) {
	held, err := os.ReadDir(example)
	if err != nil {
		t.Fatalf("reading the example: %v", err)
	}

	for _, one := range held {
		name := one.Name()

		switch {
		case one.IsDir():
			t.Errorf("the example holds a directory, %s, and is meant to be one package", name)

		case strings.HasSuffix(name, "_test.go"), slices.Contains(authored, name):
			// Written by a person, and read as usage.

		case strings.HasPrefix(name, "zz_forge_"):
			if !strings.Contains(read(t, filepath.Join(example, name)), "DO NOT EDIT") {
				t.Errorf("%s is named as generated and does not say so", name)
			}

		default:
			t.Errorf("the example holds %s, which is neither authored nor generated", name)
		}
	}
}
