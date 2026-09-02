package driver_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/okian/forge/driver"
)

// A layer forge does not ship is registered, resolved, composed, generated for,
// and what it wrote compiles.
//
// This is the whole claim the published surface exists to make, and it is made
// against a module that is not this one: the marker is declared in the third
// party's own package, the layer is written against nothing but
// [github.com/okian/forge/plugin], and the declaration names no marker of
// forge's at all.
//
// The stages are walked in one test rather than four, because what is being
// checked is that they agree. A layer that listed and did not resolve, or
// resolved and generated nothing, would pass three tests out of four and be
// useless — and the failure worth catching is the seam, not the stage.
func TestALayerForgeDoesNotShip(t *testing.T) {
	dir := copied(t)

	catalog := driver.Builtins()
	catalog.MustRegister(tally{})

	// Listed, with what it says about itself.
	if out, _ := running(t, catalog, "list"); !strings.Contains(out, "Tally") {
		t.Errorf("the layer is not in the catalog it was registered into:\n%s", out)
	} else if !strings.Contains(out, "count of the elements") {
		t.Errorf("the layer's own summary is not what the catalog prints:\n%s", out)
	}

	// Resolved, and composed with a storage layer filled in beneath it.
	out, status := running(t, catalog, "-C", dir, "explain", "-t", "People", ".")
	if status != 0 {
		t.Fatalf("explaining the declaration exited %d:\n%s", status, out)
	}

	// The stack as written, the storage filled in beneath it, and the layer's
	// own summary — which is the one thing in the report only the layer could
	// have supplied.
	for _, want := range []string{"Tally[Person]", "Slice", "count of the elements"} {
		if !strings.Contains(out, want) {
			t.Errorf("the explanation does not carry %q:\n%s", want, out)
		}
	}

	// Generated, and what was generated is what the layer wrote.
	if out, status := running(t, catalog, "-C", dir, "generate", "."); status != 0 {
		t.Fatalf("generating exited %d:\n%s", status, out)
	}

	written := read(t, filepath.Join(dir, "zz_forge_people.go"))
	for _, want := range []string{
		"func (c People) TalliedByCity() map[string]int",
		"// TalliedByCity counts the elements sharing each City.",
	} {
		if !strings.Contains(written, want) {
			t.Errorf("the generated file does not carry %q:\n%s", want, written)
		}
	}

	// And forge's own storage layer is in the same file, which is what composing
	// with a layer somebody added means.
	if !strings.Contains(written, "func (s People) Len() int") {
		t.Errorf("the storage beneath it wrote nothing:\n%s", written)
	}
}

// A layer's own diagnostic reaches the author, under a code in the range a
// layer takes.
//
// Two things at once, and both used to fail. The code is above the range
// forge's own occupy, which [plugin.Register] refused until layers were
// expected to have any; and the diagnostic is the layer's own rather than one
// forge invented on its behalf, so the message and the hint are what the layer
// wrote.
func TestALayersOwnDiagnostic(t *testing.T) {
	dir := copied(t)

	// The option is what the layer needs and is not declared required, so the
	// refusal is the layer's own rather than the option checker's.
	spec := filepath.Join(dir, "spec.go")
	held := read(t, spec)

	if err := os.WriteFile(spec,
		[]byte(strings.Replace(held, "//forge:tally by=City", "//forge:tally", 1)), 0o600); err != nil {
		t.Fatalf("rewriting the declaration: %v", err)
	}

	catalog := driver.Builtins()
	catalog.MustRegister(tally{})

	out, status := running(t, catalog, "-C", dir, "generate", ".")
	if status == 0 {
		t.Fatalf("generating was not refused:\n%s", out)
	}

	for _, want := range []string{
		"FRG6001",
		"People names no field to count by",
		"write by=<field> on the directive",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the report does not carry %q:\n%s", want, out)
		}
	}
}

// running runs one command line against a catalog and returns what it said and
// the status it ended with.
//
// A pattern is resolved against the working directory, and the fixture is a
// module of its own somewhere else — so the run is told to start there with -C,
// which is what a person generating a package they are not standing in does.
func running(t *testing.T, catalog *driver.Registry, args ...string) (string, int) {
	t.Helper()

	var out bytes.Buffer
	status := driver.Run(catalog, args, &out, &out)

	return out.String(), status
}

// copied puts the fixture module somewhere writable and returns the package
// directory to generate in.
//
// Copied because generating writes files, and a fixture the tests write into is
// one that arrives at the next run already generated — which is the state the
// test is meant to start before.
func copied(t *testing.T) string {
	t.Helper()

	root := t.TempDir()

	for _, one := range []string{"go.mod", "tally/tally.go", "domain/person.go", "domain/spec.go"} {
		held := read(t, filepath.Join("testdata", "mine", one))

		into := filepath.Join(root, one)
		if err := os.MkdirAll(filepath.Dir(into), 0o750); err != nil {
			t.Fatalf("making the copy: %v", err)
		}
		if err := os.WriteFile(into, []byte(held), 0o600); err != nil {
			t.Fatalf("writing %s: %v", one, err)
		}
	}

	return filepath.Join(root, "domain")
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
