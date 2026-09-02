package cli

import (
	"bytes"
	stdcontext "context"
	"fmt"
	goversion "go/version"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/okian/forge/internal/diag"
	generated "github.com/okian/forge/internal/generate"
	"github.com/okian/forge/internal/load"
	"github.com/okian/forge/internal/model"
	"github.com/okian/forge/plugin"
)

// floor is the oldest Go this build's output compiles under.
//
// A constant rather than a reading of the module forge was built in. What it is
// about is the language the generated code needs — generic methods on a view,
// and the standard library's own json — so it is a fact about what the layers
// emit rather than about how forge itself was compiled.
const floor = "go1.27"

// verdict is how worried a finding is.
type verdict uint8

const (
	// The zero verdict is a check that passed, and is not named because nothing
	// writes it: a finding says what it found and leaves this alone, so that
	// the ordinary case is the one nobody has to remember to spell.
	//
	// Reported all the same. A report listing only problems would leave
	// somebody unable to tell a healthy setup from one this did not look at.

	// worth is something to know that is not stopping anything.
	worth verdict = iota + 1

	// faulty is something that will stop working, and is what decides the exit
	// status.
	faulty
)

// mark is what a verdict looks like in the leftmost column.
func (v verdict) mark() string {
	switch v {
	case worth:
		return "note"
	case faulty:
		return "wrong"
	default:
		return "ok"
	}
}

// finding is one thing doctor looked at and what it found.
type finding struct {
	about string
	said  string
	hint  string
	how   verdict
}

// doctor reports on the toolchain and the setup around it.
//
// Everything it can answer without reading anybody's code, and then everything
// it can only answer by reading it. The split matters because the second half
// can fail: a directory that is not a module, or a package that does not build,
// still has a Go version and a marker module worth reporting on — and a doctor
// that refused to say anything until the load succeeded would be silent exactly
// when somebody most needs it.
func doctor(env *environment, cmd command, args []string) error {
	flags := flagsFor(cmd)

	write := flags.Bool("write-editor-config", false,
		"add the build tag to .vscode/settings.json so an editor sees both halves of a spec package")

	rest, on, err := parse(env, cmd, flags, args)
	if !on || err != nil {
		return err
	}

	if len(rest) > 0 {
		return answered(cmd, flags, "doctor takes no arguments, got %q", rest[0])
	}

	held, err := env.pipeline.follow(env, env.loadConfig())

	found := make([]finding, 0, 8)
	found = append(found, toolchain(held)...)

	// Asked only where there is something for it to be about. An editor
	// analysing the ordinary build is exactly right for a tree with no spec
	// declaration in it, and a note about one would be advice for somebody
	// else's problem.
	if *write || specs(held) > 0 {
		found = append(found, editor(env, *write))
	}

	found = append(found, examined(env.layers(), held, err)...)

	report(env, found)

	if slices.ContainsFunc(found, func(one finding) bool { return one.how == faulty }) {
		return errReported
	}
	return nil
}

// toolchain reports whether the Go here is new enough for what forge emits.
//
// Two questions wearing one name, and both have to be asked. The compiler
// running forge is one floor: nothing can build language it does not implement.
// The module's own go directive is the other, and it is the one that usually
// bites — the go command refuses a construct newer than what the directive
// names however new the compiler is, so a module saying go 1.24 cannot build
// what forge writes even on a 1.27 toolchain.
func toolchain(found resolved) []finding {
	out := []finding{compiling()}

	for _, one := range modules(found) {
		if goversion.Compare(one.version, floor) >= 0 {
			continue
		}

		out = append(out, finding{
			about: "go directive", how: faulty,
			said: one.path + " names " + one.version + ", and generated code needs " + floor,
			hint: "raise the go directive in its go.mod to " + floor + " or later",
		})
	}
	return out
}

// compiling reports the compiler forge is running under.
func compiling() finding {
	held := runtime.Version()

	if newEnough(held) {
		return finding{about: "toolchain", said: held + ", which is " + floor + " or newer"}
	}

	return finding{
		about: "toolchain", how: faulty,
		said: held + " is older than " + floor,
		hint: "generated code uses generic methods and the standard library's json, both of which need " + floor,
	}
}

// newEnough reports whether a Go version meets the floor, taking anything it
// cannot read as new enough.
//
// Newer is the safe direction for a version this cannot parse. A toolchain
// spelled devel, or built from a commit, is one somebody chose deliberately,
// and telling them it is too old on the strength of a string forge could not
// read would be a complaint about the spelling.
//
// Read by the standard library rather than by taking the version apart here.
// Go spells a prerelease go1.26rc1, with no separator a general version parser
// would find, so anything comparing the digits after the prefix reads every
// release candidate as unparseable — and then, by the rule above, as new
// enough. go1.20beta1 is seven releases below the floor.
func newEnough(held string) bool {
	if !goversion.IsValid(held) {
		return true
	}
	return goversion.Compare(held, floor) >= 0
}

// belongs is a module and the Go version its go.mod names.
type belongs struct{ path, version string }

// modules returns every module the loaded packages belong to, with the Go
// version each one names, in a stable order.
func modules(found resolved) []belongs {
	if found.Session == nil {
		return nil
	}

	seen := make(map[string]string)
	for _, pkg := range found.Session.Packages {
		if pkg.Module == nil || pkg.Module.GoVersion == "" {
			continue
		}
		seen[pkg.Module.Path] = "go" + pkg.Module.GoVersion
	}

	out := make([]belongs, 0, len(seen))
	for path, held := range seen {
		if goversion.IsValid(held) {
			out = append(out, belongs{path: path, version: held})
		}
	}

	slices.SortFunc(out, func(a, b belongs) int { return strings.Compare(a.path, b.path) })
	return out
}

// settings is the editor configuration doctor knows how to read and write, as
// a reader sees it written.
const settings = ".vscode/settings.json"

// portable is the setting that works wherever the go command does, which is
// every editor rather than the one this knows how to write.
const portable = "GOFLAGS=-tags=" + load.SpecTag

// buildFlags is the setting an editor's language server reads to decide which
// build configuration a package is analysed in.
const buildFlags = `"gopls": {"build.buildFlags": ["-tags=` + load.SpecTag + `"]}`

// editor reports whether an editor here is set up to see both halves of a
// package holding spec declarations, and writes the setting when asked.
//
// The problem it is about: a declaration forge owns the type of lives under a
// build tag, and an editor analysing the ordinary build does not see the file
// it is written in. Everything referring to it then reads as undefined — in the
// author's own source, about a declaration that is perfectly correct.
//
// Setting the tag moves the problem rather than removing it. With it, the
// editor sees the spec and not the file forge generated, so the bodies of every
// generated method are greyed out and a rename refactor will not reach them —
// which forge check catches, and which is the smaller of the two costs because
// what is greyed is code nobody edits.
func editor(env *environment, write bool) finding {
	path := filepath.Join(env.dir, ".vscode", "settings.json")

	held, err := os.ReadFile(path) //nolint:gosec // a file in the directory forge was pointed at.
	switch {
	case err == nil && analysed(held):
		return finding{about: "editor", said: settings + " already analyses the tagged build"}

	case err != nil && !os.IsNotExist(err):
		return finding{
			about: "editor", how: worth,
			said: settings + " cannot be read: " + err.Error(),
			hint: "check the file, or set the build tag however this editor is configured",
		}
	}

	if !write {
		return finding{
			about: "editor", how: worth,
			said: "nothing here analyses the tagged build, so a spec declaration reads as undefined",
			hint: "run with --write-editor-config for " + settings + ", or set " + portable +
				", which every editor's language server and the go command all honour",
		}
	}

	// Only where there is nothing to lose. Merging into a settings file means
	// understanding one, and a tool that rewrote somebody's editor
	// configuration and got it slightly wrong would be worse than one that
	// declined and said what to add.
	if len(held) > 0 {
		return finding{
			about: "editor", how: worth,
			said: settings + " already exists, and was left alone",
			hint: "add " + buildFlags + " to it, or set " + portable,
		}
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return finding{about: "editor", how: faulty, said: "could not make " + filepath.Dir(path) + ": " + err.Error()}
	}
	if err := os.WriteFile(path, []byte("{\n  "+buildFlags+"\n}\n"), 0o600); err != nil {
		return finding{about: "editor", how: faulty, said: "could not write " + settings + ": " + err.Error()}
	}

	return finding{about: "editor", said: "wrote " + settings}
}

// analysed reports whether a settings file sets the tag for the language
// server.
//
// The tag list rather than the word anywhere in the file. A file that excludes
// *.forgespec.bak, or names the tag in a comment, would otherwise read as one
// that sets it — and what somebody would then have is an editor still reporting
// their declarations as undefined and a doctor telling them it is fine.
func analysed(held []byte) bool {
	for _, part := range strings.Split(string(held), "-tags=")[1:] {
		// Up to whatever ends the flag: a quote in JSON, a space or a comma
		// where several tags are given.
		list, _, _ := strings.Cut(part, `"`)
		for _, tag := range strings.FieldsFunc(list, func(r rune) bool { return r == ',' || r == ' ' }) {
			if tag == load.SpecTag {
				return true
			}
		}
	}
	return false
}

// packagesFound reports on every package the run can see: what it declares,
// whether the markers it is written against are the ones this build resolves,
// and everything check would say about it.
//
// Everything check would say, by running what check runs. The two verbs answer
// overlapping questions about one tree and there is exactly one way to keep
// them from disagreeing, which is to have one of them ask the other rather than
// to write the same reasoning twice and watch it drift. So what appears here is
// not a paraphrase: it is the same diagnostics, in the same words, with the
// same hints.
func examined(catalog *plugin.Registry, found resolved, err error) []finding {
	if err != nil {
		return []finding{{
			about: "packages", how: worth,
			said: "nothing could be loaded: " + err.Error(),
			hint: "run this inside a module, or fix what stops it building",
		}}
	}

	held := grouped(found.Requests)
	out := []finding{markers(found)}

	for _, pkg := range held {
		out = append(out, holding(pkg)...)
	}

	// Everything wrong with the tree, whoever found it: what the load and the
	// stages after it reported, the files nothing accounts for, and the ones
	// that are missing or out of date.
	problems := found.All()

	loose := orphans(found)
	problems.Merge(&loose)

	fresh, _ := freshness(catalog, found)
	problems.Merge(&fresh)

	for _, one := range problems.All() {
		out = append(out, reported(one))
	}

	if len(held) == 0 && problems.Empty() {
		out = append(out, finding{
			about: "packages", how: worth,
			said: "nothing here declares anything over a marker",
			hint: "write a type over a marker, as in type Persons forge.Collection[Person]",
		})
	}
	return out
}

// markers reports whether the marker module the code is written against is the
// one this binary resolves.
//
// The skew it is about is real and quiet: a spec file imports the markers, and
// the layers that give those markers meaning are compiled into forge. A module
// depending on a newer marker module than the forge on somebody's path can name
// a marker that resolves to nothing, and the report for that is about the
// marker rather than about the versions — which is the wrong place to look.
func markers(found resolved) finding {
	_, bundled, _ := versions()
	path, _, _ := strings.Cut(bundled, " ")

	wanted, how := required(found, path)

	switch how {
	case itself:
		return finding{about: "markers", said: "these are the markers, so there is nothing to be out of step with"}

	case swapped:
		return finding{
			about: "markers", how: worth,
			said: path + " is replaced, so its version says nothing about what is being built against",
			hint: "the markers here are whatever the replacement holds; skew is only visible once the replace is gone",
		}

	case none:
		return finding{
			about: "markers", how: worth,
			said: "nothing here depends on " + path + ", so there is no version to compare",
		}

	case depends:
		// The ordinary case, and the only one with a version to compare. What
		// that comparison says is below.
	}

	switch {
	case bundled == path+" "+unknown:
		return finding{
			about: "markers", how: worth,
			said: "this forge records no version of its own, so skew against " + wanted + " cannot be seen",
			hint: "an installed release records one; a build run from source does not",
		}
	case wanted != strings.TrimPrefix(bundled, path+" "):
		return finding{
			about: "markers", how: worth,
			said: "the code is written against " + wanted + " and this forge resolves " + bundled,
			hint: "a marker one of them has and the other does not resolves to nothing; install the matching forge",
		}
	}

	return finding{about: "markers", said: "written against " + wanted + ", which this forge resolves"}
}

// How the marker module reaches the code being examined.
type reach uint8

const (
	// depends is the ordinary case: a module requires the markers at a version.
	depends reach = iota

	// none is a tree that does not use the markers at all.
	none

	// swapped is a replace directive, which points at a directory and so has no
	// version to compare.
	swapped

	// itself is forge's own tree, where the markers are not a dependency
	// because they are what is being read.
	itself
)

// required returns the version of the marker module the loaded packages depend
// on, and how they reach it.
func required(found resolved, path string) (string, reach) {
	if found.Session == nil {
		return "", none
	}

	for _, pkg := range found.Session.Packages {
		if pkg.Module != nil && pkg.Module.Main && pkg.Module.Path == path {
			return "", itself
		}

		for _, one := range pkg.Imports {
			if one.Module == nil || one.Module.Path != path {
				continue
			}
			if one.Module.Replace != nil {
				return "", swapped
			}
			if one.Module.Version != "" {
				return one.Module.Version, depends
			}
		}
	}
	return "", none
}

// specs counts the declarations forge owns the type of, which is what decides
// whether anything here needs an editor told about the build tag.
func specs(found resolved) int {
	held := 0
	for _, req := range found.Requests {
		if req.Model != nil && req.Declaration.Candidate.Form == model.FormSpec {
			held++
		}
	}
	return held
}

// reported turns a diagnostic into a line of the table.
//
// Faulty, with one exception. Doctor's job is to say whether the setup is
// sound, and a tree any verb refuses is one that is not — but a file written by
// a different forge is the ordinary state of a working directory, since this is
// run by hand against whatever is on the path rather than by a gate against a
// pinned one. Reporting it as broken would teach somebody to ignore the column
// that matters.
func reported(one diag.Diagnostic) finding {
	how := faulty
	if one.Code == codeToolingMoved {
		how = worth
	}

	return finding{
		about: one.Code.String(), how: how,
		said: one.Pos.String() + ": " + one.Message,
		hint: one.Hint,
	}
}

// holding says what a package declares and whether git has its generated files.
//
// Only the two things check does not answer. What is generated, whether it is
// current and what is left over are all its questions, and they arrive above
// through it.
func holding(pkg packaged) []finding {
	if pkg.dir == "" {
		return []finding{{about: pkg.path, how: worth, said: "forge cannot find the files of this package"}}
	}

	modelled, spec := 0, 0
	for _, req := range pkg.requests {
		if req.Model == nil {
			continue
		}
		modelled++
		if req.Model.Form == model.FormSpec {
			spec++
		}
	}

	// What was modelled rather than what was found. A declaration whose subject
	// was refused is reported by the refusal above, and counting it here would
	// be describing a package by declarations nothing could be generated for.
	said := fmt.Sprintf("%s, %d in a spec file", declarations(modelled), spec)
	if refused := len(pkg.requests) - modelled; refused > 0 {
		said += fmt.Sprintf(", and %d refused", refused)
	}

	out := []finding{{about: pkg.path, said: said}}
	if held, how := tracked(pkg); held != "" {
		out = append(out, finding{about: pkg.path, how: how, said: held})
	}
	return out
}

// declarations counts what a package holds, in words.
func declarations(n int) string {
	if n == 1 {
		return "1 declaration"
	}
	return fmt.Sprintf("%d declarations", n)
}

// tracked says whether the generated files of a package are committed, and how
// worried to be, or nothing where there was nobody to ask.
//
// Nothing, because being unable to ask is not evidence. A directory that is not
// a repository, or a machine with no git, has no version control to be wrong
// about — and a line saying the files are committed would be a claim this did
// not earn, which is worse than the silence it replaces.
//
// It matters where it can be answered because the files are meant to be
// committed. Somebody who has them ignored has a repository that builds for
// them and for nobody else, and finds out from a colleague.
func tracked(pkg packaged) (string, verdict) {
	names, err := filepath.Glob(filepath.Join(pkg.dir, generated.Prefix()+"*.go"))
	if err != nil || len(names) == 0 {
		return "", 0
	}

	held, asked := known(pkg.dir)
	if !asked {
		return "", 0
	}

	var loose []string
	for _, name := range names {
		if base := filepath.Base(name); !held[base] {
			loose = append(loose, base)
		}
	}

	if len(loose) == 0 {
		return "git has every generated file", 0
	}
	return "git does not track " + strings.Join(loose, ", "), worth
}

// report writes what doctor found.
//
// Aligned by the widest cell rather than to a width chosen here, because the
// middle column holds import paths and those are as long as somebody's module
// is. A hint goes under the finding it belongs to and outside the alignment: it
// is a sentence rather than a cell, and a column sized to hold one would leave
// the other two columns squeezed against the left of the terminal.
func report(env *environment, found []finding) {
	var b strings.Builder

	w := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
	for _, one := range found {
		// Writes to a tabwriter over a strings.Builder, neither of which can
		// fail: the builder's Write never returns an error and the writer holds
		// everything until Flush.
		_, _ = fmt.Fprintf(w, "%s\t%s\t%s\n", one.how.mark(), one.about, one.said)
		if one.hint != "" {
			_, _ = fmt.Fprintf(w, "\t\thint: %s\n", one.hint)
		}
	}
	_ = w.Flush()

	say(env.stdout, "%s", b.String())
}

// patience is how long git gets to answer before this gives up on it.
const patience = 5 * time.Second

// known returns the files git tracks in a directory, and whether there was
// anybody to ask.
//
// One question for the whole directory rather than one per file. A package with
// twenty declarations has twenty-odd generated files, and forking git for each
// of them turns a report into a pause.
//
// What separates "not tracked" from "could not ask" is the exit status, not the
// words. git says "did not match any file" in whatever language the environment
// asked for — there are translations of it shipped beside the binary — so a
// report that read the message would quietly answer "tracked" for everybody
// whose locale is not English, which is the wrong answer given confidently.
func known(dir string) (map[string]bool, bool) {
	// Bounded, because this is a question about files rather than a reason to
	// wait: a repository on a filesystem that has gone away answers nothing,
	// and a report that hung there would be worse than one that says it could
	// not ask.
	ctx, stop := stdcontext.WithTimeout(stdcontext.Background(), patience)
	defer stop()

	// Names separated by a zero byte, which is the only separator a filename
	// cannot hold, and read as paths rather than as patterns: a file called
	// zz_forge_[a].go is a file rather than a character class.
	cmd := exec.CommandContext(ctx, "git", "-C", dir, "--literal-pathspecs", "ls-files", "-z", "--", ".") //nolint:gosec // a directory the load reported.

	// Without taking the repository's lock, since this only ever reads and a
	// doctor that blocked somebody's own commit would be a poor diagnosis.
	cmd.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0")

	out, err := cmd.Output()
	if err != nil {
		// No repository, no git, a broken index: all of them are this being
		// unable to ask, and none of them is the author's problem.
		return nil, false
	}

	held := make(map[string]bool)
	for _, name := range bytes.Split(out, []byte{0}) {
		if len(name) > 0 {
			held[filepath.Base(string(name))] = true
		}
	}
	return held, true
}
