package index_test

import (
	"bytes"
	"go/token"
	"go/types"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"

	"github.com/okian/forge/internal/discover"
	"github.com/okian/forge/internal/emit"
	"github.com/okian/forge/internal/goldentest"
	"github.com/okian/forge/internal/layer"
	"github.com/okian/forge/internal/layers"
	"github.com/okian/forge/internal/layers/index"
	"github.com/okian/forge/internal/merge"
	"github.com/okian/forge/internal/options"
	"github.com/okian/forge/plugin"
)

// The declaration these tests generate for, and where it was written.
const local = "example.com/model"

var declaredAt = token.Position{Filename: "model/spec.go", Line: 12, Column: 6}

// subjectSource is the fixture the generated output is compiled against: a
// subject with a field of every shape the layer has an answer for, and the
// ones it has to refuse.
//
// Id beside ID is deliberate. The two are distinct fields and one generated
// name, which is the collision the layer has to catch rather than emit.
const subjectSource = "package model\n\n" +
	"import \"time\"\n\n" +
	"// Person is what the container holds.\n" +
	"type Person struct {\n" +
	"\tID     int\n" +
	"\tId     int\n" +
	"\tName   string\n" +
	"\tEmail  string\n" +
	"\tJoined time.Time\n" +
	"\tTags   []string\n" +
	"\tsecret string\n" +
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
			held("Id", types.Typ[types.Int]),
			held("Name", types.Typ[types.String]),
			held("Email", types.Typ[types.String]),
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

// declaration builds what the layer is asked to generate against, the way the
// pipeline builds it: the directive is taken apart and validated, so what the
// layer reads has been through the same checks a run applies.
//
// The form is always the spec one. The maps have to agree with the order, so
// the layer says its representation is not the author's to write and
// composition refuses it anywhere else.
func declaration(t *testing.T, directives ...string) *plugin.Context {
	t.Helper()

	set, diags := read(t, directives...)
	if !diags.Empty() {
		t.Fatalf("the directives were refused:\n%s", diags.Render())
	}

	subject := person(t)

	ctx := &plugin.Context{
		Model: &plugin.Model{
			Name: "Directory", Form: plugin.FormSpec, Subject: subject, Stack: stacked(),
			Options: set,
			Pkg:     &packages.Package{PkgPath: local},
			Pos:     declaredAt,
		},
	}
	if len(set) > 0 {
		ctx.Options = set[0]
	}

	// What the file will bind, which generation works out from the whole
	// stack and hands to every layer in it. A stack of this layer alone binds
	// what this layer binds — but it has to be said rather than assumed,
	// since a layer given none spells against nothing and would write a
	// subject from a package called errors under the name the template
	// already has.
	return ctx.Binding(index.New().Binds())
}

// read routes directives through validation, the way a run does.
func read(t *testing.T, directives ...string) ([]plugin.Options, interface {
	Empty() bool
	Render() string
},
) {
	t.Helper()

	written := make([]discover.Directive, len(directives))
	for i, text := range directives {
		written[i] = directive(text, declaredAt.Line-len(directives)+i)
	}

	set, diags := options.Read(options.Declaration{
		Pos: declaredAt, Directives: written, Stack: stacked(), Subject: person(t),
	}, layers.Builtins())

	return set, &diags
}

// stacked is the declaration's layers: this storage alone.
func stacked() []plugin.LayerRef {
	return []plugin.LayerRef{{Origin: index.New().Origin(), Kind: plugin.KindStorage}}
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

// generate asks the layer for its unit, failing the test if it refuses.
func generate(t *testing.T, ctx *plugin.Context) plugin.Unit {
	t.Helper()

	unit, err := index.New().Generate(ctx, plugin.Shape{})
	if err != nil {
		t.Fatalf("the layer refused to generate: %v", err)
	}
	return unit
}

// generated renders a unit as the file it will be written to, through the same
// merge and emit steps generation uses.
func generated(t *testing.T, ctx *plugin.Context) []byte {
	t.Helper()

	merged := merge.Units(generate(t, ctx))

	out, err := emit.File{
		Package:  "model",
		Pos:      ctx.Model.Pos,
		Imports:  merged.Imports,
		Sections: merged.Sections,
	}.Render()
	if err != nil {
		t.Fatalf("rendering the file: %v", err)
	}
	return out
}

// compiles checks that what was generated is a package, alongside the subject
// it was generated for, and records it as the golden copy.
func compiles(t *testing.T, out []byte) {
	t.Helper()

	goldentest.Check(t, goldentest.Package{
		Path: "model",
		Files: []goldentest.Source{
			{Name: "person.go", Content: []byte(subjectSource)},
			{Name: "forge.gen.go", Content: out, Generated: true},
		},
	})
}

// refusedAs asks the layer to generate expecting a refusal, and returns what
// it said.
func refusedAs(t *testing.T, code string, directives ...string) string {
	t.Helper()

	_, err := index.New().Generate(declaration(t, directives...), plugin.Shape{})
	if err == nil {
		t.Fatalf("%q was generated for", strings.Join(directives, " "))
	}
	if said := err.Error(); !strings.Contains(said, code) {
		t.Fatalf("%q was refused as %q rather than %s", strings.Join(directives, " "), said, code)
	}
	return err.Error()
}

// A unique key, which is the whole default: the constructor and the append
// refuse a held key, the lookup answers a stable pointer, and removal is by
// the key.
func TestAUniqueKey(t *testing.T) {
	out := generated(t, declaration(t, "//forge:index key=ID"))

	for _, want := range []string{
		"type Directory struct {",
		"byID  map[int]*directoryEntry",
		"func NewDirectory(elems ...Person) *Directory {",
		"func (r *Directory) ByID(k int) (*Person, bool) {",
		"func (r *Directory) Remove(k int) bool {",
		"func (r *Directory) AppendSeq(seq iter.Seq[Person]) error {",
		"ErrDirectoryDuplicate = errors.New(",
		"func (r *Directory) placeChecked(v Person) error {",
	} {
		if !bytes.Contains(out, []byte(want)) {
			t.Errorf("the output does not hold %q:\n%s", want, out)
		}
	}

	// The template's own names for what it chose between, and the helpers
	// only other arrangements reach. Any of them in a file is a second way in,
	// spelled the way forge happened to write its template. Matched as
	// declarations, since the plain words appear in prose.
	for _, unwanted := range []string{
		"NewChecked", "AppendSeqChecked",
		") spread(", ") grouped[", ") found[", ") listed[", ") delisted[",
	} {
		if bytes.Contains(out, []byte(unwanted)) {
			t.Errorf("the output holds %q, which this arrangement does not choose:\n%s", unwanted, out)
		}
	}

	compiles(t, out)
}

// Secondary lookups walk their value through the primary map, and removal
// repairs exactly the buckets the element was filed in.
func TestSecondaryLookups(t *testing.T) {
	out := generated(t, declaration(t, "//forge:index key=ID index=Name,Email"))

	for _, want := range []string{
		"byName  map[string][]int",
		"byEmail map[string][]int",
		"func (r *Directory) ByName(k string) iter.Seq[Person] {",
		"func (r *Directory) ByEmail(k string) iter.Seq[Person] {",
		"r.found(r.byName[k], r.byID)",
		"r.byName = r.listed(r.byName, v.Name, v.ID)",
		"r.byName = r.delisted(r.byName, e.elem.Name, k)",
	} {
		if !bytes.Contains(out, []byte(want)) {
			t.Errorf("the output does not hold %q:\n%s", want, out)
		}
	}

	compiles(t, out)
}

// A key declared unique=false files every element sharing one in a bucket:
// the lookup walks, removal reports how many, and there is no add to refuse.
func TestAMultiValuedKey(t *testing.T) {
	out := generated(t, declaration(t, "//forge:index key=Name unique=false"))

	for _, want := range []string{
		"byName map[string][]*directoryEntry",
		"func (r *Directory) ByName(k string) iter.Seq[Person] {",
		"func (r *Directory) Remove(k string) int {",
		"func (r *Directory) AppendSeq(seq iter.Seq[Person]) {",
		"r.spread(r.byName[k])",
	} {
		if !bytes.Contains(out, []byte(want)) {
			t.Errorf("the output does not hold %q:\n%s", want, out)
		}
	}

	for _, unwanted := range []string{"ErrDirectory", `"errors"`, ") pick[", ") noted["} {
		if bytes.Contains(out, []byte(unwanted)) {
			t.Errorf("the output holds %q, which a key held many times has no use for:\n%s", unwanted, out)
		}
	}

	compiles(t, out)
}

// conflict=replace swaps the element in place: the entry stays where every
// lookup filed it, and the secondary buckets move from the old values to the
// new.
func TestReplacingAHeldKey(t *testing.T) {
	out := generated(t, declaration(t, "//forge:index key=ID conflict=replace index=Name"))

	for _, want := range []string{
		"func (r *Directory) AppendSeq(seq iter.Seq[Person]) {",
		"func (r *Directory) place(v Person) {",
		"r.byName = r.delisted(r.byName, e.elem.Name, v.ID)",
		"e.elem = v",
	} {
		if !bytes.Contains(out, []byte(want)) {
			t.Errorf("the output does not hold %q:\n%s", want, out)
		}
	}

	if bytes.Contains(out, []byte("ErrDirectory")) {
		t.Errorf("a replacing container declares a refusal:\n%s", out)
	}

	compiles(t, out)
}

// A key from another package is spelled and imported, which is the ordinary
// case wearing its clearest face: time.Time.
func TestAForeignKeyType(t *testing.T) {
	out := generated(t, declaration(t, "//forge:index key=Joined"))

	for _, want := range []string{
		`"time"`,
		"func (r *Directory) ByJoined(k time.Time) (*Person, bool) {",
		"byJoined map[time.Time]*directoryEntry",
	} {
		if !bytes.Contains(out, []byte(want)) {
			t.Errorf("the output does not hold %q:\n%s", want, out)
		}
	}

	compiles(t, out)
}

// What the layer refuses, and as what.
func TestWhatCannotBeGeneratedFrom(t *testing.T) {
	for name, ask := range map[string]struct {
		directive string
		code      string
	}{
		"a key that cannot be a map key":        {"//forge:index key=Tags", "FRG3037"},
		"a secondary that cannot be a map key":  {"//forge:index key=ID index=Tags", "FRG3037"},
		"an unexported key":                     {"//forge:index key=secret", "FRG3038"},
		"a secondary repeating the key":         {"//forge:index key=ID index=ID", "FRG3039"},
		"a policy for keys held many times":     {"//forge:index key=Name unique=false conflict=replace", "FRG3040"},
		"secondaries over keys held many times": {"//forge:index key=Name unique=false index=Email", "FRG3041"},
		"two lookups spelled into one name":     {"//forge:index key=ID index=Id", "FRG4103"},
	} {
		t.Run(name, func(t *testing.T) {
			refusedAs(t, ask.code, ask.directive)
		})
	}
}

// A declaration that names no key is refused by validation, before this layer
// is asked anything: the schema says the option is required.
func TestAMissingKeyIsRefusedBeforeGeneration(t *testing.T) {
	_, diags := read(t, "//forge:index unique=true")

	if diags.Empty() {
		t.Fatal("a declaration with no key was validated")
	}
	if said := diags.Render(); !strings.Contains(said, "FRG3011") {
		t.Errorf("it was refused as %q rather than FRG3011", said)
	}
}

// What the layer tells the layers above it matches what it emits.
//
// The two are written separately — the surface is described from the plan and
// the bodies come from the template and the builders — so nothing but a test
// holds them together. A layer above writes its calls against the
// description, and a description that promised a method the file does not
// have is a package that does not compile.
func TestTheSurfaceMatchesWhatIsEmitted(t *testing.T) {
	cases := map[string][]string{
		"unique":              {"//forge:index key=ID"},
		"unique with lookups": {"//forge:index key=ID index=Name,Email"},
		"replacing":           {"//forge:index key=ID conflict=replace index=Name"},
		"held many times":     {"//forge:index key=Name unique=false"},
		"a foreign key":       {"//forge:index key=Joined index=Name"},
	}

	for name, directives := range cases {
		t.Run(name, func(t *testing.T) {
			ctx := declaration(t, directives...)
			out := generated(t, ctx)

			beneath := plugin.Shape{Elem: plugin.TypeRef{Pkg: local, Name: "Person"}}

			surface := index.New().Shape(ctx, beneath).Surface
			if len(surface) == 0 {
				t.Fatal("the layer describes no surface")
			}

			for _, method := range surface {
				want := "func (r *Directory) " + method.Name + method.Signature
				if !bytes.Contains(out, []byte(want)) {
					t.Errorf("the surface promises %q, and the file has no such method:\n%s", want, out)
				}
			}
		})
	}
}

// The methods are on the pointer, and the surface says so.
//
// The container is a struct holding a slice and its maps, so a method that
// reads it still takes a pointer — copying one to read it would copy the
// order's header and be a second container sharing the first's entries. A
// layer above wrapping these has to know that before it writes the call.
func TestEveryMethodTakesAPointer(t *testing.T) {
	for _, method := range index.New().Shape(declaration(t, "//forge:index key=ID"), plugin.Shape{}).Surface {
		if !method.Pointer {
			t.Errorf("%s is described as reachable on a value", method.Name)
		}
	}
}

// The layer says its representation is not the author's to write, which is
// what sends a declaration over it to a spec file.
func TestTheRepresentationIsNotTheAuthorsToWrite(t *testing.T) {
	if layer.TransparentLayer(index.New()) {
		t.Error("an index reports that any value of its underlying type is a valid one")
	}
}

// Generating twice from one declaration is the same bytes, which is what
// catches a map walked without an order somewhere in the layer.
func TestGeneratingTwiceIsTheSameBytes(t *testing.T) {
	first := generated(t, declaration(t, "//forge:index key=ID index=Name,Email"))
	second := generated(t, declaration(t, "//forge:index key=ID index=Name,Email"))

	if !bytes.Equal(first, second) {
		t.Error("one declaration generated two different files")
	}
}

// What an unwritten option means is the default the schema declares.
//
// The defaults are decided in the schema and acted on in the plan, so nothing
// but this holds them together: flipping either alone would generate one
// arrangement and document the other.
func TestTheUnwrittenOptionsAreTheDeclaredDefaults(t *testing.T) {
	unwritten := generated(t, declaration(t, "//forge:index key=ID"))

	for _, spelledOut := range []string{
		"//forge:index key=ID unique=true",
		"//forge:index key=ID conflict=error",
	} {
		if written := generated(t, declaration(t, spelledOut)); !bytes.Equal(unwritten, written) {
			t.Errorf("writing %q generates something other than writing nothing", spelledOut)
		}
	}
}
