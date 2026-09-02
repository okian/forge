package csv_test

import (
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// The layer is written against the published surface and nothing else.
//
// It is the whole claim this module exists to make, and it is not one a reader
// can check by skimming: an import of forge's own internals would compile here,
// because this module is in forge's repository and the go command's internal
// rule is about import paths rather than about modules. So it is written down.
//
// The standard library is fair game and is not the interesting half. What is
// checked is that the one forge package this layer names is the one forge
// promises: [github.com/okian/forge/plugin], whose documentation says what a
// name there means and what it costs to change it.
func TestTheLayerImportsNothingButThePublishedSurface(t *testing.T) {
	looked := 0

	for _, name := range sources(t) {
		looked++

		// A main is allowed one thing more: the driver, which is what linking a
		// layer into a binary goes through. It is not the layer, and holding it
		// to the layer's rule would be refusing the one import it exists for.
		allowed := []string{promised}
		if strings.HasPrefix(name, "cmd"+string(filepath.Separator)) {
			allowed = append(allowed, driven, own)
		}

		for _, path := range imported(t, name) {
			switch {
			case standard(path):
				// The standard library, which generated code and generators
				// alike may use.

			case slices.Contains(allowed, path):
				// What this file is written against.

			default:
				t.Errorf("%s imports %s; it may name %q and the standard library",
					name, path, allowed)
			}
		}
	}

	if looked == 0 {
		t.Fatal("no source file was read, so this checked nothing")
	}
}

// The three paths anything here may name: the surface a layer is written
// against, the driver a binary links one through, and this module itself.
const (
	promised = "github.com/okian/forge/plugin"
	driven   = "github.com/okian/forge/driver"
	own      = "github.com/okian/forge/x/csv"
)

// sources returns every non-test Go file of the module, wherever it sits.
//
// The whole tree rather than the directory this test is in. A layer that grew a
// package of its own could otherwise reach past the published surface with
// nothing noticing — the go command's internal rule is about import paths, and
// this module sits under forge's own, so an internal import here compiles.
//
// The binaries under cmd are walked too, and given the driver as well: a main
// is not the layer, but it is still code somebody has to keep honest, and
// naming what it may import is cheaper than leaving a directory unwatched.
//
// One directory is left out. The worked example is a subject and the files
// forge wrote from it, which is output rather than code anybody wrote — and it
// imports nothing of forge's at all, since generated code imports the standard
// library and the subject's own dependencies and nothing else.
func sources(t *testing.T) []string {
	t.Helper()

	var out []string

	err := filepath.WalkDir(".", func(path string, entry fs.DirEntry, err error) error {
		switch {
		case err != nil:
			return err

		case entry.IsDir():
			if path == example {
				return fs.SkipDir
			}

			return nil

		case !strings.HasSuffix(path, ".go"), strings.HasSuffix(path, "_test.go"):
			return nil
		}

		out = append(out, path)

		return nil
	})
	if err != nil {
		t.Fatalf("walking the module: %v", err)
	}

	return out
}

// standard reports whether an import path names a package of the standard
// library.
//
// By the first element rather than by the whole path, which is the rule the go
// command uses: a path whose first element holds a dot names a module somebody
// hosts somewhere, and one that does not is the standard library. Testing the
// whole path agrees for every real case and describes a different rule.
func standard(path string) bool {
	first, _, _ := strings.Cut(path, "/")
	return !strings.Contains(first, ".")
}

// The layer names the published surface at all.
//
// The other direction of the test above, and it is not redundant: a layer that
// imported nothing would pass that one, and a layer that imported nothing is a
// layer that has stopped implementing the interface.
func TestTheLayerNamesThePublishedSurface(t *testing.T) {
	for _, name := range sources(t) {
		if strings.HasPrefix(name, "cmd"+string(filepath.Separator)) {
			continue
		}
		if slices.Contains(imported(t, name), promised) {
			return
		}
	}

	t.Errorf("no file of the layer names %s, the package it is written against", promised)
}

// The binary that runs the layer is the only thing here that reaches further.
//
// It imports the driver, which is what a plugin binary's main is, and it is
// deliberately not part of the claim above: a main that links a layer is not
// the layer.
func TestTheBinaryLinksTheDriver(t *testing.T) {
	held := imported(t, filepath.Join("cmd", "forge-csv", "main.go"))

	for _, want := range []string{driven, own} {
		if !slices.Contains(held, want) {
			t.Errorf("the binary does not link %s", want)
		}
	}
}

// imported returns the paths one file imports.
func imported(t *testing.T, path string) []string {
	t.Helper()

	file, err := parser.ParseFile(token.NewFileSet(), path, nil,
		parser.SkipObjectResolution|parser.ImportsOnly)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}

	out := make([]string, 0, len(file.Imports))

	for _, one := range file.Imports {
		unquoted, err := strconv.Unquote(one.Path.Value)
		if err != nil {
			t.Fatalf("%s imports %s, which is not a path", path, one.Path.Value)
		}
		out = append(out, unquoted)
	}

	return out
}
