package emit

import (
	"fmt"
	"go/build/constraint"
	"go/format"
	"go/token"
	"slices"
	"strconv"
	"strings"

	"github.com/okian/forge/internal/diag"
)

// Diagnostics this package reports.
//
// All of them say the same thing about whose fault it is: generated code that
// will not parse, a tree that cannot be printed, and two imports that contradict
// each other are forge's mistakes rather than the author's. They are reported
// against the declaration that asked for the file, because that is the only
// line the author can act on — and they say what was produced, because the file
// was never written and there is nothing on disk to open.
var (
	codeUnparsable    = diag.Register(4001, "generated source does not parse")
	codeMalformed     = diag.Register(4002, "generated declaration is not well formed")
	codeImportClash   = diag.Register(4003, "one import path is bound to two names")
	codeBadConstraint = diag.Register(4004, "build constraint is not a constraint")
	codeBadHeader     = diag.Register(4005, "generated header cannot be written")
)

// Import is one import a generated file needs.
type Import struct {
	// Path is the import path.
	Path string

	// Name is the local name to import it under, empty for the package's own.
	// It is written only when it differs, so that a generated file reads the
	// way a hand-written one would.
	Name string
}

// String returns the import as it is written in an import block.
func (i Import) String() string {
	if i.Name == "" {
		return strconv.Quote(i.Path)
	}
	return i.Name + " " + strconv.Quote(i.Path)
}

// compare orders imports by path, then by the name they are bound to, so that
// a file's import block reads the same way twice.
func (i Import) compare(other Import) int {
	if i.Path != other.Path {
		return strings.Compare(i.Path, other.Path)
	}
	return strings.Compare(i.Name, other.Name)
}

// File is one generated file, described in the terms it will be written in.
type File struct {
	// Package is the name in the package clause.
	Package string

	// Decl is the name of the declaration the file was generated for, and Pos
	// is where that declaration is written. Everything reported about this file
	// points there: a mistake in generated code is not a line the author can
	// edit, and the declaration that asked for it is.
	Decl string
	Pos  token.Position

	// Build is the build constraint the file carries, written without the
	// "//go:build" that introduces it — "!forgespec" for a file that is part of
	// an ordinary build. It is empty for a file with no constraint.
	Build string

	// Header records how the file was produced. A file with no header still
	// says it is generated; it simply cannot be checked for staleness cheaply.
	Header Header

	// Imports holds the paths the declarations need. They are sorted and
	// deduplicated when the file is written, so a caller may add one wherever
	// it discovers it is needed.
	Imports []Import

	// Sections holds what each layer generated, in the order it will appear.
	//
	// The order is the caller's, and it is the caller's to make deterministic:
	// sorting here would separate a type from the constructor that goes with
	// it, which is the one thing a reader of generated code most wants kept
	// together.
	Sections []Section
}

// Render returns the file's bytes, formatted the way gofmt would format them.
//
// The same file rendered twice is the same bytes. That is what lets generation
// skip a write when nothing has changed, which in turn is what keeps a
// generated file out of every diff that did not touch it.
func (f File) Render() ([]byte, error) {
	var b strings.Builder

	if err := f.Header.render(&b); err != nil {
		return nil, f.report(codeBadHeader, err)
	}

	if f.Build != "" {
		if _, err := constraint.Parse("//go:build " + f.Build); err != nil {
			return nil, f.report(codeBadConstraint, err)
		}
		// The constraint goes above the package clause with a blank line under
		// it. Without the blank line the compiler still honours it, and gofmt
		// still leaves it alone — but it becomes the package's doc comment,
		// which is what every reader of the package then sees first.
		b.WriteString("\n//go:build ")
		b.WriteString(f.Build)
		b.WriteByte('\n')
	}

	b.WriteString("\npackage ")
	b.WriteString(f.Package)
	b.WriteString("\n\n")

	if err := f.renderImports(&b); err != nil {
		return nil, err
	}

	for _, section := range f.Sections {
		text, err := section.Render()
		if err != nil {
			return nil, f.report(codeMalformed, err)
		}
		if text == "" {
			continue
		}
		b.WriteString(text)
		b.WriteString("\n\n")
	}

	source := b.String()

	out, err := format.Source([]byte(source))
	if err != nil {
		return nil, f.report(codeUnparsable, fmt.Errorf("%w\n%s", err, numbered(source)))
	}

	return out, nil
}

// report turns something that went wrong while writing into a diagnostic
// pointing at the declaration that asked for the file.
func (f File) report(code diag.Code, cause error) diag.Diagnostic {
	what := "a file"
	if f.Decl != "" {
		what = f.Decl
	}

	return diag.New(code, f.Pos, "generating %s for package %s: %v", what, f.Package, cause).
		WithHint("%s", "this is a bug in forge rather than in the declaration; the file was not written")
}

// renderImports writes the import block, sorted and deduplicated.
//
// One block rather than the two gofmt would leave alone. Generated code imports
// the standard library and the subject's own dependencies, and a grouping is a
// thing a reader has to maintain in their head for no benefit here: nobody
// hand-edits this file, so nobody adds an import to the wrong group.
func (f File) renderImports(b *strings.Builder) error {
	imports := slices.Clone(f.Imports)
	slices.SortFunc(imports, Import.compare)
	imports = slices.Compact(imports)

	// One path bound to two names is two imports of it, which does not compile.
	// Sorting has already put the pair next to each other.
	//
	// The mirror of it — two paths bound to one name — is not checked here and
	// is not an oversight. An import written without a name binds whatever its
	// package declares itself to be, which is not in the path and is not
	// anywhere else in a file either, so answering it means resolving the
	// packages. That belongs to the stage that assembles a file out of what
	// every layer contributed, which is the only one that both sees them all
	// and has a load to ask. A layer keeps its own contribution clear of the
	// names it knows it will bind; the rest waits for that stage.
	for i := 1; i < len(imports); i++ {
		if imports[i].Path == imports[i-1].Path {
			return f.report(codeImportClash, fmt.Errorf("%s is imported %s and %s",
				imports[i].Path, boundAs(imports[i-1].Name), boundAs(imports[i].Name)))
		}
	}

	switch len(imports) {
	case 0:
		return nil

	case 1:
		b.WriteString("import ")
		b.WriteString(imports[0].String())
		b.WriteString("\n\n")

	default:
		b.WriteString("import (\n")
		for _, path := range imports {
			b.WriteByte('\t')
			b.WriteString(path.String())
			b.WriteByte('\n')
		}
		b.WriteString(")\n\n")
	}

	return nil
}

// boundAs says what name an import is bound to, for a message that reads.
func boundAs(name string) string {
	if name == "" {
		return "under its own name"
	}
	return "as " + strconv.Quote(name)
}

// errNotWellFormed reports a tree the printer could not walk.
func errNotWellFormed(caught any) error {
	return fmt.Errorf("a declaration is not well formed: %T: %v", caught, caught)
}

// numbered returns source with line numbers, for the error raised when
// generated code will not parse.
//
// The unparsable source is the whole of the evidence. A parse error names a
// line, and without the line the message names a place nobody can look: the
// file was never written, so there is nothing on disk to open.
func numbered(source string) string {
	var b strings.Builder
	for i, line := range strings.Split(source, "\n") {
		fmt.Fprintf(&b, "%4d | %s\n", i+1, line)
	}
	return b.String()
}
