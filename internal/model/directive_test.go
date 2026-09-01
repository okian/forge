package model_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"

	"github.com/okian/forge/internal/model"
)

// Comments that are not directives are passed over, whoever they are for.
func TestDirectivesIgnoresEverythingElse(t *testing.T) {
	src := `// Persons is a collection of people.
//
// It has a longer explanation, which mentions //forge: in prose.
//
//go:generate stringer -type=Persons
//forge:collection sort=Age
//nolint:revive // not ours either
package model

type Persons []int
`
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "model.go", src, parser.ParseComments)
	if err != nil {
		t.Fatalf("parsing: %v", err)
	}

	found := model.Directives(fset, file.Doc)
	if len(found) != 1 {
		t.Fatalf("collected %d directives, want 1: %v", len(found), found)
	}
	if found[0].Layer != "collection" {
		t.Errorf("Layer = %q, want %q", found[0].Layer, "collection")
	}

	// Nothing to read from is nothing collected, rather than a panic. Both a
	// missing group and a missing file set reach here, since a field with no
	// documentation is the ordinary case and a caller holding directives it
	// parsed itself has no file set to offer.
	if got := model.Directives(fset, nil); got != nil {
		t.Errorf("Directives with no comment group = %v, want nothing", got)
	}
	if got := model.Directives(nil, file.Doc); got != nil {
		t.Errorf("Directives with no file set = %v, want nothing", got)
	}
}

// A directive is split at the first space, and a diagnostic about one of its
// options points at the option rather than at the line.
func TestWhereADirectivePoints(t *testing.T) {
	cases := map[string]struct {
		comment string
		layer   string
		args    string
		// column is where ArgsPos lands, counting from the comment's own.
		column int
	}{
		"an option after one space": {"//forge:json fallback=stdlib", "json", "fallback=stdlib", len("//forge:json ")},
		"an option after several":   {"//forge:json   names=snake", "json", "names=snake", len("//forge:json   ")},
		"a tab between them":        {"//forge:json\tnames=snake", "json", "names=snake", len("//forge:json\t")},

		// Nothing after the layer, and nothing after the prefix. Neither is
		// rejected here — what a layer with no options means is the option
		// stage's question — so both have to come back describable.
		"no options at all": {"//forge:clone", "clone", "", len("//forge:clone")},
		"nothing at all":    {"//forge:", "", "", len("//forge:")},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			src := "package model\n\n" + tc.comment + "\ntype Person struct{}\n"

			fset := token.NewFileSet()
			file, err := parser.ParseFile(fset, "model.go", src, parser.ParseComments)
			if err != nil {
				t.Fatalf("parsing: %v", err)
			}

			found := model.Directives(fset, file.Decls[0].(*ast.GenDecl).Doc)
			if len(found) != 1 {
				t.Fatalf("collected %d directives, want 1: %v", len(found), found)
			}

			one := found[0]
			if one.Layer != tc.layer {
				t.Errorf("Layer = %q, want %q", one.Layer, tc.layer)
			}
			if one.Args != tc.args {
				t.Errorf("Args = %q, want %q", one.Args, tc.args)
			}
			if one.Text != tc.comment {
				t.Errorf("Text = %q, want the comment as written", one.Text)
			}
			if one.String() != tc.comment {
				t.Errorf("String = %q, want the comment as written", one.String())
			}

			if got, want := one.ArgsPos().Column, one.Pos.Column+tc.column; got != want {
				t.Errorf("ArgsPos column = %d, want %d — the position of %q", got, want, tc.args)
			}
		})
	}
}

// The directives naming one layer are the ones that layer reads, in the order
// they were written.
func TestTheDirectivesNamingOneLayer(t *testing.T) {
	held := []model.Directive{
		{Layer: "json", Args: "fallback=stdlib"},
		{Layer: "validate", Args: "rule=nonzero"},
		{Layer: "json", Args: "names=snake"},
	}

	found := model.Written(held, "json")
	if len(found) != 2 {
		t.Fatalf("found %d json directives, want 2: %v", len(found), found)
	}
	if found[0].Args != "fallback=stdlib" || found[1].Args != "names=snake" {
		t.Errorf("found %v, want them in the order written", found)
	}

	if got := model.Written(held, "clone"); got != nil {
		t.Errorf("a layer nothing was written for found %v, want nothing", got)
	}
}
