package emit_test

import (
	"go/ast"
	"go/build"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/okian/forge/internal/emit"
)

// section parses a fragment of Go into the shape a layer hands over: the
// declarations, the comments that belong to them, and the file set that
// explains both.
func section(t *testing.T, source string) emit.Section {
	t.Helper()

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "layer.go", "package tmpl\n\n"+source,
		parser.ParseComments|parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	return emit.Section{Decls: file.Decls, Comments: file.Comments, Fset: fset}
}

// A generated file says so on its first line, in the words the go command
// recognises, and carries what it was made from.
func TestRenderWritesTheHeaderFirst(t *testing.T) {
	out, err := emit.File{
		Package: "model",
		Build:   "!forgespec",
		Header: emit.Header{
			Forge:   "v0.1.0",
			Markers: "v0.1.0",
			Inputs:  "0123456789abcdef",
		},
		Sections: []emit.Section{section(t, "func (p *Persons) Len() int { return len(p.items) }")},
	}.Render()
	if err != nil {
		t.Fatalf("render: %v", err)
	}

	rendered := string(out)
	lines := strings.Split(rendered, "\n")

	if lines[0] != emit.Generated {
		t.Errorf("the first line is %q, want %q", lines[0], emit.Generated)
	}
	for _, want := range []string{"// forge v0.1.0", "// markers v0.1.0", "// inputs 0123456789abcdef"} {
		if !strings.Contains(rendered, want) {
			t.Errorf("the header does not record %q:\n%s", want, rendered)
		}
	}
	if !ast.IsGenerated(parsed(t, out)) {
		t.Errorf("the go command does not read this as generated:\n%s", rendered)
	}
}

// parsed reparses rendered output, which is the only way to ask the questions
// the go command asks of it.
func parsed(t *testing.T, out []byte) *ast.File {
	t.Helper()

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "zz_forge_persons.go", out, parser.ParseComments|parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("rendered output does not parse: %v\n%s", err, out)
	}
	return file
}

// A build constraint decides whether a file is in the build at all, and where
// it is written decides whether it is a constraint or the package's doc
// comment. Asking the go command is the only answer that counts.
func TestTheBuildConstraintIsHonoured(t *testing.T) {
	out, err := emit.File{
		Package:  "model",
		Build:    "!forgespec",
		Header:   emit.Header{Forge: "v0.1.0"},
		Sections: []emit.Section{section(t, "type Persons []Person")},
	}.Render()
	if err != nil {
		t.Fatalf("render: %v", err)
	}

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "zz_forge_persons.go"), out, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	ordinary := build.Default
	ordinary.Dir = dir
	pkg, err := ordinary.ImportDir(dir, 0)
	if err != nil {
		t.Fatalf("reading the package: %v", err)
	}
	if len(pkg.GoFiles) != 1 {
		t.Errorf("an ordinary build sees %v, want the generated file", pkg.GoFiles)
	}

	tagged := build.Default
	tagged.Dir = dir
	tagged.BuildTags = []string{"forgespec"}
	spec, err := tagged.ImportDir(dir, 0)
	if err == nil && len(spec.GoFiles) != 0 {
		t.Errorf("a forgespec build sees %v, want nothing", spec.GoFiles)
	}

	// And the constraint is not the package's documentation, which is what a
	// reader of the package would otherwise be shown first.
	if doc := parsed(t, out).Doc; doc != nil {
		t.Errorf("the package's doc comment is %q", doc.Text())
	}
}

// A file with nothing to record still says it is generated. The fields are how
// staleness is decided cheaply, and a file without them is only expensive to
// check rather than unreadable.
func TestRenderWithoutAHeaderStillSaysGenerated(t *testing.T) {
	out, err := emit.File{Package: "model"}.Render()
	if err != nil {
		t.Fatalf("render: %v", err)
	}

	rendered := string(out)
	if !strings.HasPrefix(rendered, emit.Generated) {
		t.Errorf("rendered:\n%s", rendered)
	}
	if strings.Contains(rendered, "inputs") {
		t.Errorf("a header nobody filled in was written anyway:\n%s", rendered)
	}
	if strings.Contains(rendered, "//go:build") {
		t.Errorf("a constraint nobody asked for was written:\n%s", rendered)
	}
}

// The same file rendered twice is the same bytes. Everything about a generated
// file being committed rests on this: a write that changes nothing is a diff in
// every review that did not touch it.
func TestRenderIsByteIdentical(t *testing.T) {
	build := func() emit.File {
		return emit.File{
			Package: "model",
			Build:   "!forgespec",
			Header:  emit.Header{Forge: "v0.1.0", Inputs: "cafebabecafebabe"},
			// Written in the order a caller happened to discover them, which is
			// not the order they will be written in.
			Imports: []emit.Import{{Path: "iter"}, {Path: "encoding/json/v2"}, {Path: "iter"}},
			Sections: []emit.Section{
				section(t, "type Persons struct{ items []Person }"),
				section(t, "func (p *Persons) Len() int { return len(p.items) }"),
			},
		}
	}

	first, err := build().Render()
	if err != nil {
		t.Fatalf("render: %v", err)
	}

	for range 8 {
		again, err := build().Render()
		if err != nil {
			t.Fatalf("render: %v", err)
		}
		if string(again) != string(first) {
			t.Fatalf("two renderings differ:\n%s\n---\n%s", first, again)
		}
	}
}

// A layer arrives with a file set of its own, because a layer is written as
// real Go and parsed on its own. Which one it is must not change a byte of the
// output, and the only way to be sure is to render the same declarations
// against two.
func TestRenderDoesNotDependOnWhichFileSet(t *testing.T) {
	const source = `
// Persons is a generated collection.
type Persons struct {
	items []Person // the backing store
}

// Len reports how many there are.
func (p *Persons) Len() int {
	// count them
	return len(p.items)
}
`

	const second = `
// Push appends one.
func (p *Persons) Push(v Person) {
	// grow
	p.items = append(p.items, v)
}
`

	// Two sections, each parsed on its own, as two layers produce them. Their
	// positions mean different things and neither file set explains the other.
	out, err := emit.File{
		Package:  "model",
		Sections: []emit.Section{section(t, source), section(t, second)},
	}.Render()
	if err != nil {
		t.Fatalf("render: %v", err)
	}

	// Every comment survives, including the ones inside a body, which are
	// reachable only through the file they were parsed from.
	rendered := string(out)
	for _, want := range []string{
		"// Persons is a generated collection.",
		"// the backing store",
		"// Len reports how many there are.",
		"// count them",
		"// Push appends one.",
		"// grow",
	} {
		if !strings.Contains(rendered, want) {
			t.Errorf("%q did not survive rendering:\n%s", want, rendered)
		}
	}

	// And every doc comment documents what it was written above, rather than
	// landing wherever its position happened to fall in another layer's file.
	for _, pair := range [][2]string{
		{"// Persons is a generated collection.", "type Persons struct"},
		{"// Len reports how many there are.", "func (p *Persons) Len()"},
		{"// Push appends one.", "func (p *Persons) Push("},
	} {
		if strings.Index(rendered, pair[0]) > strings.Index(rendered, pair[1]) {
			t.Errorf("%q moved below %q:\n%s", pair[0], pair[1], rendered)
		}
	}
}

// Imports are sorted and deduplicated wherever a caller found them, because a
// caller finds them one layer at a time and in whatever order the layers ran.
func TestImportsAreSortedAndDeduplicated(t *testing.T) {
	out, err := emit.File{
		Package: "model",
		Imports: []emit.Import{
			{Path: "iter"},
			{Path: "encoding/json/v2", Name: "json"},
			{Path: "iter"},
			{Path: "encoding/json/jsontext"},
		},
	}.Render()
	if err != nil {
		t.Fatalf("render: %v", err)
	}

	rendered := string(out)
	want := "import (\n\t\"encoding/json/jsontext\"\n\tjson \"encoding/json/v2\"\n\t\"iter\"\n)"
	if !strings.Contains(rendered, want) {
		t.Errorf("import block is not %q:\n%s", want, rendered)
	}
}

// One path bound to two names is two imports of it, which does not compile —
// and a file that will not compile is worse than one that was never written.
func TestOnePathImportedTwoWaysIsRefused(t *testing.T) {
	_, err := emit.File{
		Package: "model",
		Decl:    "Persons",
		Imports: []emit.Import{{Path: "iter"}, {Path: "iter", Name: "it"}},
	}.Render()

	if err == nil {
		t.Fatal("one path bound to two names rendered without complaint")
	}
	if !strings.Contains(err.Error(), "FRG4003") {
		t.Errorf("error %q is not a diagnostic about the clash", err)
	}
}

// One import is written on one line, which is what a reader of generated code
// expects to see and what gofmt would leave alone.
func TestASingleImportIsWrittenInline(t *testing.T) {
	out, err := emit.File{Package: "model", Imports: []emit.Import{{Path: "iter"}}}.Render()
	if err != nil {
		t.Fatalf("render: %v", err)
	}

	if want := "import \"iter\"\n"; !strings.Contains(string(out), want) {
		t.Errorf("import is not written as %q:\n%s", want, out)
	}
}

// Declarations keep the order they arrive in. Sorting them would separate a
// type from the constructor that goes with it, which is the one grouping a
// reader of generated code most wants kept.
func TestDeclarationOrderIsTheCallersOwn(t *testing.T) {
	out, err := emit.File{
		Package: "model",
		Sections: []emit.Section{
			section(t, "type Persons struct{}\n\nfunc NewPersons() *Persons { return &Persons{} }"),
			section(t, "type PersonsSeq struct{}"),
		},
	}.Render()
	if err != nil {
		t.Fatalf("render: %v", err)
	}

	rendered := string(out)
	at := 0
	for _, want := range []string{"type Persons struct", "func NewPersons()", "type PersonsSeq struct"} {
		found := strings.Index(rendered[at:], want)
		if found < 0 {
			t.Fatalf("%q is missing or out of order in:\n%s", want, rendered)
		}
		at += found
	}
}

// A layer with nothing to say for part of its output leaves a gap rather than a
// shorter slice, and a gap reaching the printer is a panic a long way from
// whatever produced it.
func TestNilDeclarationsAreSkipped(t *testing.T) {
	body := section(t, "type Persons struct{}")

	out, err := emit.File{
		Package:  "model",
		Sections: []emit.Section{{Decls: []ast.Decl{nil, body.Decls[0], nil}, Fset: body.Fset}},
	}.Render()
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if !strings.Contains(string(out), "type Persons struct") {
		t.Errorf("rendered:\n%s", out)
	}
}

// A section with nothing in it writes nothing, which is what every layer
// produces until its generator is written.
func TestEmptySectionsWriteNothing(t *testing.T) {
	out, err := emit.File{
		Package:  "model",
		Sections: []emit.Section{{}, {Decls: []ast.Decl{nil}}},
	}.Render()
	if err != nil {
		t.Fatalf("render: %v", err)
	}

	if got, want := strings.TrimSpace(string(out)), emit.Generated+"\n\npackage model"; got != want {
		t.Errorf("rendered %q, want %q", got, want)
	}
}

// Declarations built rather than parsed carry no positions and no file set, and
// that has to be the ordinary case: it is what a layer produces when it builds
// rather than rewrites.
func TestRenderWithoutAFileSet(t *testing.T) {
	out, err := emit.File{
		Package: "model",
		Sections: []emit.Section{{Decls: []ast.Decl{&ast.GenDecl{
			Doc: &ast.CommentGroup{List: []*ast.Comment{{Text: "// Persons holds people."}}},
			Tok: token.TYPE,
			Specs: []ast.Spec{&ast.TypeSpec{
				Name: ast.NewIdent("Persons"),
				Type: &ast.ArrayType{Elt: ast.NewIdent("Person")},
			}},
		}}}},
	}.Render()
	if err != nil {
		t.Fatalf("render: %v", err)
	}

	for _, want := range []string{"// Persons holds people.", "type Persons []Person"} {
		if !strings.Contains(string(out), want) {
			t.Errorf("%q is missing from:\n%s", want, out)
		}
	}
}

// Generated code that will not parse is forge's mistake, and the source is the
// whole of the evidence: the file was never written, so there is nothing on
// disk for the reported line to point at.
func TestUnparsableOutputReportsItsSource(t *testing.T) {
	_, err := emit.File{
		Package: "model",
		Decl:    "Persons",
		Pos:     token.Position{Filename: "model/spec.go", Line: 12, Column: 6},
		Sections: []emit.Section{{Decls: []ast.Decl{&ast.GenDecl{
			Tok: token.TYPE,
			Specs: []ast.Spec{&ast.TypeSpec{
				// A keyword prints as an identifier and then will not parse as
				// one, which is the shape of mistake a rewrite makes.
				Name: ast.NewIdent("func"),
				Type: ast.NewIdent("int"),
			}},
		}}}},
	}.Render()

	if err == nil {
		t.Fatal("unparsable output rendered without complaint")
	}
	for _, want := range []string{"FRG4001", "Persons", "model/spec.go:12:6", "   1 | "} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error does not mention %q:\n%v", want, err)
		}
	}
}

// A tree with a hole in it makes the printer panic rather than return, and a
// panic reaching the author is a stack trace where a diagnostic should be.
func TestAMalformedDeclarationIsReportedRatherThanPanicking(t *testing.T) {
	_, err := emit.File{
		Package: "model",
		Decl:    "Persons",
		Sections: []emit.Section{{Decls: []ast.Decl{&ast.GenDecl{
			Tok: token.TYPE,
			// A type declared as nothing at all.
			Specs: []ast.Spec{&ast.TypeSpec{Name: ast.NewIdent("Persons")}},
		}}}},
	}.Render()

	if err == nil {
		t.Fatal("a malformed declaration rendered without complaint")
	}
	for _, want := range []string{"FRG4002", "not well formed", "Persons"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error does not mention %q:\n%v", want, err)
		}
	}
}

// A constraint that is not a constraint is honoured by nothing and read as a
// comment by everything, which is a file quietly in every build.
func TestAnImpossibleBuildConstraintIsRefused(t *testing.T) {
	_, err := emit.File{Package: "model", Decl: "Persons", Build: "!!! not a constraint"}.Render()

	if err == nil {
		t.Fatal("a constraint that is not one rendered without complaint")
	}
	if !strings.Contains(err.Error(), "FRG4004") {
		t.Errorf("error %q is not a diagnostic about the constraint", err)
	}
}

// A header value carrying a line break writes lines of its own into the file,
// which is how a version string from somewhere else becomes a field nobody
// recorded.
func TestAHeaderValueCannotCarryALineBreak(t *testing.T) {
	_, err := emit.File{
		Package: "model",
		Decl:    "Persons",
		Header:  emit.Header{Forge: "v0.1.0\n// markers forged"},
	}.Render()

	if err == nil {
		t.Fatal("a header value with a line break was written")
	}
	if !strings.Contains(err.Error(), "line break") {
		t.Errorf("error %q does not say what is wrong with it", err)
	}
}

// A comment that belongs to no declaration is left out, whether it came from
// above a template's package clause or from a layer that built one and attached
// it to nothing. A comment beside a declaration it says nothing about is worse
// than no comment.
func TestACommentBelongingToNothingIsLeftOut(t *testing.T) {
	out, err := emit.File{
		Package: "model",
		Decl:    "Persons",
		Sections: []emit.Section{{
			Decls: []ast.Decl{&ast.GenDecl{
				Doc: &ast.CommentGroup{List: []*ast.Comment{{Text: "// Persons holds people."}}},
				Tok: token.TYPE,
				Specs: []ast.Spec{&ast.TypeSpec{
					Name: ast.NewIdent("Persons"),
					Type: &ast.ArrayType{Elt: ast.NewIdent("Person")},
				}},
			}},
			Comments: []*ast.CommentGroup{{List: []*ast.Comment{{Text: "// adrift"}}}},
		}},
	}.Render()
	if err != nil {
		t.Fatalf("render: %v", err)
	}

	rendered := string(out)
	if strings.Contains(rendered, "adrift") {
		t.Errorf("a comment belonging to nothing was written:\n%s", rendered)
	}
	// The declaration's own comment is not adrift and stays.
	if !strings.Contains(rendered, "// Persons holds people.\ntype Persons []Person") {
		t.Errorf("the declaration lost its own comment:\n%s", rendered)
	}
}

// A section reports whether it would write anything, which is what lets the
// stage above it leave out a layer that generated nothing rather than write a
// run of blank lines.
func TestSectionEmpty(t *testing.T) {
	cases := map[string]struct {
		subject emit.Section
		want    bool
	}{
		"nothing at all":   {emit.Section{}, true},
		"nothing but gaps": {emit.Section{Decls: []ast.Decl{nil, nil}}, true},
		"a declaration": {emit.Section{Decls: []ast.Decl{
			&ast.GenDecl{Tok: token.TYPE},
		}}, false},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := tc.subject.Empty(); got != tc.want {
				t.Errorf("Empty() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestImportString(t *testing.T) {
	cases := map[string]struct {
		subject emit.Import
		want    string
	}{
		"plain": {emit.Import{Path: "iter"}, `"iter"`},
		"named": {emit.Import{Path: "encoding/json/v2", Name: "json"}, `json "encoding/json/v2"`},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := tc.subject.String(); got != tc.want {
				t.Errorf("String() = %q, want %q", got, tc.want)
			}
		})
	}
}
