package templates_test

import (
	"go/parser"
	"go/token"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"
)

// Every import a template writes without a name binds the last element of its
// path.
//
// Reading a template's imports is how a generated file learns which package a
// qualified identifier in it refers to, and a specification without a name does
// not say: it binds whatever the package calls itself, which is only usually
// the last element of the path. math/rand/v2 is called rand, a directory named
// for what it holds is called something else again, and either would be
// recorded under the wrong name.
//
// So the answer is taken from the path, and this is what makes that true rather
// than likely. The templates are forge's own files and their imports are
// forge's to choose; the day one of them imports a package whose name is not
// the last element of its path, this fails instead of the output.
func TestEveryTemplateImportBindsTheLastElementOfItsPath(t *testing.T) {
	// Every directory called tmpl under the tree, rather than the layers' —
	// what makes a file a template is that Verbatim reads it, and the shared
	// helpers are read the same way the layers' are. A glob that named where
	// templates live today would be a claim about every template that is true
	// until somebody adds one somewhere else.
	found, err := filepath.Glob(filepath.Join("..", "..", "internal", "*", "*", "tmpl", "*.go"))
	if err != nil {
		t.Fatalf("looking for the templates: %v", err)
	}

	shallow, err := filepath.Glob(filepath.Join("..", "..", "internal", "*", "tmpl", "*.go"))
	if err != nil {
		t.Fatalf("looking for the templates: %v", err)
	}
	found = append(found, shallow...)

	// A template's own tests are not templates, and neither is a fixture
	// written to be read wrongly on purpose. Both would otherwise be held to a
	// rule about what forge writes into somebody else's package.
	found = slices.DeleteFunc(found, func(name string) bool {
		return strings.HasSuffix(name, "_test.go") || strings.Contains(name, "testdata")
	})

	if len(found) < 3 {
		t.Fatalf("only %d templates were found, and there are more than that: %v", len(found), found)
	}

	fset := token.NewFileSet()
	bare := make(map[string][]string)

	for _, name := range found {
		file, err := parser.ParseFile(fset, name, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("reading %s: %v", name, err)
		}

		for _, held := range file.Imports {
			if held.Name != nil {
				continue
			}

			unquoted, err := strconv.Unquote(held.Path.Value)
			if err != nil {
				t.Errorf("%s imports %s, which is not a path", name, held.Path.Value)
				continue
			}
			bare[unquoted] = append(bare[unquoted], name)
		}
	}

	paths := make([]string, 0, len(bare))
	for one := range bare {
		paths = append(paths, one)
	}
	if len(paths) == 0 {
		t.Fatal("the templates import nothing without a name, so this checked nothing")
	}

	loaded, err := packages.Load(&packages.Config{Mode: packages.NeedName}, paths...)
	if err != nil {
		t.Fatalf("resolving what the templates import: %v", err)
	}

	for _, pkg := range loaded {
		if pkg.Name == "" {
			t.Errorf("%s could not be resolved, so what it binds is unknown", pkg.PkgPath)
			continue
		}
		if last := path.Base(pkg.PkgPath); pkg.Name != last {
			t.Errorf("%s is called %s rather than %s, and is imported without a name by %v — "+
				"give it one, or the file it lands in records it as %s",
				pkg.PkgPath, pkg.Name, last, bare[pkg.PkgPath], last)
		}
	}
}
