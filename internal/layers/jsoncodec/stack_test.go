package jsoncodec_test

import (
	"go/token"
	"go/types"
	"strings"
	"testing"

	"github.com/okian/forge/internal/diag"
	"github.com/okian/forge/internal/emit"
	"github.com/okian/forge/internal/layer"
	"github.com/okian/forge/internal/layers/jsoncodec"
	"github.com/okian/forge/internal/model"
	"github.com/okian/forge/internal/shape"
	"github.com/okian/forge/internal/subject"
)

// declaredName is what the container these tests generate for is called, and
// storage is the marker the container's methods are attributed to.
//
// A real marker rather than an invented one, because a diagnostic about a
// method names the layer that offers it and the name an author reads has to be
// one they could go and look up.
const declaredName = "Persons"

var storage = model.TypeRef{Pkg: model.MarkerPkg, Name: "Slice"}

// The three methods of the streaming contract, in the shapes the contract gives
// them, as a container beneath the codec would offer them.
func walks(pointer bool) shape.Method {
	return shape.Method{Name: "All", Signature: "() iter.Seq[Person]", Owner: storage, Pointer: pointer}
}

func appends(refuses bool) shape.Method {
	signature := "(seq iter.Seq[Person])"
	if refuses {
		signature += " error"
	}
	return shape.Method{Name: "AppendSeq", Signature: signature, Owner: storage, Pointer: true}
}

func resets() shape.Method {
	return shape.Method{Name: "Reset", Signature: "()", Owner: storage, Pointer: true}
}

// exposing returns the shape a stack offering these methods exposes.
func exposing(methods ...shape.Method) shape.Shape {
	return shape.Shape{Caps: shape.Set(shape.Streamable)}.WithMethods(methods...)
}

// A stack that can be walked and filled gets the whole of the byte-level
// contract on the declared type, not only the pair the standard library
// dispatches to.
//
// All four are checked because they are one API: a caller who can marshal a
// container and cannot write one to a file has the half that needs a buffer
// nobody asked them to allocate.
func TestAStackThatCanBeWalkedGetsACodecOfItsOwn(t *testing.T) {
	written := source(t, declaring(t, modelPkg, "Scalars",
		exposing(walks(false), appends(false), resets())))

	for _, want := range []string{
		"func (c Persons) MarshalJSONTo(enc *jsontext.Encoder) error {",
		"func (c Persons) WriteTo(w io.Writer) (int64, error) {",
		"func (c *Persons) UnmarshalJSONFrom(dec *jsontext.Decoder) error {",
		"func (c *Persons) ReadFrom(r io.Reader) (int64, error) {",
	} {
		if !strings.Contains(written, want) {
			t.Errorf("the container's codec has no %q:\n%s", want, written)
		}
	}
}

// The elements are walked and written one at a time, rather than collected into
// something the encoder is then handed.
//
// It is the whole point of the layer composing with a container: a stack of a
// million elements costs the encoder's buffer, and a stack that materialised
// first would cost the elements twice.
func TestTheWholeStackIsWrittenInOnePass(t *testing.T) {
	written := source(t, declaring(t, modelPkg, "Scalars",
		exposing(walks(false), appends(false), resets())))

	for _, want := range []string{
		"enc.WriteToken(jsontext.BeginArray)",
		"for v := range c.All() {",
		"encodeModelScalarsJSONTo(enc, v)",
		"enc.WriteToken(jsontext.EndArray)",
	} {
		if !strings.Contains(written, want) {
			t.Errorf("the encoder does not %q:\n%s", want, written)
		}
	}

	if strings.Contains(written, "slices.Collect") || strings.Contains(written, "append(") {
		t.Errorf("the encoder gathers the elements before writing them:\n%s", written)
	}
}

// Reading empties the container first, so that reading into one twice leaves
// the second document rather than both.
func TestReadingReplacesWhatTheContainerHeld(t *testing.T) {
	written := source(t, declaring(t, modelPkg, "Scalars",
		exposing(walks(false), appends(false), resets())))

	if !strings.Contains(written, "c.Reset()") {
		t.Errorf("reading does not empty the container first:\n%s", written)
	}
	if !strings.Contains(written, "c.AppendSeq(func(yield func(Scalars) bool) {") {
		t.Errorf("reading does not hand the elements over as a sequence:\n%s", written)
	}
}

// A container that reports a refusal rather than dropping its oldest element
// has that refusal read and returned, which is the whole of what the option
// bought.
func TestAContainerThatRefusesElementsIsHeard(t *testing.T) {
	written := source(t, declaring(t, modelPkg, "Scalars",
		exposing(walks(true), appends(true), resets())))

	if !strings.Contains(written, "refused := c.AppendSeq(") {
		t.Errorf("the refusal is not read:\n%s", written)
	}
	if !strings.Contains(written, "return refused") {
		t.Errorf("the refusal is read and not returned:\n%s", written)
	}
}

// The writing half takes the receiver the walk takes.
//
// A container whose methods take a pointer is one whose values are held by
// pointer, and a codec that copied it would be the one method in the file that
// did — which for a container carrying a lock is not a style question.
func TestTheWritingHalfFollowsTheWalk(t *testing.T) {
	byValue := source(t, declaring(t, modelPkg, "Scalars", exposing(walks(false))))
	if !strings.Contains(byValue, "func (c Persons) MarshalJSONTo") {
		t.Errorf("a walk on the value produced a codec on the pointer:\n%s", byValue)
	}

	byPointer := source(t, declaring(t, modelPkg, "Scalars", exposing(walks(true))))
	if !strings.Contains(byPointer, "func (c *Persons) MarshalJSONTo") {
		t.Errorf("a walk on the pointer produced a codec on the value:\n%s", byPointer)
	}
}

// A stack offering a walk and no way to be filled gets the half it can have.
//
// Half is better than neither: a container built by its own API and only ever
// written out is a real thing, and refusing it would make the layer unusable
// over one.
func TestAStackThatCannotBeFilledIsOnlyWritten(t *testing.T) {
	written := source(t, declaring(t, modelPkg, "Scalars", exposing(walks(false))))

	if !strings.Contains(written, "func (c Persons) MarshalJSONTo") {
		t.Errorf("a stack that can be walked was not given a writer:\n%s", written)
	}
	if strings.Contains(written, "UnmarshalJSONFrom(dec *jsontext.Decoder)") {
		t.Errorf("a stack with no sink was given a reader:\n%s", written)
	}
}

// A sink with no way to be emptied is not a sink a document can be read into,
// since reading would add to what was already there.
func TestAStackThatCannotBeEmptiedIsNotReadInto(t *testing.T) {
	written := source(t, declaring(t, modelPkg, "Scalars",
		exposing(walks(false), appends(false))))

	if strings.Contains(written, "UnmarshalJSONFrom(dec *jsontext.Decoder)") {
		t.Errorf("a container that cannot be emptied was read into:\n%s", written)
	}
}

// A stack exposing nothing — which is what a decorator that withdrew the walk
// leaves — gets no codec of its own, and the subject still gets its own.
//
// It is a decision rather than an omission. A lock hands out no sequence, so
// nothing may be written that walks one, and whatever replaces it belongs to
// the layer that took it away.
func TestAStackWithNoWalkGetsNoCodecOfItsOwn(t *testing.T) {
	unit := declaring(t, modelPkg, "Scalars", shape.Shape{})

	if len(unit.Decls) != 0 {
		t.Errorf("a stack with nothing to walk was given %d declarations", len(unit.Decls))
	}
	if len(unit.Provides) == 0 {
		t.Error("the subject lost its own codec along with the container's")
	}
}

// A subject that brought its own codec is called through it rather than through
// a function forge did not write.
//
// The container is where it would go wrong invisibly: the subject's own half
// correctly generates nothing, so a container calling the function that would
// have held it names something no file declares.
func TestASubjectWithItsOwnCodecIsCalledThroughIt(t *testing.T) {
	written := source(t, declaring(t, modelPkg, "Stamp",
		exposing(walks(false), appends(false), resets())))

	for _, want := range []string{"v.MarshalJSONTo(enc)", "v.UnmarshalJSONFrom(dec)"} {
		if !strings.Contains(written, want) {
			t.Errorf("the container does not call the subject's own codec (%q):\n%s", want, written)
		}
	}
	if strings.Contains(written, "encodeModelStampJSONTo") {
		t.Errorf("the container calls a function nothing declares:\n%s", written)
	}
}

// A codec a previous run wrote is written again rather than delegated to.
//
// It is the difference between a generator that can be run twice and one that
// can be run once. Generated files are loaded with the package they belong to,
// so the codec this layer wrote last time is a codec the subject declares as
// far as go/types is concerned — and a second run that took that for an author
// overriding it would stop writing the codec, leave the call-through function
// undeclared, and produce a package that names methods nothing has.
func TestACodecAPreviousRunWroteIsWrittenAgain(t *testing.T) {
	unit, err := regenerating(t, modelPkg, "Scalars", func(token.Pos) bool { return true },
		exposing(walks(false), appends(false), resets()))
	if err != nil {
		t.Fatalf("generating: %v", err)
	}

	if len(unit.Provides) == 0 {
		t.Fatal("the subject was left without a codec, because it already had the one this wrote")
	}

	if written := source(t, unit); !strings.Contains(written, "encodeModelScalarsJSONTo(enc, v)") {
		t.Errorf("the container delegates to a codec this run is not writing:\n%s", written)
	}
}

// A codec whose halves the subject declared on the pointer is still callable
// from the walk, because a loop variable has an address.
func TestACodecDeclaredOnThePointerIsStillCalled(t *testing.T) {
	written := source(t, declaring(t, modelPkg, "Weight",
		exposing(walks(false), appends(false), resets())))

	if !strings.Contains(written, "v.MarshalJSONTo(enc)") {
		t.Errorf("the container does not call the subject's own codec:\n%s", written)
	}
}

// A layer whose streaming methods are not the ones a codec is written against
// is reported rather than generated against, because what would come out is a
// file that does not compile in a package the author cannot edit.
func TestAMethodThatIsNotTheContractIsRefused(t *testing.T) {
	cases := map[string]struct {
		exposed shape.Shape
		says    string
	}{
		"a walk that returns nothing": {
			exposed: exposing(shape.Method{Name: "All", Signature: "()", Owner: storage}),
			says:    "All()",
		},
		"a walk that takes something": {
			exposed: exposing(shape.Method{Name: "All", Signature: "(n int) iter.Seq[Person]", Owner: storage}),
			says:    "All(n int)",
		},
		"a walk that is not a signature": {
			exposed: exposing(shape.Method{Name: "All", Signature: "not a signature", Owner: storage}),
			says:    "All",
		},
		"a sink that takes two sequences": {
			exposed: exposing(walks(false), resets(),
				shape.Method{Name: "AppendSeq", Signature: "(a, b iter.Seq[Person])", Owner: storage}),
			says: "AppendSeq(a, b iter.Seq[Person])",
		},
		"a sink that returns two values": {
			exposed: exposing(walks(false), resets(),
				shape.Method{Name: "AppendSeq", Signature: "(seq iter.Seq[Person]) (int, error)", Owner: storage}),
			says: "AppendSeq",
		},
		"an emptying that answers something": {
			exposed: exposing(walks(false), appends(false),
				shape.Method{Name: "Reset", Signature: "() error", Owner: storage}),
			says: "Reset() error",
		},
	}

	for name, want := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := generate(t, modelPkg, "Scalars", want.exposed)
			if err == nil {
				t.Fatal("a codec was written over a method that is not the contract")
			}

			reported, ok := diag.From(err)
			if !ok {
				t.Fatalf("%v is not a diagnostic", err)
			}
			if got := reported.Code.String(); got != "FRG4009" {
				t.Errorf("reported as %s, want FRG4009: %s", got, reported.Message)
			}
			for _, mention := range []string{declaredName, storage.Name, want.says} {
				if !strings.Contains(reported.Message, mention) {
					t.Errorf("the complaint does not mention %q:\n%s", mention, reported.Message)
				}
			}
			if !strings.Contains(reported.Hint, "fault in the layer") {
				t.Errorf("the hint does not say whose fault it is:\n%s", reported.Hint)
			}
			if reported.Pos.Filename != declaredAt().Filename {
				t.Errorf("it points at %s rather than at the declaration", reported.Pos)
			}
		})
	}
}

// declaredAt is where the declarations in this file were written, which is
// where a diagnostic about one has to point.
func declaredAt() token.Position {
	return token.Position{Filename: "declared.go", Line: 7, Column: 6}
}

// declaring asks the layer to generate for a declaration over this subject,
// with the stack exposing this shape, and fails the test if it refuses.
func declaring(t *testing.T, pkgPath, name string, exposed shape.Shape) layer.Unit {
	t.Helper()

	unit, err := generate(t, pkgPath, name, exposed)
	if err != nil {
		t.Fatalf("generating for %s: %v", name, err)
	}
	return unit
}

// generate asks the layer for one declaration's unit and returns what it said.
func generate(t *testing.T, pkgPath, name string, exposed shape.Shape) (layer.Unit, error) {
	t.Helper()

	return regenerating(t, pkgPath, name, nil, exposed)
}

// regenerating does the same, over a load in which some declarations were written
// by a previous run rather than by the author.
func regenerating(t *testing.T, pkgPath, name string, written func(token.Pos) bool, exposed shape.Shape) (layer.Unit, error) {
	t.Helper()

	loaded := loadFixture(t)
	builder := subject.New(subject.Config{
		Fset:      loaded.Fset,
		Owned:     loaded.Owned(),
		Docs:      loaded.FieldDocs(),
		Generated: written,
	})

	pkg, ok := loaded.Package(pkgPath)
	if !ok {
		t.Fatalf("the fixture has no package %s", pkgPath)
	}

	obj := pkg.Types.Scope().Lookup(name)
	if obj == nil {
		t.Fatalf("%s declares no %s", pkgPath, name)
	}
	held, ok := types.Unalias(obj.Type()).(*types.Named)
	if !ok {
		t.Fatalf("%s is a %T, want a named type", name, obj.Type())
	}

	built, problems := builder.Build(held, subject.Site{})
	if !problems.Empty() {
		t.Fatalf("modelling %s: %s", name, problems.Render())
	}

	return jsoncodec.New().Generate(&layer.Context{
		Model: &model.Model{
			Name: declaredName, Form: model.FormSpec, Subject: built,
			Pkg: pkg, Pos: declaredAt(),
		},
		Exposed: exposed,
	}, shape.Shape{})
}

// source renders what a unit puts in the declaration's own file.
//
// Rendered rather than read off the tree, because what the tests below are
// about is the code somebody will open: a method's receiver and the shape of
// its loop are what a reader sees, and a tree walk asking the same questions
// would be a second parser to keep in step with the printer.
func source(t *testing.T, unit layer.Unit) string {
	t.Helper()

	file := emit.File{
		Package:  "model",
		Sections: []emit.Section{{Decls: unit.Decls, Comments: unit.Comments, Fset: unit.Fset}},
		Imports:  unit.Imports,
	}

	out, err := file.Render()
	if err != nil {
		t.Fatalf("rendering the container's codec: %v", err)
	}
	return string(out)
}
