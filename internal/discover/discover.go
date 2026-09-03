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

// Hint is a package-level function carrying a map directive, kept whole for
// the stage that reads it. Discovery claims it and judges nothing: whether the
// directive means anything is the reading stage's to say, which is the same
// bargain claimFields strikes for field options.
type Hint struct {
	// Layer and Args are the directive as written: "map" and whatever
	// followed it.
	Layer string
	Args  string

	// Fn is the function itself, its body kept by the loader because the
	// directive marks it as a stage's input.
	Fn *ast.FuncDecl

	// Pkg is the package the function lives in, which is where its parameter
	// types resolve.
	Pkg *packages.Package

	// Form records what kind of file the function was written in. A hint
	// belongs in a spec file, and the stage that reads hints is the one that
	// says so — with the form recorded here.
	Form model.Form

	// Pos is the position of the function's name, which is where every
	// diagnostic about this hint points.
	Pos token.Position
}

// Declarations returns every candidate declaration in the session and every
// map hint beside them, along with the diagnostics for directives that landed
// on nothing.
//
// Each result is ordered by package import path, then by file name, then by
// position within that file. The file name is not decoration: a position's
// offset is counted across the whole file set rather than within one file, so
// comparing offsets is only meaningful between candidates already known to
// share a file.
func Declarations(session *load.Session) ([]Candidate, []Hint, diag.Set) {
	var (
		found []Candidate
		hints []Hint
		diags diag.Set
	)

	if session == nil {
		return nil, nil, diags
	}

	for _, pkg := range session.Packages {
		for _, file := range pkg.Syntax {
			if written(file) {
				continue
			}

			candidates, claimed := inFile(session.Fset, pkg, file)
			found = append(found, candidates...)
			hints = append(hints, claimFuncs(session.Fset, pkg, file, claimed)...)
			reportStrays(session.Fset, file, claimed, &diags)
		}
	}

	byPosition(found, func(c Candidate) (string, token.Position) { return c.Pkg.PkgPath, c.Pos })
	byPosition(hints, func(h Hint) (string, token.Position) { return h.Pkg.PkgPath, h.Pos })

	return found, hints, diags
}

// byPosition orders a discovery result by package import path, then file, then
// position within the file.
func byPosition[T any](held []T, at func(T) (string, token.Position)) {
	slices.SortFunc(held, func(a, b T) int {
		aPkg, aPos := at(a)
		bPkg, bPos := at(b)

		if c := strings.Compare(aPkg, bPkg); c != 0 {
			return c
		}
		if c := strings.Compare(aPos.Filename, bPos.Filename); c != 0 {
			return c
		}
		return cmp.Compare(aPos.Offset, bPos.Offset)
	})
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

			attached := model.Directives(fset, docOf(gen, typeSpec))
			attached = append(attached, model.Directives(fset, typeSpec.Comment)...)
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

	claimFields(fset, file, claimed)
	return found, claimed
}

// claimFields marks the directives written above a struct field as landed, so
// that the stray check does not report them.
//
// A field-scoped option is written above the field it applies to, and this
// stage does not read it — the stage that walks the subject does, where the
// field is a field rather than a line of syntax. What is claimed here is
// therefore claimed on that stage's behalf, and the claim is worth making
// because the alternative is worse in both directions: unclaimed, every
// correctly written field option is reported as applying to nothing, and the
// only advice forge could give is to delete it.
//
// The gap this leaves is a directive above a field of a struct that is never a
// subject. Nothing reads it and nothing complains, because whether a struct is
// a subject is not decided here or in this file — it is decided by what some
// declaration elsewhere names. It is the narrower silence of the two available.
func claimFields(fset *token.FileSet, file *ast.File, claimed map[int]bool) {
	ast.Inspect(file, func(node ast.Node) bool {
		structure, ok := node.(*ast.StructType)
		if !ok || structure.Fields == nil {
			return true
		}

		for _, field := range structure.Fields.List {
			for _, directive := range model.Directives(fset, field.Doc) {
				claimed[directive.Pos.Offset] = true
			}
		}
		return true
	})
}

// claimFuncs returns the map directives written on package-level functions,
// claiming them so the stray check does not report them.
//
// Only the map layer reads functions, so only its directives are claimed: a
// directive naming any other layer on a function still lands on nothing, and
// saying so is the point of the stray check. What "map" means — the grammar of
// the arguments, the shape of the signature, which declaration the hint is for
// — is the reading stage's business, judged where source and target are known.
func claimFuncs(fset *token.FileSet, pkg *packages.Package, file *ast.File, claimed map[int]bool) []Hint {
	form := model.FormInline
	if load.SpecFile(fset, file) {
		form = model.FormSpec
	}

	var hints []Hint
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Recv != nil {
			continue
		}

		for _, directive := range model.Directives(fset, fn.Doc) {
			if directive.Layer != "map" {
				continue
			}

			claimed[directive.Pos.Offset] = true
			hints = append(hints, Hint{
				Layer: directive.Layer,
				Args:  directive.Args,
				Fn:    fn,
				Pkg:   pkg,
				Form:  form,
				Pos:   fset.Position(fn.Name.Pos()),
			})
		}
	}

	return hints
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
