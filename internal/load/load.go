package load

import (
	"fmt"
	"go/ast"
	"go/token"
	"path/filepath"
	"slices"
	"strings"

	"golang.org/x/tools/go/packages"

	"github.com/okian/forge/internal/diag"
)

// loadMode is everything forge needs and nothing more.
//
// Syntax and type information are both required: the declaration forge acts on
// is recovered from the AST, because go/types discards the instantiation that
// wrote it, while the subject's fields and tags come from the type-checker.
// Dependencies are needed because a subject may be declared in another package
// of the same module.
const loadMode = packages.NeedName |
	packages.NeedFiles |
	packages.NeedCompiledGoFiles |
	packages.NeedImports |
	packages.NeedDeps |
	packages.NeedTypes |
	packages.NeedTypesInfo |
	packages.NeedSyntax |
	packages.NeedModule

// defaultPattern is what a command loads when the author names no packages.
const defaultPattern = "./..."

// Config describes what to load.
type Config struct {
	// Dir is the directory to resolve patterns from. An empty value means the
	// process's working directory.
	Dir string

	// Patterns are go/packages patterns. An empty list means every package in
	// the module rooted at Dir.
	Patterns []string

	// Tags are build tags to set in addition to the spec tag, which is always
	// set.
	Tags []string

	// Env overrides the environment the go command runs with. An empty value
	// means the process's own environment. It exists so that a test can pin the
	// toolchain's behavior.
	Env []string
}

// patterns returns the patterns to load, defaulted.
func (c Config) patterns() []string {
	if len(c.Patterns) == 0 {
		return []string{defaultPattern}
	}
	return slices.Clone(c.Patterns)
}

// buildFlags returns the go command flags the session runs with.
func (c Config) buildFlags() []string {
	tags := append([]string{SpecTag}, c.Tags...)
	return []string{"-tags=" + strings.Join(tags, ",")}
}

// Session is one completed load: the packages, the file set their positions
// are relative to, and whatever was wrong with them.
//
// Packages are returned even when Diagnostics is not empty. A package that
// fails to type-check still yields most of its type information, and it is the
// caller's decision whether a partial answer is worth having.
type Session struct {
	// Fset holds the positions of every file in the session. Diagnostics
	// resolve their positions against it.
	Fset *token.FileSet

	// Packages holds the packages the patterns matched, ordered by import path.
	// go/packages returns roots in the order the patterns named them, so the
	// order here is the session's own and not the caller's.
	//
	// The packages themselves still carry maps — Imports, and everything under
	// TypesInfo — so ordering these is a floor and not a guarantee. Anything
	// that walks those owes its result a sort before it reaches the emitter.
	Packages []*packages.Package

	// Diagnostics holds the build errors worth reporting. Errors that only
	// exist because forge strips function bodies are not among them.
	Diagnostics diag.Set

	// dir is the directory the patterns were resolved from, used to put every
	// reported position into the same form.
	dir string

	// seen holds the diagnostics already recorded, so that one mistake the go
	// command reports three times is reported once.
	seen map[string]bool
}

// Load runs one go/packages session over the configured patterns.
//
// It returns an error only when the load could not be attempted at all — a
// malformed pattern, or a go command that will not run. A package that does
// not compile is not that: it is reported through [Session.Diagnostics], with
// the position the compiler gave it.
func Load(cfg Config) (*Session, error) {
	patterns := cfg.patterns()
	fset := token.NewFileSet()

	pkgs, err := packages.Load(&packages.Config{
		Mode:       loadMode,
		Dir:        cfg.Dir,
		Env:        cfg.Env,
		Fset:       fset,
		BuildFlags: cfg.buildFlags(),
		ParseFile:  parseFile,
	}, patterns...)
	if err != nil {
		return nil, fmt.Errorf("loading %s: %w", strings.Join(patterns, " "), err)
	}

	slices.SortStableFunc(pkgs, func(a, b *packages.Package) int {
		return strings.Compare(a.PkgPath, b.PkgPath)
	})

	dir, absErr := filepath.Abs(cfg.Dir)
	if absErr != nil {
		dir = cfg.Dir
	}

	session := &Session{Fset: fset, Packages: pkgs, dir: dir}

	if len(pkgs) == 0 {
		session.reportNoPackages(patterns)
		return session, nil
	}

	for _, pkg := range pkgs {
		session.collect(pkg)
	}

	return session, nil
}

// FileName returns the path of a parsed file.
//
// It resolves the start of the file rather than its package clause, because a
// file too broken to reach the clause is still in the package's syntax and is
// exactly the file a diagnostic most needs to name. It is also why this reads
// the position rather than indexing CompiledGoFiles: go/packages drops files
// that failed to parse from the syntax list, so the two do not correspond.
func (s *Session) FileName(file *ast.File) string {
	if s == nil || file == nil {
		return ""
	}
	return s.Fset.Position(file.FileStart).Filename
}

// Module returns the import path of the module being generated for, or the
// empty string when the load reached no package that belongs to one.
//
// The main module, taken from the load rather than inferred: go/packages marks
// which module each package belongs to, so this is the answer rather than a
// guess at it. A workspace holding several main modules reports the first by
// import path, which is at least the same one twice.
//
// It returns a path, and a path is not yet enough. Deciding whether a type is
// one forge may attach a method to still compares import path prefixes, and a
// module nested inside this one shares the prefix while belonging to neither
// its build nor its ownership — so the wrong answer is available for exactly
// the packages whose generated code would not compile. Closing that means
// asking per package rather than per path, which is a change to the stage that
// asks.
//
// A package in no module at all — GOPATH mode, or a directory outside one —
// reports nothing rather than guessing, since every type it holds is then
// equally foreign and saying so is the honest answer.
func (s *Session) Module() string {
	if s == nil {
		return ""
	}
	for _, pkg := range s.Packages {
		if pkg.Module != nil && pkg.Module.Main {
			return pkg.Module.Path
		}
	}
	return ""
}

// Package returns the loaded package with the given import path.
func (s *Session) Package(path string) (*packages.Package, bool) {
	if s == nil {
		return nil, false
	}
	for _, pkg := range s.Packages {
		if pkg.PkgPath == path {
			return pkg, true
		}
	}
	return nil, false
}
