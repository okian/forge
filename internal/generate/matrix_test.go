package generate_test

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/okian/forge/internal/compose"
	"github.com/okian/forge/internal/diag"
	"github.com/okian/forge/internal/generate"
	"github.com/okian/forge/internal/goldentest"
	"github.com/okian/forge/internal/layer"
	"github.com/okian/forge/internal/layers"
	"github.com/okian/forge/internal/model"
	"github.com/okian/forge/internal/shape"
	"github.com/okian/forge/internal/templates"
)

// The matrix is every stack that can be written over the layers this build has
// generators for, generated and compiled.
//
// It exists because the rules are stated one at a time and apply together. Each
// is tested where it is written; what nothing else asks is whether a stack that
// satisfies all of them produces a package that builds — and that answer is a
// product of the layers rather than a property of any one of them, so it is
// found by trying the combinations rather than by reasoning about them.
//
// The set grows on its own. A layer that says it is ready is in, one that says
// it is a stub is out — a stub generates nothing, and insisting its output
// compile would be asserting that unwritten code works — and a layer that says
// nothing about itself is in. That last is the interesting case and it is
// deliberate: how far along a layer is is a question about forge's own roadmap,
// which a layer written elsewhere has no answer to, so silence means a layer
// somebody else finished rather than one forge has not started.
//
// What it reaches is bounded by that set rather than by how deep it goes. Every
// layer with a generator today is storage or refining, so no arrangement of
// them holds a subject, an element, a decorator or a transport — and the four
// rules about where those may sit are unreachable here however long the stacks
// get. Those are covered where they are written, over fakes; what this adds is
// the half no fake can give, which is that the output of real layers compiles
// together.
//
// It is the combinatorial half of two. The other is in the layers package,
// where every layer is offered a shape with each capability taken away in turn
// and its answer compared against what it claims: that half stays linear in the
// number of layers however many there are, and it is what keeps this half from
// needing to be exhaustive about shapes as well as about arrangements.

// depth is how many layers a matrix stack is built from.
//
// Three, so that the arrangements which exercise a decorator or a transport are
// already enumerated on the day one lands ready. Over today's ready set they
// reach nothing: one refining layer and two storage cannot fill three places
// without either repeating a layer or naming two storages, so every stack of
// three is refused by one of those two rules and none of them is built.
//
// Kept anyway, because the cost is a fraction of a second and the alternative is
// remembering to raise it at exactly the moment somebody is thinking about
// something else.
const depth = 3

// ready returns the layers the matrix combines, in registry order.
//
// Every layer that does not say it is unfinished, which includes every layer
// that says nothing at all. See the note above on why silence counts as ready.
func ready(t *testing.T, registry *layer.Registry) []model.TypeRef {
	t.Helper()

	var out []model.TypeRef
	for _, one := range registry.All() {
		described, ok := one.(layer.Described)
		if ok && described.Stage() != layer.StageReady {
			continue
		}
		out = append(out, one.Origin())
	}

	if len(out) < 2 {
		t.Fatalf("the matrix has %d layers to combine, which combines nothing", len(out))
	}
	return out
}

// stacks returns every ordered arrangement of the given markers, up to depth,
// with repetition.
//
// With repetition on purpose. A stack naming one layer twice is a stack
// somebody can write, and the rule that refuses it is one the matrix should be
// exercising rather than assuming.
func stacks(of []model.TypeRef) [][]model.LayerRef {
	var out [][]model.LayerRef

	// Each round extends the round before it rather than everything so far,
	// which is the difference between the arrangements of each length and the
	// arrangements of each length once per length that follows it.
	level := [][]model.LayerRef{{}}

	for range depth {
		var longer [][]model.LayerRef
		for _, held := range level {
			for _, one := range of {
				longer = append(longer, append(slices.Clone(held), model.LayerRef{Origin: one}))
			}
		}
		out = append(out, longer...)
		level = longer
	}
	return out
}

// spelled names a stack the way an author would read it, outermost first.
func spelled(stack []model.LayerRef) string {
	named := make([]string, len(stack))
	for i, one := range stack {
		named[i] = one.Origin.Name
	}
	return strings.Join(named, "[") + strings.Repeat("]", len(named)-1)
}

// asked builds a declaration over a stack, in the form given.
func asked(stack []model.LayerRef, form model.Form) generate.Request {
	held := request("Persons")
	held.Model.Form = form
	held.Model.Stack = slices.Clone(stack)

	return held
}

// Every stack that composes generates a package that compiles, and every stack
// that does not is refused in a way an author can act on.
//
// The two halves are one test because the interesting thing is the line between
// them. A stack is either something forge will build or something it will
// explain, and what must not happen is either a refusal nobody can act on or a
// file that does not compile — so both outcomes are checked, whichever a given
// stack turns out to be.
func TestTheCompositionMatrix(t *testing.T) {
	registry := layers.Builtins()
	cfg := over(registry)

	held := stacks(ready(t, registry))
	built, refused := 0, 0

	for _, stack := range held {
		for _, form := range []model.Form{model.FormInline, model.FormSpec} {
			t.Run(spelled(stack)+" "+form.String(), func(t *testing.T) {
				wrong, made := checked(stack, form, cfg)
				if made {
					built++
				} else {
					refused++
				}

				for _, said := range wrong {
					t.Error(said)
				}
			})
		}
	}

	// A run where nothing built, or where nothing was refused, would pass every
	// assertion inside and mean the matrix had stopped exercising one of the two
	// things it is for.
	//
	// Asked only of a whole run. Filtering to one case with -run is the ordinary
	// way to look at a failure, and a guard that fired then would answer a
	// question nobody asked with a failure nobody caused.
	if whole := len(held) * 2; built+refused == whole && (built == 0 || refused == 0) {
		t.Errorf("the matrix built %d stacks and refused %d, so it is only exercising one of the two", built, refused)
	}
}

// over returns what generation is given for a registry, so that the matrix can
// be run against one holding a layer written to break it.
func over(registry *layer.Registry) generate.Config {
	cfg := config()
	cfg.Catalog = compose.Catalog{Registry: registry, DefaultStorage: layers.DefaultStorage()}

	return cfg
}

// checked runs one stack and returns what was wrong with the outcome, and
// whether the stack built at all.
//
// It reports rather than fails, which is what lets the harness be pointed at a
// layer written to break it: a checker that called into a testing.T could only
// ever be run in the one direction, and the question worth asking of a harness
// is whether it notices.
func checked(stack []model.LayerRef, form model.Form, cfg generate.Config) (wrong []string, built bool) {
	files, diags := generate.Package(local, "model", []generate.Request{asked(stack, form)}, cfg)

	if !diags.Empty() {
		return explained(diags), false
	}
	return compiles(stack, form, files), true
}

// explained returns what is wrong with a refusal, if anything.
//
// Not which refusal, because the matrix does not know: what is wrong with a
// given stack is what the rules say, and a test that decided that for itself
// would be a second implementation of them that agreed until one of the two
// changed. What it can insist on is that every refusal is a diagnostic somebody
// registered, at the declaration, with something to do about it — which is the
// property that makes a refusal useful rather than merely correct.
//
// All three of every refusal, rather than the first. They are independent —
// a code nobody registered says nothing about where the caret is — so reporting
// one and stopping would send somebody back for the next after each fix.
func explained(diags diag.Set) []string {
	var wrong []string

	for _, one := range diags.All() {
		if _, registered := diag.Summary(one.Code); !registered {
			wrong = append(wrong, "a refusal carries a code nobody registered: "+one.Render())
		}
		if one.Pos.Filename != declaredAt.Filename || one.Pos.Line != declaredAt.Line {
			wrong = append(wrong, "a refusal points at "+one.Pos.String()+" rather than at the declaration: "+one.Render())
		}
		if one.Hint == "" {
			wrong = append(wrong, "a refusal says nothing to do about it: "+one.Render())
		}
	}
	return wrong
}

// compiles builds what a stack generated, under both build configurations where
// the declaration is one forge owns the type of.
func compiles(stack []model.LayerRef, form model.Form, files []generate.File) []string {
	sources := []goldentest.Source{subject, using}
	if form == model.FormSpec {
		sources = append(sources, declared)
	} else {
		sources = append(sources, goldentest.Source{
			Name:    "declared.go",
			Content: []byte("package model\n\ntype Persons " + underlying(stack) + "\n"),
		})
	}

	for _, file := range files {
		sources = append(sources, goldentest.Source{Name: file.Name, Content: file.Content, Generated: true})
	}

	// Both ways round for a declaration forge owns the type of, since the build
	// that excludes its file has to hold the other half.
	tags := [][]string{nil}
	if form == model.FormSpec {
		tags = append(tags, []string{"forgespec"})
	}

	var wrong []string

	// Said directly rather than left to the compile gate. The call site below
	// does catch a layer that generated nothing, because the method it calls is
	// then missing — but that is the fixture happening to ask for something,
	// and what is meant here is that a stack which composed must have produced
	// a file. A fixture edited later should not be able to take that away.
	if len(files) == 0 {
		wrong = append(wrong, spelled(stack)+" composed and generated nothing")
	}
	if form == model.FormSpec && !among(files, generate.Stubs()) {
		wrong = append(wrong, spelled(stack)+" generated no file for the build its declaration is absent from")
	}

	for _, set := range tags {
		if err := goldentest.Compiles(goldentest.Package{Path: "model", Tags: set, Files: sources}); err != nil {
			wrong = append(wrong, fmt.Sprintf("%s generated a package that does not compile with tags %v: %v",
				spelled(stack), set, err))
		}
	}
	return wrong
}

// underlying spells the type an inline declaration over a stack would have.
//
// Every layer with a generator is a slice of what is beneath it, so a stack of
// two is a slice of a slice. Spelling it from the stack rather than writing
// []Person down once is what lets this notice a nested inline declaration
// generating code that does not fit the type it was written as — which is the
// thing the rule against nesting inline exists to prevent, and which a fixture
// that always declared the one-layer form could never see.
func underlying(stack []model.LayerRef) string {
	return strings.Repeat("[]", len(stack)) + "Person"
}

// among reports whether a file of that name was generated.
func among(files []generate.File, name string) bool {
	return slices.ContainsFunc(files, func(one generate.File) bool { return one.Name == name })
}

// using is a call site, which is what makes the two build configurations of a
// spec declaration mean anything.
//
// Without one, a package whose methods are all missing under the tag still
// type-checks: nothing asks for them. The whole point of writing a file under
// the tag is that code calling the generated API compiles either way, and that
// is only tested by having code that calls it.
//
// Through a pointer, because a declaration is not required to be copyable. A
// stack holding a lock is a struct nothing may copy, and a call site taking one
// by value would be reporting that as a fault in the generated code when it is
// a fault in the fixture. A pointer's method set holds the value methods too,
// so nothing is given up by asking this way.
var using = goldentest.Source{Name: "using.go", Content: []byte(
	"package model\n\n// counted reads the generated API, so both builds have to hold it.\n" +
		"func counted(p *Persons) int { return p.Len() }\n")}

// The combinations forge refuses by name are refused by that name.
//
// The half of the matrix that a property cannot check. Above, every refusal is
// required to be actionable and none of them is required to be a particular
// one; here a handful of stacks that must be refused say which code says so.
//
// Which the rules' own tests also do, in more detail — they check the wording
// and where the caret lands. What is here and not there is the path: these go
// through generation, so what they hold is that a stack refused by composition
// is refused by the thing an author runs, rather than composed, refused
// somewhere nobody reads, and generated anyway.
//
// Written out rather than derived. Deriving what each stack should be refused
// with would be writing the rules a second time, and two implementations of one
// rule agree until the day somebody changes one of them.
func TestWhatTheMatrixRefusesByName(t *testing.T) {
	cases := map[string]struct {
		stack []string
		form  model.Form
		code  int
	}{
		"two storage layers": {stack: []string{"Ring", "Slice"}, form: model.FormSpec, code: 1003},
		// Two of a refining layer rather than two of a storage one, which would
		// also be two storage layers and could be refused by either rule.
		"one layer twice":        {stack: []string{"Collection", "Collection"}, form: model.FormSpec, code: 1020},
		"nesting written inline": {stack: []string{"Collection", "Slice"}, form: model.FormInline, code: 1022},
		"an opaque layer inline": {stack: []string{"Ring"}, form: model.FormInline, code: 1021},
	}

	for name, want := range cases {
		t.Run(name, func(t *testing.T) {
			stack := make([]model.LayerRef, len(want.stack))
			for i, one := range want.stack {
				stack[i] = model.LayerRef{Origin: model.TypeRef{Pkg: model.MarkerPkg, Name: one}}
			}

			_, diags := generate.Package(local, "model",
				[]generate.Request{asked(stack, want.form)}, config())

			if diags.Empty() {
				t.Fatalf("%s was accepted", spelled(stack))
			}

			said := diags.Render()
			if want := fmt.Sprintf("FRG%d", want.code); !strings.Contains(said, want) {
				t.Errorf("it was refused as:\n%s\nwanting %s", said, want)
			}
		})
	}
}

// A layer that composes and then generates something that does not compile
// fails the matrix.
//
// The one thing a harness like this has to be asked. Every assertion in it is
// about output nobody wrote by hand, so a harness that quietly passed whatever
// it was given would look exactly like a harness that worked — and it would go
// on looking like one until a layer landed with the defect it was built to
// catch.
//
// It reads what the harness found rather than running it as a subtest, which is
// what checked reporting rather than failing is for: a nested test proving a
// harness fails would be a failure the run has to be told to tolerate, and one
// somebody would eventually make stop failing.
func TestTheMatrixFailsOnALayerThatDoesNotCompile(t *testing.T) {
	registry := layer.New()
	registry.MustRegister(broken{})

	for _, one := range layers.Builtins().All() {
		if one.Origin().Name == "Slice" {
			registry.MustRegister(one)
		}
	}

	cfg := over(registry)
	found := 0

	for _, stack := range stacks(ready(t, registry)) {
		for _, form := range []model.Form{model.FormInline, model.FormSpec} {
			wrong, _ := checked(stack, form, cfg)
			found += len(wrong)
		}
	}

	if found == 0 {
		t.Error("the matrix accepted a layer whose output does not compile")
	}
}

// broken is a storage layer that composes like any other and emits a package
// that does not build.
//
// Only just broken, on purpose. It declares the type it was asked for and gives
// it a method whose body names something that is not there, so the output parses
// and formats and reaches the compile gate — which is the gate being tested. A
// fake that emitted nonsense would be caught by the printer and would prove
// nothing about compiling.
type broken struct{}

func (broken) Binds() []model.Import { return nil }
func (broken) Writes() []string      { return nil }
func (broken) Origin() model.TypeRef { return model.TypeRef{Pkg: model.MarkerPkg, Name: "Broken"} }
func (broken) Kind() model.Kind      { return model.KindStorage }
func (broken) Stage() layer.Stage    { return layer.StageReady }
func (broken) Doc() string           { return "storage that emits something that does not compile" }
func (broken) Transparent() bool     { return true }

func (broken) Accepts(shape.Shape) error { return nil }

func (broken) Shape(_ *layer.Context, below shape.Shape) shape.Shape {
	below.Caps = below.Caps.With(shape.Sized, shape.Streamable)
	return below
}

func (broken) OptionSchema() []layer.OptionDef { return nil }

func (broken) Generate(ctx *layer.Context, _ shape.Shape) (layer.Unit, error) {
	const source = "package tmpl\n\n" +
		"type Broken[T any] []T\n\n" +
		"func (c Broken[T]) Wrong() int { return nowhere }\n"

	out, diags := templates.Apply(
		templates.Template{Name: "broken", Source: []byte(source)},
		templates.Rewrite{
			Param: "T", Subject: "Person", Container: "Broken",
			Declared: ctx.Model.Name, Prefix: "broken",
		},
		ctx.Model.Pos)
	if err := diags.Err(); err != nil {
		return layer.Unit{}, err
	}

	// The type is left out where the author declared it, exactly as a real
	// storage layer does: emitting it as well would be caught as a redeclared
	// type, which is not the failure this is here to cause.
	decls := out.Decls
	if ctx.Model.Form == model.FormInline {
		decls = decls[1:]
	}

	return layer.Unit{Decls: decls, Comments: out.Comments, Fset: out.Fset}, nil
}

// A refusal nobody can act on fails the matrix.
//
// The other half of the harness, and the half that runs on nearly every case:
// most stacks are refused, so most of what the matrix does is read refusals.
// A checker that stopped reading them would leave the compile gate running on
// the few stacks that build and would look, from the outside, exactly like a
// harness that worked.
//
// Pinned the way the compile gate is pinned — by a layer written to produce the
// failure, rather than by asserting the checker's shape.
func TestTheMatrixFailsOnARefusalNobodyCanActOn(t *testing.T) {
	registry := layer.New()
	registry.MustRegister(unhelpful{})

	// Beside a real one, so that the stacks are the ones the matrix builds
	// rather than a shape it never sees.
	for _, one := range layers.Builtins().All() {
		if one.Origin().Name == "Slice" {
			registry.MustRegister(one)
		}
	}

	cfg := over(registry)
	found := 0

	for _, stack := range stacks(ready(t, registry)) {
		for _, form := range []model.Form{model.FormInline, model.FormSpec} {
			wrong, _ := checked(stack, form, cfg)
			found += len(wrong)
		}
	}

	if found == 0 {
		t.Error("the matrix accepted a refusal with nothing to act on")
	}
}

// unhelpful is a storage layer that refuses every stack and says nothing about
// what to do, which is the shape of refusal the matrix exists to forbid.
type unhelpful struct{}

func (unhelpful) Binds() []model.Import { return nil }
func (unhelpful) Writes() []string      { return nil }
func (unhelpful) Origin() model.TypeRef {
	return model.TypeRef{Pkg: model.MarkerPkg, Name: "Unhelpful"}
}
func (unhelpful) Kind() model.Kind   { return model.KindStorage }
func (unhelpful) Stage() layer.Stage { return layer.StageReady }
func (unhelpful) Doc() string        { return "storage that refuses without saying what to do" }
func (unhelpful) Transparent() bool  { return true }

func (unhelpful) Accepts(shape.Shape) error { return nil }

func (unhelpful) Shape(_ *layer.Context, below shape.Shape) shape.Shape { return below }

func (unhelpful) OptionSchema() []layer.OptionDef { return nil }

func (unhelpful) Generate(ctx *layer.Context, _ shape.Shape) (layer.Unit, error) {
	return layer.Unit{}, diag.New(codeUnhelpful, ctx.Model.Pos, "no")
}

// codeUnhelpful is registered so that what the matrix objects to is the missing
// hint rather than the code, which is the point being tested.
var codeUnhelpful = diag.Register(4998, "a layer refused and said nothing useful")
