package generate_test

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/okian/forge/internal/generate"
)

// A package's output goes in one file, whose name is a constant.
//
// It used to be derived from the declaration, which read well in a diff and
// meant the name could be a declaration's name in disguise: one called Test or
// Windows or Data_linux wanted a file the compiler quietly leaves out of the
// build, which looks exactly like forge not having run. A constant cannot be
// any of those, and this is what says so.
func TestWhatAFileIsCalled(t *testing.T) {
	if got, want := generate.Name(), "forge.gen.go"; got != want {
		t.Errorf("a package's output is written to %s, want %s", got, want)
	}
	if got, want := generate.Stubs(), "forge_stubs.gen.go"; got != want {
		t.Errorf("what stands in under the tag is written to %s, want %s", got, want)
	}

	for _, name := range generate.Names() {
		if !generate.Ours(name) {
			t.Errorf("%s is a file forge writes and is not recognised as one", name)
		}
		if generate.Superseded(name) {
			t.Errorf("%s is a file forge writes and is reported as one an older forge wrote", name)
		}
	}
}

// What an older forge wrote is still recognised, which is the whole of what
// makes upgrading survivable: those files hold the declarations the new one
// holds, so the package stops building the moment it is written, and only
// something that still knows their names can say why.
func TestWhatAnOlderForgeWrote(t *testing.T) {
	for _, one := range []struct {
		name       string
		ours       bool
		superseded bool
	}{
		{"forge.gen.go", true, false},
		{"forge_stubs.gen.go", true, false},

		{"zz_forge_persons.go", true, true},
		{"zz_forge_shared.go", true, true},
		{"zz_forge_stubs.go", true, true},
		{"zz_forge_windows_gen.go", true, true},

		{"person.go", false, false},
		{"zz_forge_notes.txt", false, false},
		{"forge.gen", false, false},
		{"", false, false},
	} {
		if got := generate.Ours(one.name); got != one.ours {
			t.Errorf("Ours(%q) = %v, want %v", one.name, got, one.ours)
		}
		if got := generate.Superseded(one.name); got != one.superseded {
			t.Errorf("Superseded(%q) = %v, want %v", one.name, got, one.superseded)
		}
	}
}

// Neither name is one the go command reads a build constraint out of.
//
// The rule is about the end of a file's name: a trailing _test makes a test
// file, and a trailing operating system or architecture — or the two together —
// constrains it to that platform. A file forge writes and the compiler leaves
// out is the failure a person is least likely to attribute to a file name.
//
// Asked of the toolchain rather than of a list written here, because the only
// way to find out that a release added a platform is to ask the release.
func TestNeitherNameCarriesAConstraintNobodyWrote(t *testing.T) {
	out, err := exec.CommandContext(t.Context(), "go", "tool", "dist", "list").Output()
	if err != nil {
		t.Skipf("the toolchain would not list its platforms: %v", err)
	}

	ends := []string{"_test"}
	for _, pair := range strings.Fields(string(out)) {
		system, architecture, ok := strings.Cut(pair, "/")
		if !ok {
			continue
		}
		ends = append(ends, "_"+system, "_"+architecture, "_"+system+"_"+architecture)
	}

	for _, name := range generate.Names() {
		stem := strings.TrimSuffix(name, ".go")

		for _, end := range ends {
			if strings.HasSuffix(stem, end) {
				t.Errorf("%s ends in %s, which the build reads as a constraint", name, end)
			}
		}
	}
}
