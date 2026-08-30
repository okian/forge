package load

import (
	"go/ast"
	"go/parser"
	"go/token"
)

// parseMode keeps what forge reads and drops what it does not.
//
// Comments are kept because //forge: directives and //go:build constraints are
// comments. Object resolution is skipped because nothing here reads
// ast.Object: identifiers are resolved through go/types, which is both correct
// in the presence of dot imports and cheaper.
const parseMode = parser.ParseComments | parser.SkipObjectResolution

// parseFile is the hook go/packages calls for each file, and the only place
// forge deviates from an ordinary load.
func parseFile(fset *token.FileSet, filename string, src []byte) (*ast.File, error) {
	file, err := parser.ParseFile(fset, filename, src, parseMode)
	if file != nil {
		stripBodies(file)
		stripAssertions(file)
	}

	// A partially parsed file is still worth type-checking, so it is returned
	// alongside the error rather than instead of it — which is also the protocol
	// go/packages expects from this hook.
	return file, err
}

// stripBodies discards the body of every function declaration in the file,
// leaving its receiver, name, type parameters, signature and position intact.
//
// Nothing else is touched. In particular a function literal inside a variable
// initialiser keeps its body, because it is part of an expression the
// type-checker still has to evaluate.
func stripBodies(file *ast.File) {
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}

		if needsBody(fn) {
			fn.Body = panicBody(fn.Body)
			continue
		}
		fn.Body = nil
	}
}

// needsBody reports whether the type-checker insists this declaration have one.
//
// A body-less function declaration is ordinary Go — it is how a function
// implemented in assembly is written — with exactly two exceptions, and both
// are common enough that getting them wrong would fail the load on a large
// share of real packages. An init function must have a body, and so must a
// generic function; a method on a generic type is fine, because its type
// parameters belong to the receiver.
func needsBody(fn *ast.FuncDecl) bool {
	if fn.Type.TypeParams.NumFields() > 0 {
		return true
	}
	return fn.Recv == nil && fn.Name.Name == "init"
}

// panicBody returns a body holding nothing but a panic, keeping the braces
// where they were.
//
// A panic is a terminating statement, so this satisfies a function with
// results as well as one without, and it declares nothing that could then be
// reported as unused.
func panicBody(original *ast.BlockStmt) *ast.BlockStmt {
	return &ast.BlockStmt{
		Lbrace: original.Lbrace,
		Rbrace: original.Rbrace,
		List: []ast.Stmt{
			&ast.ExprStmt{X: &ast.CallExpr{
				Fun:  ast.NewIdent("panic"),
				Args: []ast.Expr{&ast.BasicLit{Kind: token.STRING, Value: `""`}},
			}},
		},
	}
}

// stripAssertions removes the value from a package-level variable whose names
// are all blank, leaving the declaration and its type in place.
//
// These are compile-time assertions, and the one forge's own output is checked
// by is exactly the one that cannot hold before generation has run:
//
//	var _ io.WriterTo = (*Persons)(nil)
//
// WriteTo is generated, so until it exists the assertion fails and takes the
// whole package's type information with it. Dropping the value leaves
// `var _ io.WriterTo`, which is legal, declares nothing usable, and asserts
// nothing. Whether the assertion actually holds is checked by the compiler on
// every build, which is where the author will see it.
func stripAssertions(file *ast.File) {
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.VAR {
			continue
		}

		for _, spec := range gen.Specs {
			value, ok := spec.(*ast.ValueSpec)
			if !ok || value.Type == nil || len(value.Values) == 0 {
				continue
			}
			if allBlank(value.Names) {
				value.Values = nil
			}
		}
	}
}

// allBlank reports whether every name is the blank identifier.
func allBlank(names []*ast.Ident) bool {
	for _, name := range names {
		if name.Name != "_" {
			return false
		}
	}
	return len(names) > 0
}
