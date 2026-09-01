package scalars_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/okian/forge/internal/diag"
	"github.com/okian/forge/internal/goldentest"
	"github.com/okian/forge/internal/scalars"
)

// What is written compiles, and satisfies what it was written for.
//
// The assertions are the test. Reading the source back and matching strings
// says the words are right; compiling it against the interfaces says the
// methods are, and those are different questions — a receiver on the wrong half
// of a type, a result in the wrong order, an import nothing names are all
// invisible to the first and fatal to the second.
func TestWhatIsWrittenCompiles(t *testing.T) {
	cases := map[string][]string{
		"Labelled":   {"var _ fmt.Stringer = Labelled{}"},
		"Secret":     {"var _ slog.LogValuer = Secret{}"},
		"Everything": {"var _ fmt.Stringer = Everything{}", "var _ slog.LogValuer = Everything{}"},
		"Wrapped": {
			"var _ encoding.TextMarshaler = Wrapped{}",
			"var _ encoding.TextAppender = Wrapped{}",
			"var _ encoding.TextUnmarshaler = (*Wrapped)(nil)",
		},
		"Counted": {
			"var _ encoding.TextMarshaler = Counted{}",
			"var _ encoding.TextUnmarshaler = (*Counted)(nil)",
		},

		// The rest of the scalar table. A strconv call written for the wrong
		// width, or a conversion left out of one entry, is exactly what a
		// compiler catches and a string match does not.
		"Flagged":  {"var _ encoding.TextUnmarshaler = (*Flagged)(nil)"},
		"Measured": {"var _ encoding.TextUnmarshaler = (*Measured)(nil)"},
		"Ported":   {"var _ encoding.TextUnmarshaler = (*Ported)(nil)"},
		"Quoted":   {"var _ fmt.Stringer = Quoted{}"},
		"Timed":    {"var _ fmt.Stringer = Timed{}"},
		"Held":     {"var _ slog.LogValuer = Held{}"},
		"Maybe":    {"var _ fmt.Stringer = Maybe{}"},

		// The case a run answers rather than a type: Reaching renders a field
		// through a String this same run writes for Earning.
		"Reaching": {"var _ fmt.Stringer = Reaching{}"},
	}

	for name, claims := range cases {
		t.Run(name, func(t *testing.T) {
			var diags diag.Set

			held, err := scalars.For(asked(t, name), &diags)
			if err != nil {
				t.Fatalf("writing for %s: %v", name, err)
			}
			if !diags.Empty() {
				t.Fatalf("writing for %s was refused:\n%s", name, diags.Render())
			}

			var body strings.Builder
			for _, verb := range []string{"display", "text", "log"} {
				for about, unit := range held {
					if strings.HasPrefix(about, verb+":") {
						body.WriteString(source(t, unit))
					}
				}
			}

			// And whatever this one's fields are rendered through. A subject
			// that reaches another's String does not compile beside its own
			// methods alone, which is how a case naming a method nobody writes
			// stays out of a compile check.
			for _, also := range reaches[name] {
				body.WriteString(writtenFor(t, also))
			}

			for _, one := range claims {
				body.WriteString(one + "\n")
			}

			compiling(t, name, body.String())
		})
	}
}

// compiling type-checks the written methods beside the subjects they are about.
//
// The fixture's own source, read from disk, rather than a copy of it written
// out here. What is compiled has to be one package — the methods name the type
// without qualifying it, which is how they land in the package that declares it
// — and a second copy of the subjects would be a second account of them to keep
// in step with the first.
func compiling(t *testing.T, name, body string) {
	t.Helper()

	pkg := goldentest.Package{
		Path: "model",
		Files: []goldentest.Source{
			{Name: "subject.go", Content: subjects(t)},
			{Name: "zz_written.go", Content: []byte(imported(body)), Generated: true},
		},
	}

	if err := goldentest.Compiles(pkg); err != nil {
		t.Fatalf("what was written for %s does not compile: %v\n%s", name, err, body)
	}
}

// reaches names, per subject, the others whose methods have to stand beside it
// for what was written to compile.
var reaches = map[string][]string{"Reaching": {"Earning"}}

// writtenFor returns everything the emitters wrote for one subject.
func writtenFor(t *testing.T, name string) string {
	t.Helper()

	var diags diag.Set

	held, err := scalars.For(asked(t, name), &diags)
	if err != nil {
		t.Fatalf("writing for %s: %v", name, err)
	}
	if !diags.Empty() {
		t.Fatalf("writing for %s was refused:\n%s", name, diags.Render())
	}

	var out strings.Builder
	for _, unit := range held {
		out.WriteString(source(t, unit))
	}
	return out.String()
}

// subjects returns the fixture's own declarations, with its package clause
// rewritten to the one the written methods land in.
func subjects(t *testing.T) []byte {
	t.Helper()

	held, err := os.ReadFile(filepath.Join("testdata", "tagged", "model", "model.go"))
	if err != nil {
		t.Fatalf("reading the fixture: %v", err)
	}

	return held
}

// imported puts the assembled methods in a file with everything they can name.
//
// Every import, whether or not this subject's methods reach for it, and each
// spoken for by a blank assignment so that the file compiles either way. What
// is under test is the methods; which imports the emitters ask for is settled
// where the file is really written.
func imported(body string) string {
	return "package model\n\n" +
		"import (\n\t\"encoding\"\n\t\"fmt\"\n\t\"log/slog\"\n\t\"strconv\"\n\t\"strings\"\n)\n\n" +
		"var (\n\t_ = strings.Builder{}\n\t_ = strconv.Itoa\n\t_ = slog.String\n" +
		"\t_ fmt.Stringer\n\t_ encoding.TextMarshaler\n)\n\n" + body
}
