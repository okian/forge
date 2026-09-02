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
// Read from the source of every zz_forge file in the repository, generated
// output and recorded golden alike, since both are files somebody's build would
// have to compile.
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
		case strings.HasPrefix(entry.Name(), "zz_forge_") && strings.HasSuffix(entry.Name(), ".go"):
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
