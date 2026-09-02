package csv_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/okian/forge/driver"
	"github.com/okian/forge/x/csv"
)

// catalog returns forge's own layers with this one registered into them.
//
// Into forge's rather than into an empty registry, which is what a plugin
// binary's main does and what the driver's documentation says to do: a stack
// composes across every layer the run knows, so a catalog holding this layer
// alone could generate only for a declaration naming a transport and nothing
// else — and the container beneath a transport is one of forge's.
//
// It is also the whole of the claim about taking a marker over. Csv is a marker
// forge publishes with no generator behind it, so registering this into the
// catalog that already holds the placeholder is the case that used to be
// refused.
func catalog(t *testing.T) *driver.Registry {
	t.Helper()

	held := driver.Builtins()
	if err := held.Register(csv.New()); err != nil {
		t.Fatalf("registering the layer into forge's own catalog: %v", err)
	}

	return held
}

// running runs one command line against a catalog and returns what it said and
// the status it ended with.
func running(t *testing.T, held *driver.Registry, args ...string) (string, int) {
	t.Helper()

	var out bytes.Buffer
	status := driver.Run(held, args, &out, &out)

	return out.String(), status
}

// checkout is the module this layer is written against, as an absolute path.
//
// A fixture is a module of its own in a directory the test makes, so it cannot
// reach forge by a relative path the way this module's own go.mod does. The
// path is derived rather than written down, so that moving the module does not
// leave a test pointing at where it used to be.
func checkout(t *testing.T) string {
	t.Helper()

	held, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("finding the checkout this layer is written against: %v", err)
	}

	return held
}

// fixture is one package to generate for: a subject, and the declarations over
// it that go in a file under the marker build tag.
type fixture struct {
	// name is the package's own name, and dir the directory it goes in under
	// the module the test builds. An empty dir puts it at the module root.
	name string
	dir  string

	// subject is the ordinary Go the declarations are over, and spec is what
	// goes under the build tag. Neither carries its package clause: the harness
	// writes that, so a fixture reads as the interesting half. A spec carries
	// its own imports, because which marker a declaration is over is the
	// interesting half of some of these.
	subject string
	spec    string
}

// module writes the fixtures into a module of their own and returns its
// directory.
//
// A module rather than a package, because a declaration over one of forge's
// markers imports forge — and a directory with no go.mod is not something the
// loader can resolve that import from. A filesystem replace needs no checksum,
// so the module needs no go.sum and the test needs no network.
func module(t *testing.T, held ...fixture) string {
	t.Helper()

	root := t.TempDir()

	write(t, filepath.Join(root, "go.mod"), "module example.com/fixture\n\n"+
		"go 1.27.0\n\n"+
		"require github.com/okian/forge v0.0.0\n\n"+
		"replace github.com/okian/forge => "+checkout(t)+"\n")

	for _, one := range held {
		into := filepath.Join(root, one.dir)
		if err := os.MkdirAll(into, 0o750); err != nil {
			t.Fatalf("making the fixture's directory: %v", err)
		}

		write(t, filepath.Join(into, "subject.go"),
			"package "+one.name+"\n\n"+one.subject)

		if one.spec == "" {
			continue
		}

		write(t, filepath.Join(into, "spec.go"),
			"//go:build forgespec\n\npackage "+one.name+"\n\n"+one.spec)
	}

	return root
}

// write puts a file where the fixture wants it.
func write(t *testing.T, path, content string) {
	t.Helper()

	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("writing %s: %v", filepath.Base(path), err)
	}
}

// read returns a file's contents, failing the test where it cannot.
func read(t *testing.T, path string) string {
	t.Helper()

	held, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}

	return string(held)
}

// The files forge writes into a package.
//
// One for the package, and a second only where the language requires it: a
// spec-form declaration's type is written by forge under one build constraint
// and by the author's own file under its complement, so the two can never be in
// scope together and the stub file stands in for the package under the second.
//
// Written down here rather than asked of forge, because a layer lives outside
// it: `internal/generate` names them and no module but forge's own may import
// it. So this is a layer author reading the output, which is the position the
// rest of this module is written from — and a rename of either would fail here
// rather than somewhere further from the cause.
const (
	generated = "forge.gen.go"
	stubs     = "forge_stubs.gen.go"
)

// generatedIn returns the names of the files forge wrote into a directory, in
// the order above.
func generatedIn(t *testing.T, dir string) []string {
	t.Helper()

	var out []string

	for _, one := range []string{generated, stubs} {
		if _, err := os.Stat(filepath.Join(dir, one)); err == nil {
			out = append(out, one)
		} else if !os.IsNotExist(err) {
			t.Fatalf("looking for %s in %s: %v", one, dir, err)
		}
	}

	return out
}
