package view_test

import (
	"go/format"
	"go/printer"
	"go/token"
	"strings"
	"testing"

	"github.com/okian/forge/internal/goldentest"
	"github.com/okian/forge/internal/layer"
	"github.com/okian/forge/internal/model"
	"github.com/okian/forge/internal/shape"
	"github.com/okian/forge/internal/view"
)

// A view forwards every method of the stack beneath it, and forwards nothing
// else.
//
// The forwarding is the easy half. The other half is what is not there: a view
// is built from the surface below the decorator, so a method the decorator adds
// cannot be on it — and that is the whole of why a caller inside a scope cannot
// open another one.
func TestWhatAViewForwards(t *testing.T) {
	held := written(t, asked())

	for _, want := range []string{
		"func (v SessionsView) Len() int",
		"func (v SessionsView) All() iter.Seq[Session]",
		"func (v SessionsView) AppendSeq(a0 iter.Seq[Session])",
		"func (v SessionsView) Reset()",
		"return v.held.Len()",
		"v.held.AppendSeq(a0)",

		// Two results are parenthesised and a variadic is spread, which is what
		// a call forwarding either has to do and neither is how one result is
		// written.
		"func (v SessionsView) At(a0 int) (Session, bool)",
		"func (v SessionsView) Add(a0 ...Session)",
		"v.held.Add(a0...)",
	} {
		if !strings.Contains(held, want) {
			t.Errorf("the view does not hold %q:\n%s", want, held)
		}
	}
}

// Every method of a view is on the value, whatever the method below takes.
//
// A decorator offering read access hands a value rather than a pointer to one,
// so a view with any method on its own pointer would have a method set a value
// of it did not — and the read scope would be missing whichever methods the
// stack below happened to declare on a pointer, for a reason that has nothing
// to do with reading. Nothing is lost by it: the view reaches what it wraps
// through a pointer either way.
func TestEveryMethodOfAViewIsOnTheValue(t *testing.T) {
	held := written(t, asked())

	// AppendSeq and Reset are declared on the pointer below, and are on the
	// value here.
	for _, want := range []string{
		"func (v SessionsView) AppendSeq(",
		"func (v SessionsView) Reset()",
	} {
		if !strings.Contains(held, want) {
			t.Errorf("the view does not hold %q:\n%s", want, held)
		}
	}

	if strings.Contains(held, "func (v *SessionsView)") {
		t.Errorf("a view method is on the view's own pointer:\n%s", held)
	}
}

// A view keeps a field rather than embedding what it wraps.
//
// Embedding would promote every method of the wrapped type onto the view,
// including the ones the decorator withdrew — which is exactly the set a view
// exists to not have. The field is the difference between a type that cannot
// express the call and one that merely does not mention it.
func TestAViewDoesNotEmbed(t *testing.T) {
	held := written(t, asked())

	if !strings.Contains(held, "held *sessionsStore") {
		t.Errorf("the view does not keep what it wraps in a field:\n%s", held)
	}
}

// What is written compiles, and the view really is missing what it looks
// missing.
//
// Read as strings, the two are the same claim; compiled, they are different
// ones. A method with the wrong receiver form, a result in the wrong order or
// an argument forwarded by the wrong name all read fine and none of them
// builds.
func TestWhatIsWrittenCompiles(t *testing.T) {
	held := written(t, asked())

	pkg := goldentest.Package{
		Path: "model",
		Files: []goldentest.Source{
			{Name: "store.go", Content: formatted(t, store)},
			{
				Name:      "zz_view.go",
				Content:   formatted(t, "package model\n\nimport \"iter\"\n\n"+held),
				Generated: true,
			},
		},
	}

	if err := goldentest.Compiles(pkg); err != nil {
		t.Fatalf("what was written does not compile: %v\n%s", err, held)
	}
}

// A signature the surface cannot render is reported rather than written.
//
// A surface is a description for a person to read, and forge writes it — so a
// signature that is not a signature is forge's own mistake. Reported here, with
// the method named, rather than emitted as a file that does not parse.
func TestASignatureThatIsNotOne(t *testing.T) {
	of := asked()
	of.Surface = append(of.Surface, shape.Method{Name: "Broken", Signature: "(((("})

	if _, err := view.Write(of); err == nil {
		t.Error("a signature that is not one was written out")
	} else if !strings.Contains(err.Error(), "Broken") {
		t.Errorf("the error does not name the method: %v", err)
	}
}

// A view has to be asked for by name, since nothing else can supply one.
func TestAViewWithNothingToWriteAbout(t *testing.T) {
	for name, of := range map[string]view.Asked{
		"no name":  {Held: "held", Of: "store", Guards: "Held"},
		"no field": {Name: "View", Of: "store", Guards: "Held"},
		"no type":  {Name: "View", Held: "held", Guards: "Held"},

		// The type it guards, without which the check for a way out is a check
		// for one name instead of two — and the generated type says in so many
		// words that no method on it names either.
		"nothing guarded": {Name: "View", Held: "held", Of: "store"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := view.Write(of); err == nil {
				t.Error("a view was written with nothing to write about")
			}
		})
	}
}

// A surface that can open a scope is refused rather than written.
//
// The one mistake that costs this type its reason to exist, and the only one it
// can catch: a view is written from the surface *below* the decorator, so a
// caller who hands over the surface above gets a view with the decorator's own
// methods on it and a scope a caller can re-enter through the value they were
// handed. Nothing downstream would notice — the file compiles, and the deadlock
// is somebody's, later.
func TestASurfaceThatOpensAScope(t *testing.T) {
	for name, opener := range map[string]shape.Method{
		"a write scope": {Name: "Do", Signature: "(f func(v *SessionsView))"},
		"a read scope":  {Name: "RDo", Signature: "(f func(v SessionsView))"},
	} {
		t.Run(name, func(t *testing.T) {
			of := asked()
			of.Surface = append(of.Surface, opener)

			_, err := view.Write(of)
			if err == nil {
				t.Fatal("a view was written that can open a scope")
			}
			if !strings.Contains(err.Error(), opener.Name) {
				t.Errorf("the error does not name the method: %v", err)
			}
		})
	}
}

// A method naming the type the view guards is refused, which is the way out
// nobody would think to list.
//
// It never mentions the view. A Clone handing back the decorated type, or a
// Merge taking one, gives a caller inside the scope the value the scope was
// opened on — and v.Clone().Do(f) is a second scope opened through the first.
func TestAMethodNamingWhatTheViewGuards(t *testing.T) {
	for name, one := range map[string]shape.Method{
		"handing one back": {Name: "Clone", Signature: "() Sessions"},
		"taking one":       {Name: "Merge", Signature: "(other Sessions)"},
	} {
		t.Run(name, func(t *testing.T) {
			of := asked()
			of.Surface = append(of.Surface, one)

			_, err := view.Write(of)
			if err == nil {
				t.Fatal("a method reaching the guarded type was forwarded")
			}
			if !strings.Contains(err.Error(), "Sessions") {
				t.Errorf("the error does not name what was seen: %v", err)
			}
		})
	}
}

// An import with no name to bind it to is refused rather than dropped.
//
// It cannot be written: what the methods say is iter.Seq, and a file that
// imports the path without binding it to iter does not build. Dropping it as
// unreached would be the quiet version of the same failure, since what is
// reached is decided by the name.
func TestAnImportWithNoName(t *testing.T) {
	of := asked()
	of.Imports = []model.Import{{Path: "iter"}}

	if _, err := view.Write(of); err == nil {
		t.Error("an import with no name to bind it to was accepted")
	}
}

// A package the written methods name and nothing imports is refused.
//
// The other end of pruning, and the more likely mistake: what a view names is
// decided by the surface it was handed, and what it imports is decided by
// whoever handed it over, so the two can disagree in a direction no amount of
// dropping unused imports would fix.
func TestAPackageNothingBinds(t *testing.T) {
	of := asked()
	of.Imports = nil

	_, err := view.Write(of)
	if err == nil {
		t.Fatal("a view naming a package nothing imports was written")
	}
	if !strings.Contains(err.Error(), "iter") {
		t.Errorf("the error does not name what is unbound: %v", err)
	}
}

// A method that hands the view back is refused too, since it opens a scope the
// same way round.
//
// Read off the signature rather than from a list of names, which is what makes
// this one caught at all: nobody would think to put a Clone on a list of things
// that open scopes, and a method handing back the view is a caller holding one
// outside the call it belongs to.
func TestAMethodThatHandsTheViewBack(t *testing.T) {
	of := asked()
	of.Surface = append(of.Surface, shape.Method{Name: "Clone", Signature: "() SessionsView"})

	if _, err := view.Write(of); err == nil {
		t.Error("a method handing the view back was forwarded")
	}
}

// And a surface that cannot is written, so the refusal is about the method
// rather than about being asked at all.
func TestASurfaceThatOpensNothing(t *testing.T) {
	if _, err := view.Write(asked()); err != nil {
		t.Errorf("a view over a surface with no scope-opener was refused: %v", err)
	}
}

// An import the written methods do not name is not carried into the file.
//
// A caller hands over what the stack below imports, which is more than any one
// view names: the methods are chosen per surface and the imports are not. An
// import nothing names is not a warning in Go — it is a file that does not
// build, and the file is one nobody may edit.
func TestAnImportNothingNames(t *testing.T) {
	of := asked()
	of.Imports = append(of.Imports, model.Import{Path: "bytes", Name: "bytes"})

	held, err := view.Write(of)
	if err != nil {
		t.Fatalf("writing the view: %v", err)
	}

	for _, one := range held.Imports {
		if one.Path == "bytes" {
			t.Errorf("an import nothing names was carried into the file: %v", held.Imports)
		}
	}

	// And the one the signatures do name is still there, so the pruning took
	// what was spare rather than everything.
	var names bool
	for _, one := range held.Imports {
		names = names || one.Path == "iter"
	}
	if !names {
		t.Errorf("pruning took an import the methods name: %v", held.Imports)
	}
}

// A doc of more than one line is still a comment.
//
// A surface's doc is documented as one line and every layer writes one, but a
// doc assembled from an option or a field name is a line somebody else decides
// the length of — and a second line without the marker turns the whole unit
// into something that does not parse, reported as forge's own mistake.
func TestADocOfMoreThanOneLine(t *testing.T) {
	of := asked()
	of.Surface = append(of.Surface, shape.Method{
		Name: "Two", Signature: "() int",
		Doc: "is one thing.\nAnd it is another.",
	})

	held, err := view.Write(of)
	if err != nil {
		t.Fatalf("a doc of two lines was not written: %v", err)
	}

	if !strings.Contains(source(t, held), "And it is another.") {
		t.Errorf("the second line of a doc is missing:\n%s", source(t, held))
	}
}

// asked is the decorator this package's tests are written against: a lock over
// a slice of sessions.
func asked() view.Asked {
	return view.Asked{
		Name:   view.Named("Sessions"),
		Guards: "Sessions",
		Doc:    "is the API of the sessions a scope has open.",
		Held:   "held",
		Of:     "sessionsStore",
		Surface: []shape.Method{
			{Name: "Len", Signature: "() int", Doc: "is how many sessions there are."},
			{Name: "All", Signature: "() iter.Seq[Session]"},
			{Name: "AppendSeq", Signature: "(seq iter.Seq[Session])", Pointer: true},
			{Name: "Reset", Signature: "()", Pointer: true},

			// Two results and a variadic, because a forwarded call has to hand
			// both on and neither is written the way one result is.
			{Name: "At", Signature: "(i int) (Session, bool)"},
			{Name: "Add", Signature: "(more ...Session)", Pointer: true},
		},
		Imports: []model.Import{{Path: "iter", Name: "iter"}},
	}
}

// store is what the written view is compiled against.
const store = "package model\n\n" +
	"import \"iter\"\n\n" +
	"// Session is what the store holds.\n" +
	"type Session struct{ ID string }\n\n" +
	"// sessionsStore is the stack below the lock.\n" +
	"type sessionsStore []Session\n\n" +
	"func (s sessionsStore) Len() int { return len(s) }\n\n" +
	"func (s sessionsStore) All() iter.Seq[Session] {\n" +
	"\treturn func(yield func(Session) bool) {\n" +
	"\t\tfor _, one := range s {\n\t\t\tif !yield(one) {\n\t\t\t\treturn\n\t\t\t}\n\t\t}\n\t}\n}\n\n" +
	"func (s *sessionsStore) AppendSeq(seq iter.Seq[Session]) {\n" +
	"\tfor one := range seq {\n\t\t*s = append(*s, one)\n\t}\n}\n\n" +
	"func (s *sessionsStore) Reset() { *s = (*s)[:0] }\n\n" +
	"func (s sessionsStore) At(i int) (Session, bool) {\n" +
	"\tif i < 0 || i >= len(s) {\n\t\tvar none Session\n\t\treturn none, false\n\t}\n" +
	"\treturn s[i], true\n}\n\n" +
	"func (s *sessionsStore) Add(more ...Session) { *s = append(*s, more...) }\n"

// formatted returns source as gofmt writes it.
//
// The compile check holds a package to being formatted, which is right: a
// generated file is one nobody edits and everybody reads, and forge formats
// what it writes. What is assembled here is printed declaration by declaration
// rather than rendered as a file, so the formatting is this test's to do.
func formatted(t *testing.T, held string) []byte {
	t.Helper()

	out, err := format.Source([]byte(held))
	if err != nil {
		t.Fatalf("formatting what was written: %v\n%s", err, held)
	}
	return out
}

// written returns what a view was written as, so that a test reads what an
// author would read.
func written(t *testing.T, of view.Asked) string {
	t.Helper()

	held, err := view.Write(of)
	if err != nil {
		t.Fatalf("writing the view: %v", err)
	}

	return source(t, held)
}

// source renders a unit back as Go.
func source(t *testing.T, unit layer.Unit) string {
	t.Helper()

	var b strings.Builder

	fset := unit.Fset
	if fset == nil {
		fset = token.NewFileSet()
	}

	for _, decl := range unit.Decls {
		if err := printer.Fprint(&b, fset, decl); err != nil {
			t.Fatalf("printing what was written: %v", err)
		}
		b.WriteString("\n\n")
	}

	return b.String()
}
