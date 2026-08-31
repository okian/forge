package collection

import (
	"go/token"
	"go/types"
	"slices"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"

	"github.com/okian/forge/internal/emit"
	"github.com/okian/forge/internal/layer"
	"github.com/okian/forge/internal/model"
	"github.com/okian/forge/internal/shape"
	"github.com/okian/forge/internal/shared/seq"
)

// Pluralising is where this layer meets English, which has no complete answer —
// so what is checked is that the part with one is right, including the endings
// a generator appending a single letter gets wrong.
func TestHowAFieldIsPluralised(t *testing.T) {
	cases := map[string]string{
		// The ordinary case.
		"Age":  "Ages",
		"Name": "Names",
		"ID":   "IDs",

		// A sibilant cannot take a bare s.
		"Address": "Addresses",
		"Box":     "Boxes",
		"Buzz":    "Buzzes",
		"Match":   "Matches",
		"Wish":    "Wishes",

		// A y after a consonant is a vowel and becomes ies; one after a vowel
		// is not and takes a bare s.
		"Category": "Categories",
		"Day":      "Days",
		"Key":      "Keys",

		// A y with no letter before it to look at, which is the guard that
		// keeps the rule from reading past the start of the name.
		"y": "ys",
		"Y": "Ys",

		// And the y rule is case-blind like the one above it, which the two
		// spellings of one word are what shows.
		"CATEGORY": "CATEGORies",
		"ADDRESS":  "ADDRESSes",

		// Already plural, and pluralised again, because nothing tells Tags from
		// Address. Written down because it is what the rules cost.
		"Tags": "Tagses",

		// Nothing at all, which is a field a subject cannot have and a case the
		// rules must survive rather than index into.
		"": "",
	}

	for name, want := range cases {
		if got := plural(name); got != want {
			t.Errorf("plural(%q) = %q, want %q", name, got, want)
		}
	}
}

// Every name this layer generates is held against every other and against the
// ones already on the type, because either kind of collision is one method
// declared twice in a file the author cannot edit.
func TestTwoGeneratedNamesThatAreOne(t *testing.T) {
	at := token.Position{Filename: "model/spec.go", Line: 8, Column: 6}

	// Two fields that pluralise alike, which is what pluralising makes
	// possible: Address and Addresse are two fields and Addresses is one name.
	held := plan{
		declared: "Persons", at: at,
		projections: []column{
			{field: "Address", method: "Addresses"},
			{field: "Addresse", method: "Addresses"},
		},
	}

	clashes := held.clashes()
	if clashes.Empty() {
		t.Fatal("two fields that project to one name were accepted")
	}
	for _, want := range []string{"Address", "Addresse", "Addresses"} {
		if !strings.Contains(clashes.Render(), want) {
			t.Errorf("the report does not name %s:\n%s", want, clashes.Render())
		}
	}

	// A name a layer beneath already put on the type, which needs no
	// coincidence at all: a field called Len projects to Lens, and a field
	// called Al with an order gives SortedByAl no trouble where the storage
	// layer's own Len is exactly this.
	over := plan{
		declared: "Persons", at: at,
		beneath:     shape.Shape{}.WithMethods(shape.Method{Name: "Len"}),
		projections: []column{{field: "Len", method: "Len"}},
	}

	if found := over.clashes(); found.Empty() {
		t.Error("a name the storage beneath already declared was generated again")
	}

	// Every clash rather than the first, since a subject that produced one has
	// very likely produced two.
	twice := plan{
		declared: "Persons", at: at,
		projections: []column{
			{field: "Address", method: "Addresses"},
			{field: "Addresse", method: "Addresses"},
			{field: "Box", method: "Boxes"},
			{field: "Boxe", method: "Boxes"},
		},
	}
	if found := twice.clashes(); found.Len() != 2 {
		t.Errorf("four fields making two clashes reported %d of them", found.Len())
	}

	// And a plan whose names are all distinct says nothing.
	quiet := plan{projections: []column{{method: "Ages"}, {method: "Names"}}}
	if found := quiet.clashes(); !found.Empty() {
		t.Errorf("distinct names were reported as a clash:\n%s", found.Render())
	}
}

// What the template imports is written down, and what is written down is what
// the emitted half is pruned against — so a template that grew an import nobody
// recorded is refused rather than carried into a file that may not name it.
func TestATemplateThatGrewAnImport(t *testing.T) {
	if wrong := accounted([]emit.Import{{Path: "cmp", Name: "cmp"}, {Path: "iter", Name: "iter"}}); wrong != "" {
		t.Errorf("the template's own imports were refused: %s", wrong)
	}

	wrong := accounted([]emit.Import{{Path: "encoding/json/v2", Name: "json"}})
	if wrong == "" {
		t.Fatal("an import nothing recorded a name for was accepted")
	}
	if !strings.Contains(wrong, "encoding/json/v2") {
		t.Errorf("the complaint %q does not name the import", wrong)
	}
}

// The names the spelling keeps clear of are the template's, in an order a map
// did not decide.
func TestWhatTheTemplateBinds(t *testing.T) {
	want := []model.Import{
		{Path: "cmp", Name: "cmp"},
		{Path: "iter", Name: "iter"},
		{Path: "slices", Name: "slices"},
	}
	if !slices.Equal(taken(), want) {
		t.Errorf("the template binds %v, want %v", taken(), want)
	}
}

// The recorded names are asked of the packages themselves, because they are the
// half of the list nothing else can check: a path does not say what it binds,
// and a name written down wrongly is an import the subject was not moved out of
// the way of.
func TestTheTemplateBindsWhatItSaysItDoes(t *testing.T) {
	loaded, err := packages.Load(&packages.Config{
		Mode: packages.NeedName | packages.NeedImports,
	}, "./tmpl")
	if err != nil {
		t.Fatalf("loading the template package: %v", err)
	}
	if len(loaded) != 1 {
		t.Fatalf("loaded %d packages, want the template", len(loaded))
	}

	found := make(map[string]string, len(loaded[0].Imports))
	for path, imported := range loaded[0].Imports {
		found[path] = imported.Name

		switch recorded, is := templateImports[path]; {
		case !is:
			t.Errorf("the template imports %s and nothing recorded a name for it", path)
		case recorded != imported.Name:
			t.Errorf("%s binds %q, and it is recorded as binding %q", path, imported.Name, recorded)
		}
	}

	for path := range templateImports {
		if _, imported := found[path]; !imported {
			t.Errorf("%s is recorded and the template does not import it", path)
		}
	}
}

// Every import carries the name it binds, and says separately whether that name
// has to be written.
//
// The name is what a later stage asks the file about — which import a qualified
// identifier refers to — and the path does not answer it: a package may declare
// a name that is not the last element of its path. What is written is the
// narrower question, and only an invented name has to be, so that the ordinary
// import reads the way somebody would have written it by hand.
func TestWhichImportsAreNamed(t *testing.T) {
	got := imported([]model.Import{
		{Path: "time", Name: "time"},
		{Path: "example.com/util/slices", Name: "slices2", Aliased: true},
	})

	want := []emit.Import{
		{Path: "time", Name: "time"},
		{Path: "example.com/util/slices", Name: "slices2", Aliased: true},
	}
	if !slices.Equal(got, want) {
		t.Errorf("carried %v, want %v", got, want)
	}
}

// A declaration whose name has nothing in it has no first letter to lower,
// which is the one case the prefix has to survive rather than index into.
func TestLoweringANameWithNothingInIt(t *testing.T) {
	for name, want := range map[string]string{"": "", "Persons": "persons", "persons": "persons"} {
		if got := lower(name); got != want {
			t.Errorf("lower(%q) = %q, want %q", name, got, want)
		}
	}
}

// A declaration with no doc line carries no comment, rather than one holding
// nothing — an empty comment group is a thing the printer has to place and a
// reader has to read past.
func TestADeclarationWithNothingToSay(t *testing.T) {
	if got := comment(""); got != nil {
		t.Errorf("an empty summary made %v", got)
	}
}

// Both refusals this layer can only reach through its own template are asked of
// Generate, since that is where an author would meet them.
func TestWhatGenerateRefusesATemplateFor(t *testing.T) {
	cases := map[string]struct {
		source string
		says   string
	}{
		"an import nobody recorded a name for": {
			source: "package tmpl\n\nimport \"encoding/json/v2\"\n\n" +
				"type Collection[T any] []T\n\n" +
				"func (c Collection[T]) Encoded() (json.Value, error) { return nil, nil }\n",
			says: "encoding/json/v2",
		},
		"a template that is not a template": {
			source: "package tmpl\n\ntype Elsewhere[T any] []T\n",
			says:   "declares no type called Collection",
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			was := bodies
			defer func() { bodies = was }()
			bodies = []byte(tc.source)

			_, err := New().Generate(declaring(), shape.Shape{})
			if err == nil {
				t.Fatal("the template was generated from")
			}
			if !strings.Contains(err.Error(), tc.says) {
				t.Errorf("the error %q does not say what is wrong", err)
			}
		})
	}
}

// declaring builds what a layer is asked to generate against: an inline
// declaration over a subject with one field, which provokes nothing on its own.
func declaring() *layer.Context {
	pkg := types.NewPackage("example.com/model", "model")
	obj := types.NewTypeName(token.NoPos, pkg, "Person", nil)

	return &layer.Context{
		Model: &model.Model{
			Name: "Persons",
			Form: model.FormInline,
			Subject: &model.Struct{
				Named:  types.NewNamed(obj, types.NewStruct(nil, nil), nil),
				Fields: []model.Field{{Name: "Name", Exported: true, Type: model.Classified{Type: types.Typ[types.String]}}},
			},
			Pkg: &packages.Package{PkgPath: "example.com/model"},
			Pos: token.Position{Filename: "model/spec.go", Line: 8, Column: 6},
		},
	}
}

// Everything this layer writes into syntax came out of the type checker, so a
// spelling that is not a type means forge produced one — reported as an error
// naming what it was, rather than written into a file to see what happens.
//
// Reachable only from inside the package: a spelling comes from the model, and
// the model spells types.
func TestASpellingThatIsNotAType(t *testing.T) {
	cases := map[string]plan{
		"a subject that is not one": {
			declared: "Persons",
			subject:  model.Spelling{Text: "not a type"},
			sorts:    []column{{field: "Name", method: "SortedByName", typ: model.Spelling{Text: "string"}}},
		},
		"a key that is not one": {
			declared: "Persons",
			subject:  model.Spelling{Text: "not a type"},
			indexes:  []column{{field: "Name", method: "ByName", typ: model.Spelling{Text: "string"}}},
		},
		"a field whose type is not one": {
			declared:    "Persons",
			subject:     model.Spelling{Text: "Person"},
			projections: []column{{field: "Name", method: "Names", typ: model.Spelling{Text: "not a type"}}},
		},
	}

	for name, held := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := held.build(); err == nil {
				t.Fatal("a spelling that is not a type was written into the output")
			} else if !strings.Contains(err.Error(), "not a type") {
				t.Errorf("the error %q does not say what was wrong with it", err)
			}
		})
	}
}

// An option that names a field the subject does not have is refused by
// validation, so reaching the layer with one means the two disagree about what
// the subject is. It is passed over rather than reported a second time.
func TestAnOptionNamingAFieldThatIsNotThere(t *testing.T) {
	ctx := declaring()
	ctx.Options = model.Options{
		Layer:   "collection",
		Entries: []model.Option{{Key: "sort", Value: "Nonesuch"}},
	}

	held, diags := planned(ctx, shape.Shape{})
	if !diags.Empty() {
		t.Errorf("a field validation had already refused was reported again:\n%s", diags.Render())
	}
	if len(held.sorts) != 0 {
		t.Errorf("it planned to sort by %v", held.sorts)
	}
}

// One package named by two fields is imported once, since a file importing it
// twice is a file that does not compile.
func TestAPackageTwoFieldsShare(t *testing.T) {
	moment := model.Spelling{
		Text:    "time.Time",
		Imports: []model.Import{{Path: "time", Name: "time"}},
	}

	held := plan{
		subject:     model.Spelling{Text: "Person"},
		projections: []column{{field: "Joined", typ: moment}, {field: "Left", typ: moment}},
	}

	if got := held.imports(); len(got) != 1 {
		t.Errorf("two fields of one package want %v imported, want one", got)
	}
}

// Validation refuses a field named twice, so a layer handed one anyway is being
// called wrongly — and what it must not do is act on it twice. One column, and
// for a field it cannot generate from, one complaint rather than one per
// mention.
func TestAFieldNamedTwiceReachesTheLayerOnce(t *testing.T) {
	ctx := declaring()
	ctx.Model.Subject.Fields = append(ctx.Model.Subject.Fields,
		model.Field{Name: "Tags", Exported: true, Type: model.Classified{
			Type: types.NewSlice(types.Typ[types.String]),
		}})

	ctx.Options = model.Options{
		Layer:   "collection",
		Entries: []model.Option{{Key: "index", Value: "Name,Name"}},
	}

	held, diags := planned(ctx, shape.Shape{})
	if !diags.Empty() {
		t.Errorf("a usable field named twice was reported:\n%s", diags.Render())
	}
	if len(held.indexes) != 1 {
		t.Errorf("a field named twice made %d columns", len(held.indexes))
	}

	// And one that cannot be generated from is refused once rather than once
	// per mention, which is the shape the first fix for this had.
	ctx.Options = model.Options{
		Layer:   "collection",
		Entries: []model.Option{{Key: "index", Value: "Tags,Tags"}},
	}

	if _, diags = planned(ctx, shape.Shape{}); diags.Len() != 1 {
		t.Errorf("an unusable field named twice was reported %d times:\n%s", diags.Len(), diags.Render())
	}
}

// The view is a name this layer chooses and the shared view is one it requires,
// so a declaration asking for the second is a redeclaration in a file the
// author cannot edit.
func TestTheViewNamedAfterTheSharedOne(t *testing.T) {
	ctx := declaring()
	ctx.Options = model.Options{
		Layer:   "collection",
		Entries: []model.Option{{Key: "seq", Value: seq.Name}},
	}

	_, err := New().Generate(ctx, shape.Shape{})
	if err == nil {
		t.Fatal("the view was named after the type it is declared over")
	}
	if !strings.Contains(err.Error(), "FRG4101") {
		t.Errorf("the error %q is not the collision it is", err)
	}
}

// A template that grew a method nothing here emits or leaves out is refused,
// the way an import nobody recorded is: what either produces is a file missing
// something it calls.
func TestATemplateThatGrewAMethod(t *testing.T) {
	was := bodies
	defer func() { bodies = was }()

	bodies = []byte("package tmpl\n\nimport \"iter\"\n\n" +
		"type Collection[T any] []T\n\n" +
		"func (c Collection[T]) All() iter.Seq[T] { return nil }\n\n" +
		"func (c Collection[T]) counted() int { return 0 }\n")

	_, err := New().Generate(declaring(), shape.Shape{})
	if err == nil {
		t.Fatal("a method nothing here knows about was passed over")
	}
	if !strings.Contains(err.Error(), "counted") {
		t.Errorf("the error %q does not name the method", err)
	}
}
