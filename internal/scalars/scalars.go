package scalars

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"slices"
	"strings"

	"github.com/okian/forge/internal/diag"
	"github.com/okian/forge/internal/emit"
	"github.com/okian/forge/internal/layer"
	"github.com/okian/forge/internal/model"
)

// What can be wrong with a tag an author wrote here.
var (
	codeDisplayUnrenderable = diag.Register(3021, "a display tag names a field that cannot be rendered")
	codeDisplayOption       = diag.Register(3022, "a display tag carries an option nothing reads")
	codeMethodIsAField      = diag.Register(3023, "a method the subject earns has the name of one of its fields")
)

// The tags an author writes to ask for one of these.
const (
	// displayTag marks a field as part of how the subject reads. Its name, when
	// one is written, labels the field in the rendering.
	displayTag = "display"

	// redactTag marks a field as one that does not belong in a log.
	redactTag = "redact"
)

// The packages the methods below name.
var (
	stdStrings = model.Import{Path: "strings", Name: "strings"}
	stdStrconv = model.Import{Path: "strconv", Name: "strconv"}
	stdSlog    = model.Import{Path: "log/slog", Name: "slog"}
)

// Written is one subject's worth of contributions, keyed by what each is about.
//
// The key is a verb and the subject's identity, which is what keeps a package
// from writing one method twice: two declarations over one subject each ask and
// each are answered, and the two answers are the same declarations under the
// same key.
type Written map[string]layer.Unit

// Asked is what these emitters need to know about the subject they write for.
type Asked struct {
	// Subject is the type the methods are declared on, and Local the import
	// path of the package they land in.
	Subject *model.Struct
	Local   string

	// Bound is what the file these land in already binds.
	//
	// Needed rather than nice to have: the subject is named in every method
	// written here, and a subject from a package called slices, in a file where
	// a layer has bound the standard library's, has to be written under some
	// other name — or the file binds one name to two paths and is refused
	// whole.
	//
	// What is passed is what every file of the package will bind, which is the
	// answer this needs rather than a wider one taken for convenience: where
	// these land is the file a package's subjects share, and its bindings are
	// the union across every declaration in it.
	Bound []model.Import

	// At is where the subject was declared, which is where a tag that cannot be
	// answered is reported.
	//
	// The subject rather than whichever declaration reached it, because what is
	// wrong is the tag: a package with three declarations over one subject
	// would otherwise point at whichever of them was walked first, and an
	// author would go looking at a declaration with nothing wrong with it.
	At token.Position

	// Earning names the subjects of this run that will be given a String,
	// by the identity of each.
	//
	// A field of one of those is rendered through it, and this is how that is
	// known before it is written. Asked of the run rather than of the type,
	// because only a subject earns anything here: a struct carrying a display
	// tag that nobody declared a stack over is a struct forge writes nothing
	// about, and rendering a field through its String would name a method that
	// never arrives.
	Earning map[string]bool

	// Generated reports whether a declaration was written by a generator rather
	// than by hand, as [load.Session.Generated] answers it.
	//
	// What it keeps out is forge's own last output. A field is rendered through
	// its type's String, and a String this run is about to write is not
	// evidence that the type has one — counting it would make a package that
	// builds from a committed tree fail from a clean checkout.
	Generated func(token.Pos) bool

	// Written names the methods a layer of this run has already put on the
	// subject.
	//
	// What is emitted here is earned rather than asked for: a tag says a field
	// is secret and a log value follows, without any declaration naming a
	// layer. A layer that writes the same method was asked for by name, and
	// writing both would put one method into a package twice.
	//
	// So the layer's is the one that stays, and this is how these emitters are
	// told to stand down. It is the narrower half that gives way, too: what is
	// written here is about the subject, where a layer walks everything the
	// subject reaches — so yielding loses nothing and keeps the better answer.
	Written []string
}

// wrote reports whether a layer has already written any of these methods on the
// subject.
//
// Any rather than all, because a pair that half exists is the worse outcome of
// the two: a text codec whose MarshalText a layer wrote and whose UnmarshalText
// these emitters wrote is a type that round-trips through two designs.
func (a Asked) wrote(methods ...string) bool {
	for _, one := range methods {
		if slices.Contains(a.Written, one) {
			return true
		}
	}
	return false
}

// For returns what a subject earns from its own shape and tags.
//
// Nothing, for a subject that carries none of the signals — which is the common
// case and costs a walk of the fields. What comes back is keyed for a package
// rather than for a declaration, since the receiver is the subject and two
// declarations over one subject would otherwise each contribute a copy.
func For(of Asked, diags *diag.Set) (Written, error) {
	if of.Subject == nil {
		return nil, nil
	}

	held := model.Spell(of.Subject.Type(), of.Local, of.Bound)
	out := make(Written)

	for _, one := range []struct {
		verb    string
		methods []string
		write   func(Asked, model.Spelling, *diag.Set) (layer.Unit, bool, error)
	}{
		{"display", []string{displayMethod}, displaying},
		{"text", []string{marshalMethod, unmarshalMethod, appendMethod}, texting},
		{"log", []string{logMethod}, logging},
	} {
		// A layer that writes this method has already answered the question,
		// and asked for it — where what is written here was earned from a tag.
		// Both would be the same method twice in one package, and the layer's
		// is the one that stays: it was asked for by name, and it is the fuller
		// answer, since a layer walks what the subject reaches where these
		// emitters write about the subject alone.
		if of.wrote(one.methods...) {
			continue
		}

		unit, wrote, err := one.write(of, held, diags)
		if err != nil {
			return nil, err
		}
		if wrote {
			out[one.verb+": "+model.TypeIdentity(of.Subject.Type())] = unit
		}
	}

	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

// shown returns the fields a display tag names, in the order they were written.
func shown(subject *model.Struct) []model.Field {
	var out []model.Field
	for _, field := range subject.Fields {
		held, carries := field.Tag(displayTag)
		if carries && !held.Ignored {
			out = append(out, field)
		}
	}
	return out
}

// label returns what a field is called in the rendering, and whether it is
// called anything.
//
// The tag's name when one was written, which is how every tag grammar in
// circulation says "call it this". A field with no name in its tag renders as
// its value alone, because an author who wanted a label had somewhere to put
// one.
func label(field model.Field) (string, bool) {
	held, carries := field.Tag(displayTag)
	if !carries || held.Name == "" {
		return "", false
	}
	return held.Name, true
}

// hidden returns whether a field is one a log must not carry.
func hidden(field model.Field) bool {
	held, carries := field.Tag(redactTag)
	return carries && !held.Ignored
}

// redacted returns whether any field asks to be kept out of logs.
func redacted(subject *model.Struct) bool {
	for _, field := range subject.Fields {
		if hidden(field) {
			return true
		}
	}
	return false
}

// says reports whether a type says how it reads, which is what a field that is
// not a scalar has to do to be rendered.
//
// Two ways to say it, and the second is why this is not simply
// types.Implements. A type the author gave a String has one. A type carrying a
// display tag is about to be given one by this same run, and asking the type
// checker about it would answer with whatever the last run left on disk —
// which makes a build that works from a committed tree and fails from a clean
// checkout, and makes what forge writes depend on what forge wrote.
//
// So the generated half is excluded from what the type checker is asked, and
// added back from the tag it will come from. The answer is then the same
// however many times the output has been deleted.
func says(t types.Type, of Asked) bool {
	if t == nil {
		return false
	}
	if declares(t, of) {
		return true
	}

	if held, indirect := types.Unalias(t).(*types.Pointer); indirect {
		t = held.Elem()
	}
	return of.Earning[model.TypeIdentity(t)]
}

// declares reports whether the type already has a String this run can count on.
//
// The method set rather than types.Implements, because what is wanted is the
// method itself: where it was written is what says whether it can be counted,
// and an interface satisfied is a yes with nothing behind it to ask.
//
// What cannot be counted is a generated method in the package this run is
// writing, because that is a file about to be replaced — counting it would make
// the answer depend on what the last run left behind. A generated method
// anywhere else is a file on disk that this run does not touch, which is as
// good as one the author wrote and is spelled the same way.
func declares(t types.Type, of Asked) bool {
	for _, held := range []types.Type{t, types.NewPointer(t)} {
		set := types.NewMethodSet(held)

		for i := range set.Len() {
			fn, is := set.At(i).Obj().(*types.Func)
			if !is || fn.Name() != "String" || !reads(fn) {
				continue
			}
			if rewritten(fn, of) {
				continue
			}
			return true
		}
	}
	return false
}

// rewritten reports whether a method is one this run is about to write over.
func rewritten(fn *types.Func, of Asked) bool {
	if of.Generated == nil || !of.Generated(fn.Pos()) {
		return false
	}
	return fn.Pkg() != nil && fn.Pkg().Path() == of.Local
}

// reads reports whether a method has fmt.Stringer's signature.
func reads(fn *types.Func) bool {
	sig, is := fn.Type().(*types.Signature)
	if !is || sig.Params().Len() != 0 || sig.Results().Len() != 1 {
		return false
	}

	basic, is := sig.Results().At(0).Type().(*types.Basic)
	return is && basic.Kind() == types.String
}

// taken returns a field of the subject whose name is one of the given methods,
// and whether there is one.
//
// Go does not let a type declare a field and a method under one name, so a
// subject with a field called String is one no String can be written for. It
// cannot be caught where every other collision is: that check reads what the
// package declares, and a field is declared by neither the package nor the
// type's method set.
func taken(subject *model.Struct, names ...string) (string, bool) {
	for _, field := range subject.Fields {
		if slices.Contains(names, field.Name) {
			return field.Name, true
		}
	}
	return "", false
}

// clashes reports a method that cannot be written because the subject has a
// field of that name, and says whether one was found.
func clashes(of Asked, diags *diag.Set, names ...string) bool {
	held, found := taken(of.Subject, names...)
	if !found {
		return false
	}

	diags.Add(diag.New(codeMethodIsAField, of.At,
		"%s has a field called %s, so no method of that name can be declared on it",
		of.Subject.Ref().Name, held).
		WithHint("%s", "a type cannot declare a field and a method under one name; "+
			"rename the field, or drop the tag that asks for the method"))

	return true
}

// Earns reports whether a subject will be given a String by these emitters.
//
// Asked of the run's own subjects rather than of any type carrying a display
// tag, which is a different set and a wrong answer. Only a subject earns
// anything here — a struct nobody declared a stack over is a struct forge never
// writes a line about, whatever its fields say — and a check that read the tag
// alone would render a field through a String that is never written.
//
// It is what a field of this type is rendered through, so it has to be
// decidable without asking whether anything was written yet: a run that
// consulted the last run's output would build from a committed tree and fail
// from a clean checkout.
func Earns(subject *model.Struct) bool {
	if subject == nil || len(shown(subject)) == 0 {
		return false
	}

	_, clash := taken(subject, displayMethod)
	return !clash
}

// The methods these emitters write, which are the names a subject may not have
// given to a field.
const (
	displayMethod = "String"
	logMethod     = "LogValue"

	appendMethod    = "AppendText"
	marshalMethod   = "MarshalText"
	unmarshalMethod = "UnmarshalText"
)

// wrapping returns the single field a scalar wrapper wraps, and whether the
// subject is one.
//
// A wrapper is a struct with exactly one field, that field a predeclared type,
// and its display tag written without a name. The narrowness is the point on
// each count.
//
// One field of a predeclared type, because what the text of such a type should
// be is not a design question: there is one value in it and its text form is
// that value's. A struct with two fields has a format, and a format is
// something an author picks rather than something forge guesses.
//
// The tag, because a text codec is not free of consequence. encoding/json takes
// a TextMarshaler for a type that has no JSON codec of its own, so writing one
// unasked would change a wrapper from {"ID":"x"} to "x" in every document it
// appears in — a change to somebody's wire format made because they mentioned
// forge. The tag is the same one that asks for a String, since for a wrapper
// the two are one question: how does this read as text.
//
// And written without a name, because a labelled rendering is for a person
// rather than for a round trip. A String of "id=x" beside a text form of "x"
// would be two answers to that one question.
func wrapping(subject *model.Struct) (model.Field, bool) {
	if len(subject.Fields) != 1 {
		return model.Field{}, false
	}

	held := subject.Fields[0]
	if held.Embedded || held.Type.Class != model.ClassBasic {
		return model.Field{}, false
	}
	if _, known := scalar(held.Type); !known {
		return model.Field{}, false
	}

	tag, tagged := held.Tag(displayTag)
	if !tagged || tag.Ignored || tag.Name != "" {
		return model.Field{}, false
	}

	return held, true
}

// parsed reads assembled source back as declarations.
//
// Assembled as text for the reason every layer's output is: what is written is
// a handful of methods a person will read, and a tree for them is many times
// their size. The cost is the possibility of writing something that is not Go,
// and it is paid here rather than as a file on disk that does not build.
func parsed(source, about string) ([]ast.Decl, []*ast.CommentGroup, *token.FileSet, error) {
	fset := token.NewFileSet()

	file, err := parser.ParseFile(fset, "scalars.go", "package forge\n\n"+source, parser.ParseComments)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("what was written for %s is not valid Go: %w", about, err)
	}

	return file.Decls, file.Comments, fset, nil
}

// unit packages assembled source as a layer's contribution.
func unit(w *strings.Builder, held model.Spelling, imports ...model.Import) (layer.Unit, error) {
	decls, comments, fset, err := parsed(w.String(), held.Text)
	if err != nil {
		return layer.Unit{}, err
	}

	out := layer.Unit{Decls: decls, Comments: comments, Fset: fset}
	for _, one := range append(imports, held.Imports...) {
		out.Imports = append(out.Imports, emit.Import{Path: one.Path, Name: one.Name, Aliased: one.Aliased})
	}

	return out, nil
}
