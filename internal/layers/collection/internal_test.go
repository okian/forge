package collection

import (
	"go/token"
	"go/types"
	"slices"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"

	"github.com/okian/forge/internal/model"
	"github.com/okian/forge/internal/shared/seq"
	"github.com/okian/forge/internal/words"
	"github.com/okian/forge/plugin"
)

// A projection is named through the shared inflection rather than through a
// rule this layer keeps, which is what the two ends of the range are for: a
// word the dictionary knows and one it has never heard of.
//
// The rules themselves are held by internal/words and its corpus. What is
// checked here is that this layer asks it, since a layer that grew a rule of
// its own back would pass every test in that package.
func TestAProjectionIsNamedThroughTheSharedInflection(t *testing.T) {
	for name, want := range map[string]string{
		"Age":     "Ages",
		"Address": "Addresses",
		"Child":   "Children",
		"Alias":   "Aliases",
		"Aliases": "Aliases",
		"ID":      "IDs",
	} {
		if got := words.Plural(name); got != want {
			t.Errorf("the projection of %s is %s, want %s", name, got, want)
		}
	}
}

// Two fields whose projections come out with one name is what a dictionary that
// leaves a plural alone makes possible, and it is settled rather than refused:
// the field spelled like the name keeps it, the other is projected under its
// own name with Values after it, and the pair is reported once.
func TestTwoFieldsThatProjectToOneName(t *testing.T) {
	for _, one := range []struct {
		what   string
		fields []string
		want   []string
	}{
		{
			"the plural field is declared second",
			[]string{"Alias", "Aliases"},
			[]string{"AliasValues", "Aliases"},
		},
		{
			"and first, which must not change who keeps the name",
			[]string{"Aliases", "Alias"},
			[]string{"Aliases", "AliasValues"},
		},
		{
			"neither field is spelled like the name, so the first declared keeps it",
			[]string{"Address", "Addresse"},
			[]string{"Addresses", "AddresseValues"},
		},
	} {
		var (
			diags  plugin.Diagnostics
			held   []column
			fields []model.Field
		)
		for _, name := range one.fields {
			held = append(held, column{field: name, method: words.Plural(name)})
			fields = append(fields, model.Field{Name: name, Exported: true})
		}

		got := share(held, &model.Struct{Fields: fields}, &diags)
		for at := range got {
			if got[at].method != one.want[at] {
				t.Errorf("%s: %s projects to %s, want %s",
					one.what, got[at].field, got[at].method, one.want[at])
			}
		}

		if diags.Len() != 1 {
			t.Errorf("%s: the pair produced %d reports, want one", one.what, diags.Len())
		}
		for _, want := range one.fields {
			if !strings.Contains(diags.Render(), want) {
				t.Errorf("%s: the report does not name %s:\n%s", one.what, want, diags.Render())
			}
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
		beneath:     plugin.Shape{}.WithMethods(plugin.Method{Name: "Len"}),
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
	if wrong := accounted([]plugin.Import{{Path: "cmp", Name: "cmp"}, {Path: "iter", Name: "iter"}}); wrong != "" {
		t.Errorf("the template's own imports were refused: %s", wrong)
	}

	wrong := accounted([]plugin.Import{{Path: "encoding/json/v2", Name: "json"}})
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
	want := []plugin.Import{
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
	got := imported([]plugin.Import{
		{Path: "time", Name: "time"},
		{Path: "example.com/util/slices", Name: "slices2", Aliased: true},
	})

	want := []plugin.Import{
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

			_, err := New().Generate(declaring(), plugin.Shape{})
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
func declaring() *plugin.Context {
	pkg := types.NewPackage("example.com/model", "model")
	obj := types.NewTypeName(token.NoPos, pkg, "Person", nil)

	return &plugin.Context{
		Model: &plugin.Model{
			Name: "Persons",
			Form: plugin.FormInline,
			Subject: &plugin.Struct{
				Named:  types.NewNamed(obj, types.NewStruct(nil, nil), nil),
				Fields: []plugin.Field{{Name: "Name", Exported: true, Type: plugin.Classified{Type: types.Typ[types.String]}}},
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
			subject:  plugin.Spelling{Text: "not a type"},
			sorts:    []column{{field: "Name", method: "SortedByName", typ: plugin.Spelling{Text: "string"}}},
		},
		"a key that is not one": {
			declared: "Persons",
			subject:  plugin.Spelling{Text: "not a type"},
			indexes:  []column{{field: "Name", method: "ByName", typ: plugin.Spelling{Text: "string"}}},
		},
		"a field whose type is not one": {
			declared:    "Persons",
			subject:     plugin.Spelling{Text: "Person"},
			projections: []column{{field: "Name", method: "Names", typ: plugin.Spelling{Text: "not a type"}}},
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
	ctx.Options = plugin.Options{
		Layer:   "collection",
		Entries: []model.Option{{Key: "sort", Value: "Nonesuch"}},
	}

	held, diags := planned(ctx, plugin.Shape{})
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
	moment := plugin.Spelling{
		Text:    "time.Time",
		Imports: []plugin.Import{{Path: "time", Name: "time"}},
	}

	held := plan{
		subject:     plugin.Spelling{Text: "Person"},
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
		plugin.Field{Name: "Tags", Exported: true, Type: plugin.Classified{
			Type: types.NewSlice(types.Typ[types.String]),
		}})

	ctx.Options = plugin.Options{
		Layer:   "collection",
		Entries: []model.Option{{Key: "index", Value: "Name,Name"}},
	}

	held, diags := planned(ctx, plugin.Shape{})
	if !diags.Empty() {
		t.Errorf("a usable field named twice was reported:\n%s", diags.Render())
	}
	if len(held.indexes) != 1 {
		t.Errorf("a field named twice made %d columns", len(held.indexes))
	}

	// And one that cannot be generated from is refused once rather than once
	// per mention, which is the shape the first fix for this had.
	ctx.Options = plugin.Options{
		Layer:   "collection",
		Entries: []model.Option{{Key: "index", Value: "Tags,Tags"}},
	}

	if _, diags = planned(ctx, plugin.Shape{}); diags.Len() != 1 {
		t.Errorf("an unusable field named twice was reported %d times:\n%s", diags.Len(), diags.Render())
	}
}

// The view is a name this layer chooses and the shared view is one it requires,
// so a declaration asking for the second is a redeclaration in a file the
// author cannot edit.
func TestTheViewNamedAfterTheSharedOne(t *testing.T) {
	ctx := declaring()
	ctx.Options = plugin.Options{
		Layer:   "collection",
		Entries: []model.Option{{Key: "seq", Value: seq.Name}},
	}

	_, err := New().Generate(ctx, plugin.Shape{})
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

	_, err := New().Generate(declaring(), plugin.Shape{})
	if err == nil {
		t.Fatal("a method nothing here knows about was passed over")
	}
	if !strings.Contains(err.Error(), "counted") {
		t.Errorf("the error %q does not name the method", err)
	}
}
