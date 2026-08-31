package templates_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/okian/forge/internal/emit"
	"github.com/okian/forge/internal/templates"
)

// Not everything a layer needs is specialised. A view generic over its element
// stays generic in the output, so it is read rather than rewritten — and what
// it keeps and leaves behind has to be what a rewrite keeps and leaves behind,
// or a package would gain forge's own package comment at the top of a file.
func TestATemplateEmittedAsItWasWritten(t *testing.T) {
	out, diags := templates.Verbatim(templates.Template{
		Name: "view",
		Source: []byte("// Package tmpl is the template.\npackage tmpl\n\n" +
			"import \"iter\"\n\n" +
			"// View is a view.\ntype View[U any] iter.Seq[U]\n\n" +
			"// Len counts.\nfunc (v View[U]) Len() int { return 0 }\n"),
	}, where)
	if !diags.Empty() {
		t.Fatalf("the template was refused:\n%s", diags.Render())
	}

	if len(out.Decls) != 2 {
		t.Errorf("it kept %d declarations, want the type and its method", len(out.Decls))
	}
	if want := []emit.Import{{Path: "iter"}}; !slices.Equal(out.Imports, want) {
		t.Errorf("it needs %v imported, want %v", out.Imports, want)
	}

	text, err := emit.Section{Decls: out.Decls, Comments: out.Comments, Fset: out.Fset}.Render()
	if err != nil {
		t.Fatalf("rendering: %v", err)
	}
	if strings.Contains(text, "Package tmpl") {
		t.Errorf("the template's own package comment was emitted:\n%s", text)
	}
	if !strings.Contains(text, "// View is a view.") {
		t.Errorf("what documents the declarations was left behind:\n%s", text)
	}
}

// A template that is not Go, and one that is Go and holds nothing to emit: both
// are faults in forge reported against the declaration that asked, since an
// author can do nothing about either except say so.
func TestATemplateThereIsNothingToEmitFrom(t *testing.T) {
	cases := map[string]string{
		"not Go at all":                "package tmpl\n\nfunc (\n",
		"nothing but a package clause": "// Package tmpl is the template.\npackage tmpl\n",
		"nothing but imports":          "package tmpl\n\nimport \"iter\"\n",
	}

	for name, source := range cases {
		t.Run(name, func(t *testing.T) {
			_, diags := templates.Verbatim(templates.Template{Name: "view", Source: []byte(source)}, where)
			if diags.Empty() {
				t.Fatal("a template with nothing in it was read without complaint")
			}
			if reported := diags.Render(); !strings.Contains(reported, "FRG49") {
				t.Errorf("the failure is not one of the template codes:\n%s", reported)
			}
		})
	}
}
