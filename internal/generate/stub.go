package generate

import (
	"go/ast"
	"go/token"
	"slices"

	"github.com/okian/forge/internal/emit"
	"github.com/okian/forge/internal/merge"
)

// stubs returns what a declaration contributes to the file that stands in for
// its output under the tag.
//
// The file forge writes for a spec declaration is constrained against the tag,
// so a build with the tag set does not have it — and ordinary code calling the
// methods in it does not compile. That would make a build with the tag useless
// as a check, which is the only reason the tag exists: the spec is written to
// be type-checked, and a check that cannot see the call sites checks the
// smaller half of the package.
//
// So the same declarations are written a second time under the tag itself, with
// their bodies replaced. Both configurations then hold the whole API — one
// implemented and one declared — and a call site is compiled either way.
//
// The types the declarations are of are the one thing left out. Under the tag
// the author's own files declare them — a spec file for a spec declaration, an
// ordinary file for an inline one — which is the arrangement the two
// constraints exist to produce, and writing them here as well would put those
// names in scope twice.
//
// Everything else goes in, including what the package's declarations share.
// That is what the whole package moving under one constraint costs: a helper
// type that used to sit in a file every build read is now in the file the tag
// excludes, and the stub file is where the tagged build has to find it.
//
// One section in, one section out. A section carries the positions its
// declarations are printed by, and two sections need not share them, so
// gathering them into one would print a declaration by a file it did not come
// from.
func stubs(declared []string, unit merge.Unit) []emit.Section {
	var out []emit.Section

	for _, section := range unit.Sections {
		var stubbedSection emit.Section

		for _, decl := range section.Decls {
			stub, ok := stubbed(decl, declared)
			if !ok {
				continue
			}
			stubbedSection.Decls = append(stubbedSection.Decls, stub)
		}

		if len(stubbedSection.Decls) == 0 {
			continue
		}

		stubbedSection.Fset = section.Fset
		out = append(out, stubbedSection)
	}

	return out
}

// stubbed returns the form a declaration takes in the stub file, and whether it
// belongs there at all.
//
// Copied rather than edited. The declaration is the one the real file is
// written from and the two files are written from one tree, so changing it here
// would change what the ordinary build gets — a body replaced in both places is
// a package whose methods all panic.
//
// The copy is shallow, which is what makes this cheap and is safe for the same
// reason: everything shared is read from here and written by nobody. Only the
// body is new, and it keeps the braces where the original's were so that the
// printer has somewhere to put it.
//
// Documentation is dropped, and only here. Prose belongs with the code it
// describes; repeating it over a panic would double every comment in the
// package for the benefit of a build nobody ships, and leave two copies to
// disagree.
func stubbed(decl ast.Decl, declared []string) (ast.Decl, bool) {
	switch typed := decl.(type) {
	case *ast.FuncDecl:
		if typed == nil || typed.Body == nil {
			return nil, false
		}

		stub := *typed
		stub.Doc = nil
		stub.Body = panicking(typed.Body)

		return &stub, true

	case *ast.GenDecl:
		if typed == nil || typed.Tok == token.IMPORT {
			return nil, false
		}

		specs := kept(typed.Specs, declared)
		if len(specs) == 0 {
			return nil, false
		}

		stub := *typed
		stub.Doc = nil
		stub.Specs = specs

		return &stub, true

	default:
		return nil, false
	}
}

// kept returns the specifications of a declaration that the stub file holds.
//
// Everything but the package's own declared types, which the author's files
// supply under the tag. A grouped declaration is filtered rather than dropped,
// since a type may be written beside the helpers that go with it.
//
// Left alone otherwise, including the values of variables and constants. A
// value that calls a stub is a value that panics, which matters to nobody: the
// build this file belongs to is compiled and never run, and a constant the
// author's code reads has to be here or the check fails at the call site it
// exists to check.
func kept(specs []ast.Spec, declared []string) []ast.Spec {
	out := make([]ast.Spec, 0, len(specs))

	for _, spec := range specs {
		typed, ok := spec.(*ast.TypeSpec)
		if ok && typed != nil && typed.Name != nil && slices.Contains(declared, typed.Name.Name) {
			continue
		}
		out = append(out, spec)
	}

	if len(out) == len(specs) {
		return specs
	}
	return out
}

// panicking returns a body holding nothing but a panic, where the original's
// was.
//
// A panic is a terminating statement, so it satisfies a signature with results
// as well as one without, and it declares nothing that could be reported as
// unused.
//
// The whole body sits at the original's opening brace, closing brace included.
// The printer places what it is given by position, so a statement from nowhere
// lands wherever the last one left off — and a closing brace left where a body
// of ten lines ended leaves nine blank ones behind the panic.
func panicking(original *ast.BlockStmt) *ast.BlockStmt {
	at := original.Lbrace

	return &ast.BlockStmt{
		Lbrace: at,
		Rbrace: at,
		List: []ast.Stmt{
			&ast.ExprStmt{X: &ast.CallExpr{
				Lparen: at,
				Rparen: at,
				Fun:    &ast.Ident{NamePos: at, Name: "panic"},
				Args: []ast.Expr{&ast.BasicLit{
					ValuePos: at,
					Kind:     token.STRING,
					Value:    `"forge stub"`,
				}},
			}},
		},
	}
}

// reaching returns the imports the given declarations still refer to.
//
// The real file's imports are the wrong set. A body is where most of them are
// used — a container's methods name the package they delegate to and its
// signature does not — so carrying them over would leave a file importing
// packages it does not mention, which does not compile.
//
// What is looked for is the name an import is bound to, used as the left half
// of a qualified identifier. That is what an import is for in generated code,
// and it is the only use that survives a body being thrown away.
//
// The other direction — collecting the qualifiers and insisting every one is
// bound — would be the stronger check, and is not available: whether the left
// half of a selector names a package or a value is a question about scope, and
// nothing here has resolved one. So an import nothing mentions is dropped,
// which the compiler would have refused, and a qualifier nothing imports is
// left to the compiler, which is where the answer is.
func reaching(sections []emit.Section, imports []emit.Import) []emit.Import {
	var decls []ast.Decl
	for _, section := range sections {
		decls = append(decls, section.Decls...)
	}

	return emit.Reaching(decls, imports)
}
