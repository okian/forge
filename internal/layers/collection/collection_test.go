package collection_test

import (
	"bytes"
	"go/token"
	"go/types"
	"path"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"

	"github.com/okian/forge/internal/discover"
	"github.com/okian/forge/internal/emit"
	"github.com/okian/forge/internal/goldentest"
	"github.com/okian/forge/internal/layers"
	"github.com/okian/forge/internal/layers/collection"
	"github.com/okian/forge/internal/layers/slice"
	"github.com/okian/forge/internal/merge"
	"github.com/okian/forge/internal/options"
	"github.com/okian/forge/internal/shared/seq"
	"github.com/okian/forge/plugin"
)

// The declaration these tests generate for, and where it was written.
const local = "example.com/model"

var declaredAt = token.Position{Filename: "model/spec.go", Line: 12, Column: 6}

// subjectSource is the fixture the generated output is compiled against: a
// subject with a field of every shape the layer has an answer for, and two it
// has to refuse.
const subjectSource = "package model\n\n" +
	"import \"time\"\n\n" +
	"// Person is what the collection holds.\n" +
	"type Person struct {\n" +
	"\tID      int\n" +
	"\tName    string\n" +
	"\tAddress string\n" +
	"\tCity    string\n" +
	"\tJoined  time.Time\n" +
	"\tTags    []string\n" +
	"\tsecret  string\n" +
	"}\n"

// person builds the model of that subject, field for field.
func person(t *testing.T) *plugin.Struct {
	t.Helper()

	pkg := types.NewPackage(local, "model")
	obj := types.NewTypeName(token.NoPos, pkg, "Person", nil)
	named := types.NewNamed(obj, types.NewStruct(nil, nil), nil)

	moment := types.NewNamed(
		types.NewTypeName(token.NoPos, types.NewPackage("time", "time"), "Time", nil),
		types.NewStruct(nil, nil), nil)

	return &plugin.Struct{
		Named: named,
		Fields: []plugin.Field{
			held("ID", types.Typ[types.Int]),
			held("Name", types.Typ[types.String]),
			held("Address", types.Typ[types.String]),
			held("City", types.Typ[types.String]),
			held("Joined", moment),
			held("Tags", types.NewSlice(types.Typ[types.String])),
			{Name: "secret", Type: plugin.Classified{Type: types.Typ[types.String]}},
		},
	}
}

// held builds one exported field of the subject.
func held(name string, of types.Type) plugin.Field {
	return plugin.Field{Name: name, Exported: true, Type: plugin.Classified{Type: of}}
}

// declaration builds what a layer is asked to generate against, with the
// options a directive would have written on it.
func declaration(t *testing.T, directives ...string) *plugin.Context {
	t.Helper()

	stack := stacked()
	subject := person(t)

	written := make([]discover.Directive, len(directives))
	for i, text := range directives {
		written[i] = directive(text, declaredAt.Line-len(directives)+i)
	}

	set, diags := options.Read(options.Declaration{
		Pos: declaredAt, Directives: written, Stack: stack, Subject: subject,
	}, layers.Builtins())
	if !diags.Empty() {
		t.Fatalf("the directives were refused:\n%s", diags.Render())
	}

	ctx := &plugin.Context{
		Model: &plugin.Model{
			Name: "Persons", Form: plugin.FormSpec, Subject: subject, Stack: stack,
			Options: set,
			Pkg:     &packages.Package{PkgPath: local},
			Pos:     declaredAt,
		},
	}
	if len(set) > 0 {
		ctx.Options = set[0]
	}

	// What the file will bind, which generation works out from the whole stack
	// and hands to every layer in it. This layer's own is the right answer here
	// because these tests ask what this layer does with a subject, not what a
	// stack agrees on — but it has to be said rather than assumed, since a
	// layer given none spells against nothing and would alias an import the
	// template already has.
	ctx = ctx.Binding(collection.New().Binds())

	return ctx
}

// stacked is the declaration's layers, outermost first.
func stacked() []plugin.LayerRef {
	return []plugin.LayerRef{
		{Origin: collection.New().Origin(), Kind: plugin.KindRefining},
		{Origin: slice.New().Origin(), Kind: plugin.KindStorage},
	}
}

// directive2 is one directive written above the declaration.
func directive2(text string) discover.Directive {
	return directive(text, declaredAt.Line-1)
}

// directive takes a comment apart the way discovery does, so that what these
// tests hand to validation is what a run would.
func directive(text string, line int) discover.Directive {
	const prefix = "//forge:"

	name, args, _ := strings.Cut(strings.TrimPrefix(text, prefix), " ")

	offset := len(prefix) + len(name)
	if args != "" {
		offset++
	}

	return discover.Directive{
		Layer: name, Args: args, Text: text, ArgsOffset: offset,
		Pos: token.Position{Filename: declaredAt.Filename, Line: line, Column: 1},
	}
}

// generated renders the whole stack — the storage beneath, the shared view it
// requires, and this layer — as the one file they are written to.
//
// The whole stack, because that is the only thing worth compiling. This layer
// emits methods that call the storage layer's walk and return a type the shared
// view supplies, so output that compiles on its own would be output that had
// been cut down until it did.
func generated(t *testing.T, ctx *plugin.Context) []byte {
	t.Helper()

	storage, err := slice.New().Generate(ctx, plugin.Shape{})
	if err != nil {
		t.Fatalf("the storage layer refused: %v", err)
	}

	// The shape the storage beneath actually exposes, rather than one written
	// out here: what this layer generates depends on it, and a shape assembled
	// for the test would be a second account of the storage to keep in step
	// with the first.
	query, err := collection.New().Generate(ctx, slice.New().Shape(ctx, plugin.Shape{}))
	if err != nil {
		t.Fatalf("the collection layer refused: %v", err)
	}

	shared, err := seq.Unit(ctx.Model.Pos)
	if err != nil {
		t.Fatalf("the shared view could not be read: %v", err)
	}

	merged := merge.Units(storage, query, shared)

	out, err := emit.File{
		Package: "model", Pos: ctx.Model.Pos,
		Imports: merged.Imports, Sections: merged.Sections,
	}.Render()
	if err != nil {
		t.Fatalf("rendering the file: %v", err)
	}
	return out
}

// The worked example: a collection over a subject, with an order and a lookup
// declared, generating a file that compiles.
func TestTheWorkedExample(t *testing.T) {
	ctx := declaration(t, "//forge:collection sort=Name,ID index=Name")

	goldentest.Check(t, goldentest.Package{
		Path: "model",
		Files: []goldentest.Source{
			{Name: "person.go", Content: []byte(subjectSource)},
			{Name: "forge.gen.go", Content: generated(t, ctx), Generated: true},
		},
	})
}

// A declaration with no directives still gets the half of the surface that is
// not a choice, and none of the half that is.
func TestADeclarationThatAsksForNothing(t *testing.T) {
	out := generated(t, declaration(t))

	for _, want := range []string{
		"type PersonsSeq struct {\n\tSeq[Person]\n}",
		"func (c Persons) Seq() PersonsSeq",
		"func (c Persons) Names() []string",

		// A sibilant takes es rather than a bare s, which is the rule that gets
		// Address right where a generator appending one letter gets Addresss.
		"func (c Persons) Addresses() []string",

		// A field that is already plural is left alone, which is what the
		// dictionary buys over three suffix rules: the projection is Tags
		// rather than the Tagses a rule that cannot tell Tags from Address
		// would have written.
		"func (c Persons) Tags() [][]string",
	} {
		if !bytes.Contains(out, []byte(want)) {
			t.Errorf("the output does not hold %q:\n%s", want, out)
		}
	}

	// The helpers a declaration does not reach are not emitted, so a collection
	// that named no order carries no sorting.
	for _, gone := range []string{"SortedBy", "func (c Persons) ordered", "func (c Persons) keyed"} {
		if bytes.Contains(out, []byte(gone)) {
			t.Errorf("the output holds %q, which nothing asked for:\n%s", gone, out)
		}
	}

	goldentest.Check(t, goldentest.Package{
		Path: "model",
		Files: []goldentest.Source{
			{Name: "person.go", Content: []byte(subjectSource)},
			{Name: "forge.gen.go", Content: out, Generated: true},
		},
	})
}

// An unexported field is not projected. Generated code lands in the package
// that declares the collection, which is not always the one that declares the
// subject, so a projection of one is a method that compiles for some
// declarations and not others.
func TestAnUnexportedFieldIsNotProjected(t *testing.T) {
	if out := generated(t, declaration(t)); bytes.Contains(out, []byte("secret")) {
		t.Errorf("an unexported field was projected:\n%s", out)
	}
}

// A field the layer cannot generate from is reported at the option that named
// it, because the option is what has to change — and the rest of the surface is
// not built, since a declaration is generated whole or not at all.
func TestAFieldTheOrderCannotBeTakenFrom(t *testing.T) {
	cases := map[string]struct {
		directive string
		code      string
		says      string
	}{
		"an order over something with none": {
			directive: "//forge:collection sort=Tags",
			code:      "FRG3013",
			says:      "compared for order",
		},
		"a key that cannot be one": {
			directive: "//forge:collection index=Tags",
			code:      "FRG3014",
			says:      "map key",
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := collection.New().Generate(declaration(t, tc.directive), plugin.Shape{})
			if err == nil {
				t.Fatal("a field the layer cannot generate from was accepted")
			}

			reported, ok := plugin.From(err)
			if !ok {
				t.Fatalf("the error %v is not a diagnostic", err)
			}
			if got := reported.Code.String(); got != tc.code {
				t.Errorf("code is %s, want %s", got, tc.code)
			}
			if !strings.Contains(reported.Message, tc.says) {
				t.Errorf("the message %q does not say what is wrong", reported.Message)
			}
			if !strings.Contains(reported.Message, "Tags") {
				t.Errorf("the message %q does not name the field", reported.Message)
			}
			// At the directive, which is what has to change, rather than at the
			// declaration it sits above.
			if reported.Pos.Line >= declaredAt.Line {
				t.Errorf("it points at %s, want the directive above the declaration", reported.Pos)
			}
		})
	}
}

// A time is not ordered by cmp and is a perfectly good map key, which is the
// pair of answers that shows the two checks are asking different questions.
func TestATimeIsAKeyAndNotAnOrder(t *testing.T) {
	if _, err := collection.New().Generate(declaration(t, "//forge:collection index=Joined"), plugin.Shape{}); err != nil {
		t.Errorf("a time was refused as a key: %v", err)
	}
	if _, err := collection.New().Generate(declaration(t, "//forge:collection sort=Joined"), plugin.Shape{}); err == nil {
		t.Error("a time was accepted as an order, and cmp cannot compare one")
	}
}

// The view takes the name the declaration asked for, which is what the option
// is for: a package that already has a PersonsSeq needs the generated one to be
// called something else.
func TestTheViewCanBeNamed(t *testing.T) {
	out := generated(t, declaration(t, "//forge:collection seq=PersonsView"))

	if !bytes.Contains(out, []byte("type PersonsView struct {\n\tSeq[Person]\n}")) {
		t.Errorf("the view was not named as the declaration asked:\n%s", out)
	}
	if bytes.Contains(out, []byte("PersonsSeq")) {
		t.Errorf("the default name was written as well:\n%s", out)
	}
}

// The shared view is required rather than declared, so the stage that assembles
// a package emits one copy however many declarations there ask for it.
func TestTheSharedViewIsRequiredAndNotDeclared(t *testing.T) {
	unit, err := collection.New().Generate(declaration(t), plugin.Shape{})
	if err != nil {
		t.Fatalf("the layer refused: %v", err)
	}

	if want := seq.Ref(local); len(unit.Requires) != 1 || unit.Requires[0] != want {
		t.Errorf("it requires %v, want %v", unit.Requires, want)
	}
}

// The same declaration generates the same bytes.
func TestGeneratingTwiceIsTheSameBytes(t *testing.T) {
	first := generated(t, declaration(t, "//forge:collection sort=Name index=ID"))
	second := generated(t, declaration(t, "//forge:collection sort=Name index=ID"))

	if !bytes.Equal(first, second) {
		t.Errorf("two runs differ:\n%s", first)
	}
}

// A layer asked to generate for nothing has nothing to point a diagnostic at.
func TestGeneratingWithoutADeclaration(t *testing.T) {
	for name, ctx := range map[string]*plugin.Context{
		"no context": nil,
		"no model":   {},
		"no subject": {Model: &plugin.Model{Name: "Persons"}},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := collection.New().Generate(ctx, plugin.Shape{}); err == nil {
				t.Fatal("the layer generated without a declaration")
			}
		})
	}
}

// The layer says what it needs of the stack beneath it, which is that it can be
// walked and nothing more.
func TestWhatTheLayerSitsOn(t *testing.T) {
	if err := collection.New().Accepts(plugin.Shape{Caps: plugin.Caps(plugin.Sized)}); err == nil {
		t.Error("a stack with nothing to walk was accepted")
	} else if !strings.Contains(err.Error(), "Streamable") {
		t.Errorf("the error %q does not name what is missing", err)
	}

	if err := collection.New().Accepts(plugin.Shape{Caps: plugin.Caps(plugin.Streamable)}); err != nil {
		t.Errorf("a walkable stack was refused: %v", err)
	}
}

// A field named twice in one comma list asks for the same thing twice, and
// validation says so — which is where it belongs, since the rule is meaningless
// for every option that names fields rather than for this layer's two.
func TestAFieldNamedTwiceInOneOption(t *testing.T) {
	for _, directive := range []string{
		"//forge:collection sort=Name,Name",
		"//forge:collection index=Name,Name",
	} {
		t.Run(directive, func(t *testing.T) {
			_, diags := options.Read(options.Declaration{
				Pos:        declaredAt,
				Directives: []discover.Directive{directive2(directive)},
				Stack:      stacked(),
				Subject:    person(t),
			}, layers.Builtins())

			reported := diags.Render()
			if reported == "" {
				t.Fatal("a field named twice was accepted without a word")
			}
			if !strings.Contains(reported, "FRG3016") || !strings.Contains(reported, "Name") {
				t.Errorf("the report does not say a field was named twice:\n%s", reported)
			}
		})
	}
}

// An unexported field cannot be read by generated code that lands in another
// package, and a method called Bysecret is not a name anybody would write even
// where it compiles. Projections already stop at the export boundary; the
// options have to as well.
func TestAnUnexportedFieldNamedByAnOption(t *testing.T) {
	for _, directive := range []string{
		"//forge:collection sort=secret",
		"//forge:collection index=secret",
	} {
		t.Run(directive, func(t *testing.T) {
			_, err := collection.New().Generate(declaration(t, directive), plugin.Shape{})
			if err == nil {
				t.Fatal("an unexported field was generated from")
			}

			reported, ok := plugin.From(err)
			if !ok {
				t.Fatalf("the error %v is not a diagnostic", err)
			}
			if got, want := reported.Code.String(), "FRG3015"; got != want {
				t.Errorf("code is %s, want %s", got, want)
			}
			if !strings.Contains(reported.Message, "secret") {
				t.Errorf("the message %q does not name the field", reported.Message)
			}
		})
	}
}

// Two fields from packages of one name cannot both be spelled by it, and a
// field from a package the template already imports joins that import rather
// than being aliased away from it. Spelling each field on its own is how a file
// ends up importing one path twice, or two paths once each under one name.
func TestFieldsFromPackagesThatWouldClash(t *testing.T) {
	subject := person(t)
	subject.Fields = append(subject.Fields,
		held("First", foreign("example.com/a/util", "util", "One")),
		held("Second", foreign("example.com/b/util", "util", "Two")),

		// A field of a package the template itself imports, which must not be
		// moved out of the way of an import the file is going to have anyway.
		held("Walk", foreign("iter", "iter", "Seq")),
	)

	ctx := declaration(t)
	ctx.Model.Subject = subject

	unit, err := collection.New().Generate(ctx, plugin.Shape{})
	if err != nil {
		t.Fatalf("the layer refused: %v", err)
	}

	// One name each, and one binding each. An import carries a name only where
	// the spelling had to invent one, so the rest bind their package's own —
	// which for every fixture here is the last element of the path, because
	// that is how they are built.
	bound := map[string]string{}
	paths := map[string]string{}

	for _, one := range unit.Imports {
		name := one.Name
		if name == "" {
			name = path.Base(one.Path)
		}

		if was, twice := bound[name]; twice {
			t.Errorf("%s and %s are both bound to %q", was, one.Path, name)
		}
		if was, twice := paths[one.Path]; twice {
			t.Errorf("%s is imported as %q and as %q", one.Path, was, name)
		}
		bound[name], paths[one.Path] = one.Path, name
	}

	// And the template's own import is joined rather than aliased, since the
	// storage layer beneath binds it under its own name.
	for _, one := range unit.Imports {
		if one.Path == "iter" && one.Aliased {
			t.Errorf("iter was bound as %q, and the layers beneath bind it as iter", one.Name)
		}
	}
}

// foreign builds a named type in a package of its own, which is what a field
// whose type comes from elsewhere is.
func foreign(path, name, typeName string) types.Type {
	pkg := types.NewPackage(path, name)
	obj := types.NewTypeName(token.NoPos, pkg, typeName, nil)

	return types.NewNamed(obj, types.NewStruct(nil, nil), nil)
}

// A name a layer beneath already put on the declared type is one this layer may
// not generate again, whatever produced it.
func TestANameTheStorageBeneathAlreadyHas(t *testing.T) {
	subject := person(t)
	subject.Fields = append(subject.Fields, held("Len", types.Typ[types.Int]))

	ctx := declaration(t)
	ctx.Model.Subject = subject

	beneath := plugin.Shape{Caps: plugin.Caps(plugin.Streamable)}.
		WithMethods(plugin.Method{Name: "Lens"})

	_, err := collection.New().Generate(ctx, beneath)
	if err == nil {
		t.Fatal("a method the storage beneath already declared was generated again")
	}

	reported, _ := plugin.From(err)
	if got, want := reported.Code.String(), "FRG4101"; got != want {
		t.Errorf("code is %s, want %s", got, want)
	}
	if !strings.Contains(reported.Message, "Lens") {
		t.Errorf("the message %q does not name the method", reported.Message)
	}
}

// One declared sort key is an order the type itself is in, and several are not.
//
// With one there is a natural order and sort.Sort can be handed the collection.
// With several there are several and no reason to prefer any of them, so what
// would have to be chosen is somebody's data order picked because they named a
// field first. The sorted views are generated either way; what is missing from
// the second case is only the unqualified order.
func TestWhenACollectionIsSortableInPlace(t *testing.T) {
	cases := map[string]struct {
		directive string
		sortable  bool
	}{
		"one sort key":  {"//forge:collection sort=Name", true},
		"two sort keys": {"//forge:collection sort=Name,ID", false},
		"no sort key":   {"//forge:collection index=ID", false},
	}

	for name, one := range cases {
		t.Run(name, func(t *testing.T) {
			held := string(generated(t, declaration(t, one.directive)))

			for _, want := range []string{
				"func (c Persons) Less(i, j int) bool",
				"func (c Persons) Swap(i, j int)",
			} {
				if got := strings.Contains(held, want); got != one.sortable {
					t.Errorf("%s is %v, want %v:\n%s", want, got, one.sortable, held)
				}
			}

			// The views are there whichever way it went, so a missing order is
			// read as one thing missing rather than as the option being ignored.
			if !strings.Contains(held, "SortedBy") && one.directive != "//forge:collection index=ID" {
				t.Errorf("the sorted views went with the order:\n%s", held)
			}
		})
	}
}

// The order is the declared key's, and swapping is the language's.
func TestWhatSortingInPlaceCompares(t *testing.T) {
	held := string(generated(t, declaration(t, "//forge:collection sort=Name")))

	for _, want := range []string{
		"return c[i].Name < c[j].Name",
		"c[i], c[j] = c[j], c[i]",
	} {
		if !strings.Contains(held, want) {
			t.Errorf("the ordering does not hold %q:\n%s", want, held)
		}
	}

	// And both arrive documented. A generated method that ships bare is a
	// method a reader has to work out from its body, in a file they did not
	// write — and the way to ship one is to build it so that its comment has
	// nowhere to be printed, which nothing but reading the output would catch.
	for _, want := range []string{
		"// Less reports whether the element at i sorts before the one at j, by Name.",
		"// Swap exchanges the elements at i and j",
	} {
		if !strings.Contains(held, want) {
			t.Errorf("the ordering is not documented with %q:\n%s", want, held)
		}
	}
}
