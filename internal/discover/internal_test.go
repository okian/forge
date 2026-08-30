package discover

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

// parseDecls parses a source string and returns its file and file set.
func parseDecls(t *testing.T, src string) (*token.FileSet, *ast.File) {
	t.Helper()

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "model.go", src, parser.ParseComments|parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return fset, file
}

// firstTypeDecl returns the first type declaration and its first spec.
func firstTypeDecl(t *testing.T, file *ast.File) (*ast.GenDecl, *ast.TypeSpec) {
	t.Helper()

	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.TYPE {
			continue
		}
		return gen, gen.Specs[0].(*ast.TypeSpec)
	}
	t.Fatal("no type declaration in the source")
	return nil, nil
}

// A directive is written the way Go directives are written, and the shapes an
// author can produce are worth pinning down before options are parsed out of
// them.
func TestDirectives(t *testing.T) {
	cases := map[string]struct {
		comment string
		layer   string
		args    string
	}{
		"layer and arguments":  {"//forge:collection sort=Age index=Name", "collection", "sort=Age index=Name"},
		"tab separated":        {"//forge:ring\tcap=16", "ring", "cap=16"},
		"no arguments":         {"//forge:clone", "clone", ""},
		"trailing space":       {"//forge:clone   ", "clone", ""},
		"extra inner spacing":  {"//forge:ring   cap=16  ", "ring", "cap=16"},
		"no layer at all":      {"//forge:", "", ""},
		"no layer, but a word": {"//forge: cap=16", "", "cap=16"},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			fset, file := parseDecls(t, tc.comment+"\npackage model\n\ntype Persons []int\n")

			found := directives(fset, file.Doc)
			if len(found) != 1 {
				t.Fatalf("collected %d directives, want 1: %v", len(found), found)
			}

			got := found[0]
			if got.Layer != tc.layer {
				t.Errorf("Layer = %q, want %q", got.Layer, tc.layer)
			}
			if got.Args != tc.args {
				t.Errorf("Args = %q, want %q", got.Args, tc.args)
			}
			if got.Text != tc.comment {
				t.Errorf("Text = %q, want %q", got.Text, tc.comment)
			}
			if got.String() != tc.comment {
				t.Errorf("String() = %q, want %q", got.String(), tc.comment)
			}
			if got.ArgsOffset > len(got.Text) {
				t.Errorf("ArgsOffset %d runs past the text %q", got.ArgsOffset, got.Text)
			}
			if tc.args != "" && got.Text[got.ArgsOffset:] != got.Args+trailingSpace(got.Text) {
				t.Errorf("ArgsOffset lands on %q, want it to start %q", got.Text[got.ArgsOffset:], got.Args)
			}
		})
	}
}

// trailingSpace returns the run of spaces and tabs at the end of s, which
// ArgsOffset does not trim because it is an offset into the written text.
func trailingSpace(s string) string {
	i := len(s)
	for i > 0 && (s[i-1] == ' ' || s[i-1] == '\t') {
		i--
	}
	return s[i:]
}

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
	fset, file := parseDecls(t, src)

	found := directives(fset, file.Doc)
	if len(found) != 1 {
		t.Fatalf("collected %d directives, want 1: %v", len(found), found)
	}
	if found[0].Layer != "collection" {
		t.Errorf("Layer = %q, want %q", found[0].Layer, "collection")
	}

	if got := directives(fset, nil); got != nil {
		t.Errorf("directives(nil) = %v, want nil", got)
	}
}

// Where the parser puts a comment depends on how the declaration was written,
// and a group's own comment belongs to no single declaration in it.
func TestDocOf(t *testing.T) {
	cases := map[string]struct {
		src  string
		want string
	}{
		"comment above a plain declaration": {
			"package model\n\n//forge:collection sort=Age\ntype Persons []int\n",
			"collection",
		},
		"comment above a spec in a group": {
			"package model\n\ntype (\n\t//forge:ring cap=16\n\tRecent []int\n)\n",
			"ring",
		},
		"comment above a group holding one spec": {
			// go/doc gives a single-spec group's comment to that spec, and so
			// does this: there is no ambiguity about which one it means.
			"package model\n\n//forge:ring cap=16\ntype (\n\tRecent []int\n)\n",
			"ring",
		},
		"comment above a group holding several": {
			// Here it sits above both and could not say which, so it is left for
			// the stray-directive check to report.
			"package model\n\n//forge:ring cap=16\ntype (\n\tRecent []int\n\tOlder []int\n)\n",
			"",
		},
		"trailing comment on the same line": {
			"package model\n\ntype Persons []int //forge:collection sort=Age\n",
			"",
		},
		"no comment at all": {
			"package model\n\ntype Persons []int\n",
			"",
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			fset, file := parseDecls(t, tc.src)
			gen, spec := firstTypeDecl(t, file)

			found := directives(fset, docOf(gen, spec))
			switch {
			case tc.want == "":
				if len(found) != 0 {
					t.Errorf("collected %v, want nothing", found)
				}
			case len(found) != 1 || found[0].Layer != tc.want:
				t.Errorf("collected %v, want one %s directive", found, tc.want)
			}
		})
	}
}

func TestCandidateFilter(t *testing.T) {
	cases := map[string]struct {
		decl string
		want bool
	}{
		"instantiation":           {"type Persons Collection[Person]", true},
		"nested instantiation":    {"type Persons Collection[Ring[Person]]", true},
		"qualified instantiation": {"type Persons markers.Collection[Person]", true},
		"two type arguments":      {"type Pairs Map[string, int]", true},
		"alias to instantiation":  {"type Persons = Collection[Person]", false},
		"not an instantiation":    {"type Persons []Person", false},
		"plain defined type":      {"type Celsius float64", false},
		"struct":                  {"type Person struct{ Name string }", false},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, file := parseDecls(t, "package model\n\n"+tc.decl+"\n")
			_, spec := firstTypeDecl(t, file)

			if got := candidate(spec); got != tc.want {
				t.Errorf("candidate() = %v, want %v", got, tc.want)
			}
		})
	}
}

// A candidate renders as it was written, which is what an explain or a
// diagnostic shows before anything has been resolved.
func TestCandidateString(t *testing.T) {
	cases := map[string]string{
		"type Persons Collection[Person]":                        "Persons Collection[Person]",
		"type Persons markers.Collection[markers.Ring[Person]]":  "Persons markers.Collection[markers.Ring[Person]]",
		"type Pairs Map[string, int]":                            "Pairs Map[string, int]",
		"type Pointers Collection[*Person]":                      "Pointers Collection[*Person]",
		"type Maps Collection[map[string]Person]":                "Maps Collection[map[string]Person]",
		"type Funcs Collection[func(int) error]":                 "Funcs Collection[func(int) error]",
		"type Arrays Collection[[]Person]":                       "Arrays Collection[[]Person]",
		"type Deep Collection[Ring[Json[Person]]]":               "Deep Collection[Ring[Json[Person]]]",
		"type Mixed Pair[markers.Ring[Person], *markers.Person]": "Mixed Pair[markers.Ring[Person], *markers.Person]",
	}

	for decl, want := range cases {
		t.Run(decl, func(t *testing.T) {
			_, file := parseDecls(t, "package model\n\n"+decl+"\n")
			_, spec := firstTypeDecl(t, file)

			got := Candidate{Name: spec.Name.Name, Spec: spec}.String()
			if got != want {
				t.Errorf("String() = %q, want %q", got, want)
			}
		})
	}

	// A candidate with nothing resolved still names itself.
	if got, want := (Candidate{Name: "Persons"}).String(), "Persons"; got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
}

// A comment that was meant as a directive is worth reporting; ordinary prose
// that happens to start with the word is not.
func TestIsLayerName(t *testing.T) {
	cases := map[string]bool{
		"collection": true,
		"lru":        true,
		"json2":      true,
		"":           false,
		"Collection": false,
		"two words":  false,
		"has-dash":   false,
	}

	for input, want := range cases {
		if got := isLayerName(input); got != want {
			t.Errorf("isLayerName(%q) = %v, want %v", input, got, want)
		}
	}
}

// The two ways to write a directive wrongly, and the prose that must not be
// mistaken for either.
func TestResemblesDirective(t *testing.T) {
	cases := map[string]bool{
		"// forge:collection sort=Age":  true,
		"//  forge:ring cap=8":          true,
		"//\tforge:clone":               true,
		"/*forge:collection sort=Age*/": true,
		"/* forge:ring cap=8 */":        true,

		"//forge:collection sort=Age":             true, // a real one; checked before this
		"// forge: a place where metal is worked": false,
		"// forge:Collection":                     false,
		"// forged:collection":                    false,
		"// mentions //forge: in prose":           false,
		"//go:generate stringer":                  false,
		"// Persons is a collection.":             false,
		"not a comment at all":                    false,
		"":                                        false,
	}

	for text, want := range cases {
		if got := resemblesDirective(text); got != want {
			t.Errorf("resemblesDirective(%q) = %v, want %v", text, got, want)
		}
	}
}
