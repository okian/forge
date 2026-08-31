package generate_test

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/okian/forge/internal/generate"
)

// A file is named after the declaration it was generated for, in lower case, so
// that a diff says which declaration changed before anybody opens it.
func TestWhatAFileIsCalled(t *testing.T) {
	cases := map[string]string{
		"Persons":  "zz_forge_persons.go",
		"persons":  "zz_forge_persons.go",
		"Sessions": "zz_forge_sessions.go",

		// A name the Go build reads as something. A file ending in _test.go is
		// not in the ordinary build at all, and one ending in an operating
		// system or an architecture carries a constraint nobody wrote — which
		// looks exactly like forge not having run.
		"Test":    "zz_forge_test_gen.go",
		"Windows": "zz_forge_windows_gen.go",
		"Android": "zz_forge_android_gen.go",
		"Arm64":   "zz_forge_arm64_gen.go",
		"Wasm":    "zz_forge_wasm_gen.go",

		// And a name that merely contains one is not one.
		"Windowsill": "zz_forge_windowsill.go",
		"Tester":     "zz_forge_tester.go",

		// A declaration's own underscores land where the rule looks, which is
		// the end of the file's name and not one element of it.
		"My_test":          "zz_forge_my_test_gen.go",
		"Data_linux":       "zz_forge_data_linux_gen.go",
		"Linux_amd64":      "zz_forge_linux_amd64_gen.go",
		"Foo_windows_test": "zz_forge_foo_windows_test_gen.go",

		// And an underscore that lands somewhere harmless is harmless.
		"Two_words": "zz_forge_two_words.go",
	}

	for declared, want := range cases {
		if got := generate.Named(declared); got != want {
			t.Errorf("%s is written to %s, want %s", declared, got, want)
		}
	}

	if got, want := generate.Shared(), "zz_forge_shared.go"; got != want {
		t.Errorf("what a package shares is written to %s, want %s", got, want)
	}
}

// The operating systems and architectures a file name can carry are written
// down here because nothing exports them, so they are asked of the toolchain
// rather than trusted.
//
// Being wrong about one is a generated file the compiler silently leaves out,
// which is the failure a person is least likely to attribute to a file name —
// and the only way to find out that a release added one is to ask the release.
func TestTheNamesTheBuildReadsAreAllKnown(t *testing.T) {
	out, err := exec.CommandContext(t.Context(), "go", "tool", "dist", "list").Output()
	if err != nil {
		t.Skipf("the toolchain would not list its platforms: %v", err)
	}

	for _, pair := range strings.Fields(string(out)) {
		system, architecture, ok := strings.Cut(pair, "/")
		if !ok {
			continue
		}

		// Each on its own, the two together as the go command reads a pair, and
		// each at the end of a longer name — which is where a declaration's own
		// underscore puts it.
		for _, name := range []string{
			system, architecture,
			system + "_" + architecture,
			"Held_" + system, "Held_" + architecture,
		} {
			if got := generate.Named(name); !strings.HasSuffix(got, "_gen.go") {
				t.Errorf("a declaration called %s is written to %s, which the build reads as %s",
					name, got, pair)
			}
		}
	}
}
