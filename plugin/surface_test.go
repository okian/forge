package plugin_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"slices"
	"strings"
	"testing"
)

// surface is every name this package publishes.
//
// Written down because the package's promise is that a name here keeps its
// meaning: a rename, a deletion, or a signature change is what the promise is
// against, and nothing else in the tree would notice one. A layer outside this
// module is the thing that notices, and it notices by failing to build long
// after the change was made.
//
// Adding to it is fine and is what the promise allows, so a new name here fails
// this test once and is added. Losing one is the thing to stop.
var surface = []string{
	// Implementing a layer.
	"Layer", "Kind", "Stage", "Registry", "NewRegistry",
	"Described", "Transparent", "Enclosing", "Constructing",
	"KindInvalid", "KindStorage", "KindRefining", "KindElement",
	"KindDecorator", "KindTransport",
	"StageReady", "StageStub", "StageStaged",

	// What it is asked about, and what it answers with.
	"Context", "Unit", "Assertion", "Constructor", "Model", "Shape",
	"Cap", "CapSet", "Method", "Caps", "Every",
	"Sized", "Ordered", "Indexed", "Keyed", "Streamable",
	"Structured", "Encodable", "Comparable", "Bounded", "Concurrent",

	// The subject.
	"Struct", "Field", "Classified", "Class",
	"ClassInvalid", "ClassNamed", "ClassBasic", "ClassStruct", "ClassPointer",
	"ClassSlice", "ClassArray", "ClassMap", "ClassInterface", "ClassChan",
	"ClassFunc",
	"TypeRef", "RefOf", "TypeString", "TypeIdentity",
	"Import", "Spelling", "Spell",
	"Camel", "Lower", "Upper", "Join", "Export", "Words", "Around",
	"Block", "Locals", "Exported",
	"Plural", "Singular", "IsPlural",
	"Through", "Unattachable", "Uncopyable", "Unnameable",
	"MarkerPkg", "LayerRef", "Form", "FormInvalid", "FormInline", "FormSpec",
	"Directive", "DirectivePrefix", "Written",

	// Options.
	"OptionDef", "Options", "ValueKind", "Scope",
	"ValueNone", "ValueBool", "ValueInt", "ValueString",
	"ValueEnum", "ValueField", "ValueFields",
	"ScopeDeclaration", "ScopeField",

	// Diagnostics and tags.
	"Code", "Register", "Diagnostic", "New", "From", "Diagnostics",
	"Tag", "TagOption", "DirectiveOption", "Problem", "ParseTag",

	// Emission.
	"CommentWidth", "Wrapped", "Qualifiers", "Reaching",
}

// The published surface is what is written down, and nothing has been lost from
// it.
//
// Both directions, and they are not the same claim. A name in the package and
// not in the list is a name somebody added and did not think of as public — it
// is public, and saying so here is how it gets read as part of the promise. A
// name in the list and not in the package is the promise broken.
func TestThePublishedSurface(t *testing.T) {
	found := exported(t)

	for _, want := range surface {
		if !slices.Contains(found, want) {
			t.Errorf("%s is written down as published and is not there any more", want)
		}
	}

	for _, held := range found {
		if !slices.Contains(surface, held) {
			t.Errorf("%s is published and is not written down — add it to the list", held)
		}
	}
}

// exported returns every name this package declares at the top level and
// exports.
//
// Read from the source rather than through reflection, because a type and a
// constant are not values: what is being asked is what a reader of the package
// can name, and the declarations are what say.
func exported(t *testing.T) []string {
	t.Helper()

	held, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("reading the package: %v", err)
	}

	var out []string

	for _, one := range held {
		name := one.Name()
		if one.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}

		file, err := parser.ParseFile(token.NewFileSet(), name, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("reading %s: %v", name, err)
		}

		for _, decl := range file.Decls {
			out = append(out, declared(decl)...)
		}
	}

	slices.Sort(out)
	return out
}

// declared returns the exported names one declaration introduces.
func declared(decl ast.Decl) []string {
	var out []string

	switch held := decl.(type) {
	case *ast.FuncDecl:
		// A method belongs to its type rather than to the package, and this
		// package declares none: everything here is a function or a name.
		if held.Recv == nil && held.Name.IsExported() {
			out = append(out, held.Name.Name)
		}

	case *ast.GenDecl:
		for _, spec := range held.Specs {
			switch one := spec.(type) {
			case *ast.TypeSpec:
				if one.Name.IsExported() {
					out = append(out, one.Name.Name)
				}
			case *ast.ValueSpec:
				for _, ident := range one.Names {
					if ident.IsExported() {
						out = append(out, ident.Name)
					}
				}
			}
		}
	}

	return out
}
