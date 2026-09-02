package goldentest_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/okian/forge/internal/generate"
)

// A type cannot have a field and a method of one name.
//
// This is not style; it does not compile. It is asserted anyway, over every
// generated file in the tree at once, because the failure it guards against is
// the one no layer can see on its own: a layer that puts a method on a struct
// it also gives fields to is agreeing with itself, and the second layer over
// the same subject is the one that breaks the package. Every golden is
// type-checked, so a clash inside one file is already caught; what this adds is
// the clash spread across two of them, and a sentence saying which rule was
// broken rather than a type-checker's line about a redeclaration.
//
// Read from the source of every file forge writes in the repository, generated
// output and recorded golden alike — including the names an older forge used,
// since a golden recorded under one is still a file somebody's build would have
// to compile.
func TestNoGeneratedTypeHasAFieldAndAMethodOfOneName(t *testing.T) {
	fields, methods := declared(t)

	for typ, held := range methods {
		for _, name := range held {
			if slices.Contains(fields[typ], name) {
				t.Errorf("%s has both a field and a method called %s, which does not compile", typ, name)
			}
		}
	}

	if len(methods) == 0 {
		t.Fatal("no generated method was found at all, so this test is asserting nothing")
	}
}

// declared reads every generated file in the tree and returns the fields and
// the methods each type carries, keyed by the package directory and the type's
// name together — two packages may each declare a Persons, and they are two
// types.
func declared(t *testing.T) (map[string][]string, map[string][]string) {
	t.Helper()

	fields, methods := map[string][]string{}, map[string][]string{}

	for _, at := range emitted(t) {
		file, err := parser.ParseFile(token.NewFileSet(), at, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("reading %s: %v", at, err)
		}

		in := filepath.Dir(at)
		for _, decl := range file.Decls {
			read(in, decl, fields, methods)
		}
	}
	return fields, methods
}

// read records what one declaration puts on a type.
func read(in string, decl ast.Decl, fields, methods map[string][]string) {
	switch held := decl.(type) {
	case *ast.FuncDecl:
		if held.Recv == nil || len(held.Recv.List) == 0 {
			return
		}
		if of := receiver(held.Recv.List[0].Type); of != "" {
			methods[in+"."+of] = append(methods[in+"."+of], held.Name.Name)
		}

	case *ast.GenDecl:
		for _, spec := range held.Specs {
			typ, is := spec.(*ast.TypeSpec)
			if !is {
				continue
			}
			structure, is := typ.Type.(*ast.StructType)
			if !is || structure.Fields == nil {
				continue
			}
			for _, field := range structure.Fields.List {
				for _, name := range field.Names {
					fields[in+"."+typ.Name.Name] = append(fields[in+"."+typ.Name.Name], name.Name)
				}
			}
		}
	}
}

// receiver names the type a method is declared on, through a pointer and
// through type parameters.
func receiver(held ast.Expr) string {
	switch of := held.(type) {
	case *ast.StarExpr:
		return receiver(of.X)
	case *ast.IndexExpr:
		return receiver(of.X)
	case *ast.IndexListExpr:
		return receiver(of.X)
	case *ast.Ident:
		return of.Name
	default:
		return ""
	}
}

// emitted returns every file forge wrote that is committed to the tree, which
// is the output under examples and every golden recorded beside a test.
func emitted(t *testing.T) []string {
	t.Helper()

	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatalf("finding the module root: %v", err)
	}

	var out []string
	err = filepath.WalkDir(root, func(at string, entry fs.DirEntry, err error) error {
		switch {
		case err != nil:
			return err
		case entry.IsDir() && (entry.Name() == ".git" || entry.Name() == ".claude"):
			return fs.SkipDir
		case entry.IsDir():
			return nil
		case generate.Ours(entry.Name()):
			out = append(out, at)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking the tree: %v", err)
	}

	slices.Sort(out)
	return out
}

// A generated local must not shadow a package the file imports.
//
// It does not fail to compile. It fails on the next line that meant the
// package, in a file the author cannot edit, about a collision they caused by
// naming a field slices — which is why the rule is asserted here rather than
// left to the compiler like the one above it.
//
// Every name a body binds is asked, not only the ones a layer called a local:
// a parameter and a receiver shadow exactly as hard as a variable does, and a
// range key hardest of all, since it is the one a walk over a map of packages
// would reach for.
func TestNoGeneratedLocalShadowsAnImport(t *testing.T) {
	for _, at := range emitted(t) {
		file, err := parser.ParseFile(token.NewFileSet(), at, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("reading %s: %v", at, err)
		}

		imports := bound(file)
		if len(imports) == 0 {
			continue
		}

		for _, name := range binds(file) {
			if slices.Contains(imports, name) {
				t.Errorf("%s binds %s, which is a package the file imports", at, name)
			}
		}
	}
}

// bound returns the name each of a file's imports is known by.
func bound(file *ast.File) []string {
	var out []string

	for _, one := range file.Imports {
		switch {
		case one.Name != nil:
			out = append(out, one.Name.Name)
		default:
			path := strings.Trim(one.Path.Value, `"`)
			out = append(out, path[strings.LastIndexByte(path, '/')+1:])
		}
	}
	return out
}

// binds returns every name a function in the file binds inside itself: the
// receiver, the parameters, the results, and everything a body declares.
func binds(file *ast.File) []string {
	var out []string

	for _, decl := range file.Decls {
		held, is := decl.(*ast.FuncDecl)
		if !is {
			continue
		}

		out = append(out, named(held.Recv)...)
		out = append(out, named(held.Type.Params)...)
		out = append(out, named(held.Type.Results)...)

		ast.Inspect(held, func(node ast.Node) bool {
			out = append(out, declaring(node)...)
			return true
		})
	}
	return out
}

// declaring returns the names one statement binds.
func declaring(node ast.Node) []string {
	switch held := node.(type) {
	case *ast.AssignStmt:
		if held.Tok != token.DEFINE {
			return nil
		}
		return identifiers(held.Lhs)

	case *ast.RangeStmt:
		if held.Tok != token.DEFINE {
			return nil
		}
		return identifiers([]ast.Expr{held.Key, held.Value})

	case *ast.ValueSpec:
		var out []string
		for _, name := range held.Names {
			out = append(out, name.Name)
		}
		return out

	case *ast.FuncLit:
		return append(named(held.Type.Params), named(held.Type.Results)...)

	default:
		return nil
	}
}

// named returns the names a field list gives, skipping the ones it leaves out.
func named(list *ast.FieldList) []string {
	if list == nil {
		return nil
	}

	var out []string
	for _, field := range list.List {
		for _, name := range field.Names {
			if name.Name != "_" {
				out = append(out, name.Name)
			}
		}
	}
	return out
}

// identifiers returns the plain names among these expressions, dropping the
// blank and anything that is not a bare identifier.
func identifiers(held []ast.Expr) []string {
	var out []string

	for _, one := range held {
		if name, is := one.(*ast.Ident); is && name.Name != "_" {
			out = append(out, name.Name)
		}
	}
	return out
}
