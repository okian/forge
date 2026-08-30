package goldentest

import (
	"bytes"
	"errors"
	"fmt"
	"go/ast"
	"go/build"
	"go/build/constraint"
	"go/format"
	"go/importer"
	"go/parser"
	"go/scanner"
	"go/token"
	"go/types"
	"runtime"
	"slices"
	"strings"
	"sync"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/assign"
	"golang.org/x/tools/go/analysis/passes/bools"
	"golang.org/x/tools/go/analysis/passes/copylock"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/analysis/passes/nilfunc"
	"golang.org/x/tools/go/analysis/passes/structtag"
	"golang.org/x/tools/go/analysis/passes/unreachable"
	"golang.org/x/tools/go/ast/inspector"
)

// Source is one file of a package under test.
type Source struct {
	// Name is the file's name, which is what a failure is reported against and
	// what a recorded copy is filed under. It names a file and not a path:
	// every file of a package sits in one directory.
	Name string

	// Content is the file's bytes.
	Content []byte

	// Generated records that this file is output to be held to a recorded
	// copy. A file that is not is part of the fixture the output was generated
	// for — the subject, the types it reaches — and is here so that the output
	// has something to compile against.
	Generated bool
}

// Package is generated output together with everything it needs to compile.
type Package struct {
	// Path is the import path the package is type-checked as, and the name a
	// failure is reported under. The package *clause* does not come from here —
	// it comes from the files, which have to agree with each other about it.
	Path string

	// Tags names the build configuration the files are checked in. Generation
	// emits files constrained against each other — a real declaration under one
	// tag and a stub of the same name under its negation — so "which files are
	// in this package" is a question with more than one answer, and the answer
	// has to be given rather than assumed.
	//
	// A file's //go:build line is evaluated against these tags plus the running
	// GOOS and GOARCH; every other tag reads as false. A constraint written into
	// a file's *name* is not interpreted, because forge chooses the names it
	// emits and can avoid the ones that would mean something.
	Tags []string

	// Files holds the generated output and the fixture it was generated for.
	Files []Source
}

// analyses are run over output that already compiles.
//
// They are the checks that catch what the type-checker does not and that a
// generator can plausibly get wrong: a struct tag that is not a struct tag, a
// field assigned to itself, a condition repeated on both sides of an operator,
// a value copied that holds a lock, a comparison against a function rather than
// a call to one, code after a return. The set is deliberately short — an
// analysis that has never fired on generated output is a dependency and a
// slowdown rather than a check, so each one here has a case in this package's
// own tests that it and only it catches.
var analyses = []*analysis.Analyzer{
	assign.Analyzer,
	bools.Analyzer,
	copylock.Analyzer,
	nilfunc.Analyzer,
	structtag.Analyzer,
	unreachable.Analyzer,
}

// Compiles reports everything wrong with a package: what will not parse, what
// will not type-check, what the analyses object to, and what is not formatted.
//
// The first three are a chain, and only the first of them to find anything
// reports: code that does not parse has no meaningful type errors, and code
// that does not type-check gives the analyses nothing to work with. Formatting
// is not in that chain — a misaligned file parses and type-checks fine — so it
// is checked last, after everything that could be a reason the output is wrong
// rather than merely untidy. Within a stage every problem is reported rather
// than the first, because a generated file that is wrong is usually wrong in
// one way many times over, and being shown one of them per run is being shown a
// tenth of the story per run.
func Compiles(pkg Package) error {
	if pkg.Path == "" {
		return errors.New("a package with no import path")
	}
	if err := distinct(pkg.Files); err != nil {
		return fmt.Errorf("package %s: %w", pkg.Path, err)
	}

	fset := token.NewFileSet()

	var parsed []*ast.File
	var problems []string
	for _, source := range pkg.Files {
		file, err := parser.ParseFile(fset, source.Name, source.Content, parser.ParseComments|parser.SkipObjectResolution)
		if err != nil {
			problems = append(problems, spread(err)...)
			continue
		}
		parsed = append(parsed, file)
	}
	if len(problems) > 0 {
		return fmt.Errorf("package %s does not parse:\n%s", pkg.Path, strings.Join(problems, "\n"))
	}

	files, problems := selected(fset, parsed, pkg.Tags)
	if len(problems) > 0 {
		return fmt.Errorf("package %s has a build constraint that does not read:\n%s", pkg.Path, strings.Join(problems, "\n"))
	}
	if len(files) == 0 {
		if len(parsed) > 0 {
			return fmt.Errorf("package %s has no files left once %s is built: every one of them is constrained out",
				pkg.Path, configuration(pkg.Tags))
		}
		return fmt.Errorf("package %s has no files in it", pkg.Path)
	}

	info := &types.Info{
		Types:      make(map[ast.Expr]types.TypeAndValue),
		Defs:       make(map[*ast.Ident]types.Object),
		Uses:       make(map[*ast.Ident]types.Object),
		Selections: make(map[*ast.SelectorExpr]*types.Selection),
		Scopes:     make(map[ast.Node]*types.Scope),
		Instances:  make(map[*ast.Ident]types.Instance),
	}

	var failures []types.Error
	config := types.Config{
		Importer: stdlib,
		Sizes:    sizes,
		Error:    func(err error) { failures = append(failures, reported(fset, err)) },
	}

	// The error Check returns is the first of the ones it reported, and every
	// one of them reached failures — which is the list worth showing.
	checked, _ := config.Check(pkg.Path, fset, files, info)
	if len(failures) > 0 {
		return fmt.Errorf("package %s does not compile:\n%s", pkg.Path, strings.Join(ordered(failures), "\n"))
	}

	if found := analyse(fset, pkg, files, checked, info); len(found) > 0 {
		return fmt.Errorf("package %s compiles and does not hold up:\n%s", pkg.Path, strings.Join(found, "\n"))
	}

	if unformatted := formatting(pkg.Files); len(unformatted) > 0 {
		return fmt.Errorf("package %s is not formatted:\n%s", pkg.Path, strings.Join(unformatted, "\n"))
	}

	return nil
}

// configuration names a build configuration the way somebody would say it.
func configuration(tags []string) string {
	if len(tags) == 0 {
		return "with no tags"
	}
	return "with " + strings.Join(tags, ", ")
}

// formatting reports the generated files gofmt would rewrite, and where.
//
// Only the generated ones. A fixture is written by whoever wrote the test and
// is theirs to lay out; output is written by forge, and output gofmt would move
// is a diff waiting for the first person to run the formatter over the tree.
// The lint configuration excludes generated files from the formatter on the
// grounds that this suite checks them, which is only true if it does.
//
// Saying where costs nothing — the formatted bytes are already in hand — and
// turns "run gofmt yourself" into something a reader can act on.
func formatting(files []Source) []string {
	var problems []string

	for _, source := range files {
		if !source.Generated {
			continue
		}
		formatted, err := format.Source(source.Content)
		if err != nil || bytes.Equal(formatted, source.Content) {
			continue
		}
		problems = append(problems, fmt.Sprintf("%s is not formatted; gofmt would rewrite it:\n%s",
			source.Name, difference("gofmt", string(formatted), "the file", string(source.Content))))
	}

	return problems
}

// distinct refuses a package that names one file twice.
//
// Two files under one name would each be recorded over the other's golden, so
// the run that wrote them passes and the next one fails against whichever won:
// a suite that cannot be made green by rerunning it, and whose failure blames
// the output rather than the name. The mistake is easy to make, because the
// files come from a slice of structs rather than from a directory.
func distinct(files []Source) error {
	seen := make(map[string]bool, len(files))

	var repeated []string
	for _, source := range files {
		if source.Name == "" {
			return errors.New("a file with no name in it")
		}
		if seen[source.Name] {
			repeated = append(repeated, source.Name)
			continue
		}
		seen[source.Name] = true
	}

	if len(repeated) == 0 {
		return nil
	}

	slices.Sort(repeated)
	return fmt.Errorf("more than one file named %s", strings.Join(slices.Compact(repeated), ", "))
}

// spread flattens what a parse reported into one problem per line.
//
// A failed parse carries a whole list, and printing that list as an error shows
// the first of them and a count of the rest — which is the one shape of report
// this package exists to avoid.
func spread(err error) []string {
	list, ok := errors.AsType[scanner.ErrorList](err)
	if !ok {
		return []string{err.Error()}
	}

	out := make([]string, len(list))
	for i, failure := range list {
		out[i] = failure.Error()
	}
	return out
}

// selected keeps the files that belong to a build configuration.
func selected(fset *token.FileSet, files []*ast.File, tags []string) ([]*ast.File, []string) {
	var kept []*ast.File
	var problems []string

	for _, file := range files {
		line, ok := constrained(file)
		if !ok {
			kept = append(kept, file)
			continue
		}

		expr, err := constraint.Parse(line.Text)
		if err != nil {
			// Named, because generation writes the same constraint into every
			// file of a configuration and an unnamed one sends the reader
			// through all of them.
			problems = append(problems, fmt.Sprintf("%s: %s: %v", fset.Position(line.Pos()), line.Text, err))
			continue
		}
		if expr.Eval(func(tag string) bool { return satisfied(tag, tags) }) {
			kept = append(kept, file)
		}
	}

	return kept, problems
}

// constrained returns a file's //go:build line, if it has one.
//
// It only counts before the package clause. The same text further down is a
// comment about a build constraint rather than one, and the go command reads it
// the same way.
func constrained(file *ast.File) (*ast.Comment, bool) {
	for _, group := range file.Comments {
		if group.Pos() > file.Package {
			break
		}
		for _, line := range group.List {
			if constraint.IsGoBuild(line.Text) {
				return line, true
			}
		}
	}
	return nil, false
}

// satisfied reports whether a build tag holds in a configuration.
//
// This follows go/build's own answer rather than a convenient subset of it. A
// tag it got wrong would not fail — it would select a different set of files
// and then certify a configuration that no build produces, which is the one
// outcome worse than refusing to answer. The tag most likely to matter is the
// language version: forge's floor is a go1.N tag, and a layer emitting a real
// implementation under it and a fallback under its negation would otherwise
// have the fallback checked and the implementation silently dropped.
func satisfied(tag string, tags []string) bool {
	if slices.Contains(tags, tag) {
		return true
	}
	switch tag {
	case runtime.GOOS, runtime.GOARCH, build.Default.Compiler:
		return true
	case "unix":
		return unixLike[runtime.GOOS]
	case "cgo":
		return build.Default.CgoEnabled
	}
	return slices.Contains(build.Default.ReleaseTags, tag) ||
		slices.Contains(build.Default.ToolTags, tag)
}

// unixLike is the set of systems the "unix" build tag holds on, which go/build
// keeps in a package this one cannot import.
var unixLike = map[string]bool{
	"aix":       true,
	"android":   true,
	"darwin":    true,
	"dragonfly": true,
	"freebsd":   true,
	"hurd":      true,
	"illumos":   true,
	"ios":       true,
	"linux":     true,
	"netbsd":    true,
	"openbsd":   true,
	"solaris":   true,
}

// stdlib resolves the imports of packages under test, and resolves nothing
// else.
//
// Generated code imports the standard library and the package it is emitted
// into, never a third party: a generated file that needed one would version-skew
// against the binary that wrote it, which is the whole reason there is no
// runtime package to depend on. That is a promise, and a promise a suite does
// not check is a comment.
//
// One importer for the process, because resolving a standard library package
// from source costs tens to hundreds of milliseconds, and a suite that pays
// that per case is a suite that runs in minutes.
var stdlib = &library{}

// sizes are what the analyses measure a value against, taken from the machine
// running them rather than fixed, so that a report about a struct's layout
// describes the build it came from.
var sizes = types.SizesFor("gc", runtime.GOARCH)

// library is a [types.Importer] that refuses anything outside the standard
// library before it reads anything at all.
type library struct {
	mu   sync.Mutex
	from types.Importer
}

// Import resolves a standard library package and refuses everything else.
func (l *library) Import(path string) (*types.Package, error) {
	if !standard(path) {
		return nil, fmt.Errorf("%s is not in the standard library, and generated code imports nothing else", path)
	}

	// Serialised rather than one importer per call: caching what it has already
	// resolved is the point of sharing it, and nothing documents an importer as
	// safe to use from two goroutines at once.
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.from == nil {
		// From source rather than from a compiler's export data, so that the
		// gate works on a machine that has never built anything.
		l.from = importer.ForCompiler(token.NewFileSet(), "source", nil)
	}
	return l.from.Import(path)
}

// standard reports whether an import path names a standard library package
// generated code is allowed to reach for.
//
// The first rule is the go command's own: a path whose first element carries a
// dot is a module path, and everything else is under GOROOT. Deciding it from
// the text rather than by looking is deliberate — asking the filesystem would
// make the answer depend on what happens to be in the module cache, and asking
// the network would make a unit test depend on a proxy being up.
//
// Being under GOROOT is not the same as being importable, though. An internal
// or vendored element carries no dot and would pass the first rule, while go
// build refuses both from outside the tree they belong to — so a layer that
// reached into internal/abi for a fast path would be certified here and fail in
// the build it was generated for.
func standard(path string) bool {
	if path == "" || strings.HasPrefix(path, "/") {
		return false
	}

	first, _, _ := strings.Cut(path, "/")
	if strings.Contains(first, ".") {
		return false
	}

	for element := range strings.SplitSeq(path, "/") {
		if element == "internal" || element == "vendor" || element == "." || element == ".." {
			return false
		}
	}
	return true
}

// ordered renders type errors in one order.
//
// They arrive in the order the files were given, which is the caller's order,
// and a caller that builds its file list from a map reports differently every
// run. Position is the order a reader would put them in anyway.
func ordered(failures []types.Error) []string {
	slices.SortStableFunc(failures, func(a, b types.Error) int {
		return earlier(position(a), position(b))
	})

	out := make([]string, len(failures))
	for i, failure := range failures {
		out[i] = failure.Error()
	}
	return out
}

// reported turns what the type-checker said into a failure that can be put in
// order with the others.
//
// go/types reports a types.Error, which carries where it happened. Anything
// else arriving here has no position, and reporting it against the top of the
// package is better than dropping the one thing it does carry.
func reported(fset *token.FileSet, err error) types.Error {
	if failure, ok := errors.AsType[types.Error](err); ok {
		return failure
	}
	return types.Error{Fset: fset, Msg: err.Error()}
}

// position resolves where a type error happened, tolerating one that never had
// a position to begin with.
func position(failure types.Error) token.Position {
	if failure.Fset == nil {
		return token.Position{}
	}
	return failure.Fset.Position(failure.Pos)
}

// earlier orders two positions the way a reader reads them: by file, then down
// the file. Comparing the rendered text instead would put line 10 before line
// 9.
func earlier(a, b token.Position) int {
	if a.Filename != b.Filename {
		return strings.Compare(a.Filename, b.Filename)
	}
	if a.Line != b.Line {
		return a.Line - b.Line
	}
	return a.Column - b.Column
}

// finding is one thing an analysis objected to.
type finding struct {
	at      token.Position
	from    string
	message string
}

// String renders a finding the way a compiler renders an error, so that an
// editor can jump to it.
func (f finding) String() string {
	if f.at.Filename == "" {
		return fmt.Sprintf("%s: %s", f.from, f.message)
	}
	return fmt.Sprintf("%s: %s: %s", f.at, f.from, f.message)
}

// analyse runs the analyses over a package that already type-checks.
//
// They are run here rather than by shelling out, because a golden suite that
// starts a process per file is a golden suite people stop running. What they
// need in return is a package that has already been type-checked, which this
// stage has to hand.
func analyse(fset *token.FileSet, pkg Package, files []*ast.File, checked *types.Package, info *types.Info) []string {
	var found []finding

	// Every analysis here walks the syntax the same way, so the walk is built
	// once and handed to all of them, which is what an analysis driver does.
	walk := inspector.New(files)

	for _, analyzer := range analyses {
		if why := unsupported(analyzer); why != "" {
			found = append(found, finding{from: analyzer.Name, message: "cannot run here: " + why})
			continue
		}

		pass := &analysis.Pass{
			Analyzer:   analyzer,
			Fset:       fset,
			Files:      files,
			Pkg:        checked,
			TypesInfo:  info,
			TypesSizes: sizes,
			ResultOf:   map[*analysis.Analyzer]any{inspect.Analyzer: walk},
			Report: func(d analysis.Diagnostic) {
				found = append(found, finding{at: fset.Position(d.Pos), from: analyzer.Name, message: d.Message})
			},
			ReadFile: func(name string) ([]byte, error) {
				for _, source := range pkg.Files {
					if source.Name == name {
						return source.Content, nil
					}
				}
				return nil, fmt.Errorf("%s is not a file of package %s", name, pkg.Path)
			},

			// No fact plumbing. Facts are how an analysis carries a conclusion
			// about one package into the analysis of another, and this driver
			// only ever holds one. The four functions a fact-carrying analysis
			// would reach for are left off rather than stubbed out to answer
			// "no": a stub would let such an analysis run here and quietly
			// conclude the wrong thing, where nil makes it fail loudly — and
			// unsupported turns that failure into a message first.
		}

		found = append(found, ran(analyzer, pass)...)
	}

	// One order, whichever order the analyses ran in and wherever in a file they
	// fired, so that a failure reads the same way twice.
	slices.SortStableFunc(found, func(a, b finding) int {
		if at := earlier(a.at, b.at); at != 0 {
			return at
		}
		if a.from != b.from {
			return strings.Compare(a.from, b.from)
		}
		return strings.Compare(a.message, b.message)
	})

	out := make([]string, len(found))
	for i, f := range found {
		out[i] = f.String()
	}
	return out
}

// unsupported says why an analysis cannot run against this driver, or nothing
// if it can.
//
// The driver is deliberately small: one package, one shared walk, no facts.
// What an analysis might otherwise reach for is absent rather than stubbed, so
// adding one that needs more would not fail — it would dereference nothing and
// take the test binary down with it, and lazily, on whichever input first
// reaches the code that asks. Naming the gap is the difference between a
// message here and a crash in somebody else's package.
func unsupported(analyzer *analysis.Analyzer) string {
	if len(analyzer.FactTypes) > 0 {
		return "it carries facts between packages, and this driver holds one"
	}
	for _, needed := range analyzer.Requires {
		if needed != inspect.Analyzer {
			return "it needs the result of " + needed.Name + ", which this driver does not produce"
		}
	}
	return ""
}

// ran runs one analysis, turning a failure or a panic into a finding.
//
// An analysis that panics would otherwise take the test binary down and be
// reported as a crash in whichever layer's package happened to be running,
// which is the wrong package to go looking in.
func ran(analyzer *analysis.Analyzer, pass *analysis.Pass) (found []finding) {
	defer func() {
		if r := recover(); r != nil {
			found = append(found, finding{from: analyzer.Name, message: fmt.Sprintf("panicked: %v", r)})
		}
	}()

	if _, err := analyzer.Run(pass); err != nil {
		found = append(found, finding{from: analyzer.Name, message: fmt.Sprintf("could not run: %v", err)})
	}
	return found
}
