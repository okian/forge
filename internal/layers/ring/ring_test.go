package ring_test

import (
	"bytes"
	"go/token"
	"go/types"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"

	"github.com/okian/forge/internal/emit"
	"github.com/okian/forge/internal/goldentest"
	"github.com/okian/forge/internal/layer"
	"github.com/okian/forge/internal/layers/ring"
	"github.com/okian/forge/internal/merge"
	"github.com/okian/forge/internal/model"
	"github.com/okian/forge/plugin"
)

// where the declaration these tests generate for was written, and the package
// it was written in. Generated code lands in that package, which is what
// decides whether the subject is spelled bare or qualified.
var (
	declaredAt = token.Position{Filename: "model/person.go", Line: 10, Column: 6}
	local      = "example.com/model"
)

// subjectSource is the fixture the generated output is compiled against. It is
// the whole of what the output needs: a storage layer stores what it is given
// and never reads it, so two fields are two fields.
const subjectSource = "package model\n\n" +
	"type Person struct {\n\tName string\n\tAge  int\n}\n"

// person is the subject the declarations below are specialised to.
func person(pkgPath, pkgName string) *plugin.Struct {
	pkg := types.NewPackage(pkgPath, pkgName)
	obj := types.NewTypeName(token.NoPos, pkg, "Person", nil)

	return &plugin.Struct{
		Named: types.NewNamed(obj, types.NewStruct(nil, nil), nil),
		Fields: []plugin.Field{
			{Name: "Name", Exported: true},
			{Name: "Age", Exported: true},
		},
	}
}

// declaration builds what the layer is asked to generate against, the way the
// pipeline builds it, with the options an author would have written.
//
// The form is always the spec one. A ring's representation does not uphold its
// own invariants, so the layer says it is not transparent and composition
// refuses it anywhere else — a test generating an inline one would be testing
// something no run can produce.
func declaration(name string, written ...string) *plugin.Context {
	entries := make([]model.Option, 0, len(written))
	for _, one := range written {
		key, value, _ := strings.Cut(one, "=")
		entries = append(entries, model.Option{Key: key, Value: value})
	}

	ctx := &plugin.Context{
		Model: &plugin.Model{
			Name:    name,
			Form:    plugin.FormSpec,
			Subject: person(local, "model"),
			Stack:   []plugin.LayerRef{{Origin: ring.New().Origin(), Kind: plugin.KindStorage}},
			Pkg:     &packages.Package{PkgPath: local},
			Pos:     declaredAt,
		},
		Options: plugin.Options{Layer: "ring", Entries: entries, Pos: declaredAt},
	}

	// What the file will bind, which generation works out from the whole stack
	// and hands to every layer in it. A stack of this layer alone binds what
	// this layer binds — but it has to be said rather than assumed, since a
	// layer given none spells against nothing and would write a subject from a
	// package called errors under the name the template already has.
	return ctx.Binding(ring.New().Binds())
}

// generate asks the layer for its unit, failing the test if it refuses.
func generate(t *testing.T, ctx *plugin.Context) plugin.Unit {
	t.Helper()

	unit, err := ring.New().Generate(ctx, plugin.Shape{})
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
		Decl:     ctx.Model.Name,
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
// it was generated for.
func compiles(t *testing.T, out []byte) {
	t.Helper()

	goldentest.Check(t, goldentest.Package{
		Path: "model",
		Files: []goldentest.Source{
			{Name: "person.go", Content: []byte(subjectSource)},
			{Name: "zz_forge_persons.go", Content: out, Generated: true},
		},
	})
}

// A declaration that did not say how big it is takes that from its caller, and
// the whole of what it generates compiles.
func TestACapacityTheCallerDecides(t *testing.T) {
	out := generated(t, declaration("Persons"))

	for _, want := range []string{
		"type Persons struct {",
		"func NewPersons(size int) *Persons {",
		"func (r *Persons) Push(v Person)",
	} {
		if !bytes.Contains(out, []byte(want)) {
			t.Errorf("the output does not hold %q:\n%s", want, out)
		}
	}

	// The constant belongs to the other answer, and its constructor with it.
	// Either left behind is a name in somebody's package that nothing uses.
	for _, unwanted := range []string{"personsFixedCap", "NewFixed"} {
		if bytes.Contains(out, []byte(unwanted)) {
			t.Errorf("the output holds %q, which belongs to the fixed form:\n%s", unwanted, out)
		}
	}

	compiles(t, out)
}

// A declaration that said how big it is has that written into it as a constant,
// and its constructor takes nothing.
//
// The number is written rather than referred to, so that reading the generated
// file answers the question the directive asked.
func TestACapacityTheDeclarationFixes(t *testing.T) {
	out := generated(t, declaration("Persons", "cap=1024"))

	for _, want := range []string{
		"personsFixedCap = 1024",
		"func NewPersons() *Persons {",
	} {
		if !bytes.Contains(out, []byte(want)) {
			t.Errorf("the output does not hold %q:\n%s", want, out)
		}
	}

	if bytes.Contains(out, []byte("func NewPersons(size")) {
		t.Errorf("a container whose size is fixed still asks for one:\n%s", out)
	}

	compiles(t, out)
}

// The overwriting policy is what a declaration gets when it says nothing, and
// the methods that add elements return nothing.
func TestTheOverwritingPolicy(t *testing.T) {
	for _, ctx := range []*plugin.Context{declaration("Persons"), declaration("Persons", "overflow=overwrite")} {
		out := generated(t, ctx)

		if !bytes.Contains(out, []byte("func (r *Persons) Push(v Person) {")) {
			t.Errorf("the output does not push without an error:\n%s", out)
		}

		// The refusing half and everything only it needed. An error nothing
		// returns is a variable somebody's linter reports.
		for _, unwanted := range []string{"Checked", "ErrPersonsFull", `"errors"`} {
			if bytes.Contains(out, []byte(unwanted)) {
				t.Errorf("the output holds %q, which belongs to the refusing form:\n%s", unwanted, out)
			}
		}

		compiles(t, out)
	}
}

// The refusing policy returns an error from both methods that add elements, and
// names it after the declaration so a caller can match it.
//
// The names are the contract's rather than the template's: a caller writes Push
// whichever policy the declaration chose, and finds out which by whether there
// is an error to handle.
func TestTheRefusingPolicy(t *testing.T) {
	out := generated(t, declaration("Persons", "overflow=error"))

	for _, want := range []string{
		"func (r *Persons) Push(v Person) error {",
		"func (r *Persons) AppendSeq(seq iter.Seq[Person]) error {",
		"ErrPersonsFull = errors.New(",
	} {
		if !bytes.Contains(out, []byte(want)) {
			t.Errorf("the output does not hold %q:\n%s", want, out)
		}
	}

	// The template's own names for the methods it chose between. One reaching a
	// file would be a second way to push, spelled the way forge happened to
	// write its template.
	for _, unwanted := range []string{"PushChecked", "AppendSeqChecked"} {
		if bytes.Contains(out, []byte(unwanted)) {
			t.Errorf("the output holds the template's name %q:\n%s", unwanted, out)
		}
	}

	compiles(t, out)
}

// A capacity that could hold nothing is refused, at the declaration that fixed
// it rather than at the first push.
func TestACapacityThatHoldsNothing(t *testing.T) {
	for _, written := range []string{"cap=0", "cap=-8"} {
		_, err := ring.New().Generate(declaration("Persons", written), plugin.Shape{})

		if err == nil {
			t.Fatalf("%s was generated for", written)
		}
		if said := err.Error(); !strings.Contains(said, "FRG3017") {
			t.Errorf("%s was refused as %q", written, said)
		}
	}
}

// The layer says its representation is not the author's to write, which is what
// sends a declaration over it to a spec file.
//
// A buffer, a head and a count are only meaningful together, and the language
// offers no way to stop somebody writing a value where they are not. Saying so
// is the whole of how that is enforced: nothing else in the pipeline knows what
// the three fields mean.
func TestTheRepresentationIsNotTheAuthorsToWrite(t *testing.T) {
	if layer.TransparentLayer(ring.New()) {
		t.Error("a ring reports that any value of its underlying type is a valid one")
	}
}

// What the layer tells the layers above it matches what it emits.
//
// The two are written separately — the surface is described for a shape and the
// bodies come from a template — so nothing but a test holds them together. A
// layer above writes its calls against the description, and a description that
// promised a method the file does not have is a package that does not compile.
func TestTheSurfaceMatchesWhatIsEmitted(t *testing.T) {
	cases := map[string][]string{
		"overwriting":           nil,
		"refusing":              {"overflow=error"},
		"fixed and overwriting": {"cap=64"},

		// Both options answered the way that is not the default, which is the
		// only combination where every drop, both renames and the capacity are
		// decided at once.
		"fixed and refusing": {"cap=64", "overflow=error"},
	}

	for name, written := range cases {
		t.Run(name, func(t *testing.T) {
			ctx := declaration("Persons", written...)
			out := generated(t, ctx)

			// Asked against the element the stack beneath carries, which is
			// what a layer above would ask against: a shape with none has the
			// layer spell its own type parameter, and no file holds that.
			beneath := plugin.Shape{Elem: plugin.TypeRef{Pkg: local, Name: "Person"}}

			for _, method := range ring.New().Shape(ctx, beneath).Surface {
				want := "func (r *Persons) " + method.Name + method.Signature
				if !bytes.Contains(out, []byte(want)) {
					t.Errorf("the surface promises %q, and the file has no such method:\n%s", want, out)
				}
			}
		})
	}
}

// The methods a walk is made of are on the pointer, and the surface says so.
//
// A ring is a struct rather than a slice, so a method that reads it still takes
// a pointer — copying one to read it would copy the buffer header and be a
// second container sharing the first's elements. A layer above wrapping these
// has to know that before it writes the call.
func TestEveryMethodTakesAPointer(t *testing.T) {
	for _, method := range ring.New().Shape(declaration("Persons"), plugin.Shape{}).Surface {
		if !method.Pointer {
			t.Errorf("%s is described as reachable on a value", method.Name)
		}
	}
}

// Both options answered at once, since each decides two declarations and the
// four answers have to agree about what is left.
func TestACapacityAndAPolicyTogether(t *testing.T) {
	out := generated(t, declaration("Persons", "cap=64", "overflow=error"))

	for _, want := range []string{
		"personsFixedCap = 64",
		"func NewPersons() *Persons {",
		"func (r *Persons) Push(v Person) error {",
		"func (r *Persons) AppendSeq(seq iter.Seq[Person]) error {",
		"ErrPersonsFull",
	} {
		if !bytes.Contains(out, []byte(want)) {
			t.Errorf("the output does not hold %q:\n%s", want, out)
		}
	}

	for _, unwanted := range []string{"NewFixed", "Checked", "func NewPersons(size"} {
		if bytes.Contains(out, []byte(unwanted)) {
			t.Errorf("the output holds %q, which this combination does not choose:\n%s", unwanted, out)
		}
	}

	compiles(t, out)
}

// A capacity larger than a 32-bit int is refused, because the constant it
// becomes is one a smaller word cannot hold.
//
// The output is committed and built wherever the module is, so a number that
// only compiles where forge ran is a file that does not compile there.
func TestACapacityBiggerThanEveryPlatform(t *testing.T) {
	_, err := ring.New().Generate(declaration("Persons", "cap=2147483648"), plugin.Shape{})

	if err == nil {
		t.Fatal("a capacity beyond a 32-bit int was generated for")
	}
	if said := err.Error(); !strings.Contains(said, "FRG3017") {
		t.Errorf("it was refused as %q", said)
	}
}

// A refused capacity points at the number rather than at the directive holding
// it, so the caret is under what has to change.
func TestWhereARefusedCapacityPoints(t *testing.T) {
	ctx := declaration("Persons", "cap=0")
	ctx.Options.Entries[0].Pos = token.Position{Filename: "model/person.go", Line: 9, Column: 14}

	_, err := ring.New().Generate(ctx, plugin.Shape{})
	if err == nil {
		t.Fatal("a capacity of nothing was generated for")
	}

	if said := err.Error(); !strings.Contains(said, "9:14") {
		t.Errorf("the report points somewhere other than the option: %s", said)
	}
}

// What an unwritten policy means is the default the schema declares.
//
// The two are decided in different places — the schema says what an author who
// writes nothing gets, and generation has to act on it before anything has
// filled it in — so nothing but this holds them together. Flipping either alone
// would generate one policy and document the other.
func TestTheUnwrittenPolicyIsTheDeclaredDefault(t *testing.T) {
	var declared string
	for _, def := range ring.New().OptionSchema() {
		if def.Key == "overflow" {
			declared = def.Default
		}
	}
	if declared == "" {
		t.Fatal("the schema declares no default for overflow")
	}

	unwritten := generated(t, declaration("Persons"))
	written := generated(t, declaration("Persons", "overflow="+declared))

	if !bytes.Equal(unwritten, written) {
		t.Errorf("writing overflow=%s generates something other than writing nothing", declared)
	}
}

// A subject from a package named like one the template imports is bound to a
// name of its own and spelled by that.
//
// This layer's own name for it is errors, which a full ring reaches for: the
// error a push into one returns is declared with it. A subject from a package
// of that name, spelled bare, would leave the file importing two things under
// one name — in generated code the author cannot edit, over a collision they
// caused by naming a package errors.
//
// What the subject is moved out of the way of is what the whole package's files
// will bind, which reaches the layer through the context rather than being
// looked up here: see [plugin.Context.Bound]. A stack of this layer alone binds
// what this layer binds, which is what the helper hands over.
//
// The refusing policy, because that is the one that reaches for errors: it
// declares the error a full ring returns. Under the other the template imports
// nothing of the name and the subject would keep it, which is correct and is
// not what this is about.
func TestASubjectWhosePackageNameIsAlreadyTaken(t *testing.T) {
	ctx := declaration("Persons", "overflow=error")
	ctx.Model.Subject = person("example.com/util/errors", "errors")

	out := generated(t, ctx)

	for _, want := range []string{
		`errors2 "example.com/util/errors"`,
		"errors2.Person",

		// And the template's own is still spelled bare, since moving the
		// subject aside is the whole of what leaves that name free.
		"errors.New(",
	} {
		if !bytes.Contains(out, []byte(want)) {
			t.Errorf("the output does not hold %q:\n%s", want, out)
		}
	}
}
