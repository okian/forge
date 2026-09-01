package generate

import (
	"go/parser"
	"go/token"
	"go/types"
	"strings"
	"testing"

	"github.com/okian/forge/internal/diag"
	"github.com/okian/forge/internal/discover"
	"github.com/okian/forge/internal/emit"
	"github.com/okian/forge/internal/model"
)

// A method that shares a name with one an interface asks for, and not its
// signature, earns nothing.
//
// This is the whole risk in deciding from syntax. What is compared is the way
// the types are written, and a near miss that were let through would be an
// assertion in a file somebody else compiles — so the failure would arrive as a
// build error in their package, about a line nobody there wrote.
func TestAMethodThatOnlyLooksLikeTheOne(t *testing.T) {
	cases := map[string]string{
		"the wrong results":  "func (p *Persons) WriteTo(w io.Writer) error { return nil }",
		"too few results":    "func (p *Persons) WriteTo(w io.Writer) {}",
		"the wrong argument": "func (p *Persons) WriteTo(w *bytes.Buffer) (int64, error) { return 0, nil }",
		"too many arguments": "func (p *Persons) WriteTo(w io.Writer, n int) (int64, error) { return 0, nil }",
		"variadic":           "func (p *Persons) WriteTo(w ...io.Writer) (int64, error) { return 0, nil }",
		"on another type":    "func (o *Others) WriteTo(w io.Writer) (int64, error) { return 0, nil }",
	}

	for name, written := range cases {
		t.Run(name, func(t *testing.T) {
			if satisfies(having(t, written), writerTo(t), nil) {
				t.Errorf("%s earned io.WriterTo:\n%s", name, written)
			}
		})
	}

	// And the one that does match, so that the cases above are read as near
	// misses rather than as a check that never says yes.
	written := "func (p *Persons) WriteTo(w io.Writer) (int64, error) { return 0, nil }"
	if !satisfies(having(t, written), writerTo(t), nil) {
		t.Errorf("the method the interface asks for earned nothing:\n%s", written)
	}
}

// The walk is claimed from what it answers with, so something else called All
// is not the walk.
//
// An author is free to declare a method by that name, and one that hands back a
// slice is a reasonable thing to have written. Claiming its signature would be
// harmless in itself — but the claim is written as the walk's, and a reader
// would take it for one.
func TestAllThatIsNotTheWalk(t *testing.T) {
	cases := map[string]string{
		"a slice":        "func (p *Persons) All() []Person { return nil }",
		"a pair":         "func (p *Persons) All() iter.Seq2[int, Person] { return nil }",
		"two results":    "func (p *Persons) All() (iter.Seq[Person], error) { return nil, nil }",
		"an argument":    "func (p *Persons) All(from int) iter.Seq[Person] { return nil }",
		"nothing at all": "func (p *Persons) All() {}",
	}

	for name, written := range cases {
		t.Run(name, func(t *testing.T) {
			if _, walking := walk(having(t, written), about(), nil, &diag.Set{}); walking {
				t.Errorf("%s was claimed as the walk:\n%s", name, written)
			}
		})
	}

	written := "func (p *Persons) All() iter.Seq[Person] { return nil }"
	held, walking := walk(having(t, written), about(), nil, &diag.Set{})
	if !walking {
		t.Fatalf("the walk was not recognised:\n%s", written)
	}
	if held != "func(*Persons) iter.Seq[Person]" {
		t.Errorf("the walk is claimed as %q", held)
	}
}

// having returns the methods a declaration ends up with, given source for them.
func having(t *testing.T, source string) map[string]has {
	t.Helper()

	fset := token.NewFileSet()

	file, err := parser.ParseFile(fset, "held.go", "package model\n\n"+source, 0)
	if err != nil {
		t.Fatalf("the fixture is not valid Go: %v", err)
	}

	return methods([]emit.Section{{Decls: file.Decls, Fset: fset}}, about(), nil)
}

// about is the declaration the claims in this file are about: a Persons whose
// elements are the Person its own package declares.
func about() synthesis {
	return synthesis{
		declared: "Persons",
		elem:     model.Spelling{Text: "Person", Local: "example.com/model"},
		pkg:      "example.com/model",
		held:     declared{methods: map[string]map[string]*types.Func{}},
	}
}

// A package of somebody's own called io is not the io a claim would name.
//
// The hazard is narrow and the consequence is not: a claim is written into a
// file somebody else compiles, and one earned by a lookalike would fail there,
// on a line nobody in that package wrote. A package no row is about is written
// by its import path, which no Go source writes, and that is what keeps the two
// apart.
//
// The module paths matter here. Deciding this from the shape of a path — a dot
// in the first element, the test the go command applies with GOROOT behind it —
// is what the cases below are written to catch: `module myapp` is legal and
// ordinary in a private tree, and myapp/io would pass any such test.
func TestATypeThatOnlyLooksLikeTheStandardLibrarys(t *testing.T) {
	const local = "example.com/model"

	cases := map[string]struct {
		path, name, want string
	}{
		"the standard library's": {"io", "io", "io.Writer"},
		"a published module's":   {"example.com/model/io", "io", "example.com/model/io.Writer"},
		"a module with no dot":   {"myapp/io", "io", "myapp/io.Writer"},
		"a single-element path":  {"io2", "io", "io2.Writer"},
		"the local package's":    {local, "model", "Writer"},
	}

	for name, one := range cases {
		t.Run(name, func(t *testing.T) {
			held := writerIn(one.path, one.name)

			got := tupled(types.NewTuple(types.NewParam(token.NoPos, nil, "w", held)), local, nil)
			if len(got) != 1 || got[0] != one.want {
				t.Errorf("%s Writer is spelled %v, want %q", name, got, one.want)
			}
		})
	}
}

// A file that has already given a package's name to something else cannot make
// a claim about that package.
//
// The name is what a claim is written with, and one written with a name that
// means somebody else's package is a claim about somebody else's package. The
// alternative is to write it anyway and let the file be refused for binding one
// name to two paths — which is a message about forge having made a mistake,
// delivered instead of the answer.
func TestAClaimWhoseNameIsAlreadyTaken(t *testing.T) {
	held := binding{"myapp/io": {Path: "myapp/io", Name: "io"}}

	if _, can := held.name(stdIO); can {
		t.Error("io.WriterTo can be named in a file where io means something else")
	}

	// And no row about that package is earned, whatever the declaration
	// happens to declare. In such a file a WriteTo taking an io.Writer takes
	// somebody else's, which is not the interface — so the row failing is the
	// right answer rather than a lucky one.
	written := "func (p *Persons) WriteTo(w io.Writer) (int64, error) { return 0, nil }"
	if satisfies(having(t, written), writerTo(t), held) {
		t.Errorf("io.WriterTo was earned in a file where io means something else:\n%s", written)
	}
}

// A package outside the table is written as Go writes it, unless its name is
// one a claim already uses.
//
// Writing every such package by path would be simpler and wrong: the walk's own
// element is compared against the way the file spells it, and a path there is a
// disagreement between two spellings of one type reported as a disagreement
// about the type. So a path is written only where a name would be ambiguous,
// which is the only place it buys anything.
func TestWhenAPathIsWrittenInsteadOfAName(t *testing.T) {
	const local = "example.com/model"

	held := binding{
		stdIO.Path:           {Path: stdIO.Path, Name: "io"},
		"example.com/domain": {Path: "example.com/domain", Name: "domain"},
		"example.com/legacy": {Path: "example.com/legacy", Name: "old"},
	}

	cases := map[string]struct {
		path, name, want string
	}{
		// Nothing is written under domain, so Go's own spelling stands — and
		// the walk over a domain.Person compares equal to the way the file
		// writes it.
		"a package the file imports": {"example.com/domain", "domain", "domain.Writer"},

		// Aliased by the file, so the file's name rather than the package's.
		"one the file renamed": {"example.com/legacy", "legacy", "old.Writer"},

		// Its name is what io.Writer is written under here, so it takes a path.
		"one whose name is taken": {"myapp/io", "io", "myapp/io.Writer"},
	}

	// And a package the table is about, in a file that has given its name away,
	// takes a path too: nothing here can be written as that package, so nothing
	// may be compared as it either.
	t.Run("one the table is about and the file cannot name", func(t *testing.T) {
		gone := binding{"myapp/json": {Path: "myapp/json", Name: "json"}}

		got := tupled(
			types.NewTuple(types.NewParam(token.NoPos, nil, "w", writerIn(stdJSON.Path, "json"))),
			local, gone,
		)
		if len(got) != 1 || got[0] != stdJSON.Path+".Writer" {
			t.Errorf("the standard library's json is spelled %v, want %q", got, stdJSON.Path+".Writer")
		}
	})

	for name, one := range cases {
		t.Run(name, func(t *testing.T) {
			got := tupled(
				types.NewTuple(types.NewParam(token.NoPos, nil, "w", writerIn(one.path, one.name))),
				local, held,
			)
			if len(got) != 1 || got[0] != one.want {
				t.Errorf("%s Writer is spelled %v, want %q", name, got, one.want)
			}
		})
	}
}

// What a file already binds is carried whole into the claim's own import.
//
// A name is half of what an import line needs; whether it has to be written out
// is the other half. An import that kept the name and lost that bit binds one
// name and is referred to by another, and nothing downstream catches it: the
// two compare equal where imports are compacted, and the one forge invented is
// the one that survives.
func TestAClaimKeepsTheImportTheFileAlreadyHas(t *testing.T) {
	aliased := emit.Import{Path: "io", Name: "stdio", Aliased: true}
	held := binding{"io": aliased}

	if got, _ := held.binds(stdIO); got != aliased {
		t.Errorf("the claim's import is %+v, want the one the file already has, %+v", got, aliased)
	}

	// And a file with no import for the path gets one under the package's own
	// name, which needs no alias.
	empty := binding{}
	if got, _ := empty.binds(stdIO); got.Aliased || got.Name != "io" {
		t.Errorf("an import forge adds itself is %+v, want io bound plainly", got)
	}
}

// A type belonging to no package is written with no package name.
//
// Go's printer does not ask about a predeclared type today, so this is the
// branch that exists to be wrong quietly rather than loudly. Asked directly,
// because nothing else can reach it.
func TestATypeBelongingToNoPackage(t *testing.T) {
	if got := spelling("example.com/model", nil)(nil); got != "" {
		t.Errorf("a type with no package is qualified by %q", got)
	}
}

// The element's own binding is part of what a claim compares against.
//
// The element is spelled before any of this runs, and it may have been renamed
// on the way: a package whose own name a layer already took is bound under
// another one, and that binding is carried with the spelling rather than with
// the unit. A comparison that read only the unit would write the element one
// way and read a method naming it another, then report two spellings of one
// type as a disagreement about the type — refusing a build that is correct.
func TestTheElementsOwnBindingIsRead(t *testing.T) {
	const local = "example.com/model"

	// A layer took slices for the standard library's, so the subject's package
	// was bound under another name.
	elem := model.Spelling{
		Text:    "slices2.Person",
		Imports: []model.Import{{Path: "example.com/util/slices", Name: "slices2", Aliased: true}},
		Local:   local,
	}

	as := bindings([]emit.Import{{Path: "slices", Name: "slices"}}, importing(elem)...)

	pkg := types.NewPackage("example.com/util/slices", "slices")
	held := types.NewNamed(types.NewTypeName(token.NoPos, pkg, "Person", nil), types.NewStruct(nil, nil), nil)

	got := tupled(types.NewTuple(types.NewParam(token.NoPos, nil, "v", held)), local, as)
	if len(got) != 1 || got[0] != elem.Text {
		t.Errorf("a method naming the element reads %v, want %q — the way the element is written", got, elem.Text)
	}
}

// And two packages whose names are the same word are not written the same way.
//
// This is the case the walk's own comparison exists for. An element from one
// domain and a method answering with another domain would read alike if both
// were written by name, and forge would write an assertion it had every means
// to know was false — into a file the author may not edit.
func TestTwoPackagesOfOneName(t *testing.T) {
	const local = "example.com/model"

	as := bindings([]emit.Import{{Path: "example.com/a/domain", Name: "domain"}})

	cases := map[string]struct{ path, want string }{
		"the one the file imports": {"example.com/a/domain", "domain.Person"},
		"the other one":            {"example.com/b/domain", "example.com/b/domain.Person"},
	}

	for name, one := range cases {
		t.Run(name, func(t *testing.T) {
			pkg := types.NewPackage(one.path, "domain")
			held := types.NewNamed(types.NewTypeName(token.NoPos, pkg, "Person", nil), types.NewStruct(nil, nil), nil)

			got := tupled(types.NewTuple(types.NewParam(token.NoPos, nil, "v", held)), local, as)
			if len(got) != 1 || got[0] != one.want {
				t.Errorf("%s Person is written %v, want %q", name, got, one.want)
			}
		})
	}
}

// A skip naming something the file could not write is told that, rather than
// that the methods did not add up.
//
// The two are different mistakes and only one of them is the author's. A
// declaration whose methods satisfy an interface, in a file where a layer has
// taken the name that claim would need, claims nothing — and an author who
// skips it is not wrong about their own code. Telling them the methods do not
// add up would send them to read the methods.
func TestASkipForAClaimTheFileCouldNotWrite(t *testing.T) {
	written := discover.Directive{
		Layer: "skip", Args: "json.MarshalerTo", Text: "//forge:skip json.MarshalerTo",
		ArgsOffset: len("//forge:skip "),
		Pos:        token.Position{Filename: "model.go", Line: 4, Column: 1},
	}

	of := about()
	of.skipped = []discover.Directive{written}

	diags := &diag.Set{}
	unclaimed(claimable{unnameable: []string{"json.MarshalerTo"}}, of, diags)

	if diags.Empty() {
		t.Fatal("a skip for a claim the file could not write was passed over")
	}

	one := diags.All()[0]
	if !strings.Contains(one.Message, "cannot name it") {
		t.Errorf("the complaint does not say the file could not write it:\n%s", one.Message)
	}
	if strings.Contains(one.Message, "does not claim") {
		t.Errorf("the complaint says the methods did not add up:\n%s", one.Message)
	}
}

// Every package the table names is one a claim knows how to write.
//
// The two are declared apart and have to agree, and the way they fail to is
// silent and lopsided: a row naming a package the map does not hold is written
// by its own name where a method the author declared is written by its import
// path, so the claim keeps working until somebody overrides the method and then
// quietly stops. A single-element path survives the omission by luck, since its
// path and its name are the same word, and the standard library forge already
// reaches into is full of paths that are not.
func TestEveryPackageARowNamesCanBeWritten(t *testing.T) {
	held := func(one model.Import, where string) {
		t.Helper()
		if one.Path == "" {
			return
		}
		if _, known := tabled[one.Path]; !known {
			t.Errorf("%s names %s, which the table cannot write", where, one.Path)
		}
	}

	for _, row := range synthesised {
		held(row.from, row.spelled())

		for _, need := range row.needs {
			for _, one := range append(append([]spelled(nil), need.params...), need.results...) {
				held(one.from, row.spelled()+"."+need.name)
			}
		}
	}

	// And the walk's, which is not a row and is named the same way.
	held(stdIter, "the walk")
}

// writerIn returns a named type called Writer, declared in a package.
func writerIn(path, name string) types.Type {
	pkg := types.NewPackage(path, name)
	obj := types.NewTypeName(token.NoPos, pkg, "Writer", nil)

	return types.NewNamed(obj, types.NewInterfaceType(nil, nil), nil)
}

// writerTo is the row the near misses are held against.
func writerTo(t *testing.T) synthetic {
	t.Helper()

	for _, one := range synthesised {
		if one.spelled() == "io.WriterTo" {
			return one
		}
	}

	t.Fatal("nothing in the table claims io.WriterTo")
	return synthetic{}
}
