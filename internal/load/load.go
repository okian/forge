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
// It returns a path, which answers who the module is and not which packages are
// its own. Those are different questions and the second one is what decides
// whether forge may attach a method to a type: a module nested inside this one
// shares its path prefix and belongs to somebody else. [Session.Owned] answers
// that one, and is what the stage building subjects asks — this is for naming
// the module, which is all anything else wants it for.
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

// Owned returns the import paths of every loaded package that belongs to the
// module being generated for.
//
// The exact answer to a question a prefix cannot answer. Membership looks like
// a string test — a package under the module path is the module's — and a
// module with another nested inside it breaks that: the nested module's
// packages share the prefix and belong to somebody else. The difference matters
// because it decides whether a layer may attach a method, and attaching one to
// a type in a module forge does not own is a compile error in generated code
// rather than a mislabel.
//
// Every loaded package rather than the roots, since a subject may be declared
// in a package the roots merely import — and whether *that* package is the
// module's is the question being asked about it.
//
// Every main module, where a workspace has more than one. They are all being
// built together and are all the author's to change, so a type in any of them
// is one forge could reach by generating into its package — which is the
// question this is asked in aid of.
func (s *Session) Owned() map[string]bool {
	out := make(map[string]bool)
	if s == nil {
		return out
	}

	seen := make(map[string]bool)

	var walk func(pkg *packages.Package)
	walk = func(pkg *packages.Package) {
		if pkg == nil || seen[pkg.PkgPath] {
			return
		}
		seen[pkg.PkgPath] = true

		if pkg.Module != nil && pkg.Module.Main {
			out[pkg.PkgPath] = true
		}
		for _, one := range pkg.Imports {
			walk(one)
		}
	}

	for _, pkg := range s.Packages {
		walk(pkg)
	}
	return out
}

// FieldDocs returns the comment written above each struct field in the load,
// keyed by the position of the field's name.
//
// It exists because the stage that walks a subject walks types rather than
// syntax. A field's options are written above it as an ordinary comment, and
// go/types carries no comments at all — so the two have to be brought back
// together, and a position is what they have in common: the name in the syntax
// and the variable in the type both report the same one.
//
// The whole import graph rather than the requested packages, for the same
// reason [Session.Owned] walks it: a subject is often declared in a package
// that is imported rather than named, and its fields are documented where they
// are declared.
//
// One field per key even where several share a declaration. In `A, B int` the
// comment above the line documents both, and both names report their own
// position, so both find it.
func (s *Session) FieldDocs() map[token.Pos]*ast.CommentGroup {
	out := make(map[token.Pos]*ast.CommentGroup)
	if s == nil {
		return out
	}

	seen := make(map[string]bool)

	var walk func(pkg *packages.Package)
	walk = func(pkg *packages.Package) {
		if pkg == nil || seen[pkg.PkgPath] {
			return
		}
		seen[pkg.PkgPath] = true

		for _, file := range pkg.Syntax {
			documented(file, out)
		}
		for _, one := range pkg.Imports {
			walk(one)
		}
	}

	for _, pkg := range s.Packages {
		walk(pkg)
	}
	return out
}

// documented records every documented struct field in one file.
//
// Struct fields alone. An interface's methods and a function's parameters are
// both *ast.Field and neither is a field of a subject, so recording them would
// put entries under positions nothing looks up — harmless, but it would make
// the map a claim about something wider than it is.
func documented(file *ast.File, into map[token.Pos]*ast.CommentGroup) {
	ast.Inspect(file, func(node ast.Node) bool {
		structure, ok := node.(*ast.StructType)
		if !ok || structure.Fields == nil {
			return true
		}

		for _, field := range structure.Fields.List {
			if field.Doc == nil {
				continue
			}
			for _, name := range field.Names {
				into[name.Pos()] = field.Doc
			}

			// An embedded field has no name written for it, and go/types gives
			// it one anyway: the last identifier of the type it embeds. That
			// identifier's position is what the variable reports, and it is not
			// where the field starts — a *T begins at the star and a pkg.T at
			// the package name, so keying on the field would file both under a
			// position nothing ever looks up.
			if len(field.Names) == 0 {
				if at := embedded(field.Type); at.IsValid() {
					into[at] = field.Doc
				}
			}
		}
		return true
	})
}

// embedded returns the position go/types reports for an embedded field, which
// is the position of the name it takes.
//
// The name an embedded field takes is the last identifier of the type: Buffer
// for bytes.Buffer, and the same again through a pointer or a type argument.
// Anything else is not a type that can be embedded, and reports no position
// rather than a wrong one.
func embedded(held ast.Expr) token.Pos {
	switch held := held.(type) {
	case *ast.Ident:
		return held.Pos()
	case *ast.StarExpr:
		return embedded(held.X)
	case *ast.SelectorExpr:
		return held.Sel.Pos()
	case *ast.IndexExpr:
		return embedded(held.X)
	case *ast.IndexListExpr:
		return embedded(held.X)
	default:
		return token.NoPos
	}
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
