package discover

import (
	"cmp"
	"go/ast"
	"go/token"
	"go/types"
	"slices"
	"strings"

	"golang.org/x/tools/go/packages"

	"github.com/okian/forge/internal/diag"
	"github.com/okian/forge/internal/emit"
	"github.com/okian/forge/internal/load"
	"github.com/okian/forge/internal/model"
)

// Candidate is a type declaration that might be a generation request.
//
// It is a candidate rather than a request because nothing here has followed
// the instantiation to its origin yet. A declaration over a generic type of
// the author's own is indistinguishable at this stage and is dropped later,
// silently, by the stage that can tell the difference.
type Candidate struct {
	// Name is the declared type's identifier.
	Name string

	// Pkg is the package the declaration lives in.
	Pkg *packages.Package

	// File is the file it was written in.
	File *ast.File

	// Spec is the declaration itself. Its Type field is the instantiation to
	// resolve.
	Spec *ast.TypeSpec

	// Form records whether the declaration was written inline, in an ordinary
	// file, or in a spec file that forge owns the real declaration for.
	Form model.Form

	// Directives holds the //forge: comments attached to the declaration:
	// those written above it first, in order, then one written after it on the
	// same line.
	Directives []Directive

	// Pos is the position of the declared name, which is where every
	// diagnostic about this declaration points.
	Pos token.Position
}

// String returns the declaration as it reads in source, without resolving
// anything: "Persons Collection[Ring[Json[Person]]]".
func (c Candidate) String() string {
	if c.Spec == nil {
		return c.Name
	}
	return c.Name + " " + types.ExprString(c.Spec.Type)
}

// Declarations returns every candidate declaration in the session, along with
// the diagnostics for directives that landed on nothing.
//
// The result is ordered by package import path, then by file name, then by
// position within that file. The file name is not decoration: a position's
// offset is counted across the whole file set rather than within one file, so
// comparing offsets is only meaningful between candidates already known to
// share a file.
func Declarations(session *load.Session) ([]Candidate, diag.Set) {
	var (
		found []Candidate
		diags diag.Set
	)

	if session == nil {
		return nil, diags
	}

	for _, pkg := range session.Packages {
		for _, file := range pkg.Syntax {
			if written(file) {
				continue
			}

			candidates, claimed := inFile(session.Fset, pkg, file)
			found = append(found, candidates...)
			reportStrays(session.Fset, file, claimed, &diags)
		}
	}

	slices.SortFunc(found, func(a, b Candidate) int {
		if c := strings.Compare(a.Pkg.PkgPath, b.Pkg.PkgPath); c != 0 {
			return c
		}
		if c := strings.Compare(a.Pos.Filename, b.Pos.Filename); c != 0 {
			return c
		}
		return cmp.Compare(a.Pos.Offset, b.Pos.Offset)
	})

	return found, diags
}

// written reports whether a file is one forge produced.
//
// Its own output is not input. Generated code holds declarations that look
// exactly like requests — the shared sequence view is a defined type over an
// instantiation, which is the shape a candidate is recognised by — and reading
// them back means a run finding a declaration nobody wrote, in a file the
// author does not edit, that the run before it created. What that produces is a
// count that is wrong, a diagnostic pointing into generated code, and one more
// of these every time the catalog grows a helper.
//
// Forge's own marker rather than the general convention. Another generator's
// output may legitimately hold a declaration written against these markers —
// generating a schema and then generating from it is an ordinary arrangement —
// and refusing to read those would break it silently. What is refused is only
// what forge itself wrote, which is what forge itself will overwrite.
func written(file *ast.File) bool {
	if file == nil {
		return false
	}

	// Before the package clause, which is where the go command's own rule puts
	// it and where the emitter writes it. A line further down is a comment
	// inside somebody's code that happens to read alike.
	for _, group := range file.Comments {
		if group.Pos() > file.Package {
			break
		}
		for _, line := range group.List {
			// Trailing space is ignored, as the reader of a generated header
			// ignores it: a file forge wrote is read back after anything at all
			// may have reformatted it, and a rule that answered differently
			// from the one in the emitter would make a file forge's for one
			// stage and somebody else's for the next.
			if strings.TrimRight(line.Text, " \t") == emit.Generated {
				return true
			}
		}
	}

	return false
}

// inFile returns the candidates declared in one file, and the offsets of the
// directive comments they claimed.
func inFile(fset *token.FileSet, pkg *packages.Package, file *ast.File) ([]Candidate, map[int]bool) {
	form := model.FormInline
	if load.SpecFile(fset, file) {
		form = model.FormSpec
	}

	var found []Candidate
	claimed := make(map[int]bool)

	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.TYPE {
			continue
		}

		for _, spec := range gen.Specs {
			typeSpec, ok := spec.(*ast.TypeSpec)
			if !ok || !candidate(typeSpec) {
				continue
			}

			attached := directives(fset, docOf(gen, typeSpec))
			attached = append(attached, directives(fset, typeSpec.Comment)...)
			for _, directive := range attached {
				claimed[directive.Pos.Offset] = true
			}

			found = append(found, Candidate{
				Name:       typeSpec.Name.Name,
				Pkg:        pkg,
				File:       file,
				Spec:       typeSpec,
				Form:       form,
				Directives: attached,
				Pos:        fset.Position(typeSpec.Name.Pos()),
			})
		}
	}

	return found, claimed
}

// candidate reports whether a type declaration is one worth resolving.
//
// Two conditions, both syntactic. It must be a defined type rather than an
// alias: an alias to an instantiation keeps the methods it already has and
// asks for nothing. And its right-hand side must be an instantiation, which is
// the only shape a stack can be written in.
//
// A declaration that is itself generic — type Wrapper[T any] Collection[T] —
// passes both and is deliberately kept. Its subject is a type parameter rather
// than a concrete type, which nothing can be generated for, but a layer will
// claim it at resolution and the composition rules can then say so. Rejecting
// it here would turn a diagnosable mistake into silence.
func candidate(spec *ast.TypeSpec) bool {
	if spec.Assign.IsValid() {
		return false
	}

	switch ast.Unparen(spec.Type).(type) {
	case *ast.IndexExpr, *ast.IndexListExpr:
		return true
	default:
		return false
	}
}

// docOf returns the comment group a declaration's directives live in.
//
// Where that is depends on how the declaration was written. The parser gives
// the comment above `type Persons Collection[Person]` to the declaration, and
// the comment above a spec inside a parenthesised `type (...)` group to the
// spec. A group holding a single spec is treated as the plain form, matching
// what go/doc does with it. A group holding several is not: its comment sits
// above all of them and could not say which one it means, so a directive there
// is reported rather than guessed at.
func docOf(gen *ast.GenDecl, spec *ast.TypeSpec) *ast.CommentGroup {
	if spec.Doc != nil {
		return spec.Doc
	}
	if !gen.Lparen.IsValid() || len(gen.Specs) == 1 {
		return gen.Doc
	}
	return nil
}
