package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/okian/forge/internal/diag"
	"github.com/okian/forge/internal/load"
)

// usage is what forge prints when it is run with no command, or asked.
const usage = `forge — type-driven code generation for Go

Usage:
  forge <command> [flags] [packages]

Commands:
  generate   Resolve declarations and write generated files
  check      Validate declarations and verify outputs are fresh (CI gate)
  explain    Show the resolved stack, shapes, and methods for one declaration
  list       List registered layers, kinds, and option schemas
  doctor     Diagnose toolchain, build tags, and editor configuration
  version    Print version and build info

Global flags:
  -C dir     Run as if forge had been started in dir
  -q         Report only what went wrong
  -v         Report what is being done

Run "forge <command> -h" for command flags.
`

// A command is one verb.
type command struct {
	// name is what the author types.
	name string

	// takes describes the arguments after the flags, for the command's own
	// usage line. An empty value means the command takes none.
	takes string

	// about is the one line the command list shows.
	about string

	// run does the work. It is given its own entry so that it can print its
	// usage, and everything after the command name — flags included, because a
	// command parses its own and nothing has to be reserved globally.
	run func(env *environment, cmd command, args []string) error
}

// commands are the verbs, in the order the usage lists them, which is the order
// somebody meets them: the two that do the work, then the two that explain it,
// then the two that report on forge itself.
var commands = []command{
	{name: "generate", takes: "[packages]", about: "Resolve declarations and write generated files", run: generate},
	{name: "check", takes: "[packages]", about: "Validate declarations and verify outputs are fresh", run: check},
	{
		name: "explain", takes: "[package]", run: explain,
		about: "Show the resolved stack for one declaration.\nThe answer is written whether or not anything was wrong with it; the status says which.",
	},
	{name: "list", about: "List registered layers, kinds, and option schemas", run: list},
	{name: "doctor", about: "Diagnose toolchain, build tags, and editor configuration", run: doctor},
	{name: "version", about: "Print version and build info", run: version},
}

// environment is what a command is given besides its arguments.
type environment struct {
	// stdout is where a command writes what was asked for. Diagnostics do not
	// go here: a run whose output is piped into another program should not have
	// its complaints arrive in the pipe.
	stdout io.Writer

	// stderr is where diagnostics and progress go.
	stderr io.Writer

	// quiet suppresses progress, leaving only what went wrong. It wins over
	// verbose, so that a script that sets it is not made chatty by an inherited
	// flag it did not choose.
	quiet bool

	// verbose asks for progress that is otherwise not worth reporting.
	verbose bool

	// dir is the directory patterns resolve from, which is where forge was
	// started unless -C said otherwise.
	dir string

	// pipeline is the shared path to a resolved declaration, held here rather
	// than reached for, so that a test can give a command declarations that
	// were never on disk.
	pipeline pipeline
}

// errNotBuilt reports a verb whose work this binary cannot yet do.
//
// A verb that is listed and does nothing would be worse than one that is
// missing: somebody would run it, see no complaint, and conclude their
// declarations produced nothing. This is not a diagnostic about their code, but
// it ends the run the same way, because the run did not do what was asked.
var errNotBuilt = errors.New("is not in this build")

// errReported ends a run that found something wrong with the input and has
// already said so.
//
// The diagnostics are the output, so repeating them as an error would print
// every one of them twice. What is left is to exit with the status that says a
// run reported.
var errReported = errors.New("declarations were reported")

// misuse reports a mistake in the command line, which is a different failure
// from a mistake in the input and exits differently.
type misuse struct {
	err error

	// answer prints the usage that addresses this mistake, or is nil when the
	// list of commands is the answer. A mistyped flag of one command is
	// answered by that command's flags, and printing the command list instead
	// would bury the answer under something that does not contain it.
	//
	// A function rather than a printed message, so that one place decides what
	// order a complaint and its usage come in. Printing where the mistake was
	// noticed put them in one order for a command's flags and the other for the
	// global ones.
	answer func(w io.Writer)
}

// Error returns what was wrong with the command line.
func (m misuse) Error() string { return m.err.Error() }

// Unwrap returns the underlying failure.
func (m misuse) Unwrap() error { return m.err }

// misusedf reports a command line that does not name a run forge can attempt.
func misusedf(format string, args ...any) error {
	return misuse{err: fmt.Errorf(format, args...)}
}

// say writes one line of output, dropping a failed write.
//
// A tool that cannot write to its own output stream has nothing left to report
// that with: the report would go to the stream that just failed. Nor is the
// common case reachable here — a reader that stops reading raises SIGPIPE on
// the standard streams, and the runtime ends the process before the write
// returns. What reaches this is a stream closed some other way, where writing
// nothing and ending well is what every tool in the toolchain does.
func say(w io.Writer, format string, args ...any) {
	_, _ = fmt.Fprintf(w, format, args...)
}

// run dispatches one command line and returns the status to exit with.
func run(args []string, stdout, stderr io.Writer) int {
	env := &environment{stdout: stdout, stderr: stderr, pipeline: stages()}

	rest, err := global(env, args)
	if err == nil {
		err = dispatch(env, rest)
	}
	return status(stdout, stderr, err)
}

// status says whatever is left to say about how a run ended and returns the
// process status for it.
//
// Three statuses and no more: the run did what was asked, the run reported
// something wrong with the input, or the command line did not name a run. A
// caller in a shell script can act on all three, which is the whole of what
// exit statuses are for.
func status(stdout, stderr io.Writer, err error) int {
	switch {
	case err == nil:
		return diag.ExitOK

	// Help is a run that did what it was asked, and what it was asked for goes
	// to stdout so it can be paged and piped.
	case errors.Is(err, flag.ErrHelp):
		say(stdout, "%s", usage)
		return diag.ExitOK

	// Already said, and saying it again under a heading would make one run read
	// as two failures.
	case errors.Is(err, errReported):
		return diag.ExitDiagnostics

	default:
		say(stderr, "forge: %v\n", err)
		if wrong, is := errors.AsType[misuse](err); is {
			if wrong.answer != nil {
				wrong.answer(stderr)
			} else {
				say(stderr, "%s", usage)
			}
			return diag.ExitUsage
		}
		return diag.ExitDiagnostics
	}
}

// global reads the flags that come before the command name and returns what is
// left.
//
// Before rather than after, because they are about the run and not about the
// verb: -C decides which module is being worked on at all, and a flag that
// changes that has to be settled before anything reads a file.
func global(env *environment, args []string) ([]string, error) {
	flags := flag.NewFlagSet("forge", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.Usage = func() {}

	var dirs directories
	flags.Var(&dirs, "C", "run as if forge had been started in dir")
	flags.BoolVar(&env.quiet, "q", false, "report only what went wrong")
	flags.BoolVar(&env.verbose, "v", false, "report what is being done")

	// A request for help travels as a misuse and is unwrapped by whoever ends
	// the run, rather than being short-circuited here: two paths to one outcome
	// is one path that can rot without anything noticing.
	if err := flags.Parse(args); err != nil {
		return nil, misuse{err: err}
	}

	dir := dirs.resolve()
	if dir != "" {
		// Checked here rather than left to the load, so that a directory nobody
		// has is reported as the mistake in the command line that it is, before
		// a load spends a second failing to find anything in it. A file is that
		// mistake too: the load's answer to one names the go binary it could not
		// start, which is not the argument anybody got wrong.
		switch info, err := os.Stat(dir); {
		case err != nil:
			return nil, misusedf("%v", err)
		case !info.IsDir():
			return nil, misusedf("%s is not a directory", dir)
		}
	}
	env.dir = dir

	return flags.Args(), nil
}

// directories collects the -C flags in the order they were given.
//
// The directory is resolved into the load rather than entered with a chdir.
// Both put a command in the same place, since everything downstream works from
// the positions the load already resolved; only one of them leaves the process
// where it started, which is what lets a run be repeated in one process and
// keeps a failed run from moving the ground under the next one.
type directories []string

// String returns the directory the flags name together.
func (d *directories) String() string { return d.resolve() }

// Set records one -C.
func (d *directories) Set(value string) error {
	*d = append(*d, value)
	return nil
}

// resolve folds the flags into one directory: each is read relative to the one
// before it, and an absolute one starts again.
//
// Joined lexically rather than entered one at a time, which is where this and
// git part company — git walks into each in turn, so a link followed by ".."
// lands where the link pointed, and this lands beside the link. Lexical is the
// answer that matches a tool which never moves: the directory it reports is the
// one it was given, spelled the way it was given.
func (d *directories) resolve() string {
	where := ""
	for _, step := range *d {
		switch {
		case step == "":
			// Named nowhere, which git takes as naming here.
		case filepath.IsAbs(step) || where == "":
			where = step
		default:
			where = filepath.Join(where, step)
		}
	}
	return where
}

// dispatch runs the named command.
func dispatch(env *environment, args []string) error {
	if len(args) == 0 {
		return misusedf("no command")
	}

	name, rest := args[0], args[1:]
	for _, cmd := range commands {
		if cmd.name == name {
			return cmd.run(env, cmd, rest)
		}
	}

	// Naming the closest command rather than only refusing: the verb list is
	// short enough that a wrong one is nearly always a typo, and whoever typed
	// it is a character away from the run they wanted.
	if closest, ok := nearest(name); ok {
		return misusedf("unknown command %q; did you mean %q?", name, closest)
	}
	return misusedf("unknown command %q", name)
}

// nearest returns the command a mistyped name most likely meant.
//
// Prefix and containment only. An edit distance would guess at names nobody
// typed, and a wrong guess costs more than no guess: it sends somebody to read
// the help for a command they did not want.
func nearest(name string) (string, bool) {
	if name == "" {
		return "", false
	}

	for _, cmd := range commands {
		if strings.HasPrefix(cmd.name, name) || strings.Contains(name, cmd.name) {
			return cmd.name, true
		}
	}
	return "", false
}

// flagsFor returns a flag set for one command.
//
// The flag package's own reporting is silenced. It writes the help somebody
// asked for and the complaint about a flag they got wrong to one place, and
// those belong on different streams — so both are printed here instead.
func flagsFor(cmd command) *flag.FlagSet {
	flags := flag.NewFlagSet(cmd.name, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.Usage = func() {}
	return flags
}

// parse reads one command's flags and returns everything that was not one. It
// reports whether the command should go on: a request for help has been
// answered by the time it returns.
func parse(env *environment, cmd command, flags *flag.FlagSet, args []string) ([]string, bool, error) {
	rest, err := interleaved(flags, args)
	switch {
	case err == nil:
		return rest, true, nil
	case errors.Is(err, flag.ErrHelp):
		describe(env.stdout, cmd, flags)
		return nil, false, nil
	default:
		// The answer to a flag this command does not take is the list of flags
		// it does take, which the command list does not contain.
		return nil, false, misuse{
			err:    err,
			answer: func(w io.Writer) { describe(w, cmd, flags) },
		}
	}
}

// interleaved reads flags written anywhere among the arguments, and returns the
// arguments that were not flags in the order they were written.
//
// The flag package stops at the first thing that is not a flag, so
// "explain ./model -t Persons" would read -t as a package name and then report
// that no type was asked about — denying the author supplied the flag they are
// looking at. Nothing about the shape of a command line says the flags come
// first, and the documented spelling of this one puts them last.
//
// It parses repeatedly, taking one non-flag argument out of the way each time.
// Every round consumes at least that argument, so it ends.
//
// Everything after a bare -- is an argument whatever it looks like, and stays
// one: the terminator is how a caller passes a package whose name begins with a
// dash, and resuming after it would make that promise last exactly one word.
func interleaved(flags *flag.FlagSet, args []string) ([]string, error) {
	args, literal := terminated(args)

	var rest []string
	for len(args) > 0 {
		if err := flags.Parse(args); err != nil {
			return nil, err
		}

		left := flags.Args()
		if len(left) == 0 {
			break
		}
		rest = append(rest, left[0])
		args = left[1:]
	}

	return append(rest, literal...), nil
}

// terminated splits a command line at the first bare --, returning what comes
// before it and what comes after.
func terminated(args []string) (before, after []string) {
	for i, arg := range args {
		if arg == "--" {
			return args[:i], args[i+1:]
		}
	}
	return args, nil
}

// describe writes one command's usage.
func describe(w io.Writer, cmd command, flags *flag.FlagSet) {
	line := "forge " + cmd.name
	if defined(flags) {
		line += " [flags]"
	}
	if cmd.takes != "" {
		line += " " + cmd.takes
	}

	say(w, "%s\n\n%s\n", line, cmd.about)

	if !defined(flags) {
		return
	}

	say(w, "\nFlags:\n")
	flags.SetOutput(w)
	flags.PrintDefaults()
	flags.SetOutput(io.Discard)
}

// defined reports whether a command has flags of its own, so that one without
// any does not print an empty heading.
func defined(flags *flag.FlagSet) bool {
	found := false
	flags.VisitAll(func(*flag.Flag) { found = true })
	return found
}

// everything is the pattern a verb loads when the command line names none: the
// whole module, which is what a generator is nearly always run over.
const everything = "./..."

// loadConfig configures a load from what a command was given.
//
// The patterns are defaulted here rather than left to the load, so that what is
// reported as being loaded is what was loaded. Tags and environment are left
// alone: a run generates for the build the author is in, and a flag that
// changed that would let generated files be written for one configuration while
// the package is compiled in another.
func (env *environment) loadConfig(patterns ...string) load.Config {
	if len(patterns) == 0 {
		patterns = []string{everything}
	}
	return load.Config{Dir: env.dir, Patterns: patterns}
}

// progress reports what is being done, for somebody who asked to be told.
//
// To stderr, because it is not what the run was for. Quiet wins over verbose,
// so that a script setting it is not made chatty by a flag it inherited and did
// not choose.
func (env *environment) progress(format string, args ...any) {
	if env.verbose && !env.quiet {
		say(env.stderr, format+"\n", args...)
	}
}

// report writes a set of diagnostics and says whether there were any.
//
// To stderr, sorted, in the one rendering every diagnostic uses: a run whose
// output feeds another program must not have its complaints arrive in the pipe,
// and a run that rendered them differently per verb would make them harder to
// recognise than to read.
func (env *environment) report(diags diag.Set) bool {
	if diags.Empty() {
		return false
	}
	say(env.stderr, "%s\n", diags.Render())
	return true
}
