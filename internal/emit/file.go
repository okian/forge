package emit

import (
	"fmt"
	"go/ast"
	"go/build/constraint"
	"go/format"
	"go/token"
	"slices"
	"strconv"
	"strings"

	"github.com/okian/forge/internal/diag"
	"github.com/okian/forge/internal/model"
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
//
// The same type a spelling asks for, rather than a second one of the same
// shape. What a layer is told a type needs is what the file has to bind, and
// two types spelled alike meant every layer copying one into the other field by
// field — a dozen packages did, in twenty-odd places, and one of the copies was
// a function whose whole body was the conversion.
type Import = model.Import

// compare orders imports by path, then by the name they are bound to, and last
// by whether that name is written, so that a file's import block reads the same
// way twice.
//
// The last of those is not decoration. Two spellings of one binding are one
// import and one of them is kept, so what decides which is whichever the sort
// left first — and an order that called them equal would leave that to the
// order they were found in, which is the order the layers happened to run. The
// file would then be a function of something no fingerprint records: the bytes
// would move while the header stood still, which is the one failure a generated
// file cannot report about itself.
func ordered(i, other Import) int {
	if i.Path != other.Path {
		return strings.Compare(i.Path, other.Path)
	}
	if i.Name != other.Name {
		return strings.Compare(i.Name, other.Name)
	}
	return compareBool(i.Aliased, other.Aliased)
}

// compareBool orders false before true.
func compareBool(i, other bool) int {
	switch {
	case i == other:
		return 0
	case other:
		return -1
	default:
		return 1
	}
}

// same reports two imports that would be written as one.
//
// Whether the name was written is not part of it. Two layers reaching one
// package, one of them binding it to the name it already has and one leaving it
// bare, ask for the same import in two spellings — and the file needs it once.
//
// The one kept is the one written without the name, which is what ordering them
// by it arranges: false sorts first and compaction keeps the first. It is the
// spelling somebody would have written by hand, and the choice has to be made
// somewhere rather than fall out of which layer ran first.
func same(i, other Import) bool {
	return i.Path == other.Path && i.Name == other.Name
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
	slices.SortFunc(imports, ordered)
	imports = slices.CompactFunc(imports, same)

	// One path bound to two names is two imports of it, which does not compile.
	// Sorting has already put the pair next to each other.
	for i := 1; i < len(imports); i++ {
		if imports[i].Path == imports[i-1].Path {
			return f.report(codeImportClash, fmt.Errorf("%s is imported %s and %s",
				imports[i].Path, boundAs(imports[i-1]), boundAs(imports[i])))
		}
	}

	// And the mirror of it, two paths bound to one name, which is the same
	// failure read the other way: the second binding wins and every qualified
	// identifier meant for the first resolves to the wrong package, or to
	// nothing.
	//
	// Answerable here because an import carries the name it binds rather than
	// only the name it writes. It is checked here rather than by whatever
	// assembled the file, because a file is the scope a name is bound in and
	// this is what writes one — a layer sees only its own contribution, and two
	// layers can each be blameless.
	held := make(map[string]string, len(imports))
	for _, one := range imports {
		if first, taken := held[one.Name]; taken {
			return f.report(codeImportClash, fmt.Errorf("%s and %s are both imported as %s",
				first, one.Path, strconv.Quote(one.Name)))
		}
		held[one.Name] = one.Path
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

// Reaching returns the imports these declarations still need.
//
// A caller that emits a chosen part of what it generated — a template variant
// the options did not select, a body thrown away — is left holding the import
// set of the whole. An import nothing left names is a file that does not
// compile, so the set has to be narrowed to what survived.
//
// The name an import binds is what is looked for, used to the left of a dot.
// That is exact rather than a guess, because an import carries the name it
// binds whether or not the name is written.
//
// It over-collects, and in the safe direction: a receiver called s and a
// package called s read the same way without the type information a caller at
// this stage does not have, so an import may be kept that nothing needed. What
// that costs is an unused import in a file whose first build says so plainly.
//
// Two kinds are kept whatever the declarations say, because for those the same
// reasoning runs the other way. An import bound to the blank name is there for
// what loading the package does rather than for anything it offers, so no
// declaration will ever name it and dropping it changes what the file does with
// nothing to say so. One bound to the dot puts its names in the file's own
// scope, where they are written without a qualifier and so are invisible to
// this. Both would be dropped in silence, which is the failure that is worth
// avoiding: an unused import is a build error, and a missing side effect is a
// program that behaves differently.
func Reaching(decls []ast.Decl, imports []Import) []Import {
	named := Qualifiers(decls)

	out := make([]Import, 0, len(imports))
	for _, one := range imports {
		if one.Name == "_" || one.Name == "." || named[one.Name] {
			out = append(out, one)
		}
	}
	return out
}

// Qualifiers returns the names used to the left of a dot in these declarations.
//
// It is the question "which packages does this code still refer to", asked
// without type information, so the answer is the set of names that *could* be
// a package qualifier: a receiver called s and a package called s are the same
// text here, and telling them apart is what a type checker is for.
//
// Answering wide is what makes it useful. Every caller uses it to decide
// whether something may be dropped, and a set that missed a name would drop
// something needed — where a set that holds one too many only ever declines to
// drop.
func Qualifiers(decls []ast.Decl) map[string]bool {
	named := make(map[string]bool)

	for _, decl := range decls {
		ast.Inspect(decl, func(node ast.Node) bool {
			selector, is := node.(*ast.SelectorExpr)
			if !is {
				return true
			}
			if ident, is := selector.X.(*ast.Ident); is {
				named[ident.Name] = true
			}
			return true
		})
	}
	return named
}

// boundAs says what name an import is bound to, for a message that reads.
func boundAs(held Import) string {
	if !held.Aliased {
		return "under its own name"
	}
	return "as " + strconv.Quote(held.Name)
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
