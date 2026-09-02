package cli

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/okian/forge/internal/diag"
	"github.com/okian/forge/internal/goldentest"
	"github.com/okian/forge/internal/layers"
)

// ran is one finished command line: what it wrote and what it exited with.
type ran struct {
	status int
	out    string
	err    string
}

// forge runs one command line, keeping the two streams apart so that a test can
// say which one something arrived on.
func forge(args ...string) ran {
	var out, err bytes.Buffer
	status := Run(layers.Builtins(), args, &out, &err)
	return ran{status: status, out: out.String(), err: err.String()}
}

// A tool run with nothing to do says what it can do. It is not a run that
// succeeded, though: the command line named no work, which is a mistake in the
// command line.
func TestForgeWithNoCommand(t *testing.T) {
	got := forge()

	if got.status != diag.ExitUsage {
		t.Errorf("exited %d, want %d", got.status, diag.ExitUsage)
	}
	if !strings.Contains(got.err, "no command") {
		t.Errorf("nothing said what was missing:\n%s", got.err)
	}
	if !strings.Contains(got.err, "Commands:") {
		t.Errorf("the commands were not listed:\n%s", got.err)
	}
	if got.out != "" {
		t.Errorf("a failed run wrote to stdout:\n%s", got.out)
	}
}

// Help is a run that did what it was asked, and what it was asked for goes to
// stdout so it can be paged and piped.
func TestForgeAskedForHelp(t *testing.T) {
	for _, arg := range []string{"-h", "-help", "--help"} {
		t.Run(arg, func(t *testing.T) {
			got := forge(arg)

			if got.status != diag.ExitOK {
				t.Errorf("exited %d, want %d", got.status, diag.ExitOK)
			}
			if got.out != usage {
				t.Errorf("stdout is not the usage:\n%s", got.out)
			}
			if got.err != "" {
				t.Errorf("help arrived on stderr:\n%s", got.err)
			}
		})
	}
}

// The usage is the one piece of this tool whose exact text is specified, and
// comparing it against the constant it was printed from would pass whatever
// either of them became. A recorded copy is what turns an edit to it into a
// diff somebody reads.
func TestTheUsageIsWhatItWasWrittenToBe(t *testing.T) {
	goldentest.Compare(t, "usage.txt", []byte(forge("-h").out))
}

// The usage lists every command there is, and every command it lists exists.
// Either half being wrong is a tool that documents itself incorrectly, which is
// worse than one that documents itself thinly.
func TestTheUsageAndTheCommandsAgree(t *testing.T) {
	for _, cmd := range commands {
		if !strings.Contains(usage, "\n  "+cmd.name+" ") {
			t.Errorf("%s is a command and is not in the usage", cmd.name)
		}
	}

	inside, _, _ := strings.Cut(usage, "\nGlobal flags:")
	_, listed, _ := strings.Cut(inside, "Commands:\n")

	for line := range strings.SplitSeq(strings.TrimSpace(listed), "\n") {
		name, _, _ := strings.Cut(strings.TrimSpace(line), " ")
		if _, ok := lookup(name); !ok {
			t.Errorf("the usage lists %q and there is no such command", name)
		}
	}
}

// lookup finds a command by name.
func lookup(name string) (command, bool) {
	for _, cmd := range commands {
		if cmd.name == name {
			return cmd, true
		}
	}
	return command{}, false
}

// Every global flag the usage names is one the tool takes, or the text sends
// somebody to type something that fails.
func TestEveryGlobalFlagInTheUsageIsTaken(t *testing.T) {
	_, flags, _ := strings.Cut(usage, "Global flags:\n")
	flags, _, _ = strings.Cut(flags, "\n\n")

	for line := range strings.SplitSeq(strings.TrimSpace(flags), "\n") {
		name, _, _ := strings.Cut(strings.TrimSpace(line), " ")

		args := []string{name, "version"}
		if name == "-C" {
			args = []string{name, ".", "version"}
		}

		if got := forge(args...); got.status != diag.ExitOK {
			t.Errorf("the usage names %s and running it exited %d:\n%s", name, got.status, got.err)
		}
	}
}

// A command nobody has is a mistake in the command line, and the tool says so
// rather than doing something else.
func TestAnUnknownCommand(t *testing.T) {
	got := forge("bogus")

	if got.status != diag.ExitUsage {
		t.Errorf("exited %d, want %d", got.status, diag.ExitUsage)
	}
	if !strings.Contains(got.err, `unknown command "bogus"`) {
		t.Errorf("the failure does not name what it did not know:\n%s", got.err)
	}
}

// The verb list is short enough that a wrong name is nearly always a typo, and
// whoever typed it is a character away from the run they wanted.
func TestACommandNearlyTyped(t *testing.T) {
	cases := []struct{ typed, meant string }{
		{typed: "doc", meant: "doctor"},
		{typed: "forgelist", meant: "list"},
		{typed: "gen", meant: "generate"},
		{typed: "ver", meant: "version"},
	}

	for _, tc := range cases {
		typed, meant := tc.typed, tc.meant
		t.Run(typed, func(t *testing.T) {
			got := forge(typed)

			if got.status != diag.ExitUsage {
				t.Errorf("exited %d, want %d", got.status, diag.ExitUsage)
			}
			if !strings.Contains(got.err, `did you mean "`+meant+`"`) {
				t.Errorf("nothing suggested %s:\n%s", meant, got.err)
			}
		})
	}
}

// And a name nothing resembles gets no guess. A wrong suggestion costs more
// than none: it sends somebody to read about a command they did not want.
func TestACommandNothingResembles(t *testing.T) {
	got := forge("xyzzy")

	if strings.Contains(got.err, "did you mean") {
		t.Errorf("a name nothing resembles was guessed at:\n%s", got.err)
	}
}

// A flag the tool does not take is a mistake in the command line and not in
// anybody's code.
func TestAGlobalFlagNobodyDefined(t *testing.T) {
	got := forge("-nonesuch", "version")

	if got.status != diag.ExitUsage {
		t.Errorf("exited %d, want %d", got.status, diag.ExitUsage)
	}
	if !strings.Contains(got.err, "nonesuch") {
		t.Errorf("the failure does not name the flag:\n%s", got.err)
	}
}

// The same for a flag one command does not take, which the command itself
// refuses because it is the one that decides what its flags mean.
func TestACommandFlagNobodyDefined(t *testing.T) {
	got := forge("version", "-nonesuch")

	if got.status != diag.ExitUsage {
		t.Errorf("exited %d, want %d", got.status, diag.ExitUsage)
	}
	if !strings.Contains(got.err, "nonesuch") {
		t.Errorf("the failure does not name the flag:\n%s", got.err)
	}
}

// And the answer it gets is the flags that command does take. The list of
// commands does not contain them, so printing it would bury the answer under
// something that cannot address the question.
func TestTheAnswerToAFlagOneCommandDoesNotTake(t *testing.T) {
	got := forge("generate", "-dryrun")

	if got.status != diag.ExitUsage {
		t.Errorf("exited %d, want %d", got.status, diag.ExitUsage)
	}
	for _, want := range []string{"forge generate", "-dry-run", "-diff"} {
		if !strings.Contains(got.err, want) {
			t.Errorf("the answer does not mention %q:\n%s", want, got.err)
		}
	}
	// The list of commands is the answer to a wrong command, not to a wrong
	// flag of the right one.
	if strings.Contains(got.err, "Commands:") {
		t.Errorf("the command list was printed instead of the command's flags:\n%s", got.err)
	}
	// A failed run writes nothing to the stream its output would go to.
	if got.out != "" {
		t.Errorf("a failed run wrote to stdout:\n%s", got.out)
	}
	// The complaint comes first, the same way round as for a global flag, so
	// that one tool does not answer two mistakes in two orders.
	if strings.Index(got.err, "nonesuch") > strings.Index(got.err, "forge generate") &&
		strings.Index(got.err, "dryrun") > strings.Index(got.err, "forge generate") {
		t.Errorf("the usage was printed before the complaint:\n%s", got.err)
	}
}

// The flag package writes the help somebody asked for and the complaint about a
// flag they got wrong to one place, and it writes them to the process streams
// rather than to whatever a caller was given — which a test inspecting its own
// buffers cannot see. Both are printed here instead, so its own reporting is
// silenced.
func TestTheFlagPackageSaysNothingItself(t *testing.T) {
	var out, errs bytes.Buffer
	env := &environment{stdout: &out, stderr: &errs}

	if _, err := global(env, []string{"-nonesuch", "version"}); err == nil {
		t.Fatal("a flag nobody defined was accepted")
	}
	if out.Len() != 0 || errs.Len() != 0 {
		t.Errorf("the flag package reported for itself:\nstdout: %s\nstderr: %s", out.String(), errs.String())
	}
}

// -C decides which module is worked on, and it decides it without moving the
// process: a run that left the process somewhere else could not be repeated,
// and a failed one would move the ground under whatever ran next.
func TestRunningSomewhereElse(t *testing.T) {
	was, err := os.Getwd()
	if err != nil {
		t.Fatalf("reading the working directory: %v", err)
	}

	dir := t.TempDir()
	if got := forge("-C", dir, "version"); got.status != diag.ExitOK {
		t.Fatalf("exited %d:\n%s", got.status, got.err)
	}

	now, err := os.Getwd()
	if err != nil {
		t.Fatalf("reading the working directory: %v", err)
	}
	if now != was {
		t.Errorf("the run moved the process to %s", now)
	}
}

// The flags chain the way git's do: each is read relative to the one before it,
// and an absolute one starts again. Last-wins would make -C a -C b mean b,
// which is not what somebody who typed a path in two parts asked for.
func TestRunningSomewhereElseTwice(t *testing.T) {
	root := t.TempDir()
	nested := filepath.Join(root, "one", "two")
	if err := os.MkdirAll(nested, 0o750); err != nil {
		t.Fatalf("making %s: %v", nested, err)
	}

	cases := []struct {
		name string
		args []string
		want string
	}{
		{name: "one at a time", args: []string{"-C", root, "-C", "one", "-C", "two"}, want: nested},
		{name: "all at once", args: []string{"-C", filepath.Join(root, "one", "two")}, want: nested},
		{name: "starting again", args: []string{"-C", filepath.Join(root, "one"), "-C", nested}, want: nested},
		{name: "naming nowhere", args: []string{"-C", "", "-C", nested}, want: nested},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := &environment{stdout: io.Discard, stderr: io.Discard}

			if _, err := global(env, append(tc.args, "version")); err != nil {
				t.Fatalf("reading the flags: %v", err)
			}
			if env.dir != tc.want {
				t.Errorf("resolved to %s, want %s", env.dir, tc.want)
			}
		})
	}
}

// A directory that is not one is a mistake in the command line, and one worth
// catching before a load spends a second failing to find anything in it — the
// load's own answer names the go binary it could not start, which is not the
// argument anybody got wrong.
func TestRunningSomewhereThatIsNotADirectory(t *testing.T) {
	dir := t.TempDir()

	file := filepath.Join(dir, "afile")
	if err := os.WriteFile(file, []byte("not a directory"), 0o600); err != nil {
		t.Fatalf("writing %s: %v", file, err)
	}

	cases := map[string]string{
		"nowhere at all": filepath.Join(dir, "nowhere"),
		"a file":         file,
	}

	for _, name := range []string{"a file", "nowhere at all"} {
		t.Run(name, func(t *testing.T) {
			got := forge("-C", cases[name], "version")

			if got.status != diag.ExitUsage {
				t.Errorf("exited %d, want %d:\n%s", got.status, diag.ExitUsage, got.err)
			}
			if !strings.Contains(got.err, filepath.Base(cases[name])) {
				t.Errorf("the failure does not name what was given:\n%s", got.err)
			}
		})
	}
}

// Every command explains itself, and does it on stdout, because help is what
// was asked for rather than a complaint about the asking.
func TestEveryCommandExplainsItself(t *testing.T) {
	for _, cmd := range commands {
		t.Run(cmd.name, func(t *testing.T) {
			got := forge(cmd.name, "-h")

			if got.status != diag.ExitOK {
				t.Errorf("exited %d, want %d", got.status, diag.ExitOK)
			}
			// The whole line, not a prefix of it: a usage that lost "[packages]"
			// still starts "forge generate".
			want := "forge " + cmd.name
			if strings.Contains(got.out, "\nFlags:\n") {
				want += " [flags]"
			}
			if cmd.takes != "" {
				want += " " + cmd.takes
			}

			line, _, _ := strings.Cut(got.out, "\n")
			if line != want {
				t.Errorf("the usage line is %q, want %q", line, want)
			}
			if !strings.Contains(got.out, cmd.about) {
				t.Errorf("the usage does not say what the command is for:\n%s", got.out)
			}
			if got.err != "" {
				t.Errorf("help arrived on stderr:\n%s", got.err)
			}
		})
	}
}

// A command with no flags of its own says so by leaving the heading out, rather
// than printing one with nothing under it.
func TestACommandWithNoFlags(t *testing.T) {
	got := forge("version", "-h")

	if strings.Contains(got.out, "Flags:") {
		t.Errorf("a command with no flags printed a flag heading:\n%s", got.out)
	}
}

// And a command that has them lists them, since that is what the usage sends
// somebody here for.
func TestACommandWithFlags(t *testing.T) {
	got := forge("generate", "-h")

	for _, want := range []string{"Flags:", "-dry-run", "-diff"} {
		if !strings.Contains(got.out, want) {
			t.Errorf("the usage does not mention %q:\n%s", want, got.out)
		}
	}
}

// A command that takes no arguments refuses them rather than ignoring them,
// since an argument somebody typed was meant to do something.
func TestCommandsThatTakeNoArguments(t *testing.T) {
	for _, name := range []string{"version", "list", "doctor"} {
		t.Run(name, func(t *testing.T) {
			got := forge(name, "./...")

			if got.status != diag.ExitUsage {
				t.Errorf("exited %d, want %d", got.status, diag.ExitUsage)
			}
			if !strings.Contains(got.err, "./...") {
				t.Errorf("the failure does not name what it would not take:\n%s", got.err)
			}
		})
	}
}

// Every command's flags are the ones it is documented to take. A flag that
// quietly went missing would be a script that stops working for a reason the
// script cannot see.
func TestEveryCommandTakesTheFlagsItIsDocumentedFor(t *testing.T) {
	documented := map[string][]string{
		"generate": {"dry-run", "diff"},
		"check":    nil,
		"explain":  {"t", "json"},
		"list":     {"json"},
		"doctor":   {"write-editor-config"},
		"version":  nil,
	}

	for _, cmd := range commands {
		t.Run(cmd.name, func(t *testing.T) {
			want, known := documented[cmd.name]
			if !known {
				t.Fatalf("%s is a command and nothing here says what flags it takes", cmd.name)
			}

			// Defined by running the command with -h, since that is the only
			// path that builds the flags, and so the only one that proves what
			// somebody reading the help would find.
			got := forge(cmd.name, "-h")

			for _, name := range want {
				if !strings.Contains(got.out, "\n  -"+name) {
					t.Errorf("%s does not take -%s:\n%s", cmd.name, name, got.out)
				}
			}
			if len(want) == 0 && strings.Contains(got.out, "\n  -") {
				t.Errorf("%s takes flags nothing documents:\n%s", cmd.name, got.out)
			}
		})
	}
}

// The flag package writes the help somebody asked for and the complaint about a
// flag they got wrong to one place. Both are printed here instead, so its own
// reporting is silenced — and a test that only inspects its own buffers cannot
// see output escaping to the real process streams.
func TestTheFlagPackageReportsNothingItself(t *testing.T) {
	for _, cmd := range commands {
		if out := flagsFor(cmd).Output(); out != io.Discard {
			t.Errorf("%s would let the flag package report to %T", cmd.name, out)
		}
	}

	env := &environment{stdout: io.Discard, stderr: io.Discard}
	if _, err := global(env, []string{"version"}); err != nil {
		t.Fatalf("reading the flags: %v", err)
	}
}
