package jsoncodec_test

import (
	"go/token"
	"go/types"
	"strings"
	"testing"

	"github.com/okian/forge/internal/emit"
	"github.com/okian/forge/internal/layers/jsoncodec"
	"github.com/okian/forge/internal/load"
	"github.com/okian/forge/internal/subject"
	"github.com/okian/forge/plugin"
)

// The methods a text codec is made of, named here because what these tests do
// is claim them on a type that does not have them yet.
const (
	marshalText   = "MarshalText"
	appendText    = "AppendText"
	unmarshalText = "UnmarshalText"
)

// A field whose type this run is giving a text codec is written through it,
// although the package does not declare one yet.
//
// The composition this exists for, and the reason it cannot be answered by
// looking. A closed set is declared over a named integer by one declaration,
// and a struct holding one of its members is given a codec by another; both are
// generated in the same run, and the first has written nothing when the second
// is planned. Reading the package instead would find the methods on every run
// but the first — so the field would go out as a number into an empty checkout
// and as a name on the next run, from two declarations neither of which had
// changed.
func TestAFieldWhoseTypeThisRunGivesATextCodec(t *testing.T) {
	held := texting(t, "Named", "Counter", marshalText, unmarshalText)

	if !strings.Contains(held, "held.MarshalText()") {
		t.Errorf("the field was not written through its text codec:\n%s", held)
	}
	if !strings.Contains(held, "jsontext.String(string(text))") {
		t.Errorf("the text was not written as a JSON string:\n%s", held)
	}
	if !strings.Contains(held, "UnmarshalText([]byte(raw.String()))") {
		t.Errorf("the field is not read back through its text codec:\n%s", held)
	}

	// And the number it is underneath is not what goes out, which is the whole
	// of what was wrong before this was asked.
	if strings.Contains(held, "jsontext.Uint(uint64(v.Count))") {
		t.Errorf("the field is still written as the number behind it:\n%s", held)
	}
}

// One half of a text codec is not a text codec, and the type is written as what
// it is made of.
//
// Unlike a JSON codec, this layer was never going to write either half, so
// there is nothing to collide with and nothing an author has to resolve. What
// it must not do is write a value through a reader nothing wrote a writer for,
// or read one back through a writer nothing wrote a reader for — either leaves
// a document that can be produced and not loaded.
func TestOneHalfOfATextCodecIsNotOne(t *testing.T) {
	for _, half := range [][]string{{marshalText}, {appendText}, {unmarshalText}} {
		held := texting(t, "Named", "Counter", half...)

		if strings.Contains(held, "MarshalText") || strings.Contains(held, "UnmarshalText") {
			t.Errorf("a type declaring only %v was written through it:\n%s", half, held)
		}
		if !strings.Contains(held, "jsontext.Uint(uint64(v.Count))") {
			t.Errorf("a type declaring only %v was not written as its own form:\n%s", half, held)
		}
	}
}

// The appender is taken where it is the only writing half there is.
//
// encoding.TextAppender is the newer of the two and a type may carry it alone.
// It is passed nothing to append to, because nothing here holds a buffer worth
// appending into — the byte slice that comes back becomes a token either way.
func TestTheAppenderIsTakenWhenItIsTheOnlyWriter(t *testing.T) {
	held := texting(t, "Named", "Counter", appendText, unmarshalText)

	if !strings.Contains(held, "held.AppendText(nil)") {
		t.Errorf("the appender was not called, or not called with nothing:\n%s", held)
	}
	if strings.Contains(held, "MarshalText") {
		t.Errorf("a half the type does not have was called:\n%s", held)
	}
}

// MarshalText is preferred where the type has both writing halves.
//
// The standard library prefers the appender, having a buffer of its own to
// append into. This has none, so there is nothing to prefer it for — and the
// half that reads plainly in a file somebody will open is the one to write.
func TestWhichWritingHalfIsCalled(t *testing.T) {
	held := texting(t, "Named", "Counter", marshalText, appendText, unmarshalText)

	if !strings.Contains(held, "held.MarshalText()") {
		t.Errorf("MarshalText was not preferred:\n%s", held)
	}
	if strings.Contains(held, "AppendText") {
		t.Errorf("both writing halves were called:\n%s", held)
	}
}

// A codec is called through a local whoever wrote it, so that what is generated
// does not turn on which receiver the method happens to have.
//
// The receiver is a fact about the package, and for a method this run has not
// written yet there is no package to read it from: on a clean checkout there is
// no receiver and on the next run there is. A local suits either — a method on
// the pointer needs something addressable and a method on the value will take
// one — so the question is not asked at all.
//
// Held against the author's own codec, which is the same question answered from
// the other side: [Colour] declares a text codec with a value receiver and is
// written exactly as a type whose codec this run is about to write.
func TestATextCodecIsCalledTheSameWayWhoeverWroteIt(t *testing.T) {
	author := authored(t, "Coloured")
	run := texting(t, "Named", "Counter", marshalText, unmarshalText)

	for _, want := range []string{"held := v.", "text, err := held.MarshalText()"} {
		if !strings.Contains(author, want) {
			t.Errorf("the author's codec does not carry %q:\n%s", want, author)
		}
		if !strings.Contains(run, want) {
			t.Errorf("the run's codec does not carry %q:\n%s", want, run)
		}
	}
}

// texting renders the codec for a subject, telling the layer that this run will
// put these methods on one of the types it holds.
//
// The type is named rather than resolved from the field, because what is being
// set up is the state a neighbour declaration leaves behind — and a test that
// resolved it would be reading the same package the layer is not allowed to
// believe.
func texting(t *testing.T, held, on string, methods ...string) string {
	t.Helper()

	loaded := loadFixture(t)
	pkg, ok := loaded.Package(modelPkg)
	if !ok {
		t.Fatalf("the fixture has no package %s", modelPkg)
	}

	obj := pkg.Types.Scope().Lookup(on)
	if obj == nil {
		t.Fatalf("%s declares no %s", modelPkg, on)
	}

	return rendering(t, loaded, held, map[string][]string{
		plugin.TypeIdentity(obj.Type()): methods,
	})
}

// authored renders the codec for a subject with nothing said about the run,
// which is what a package holding only what its author wrote looks like.
func authored(t *testing.T, held string) string {
	t.Helper()
	return rendering(t, loadFixture(t), held, nil)
}

// rendering generates for one fixture subject and returns the file it would be
// written into.
func rendering(t *testing.T, loaded *load.Session, name string, writes map[string][]string) string {
	t.Helper()

	pkg, ok := loaded.Package(modelPkg)
	if !ok {
		t.Fatalf("the fixture has no package %s", modelPkg)
	}

	obj := pkg.Types.Scope().Lookup(name)
	if obj == nil {
		t.Fatalf("%s declares no %s", modelPkg, name)
	}
	subjectType, ok := types.Unalias(obj.Type()).(*types.Named)
	if !ok {
		t.Fatalf("%s is a %T, want a named type", name, obj.Type())
	}

	built, problems := subject.New(subject.Config{
		Fset:  loaded.Fset,
		Owned: loaded.Owned(),
		Docs:  loaded.FieldDocs(),
	}).Build(subjectType, subject.Site{})
	if !problems.Empty() {
		t.Fatalf("modelling %s: %s", name, problems.Render())
	}

	ctx := (&plugin.Context{
		Model: &plugin.Model{
			Name: name, Form: plugin.FormInline, Subject: built,
			Pkg: pkg, Pos: token.Position{Filename: "person.go"},
		},
	}).Writing(writes, nil)

	unit, err := jsoncodec.New().Generate(ctx, plugin.Shape{})
	if err != nil {
		t.Fatalf("generating a codec for %s: %v", name, err)
	}

	file := emit.File{Package: "model"}
	for _, key := range sortedKeys(unit.Provides) {
		one := unit.Provides[key]
		file.Sections = append(file.Sections, emit.Section{
			Decls: one.Decls, Comments: one.Comments, Fset: one.Fset,
		})
		file.Imports = append(file.Imports, one.Imports...)
	}

	out, err := file.Render()
	if err != nil {
		t.Fatalf("rendering the codec for %s: %v", name, err)
	}
	return string(out)
}
