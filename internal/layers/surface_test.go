package layers_test

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// The layer packages written against nothing but the published surface, and the
// reason each of the others is not.
//
// Fourteen directories, of which twelve claim a marker: embedded and failures
// are helpers the others share rather than layers of their own, and they are
// held to the same rule because they are written the same way.
//
// A layer here imports no package of forge's that a third party could not
// import, so it is the proof that [github.com/okian/forge/plugin] is enough to
// write one with. Held as a list rather than counted, because what matters is
// which: a layer that started reaching for something internal would keep the
// count and lose the claim.
//
// The rest reach for machinery that is deliberately not published, and the
// entry says which. Two kinds. The template rewriter and the shared views are
// how forge writes its own container bodies and hands out its own sequences —
// an implementation strategy rather than a contract, and publishing either
// would freeze the shape of every template in the tree. The sibling layers are
// forge reusing itself: an error type, an embedded-field walk, a check one
// layer runs on another's behalf. A third party writes their own, which is what
// having none of forge's own means.
var against = map[string]string{
	"clone":    "",
	"embedded": "",
	"enum":     "",
	"patch":    "",

	"jsoncodec": "the shared JSON wire runtime the generated codecs call into",

	"builder":     "the failure type and the check layer it runs",
	"collection":  "the template rewriter and the shared sequence view",
	"contenthash": "the embedded-field walk",
	"failures":    "the embedded-field walk",
	"guarded":     "the locked view a decorator hands into a closure",
	"redact":      "the scalar helpers a display tag earns",
	"ring":        "the template rewriter",
	"slice":       "the template rewriter",
	"validate":    "the failure type",
}

// Every layer is either written against the published surface alone or written
// down as reaching past it.
//
// Two directions, and both matter. A layer listed as needing nothing that
// starts importing something internal has quietly stopped being proof that a
// third party could have written it; a layer listed as needing something that
// stops needing it is proof nobody is claiming, and the list should say so.
//
// The imports are read from the source rather than from the build, because what
// is being asked is what the files say — a package that could be reached
// through another package's import is not one this layer names, and naming is
// what a third party is stopped from doing.
func TestWhichLayersAreWrittenAgainstThePublishedSurface(t *testing.T) {
	found := layerPackages(t)

	if got, want := slices.Sorted(keys(found)), slices.Sorted(keys(against)); !slices.Equal(got, want) {
		t.Fatalf("the tree holds %v and this test knows %v", got, want)
	}

	for name, reason := range against {
		held := found[name]

		switch {
		case reason == "" && len(held) > 0:
			t.Errorf("%s is written down as needing nothing internal and imports %v", name, held)

		case reason != "" && len(held) == 0:
			t.Errorf("%s is written down as needing %q and imports nothing internal — "+
				"move it up and say so", name, reason)
		}
	}
}

// layerPackages returns the internal packages each layer imports, by layer.
//
// The empty entry is the interesting one and is kept: a layer that imports
// nothing has to appear, or the two lists could not be compared.
func layerPackages(t *testing.T) map[string][]string {
	t.Helper()

	held, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("looking for the layers: %v", err)
	}

	out := make(map[string][]string)

	for _, one := range held {
		// A directory holding Go of its own. Anything else beside the layers is
		// not one of them: testdata is a directory the go command ignores and
		// this has to ignore too, or a fixture added under it is reported as a
		// layer nobody wrote.
		if !one.IsDir() || one.Name() == "testdata" {
			continue
		}

		found := importsOf(t, one.Name())
		if found == nil && !holds(t, one.Name()) {
			continue
		}
		out[one.Name()] = found
	}

	if len(out) == 0 {
		t.Fatal("no layer packages were found, so nothing was checked")
	}
	return out
}

// holds reports whether a directory has Go source of its own, which is what
// tells a layer from a directory that only holds one.
func holds(t *testing.T, dir string) bool {
	t.Helper()

	found, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading %s: %v", dir, err)
	}

	for _, one := range found {
		if !one.IsDir() && strings.HasSuffix(one.Name(), ".go") {
			return true
		}
	}
	return false
}

// importsOf returns the internal packages a directory's own source imports,
// without repeats.
//
// Tests are left out. A test may reach for whatever it needs to set a fixture
// up — the golden harness, a loader — and what is being asked here is what the
// layer itself is written against.
func importsOf(t *testing.T, dir string) []string {
	t.Helper()

	found, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading %s: %v", dir, err)
	}

	var out []string
	for _, one := range found {
		name := one.Name()
		if one.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}

		file, err := parser.ParseFile(token.NewFileSet(), filepath.Join(dir, name), nil,
			parser.ImportsOnly|parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("reading the imports of %s: %v", name, err)
		}

		for _, spec := range file.Imports {
			path, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				t.Fatalf("%s imports something unquotable: %v", name, spec.Path.Value)
			}

			held, is := strings.CutPrefix(path, "github.com/okian/forge/internal/")
			if is && !slices.Contains(out, held) {
				out = append(out, held)
			}
		}
	}

	slices.Sort(out)
	return out
}

// keys yields a map's keys, so that two sets of names can be compared without
// depending on how a map iterated.
func keys[V any](held map[string]V) func(func(string) bool) {
	return func(yield func(string) bool) {
		for key := range held {
			if !yield(key) {
				return
			}
		}
	}
}
