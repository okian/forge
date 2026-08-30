package subject_test

import (
	"go/token"
	"go/types"
	"path/filepath"
	"slices"
	"testing"

	"github.com/okian/forge/internal/load"
	"github.com/okian/forge/internal/model"
	"github.com/okian/forge/internal/subject"
)

// elsewhere is a module path none of the fixture's packages belong to, which is
// how a test gets a cross-module boundary without a second module: what makes a
// type external is which module it is in, and the builder is told which one
// that is.
const elsewhere = "elsewhere"

// outside returns a builder for which every fixture type is another module's.
func outside(t *testing.T, loaded *load.Session) *subject.Builder {
	t.Helper()

	return subject.New(subject.Config{Fset: loaded.Fset, Module: elsewhere})
}

// An external struct is followed through the fields generated code could read
// and stopped at the ones it could not. Stopping at the type itself would leave
// a subject forge is expected to emit a standalone codec for with no model of
// what that codec has to encode; following everything would walk another
// module's internals, which nothing here will ever write a line for.
func TestAnExternalStructIsFollowedThroughItsExportedFields(t *testing.T) {
	loaded := session(t)

	inside, diags := builder(t, loaded).Build(named(t, loaded, "Hidden"), token.Position{})
	if !diags.Empty() {
		t.Fatalf("Hidden does not model clean:\n%s", diags.Render())
	}
	if got, want := closureNames(inside), []string{"Unit", "Address"}; !slices.Equal(got, want) {
		t.Fatalf("inside the module the closure is %v, want %v", got, want)
	}

	foreign, diags := outside(t, loaded).Build(named(t, loaded, "Hidden"), token.Position{})
	if !diags.Empty() {
		t.Fatalf("Hidden does not model clean from outside:\n%s", diags.Render())
	}
	if !foreign.External {
		t.Fatal("Hidden is not external to a module it does not belong to")
	}

	// Shown is followed and hidden is not, so Address — reached only through
	// the unexported field — is absent.
	if got, want := closureNames(foreign), []string{"Unit"}; !slices.Equal(got, want) {
		t.Errorf("outside the module the closure is %v, want %v", got, want)
	}

	// The fields themselves are still described. What stops is the walk, not
	// the description.
	if got, want := len(foreign.Fields), 2; got != want {
		t.Errorf("described %d fields, want %d", got, want)
	}
}

// A type whose every field is unexported contributes its name and nothing else,
// which is what a standard library type reached from a subject should do.
func TestAnExternalStructWithNothingReadableIsALeaf(t *testing.T) {
	external := build(t, "External")

	when := external.Closure[0]
	if got, want := when.Ref().Name, "Time"; got != want {
		t.Fatalf("the first reached type is %s, want %s", got, want)
	}
	if len(when.Closure) != 0 {
		t.Errorf("time.Time reaches %v, want nothing readable from here", closureNames(when))
	}
}

// A tag in a dependency is not the author's to fix, and a diagnostic pointing
// into a module cache with a hint about how to write the tag is worse than no
// diagnostic at all.
func TestAMalformedTagInAnotherModuleIsNotReported(t *testing.T) {
	loaded := session(t)

	if _, diags := builder(t, loaded).Build(named(t, loaded, "Tagged"), token.Position{}); diags.Len() != 1 {
		t.Fatalf("inside the module the tag reported %d diagnostics, want 1", diags.Len())
	}

	built, diags := outside(t, loaded).Build(named(t, loaded, "Tagged"), token.Position{})
	if !diags.Empty() {
		t.Errorf("a tag in another module was reported:\n%s", diags.Render())
	}
	// It is still parsed, so a layer can see what is there and decide.
	if field, _ := built.Field("Broken"); len(field.Tags) == 0 {
		t.Error("the tag was not parsed at all")
	}
}

// What is recorded about a field is about the field's own type. A pointer to a
// type that implements an interface implements it too, and a pointer to another
// module's type is no more attachable than the type is.
func TestFieldsAnswerForTheTypeAsWritten(t *testing.T) {
	loaded := session(t)
	stringer := stringerLike(model.TypeRef{Pkg: "fmt", Name: "Stringer"})

	built, diags := subject.New(subject.Config{
		Fset:       loaded.Fset,
		Module:     fixtureModule,
		Interfaces: []subject.Interface{stringer},
	}).Build(named(t, loaded, "Shapes"), token.Position{})
	if !diags.Empty() {
		t.Fatalf("Shapes does not model clean:\n%s", diags.Render())
	}

	for _, name := range []string{"Value", "Ptr"} {
		if field, _ := built.Field(name); !field.Satisfies(stringer.Ref) {
			t.Errorf("%s does not satisfy %v", name, stringer.Ref)
		}
	}
	// A slice of them is not one of them.
	if field, _ := built.Field("Slice"); field.Satisfies(stringer.Ref) {
		t.Errorf("Slice satisfies %v", stringer.Ref)
	}

	// And the same for the module boundary: what is written is what answers.
	foreign, _ := outside(t, loaded).Build(named(t, loaded, "Shapes"), token.Position{})
	for _, name := range []string{"Value", "Ptr"} {
		if field, _ := foreign.Field(name); !field.External {
			t.Errorf("%s is not external to a module it does not belong to", name)
		}
	}
	if field, _ := foreign.Field("Slice"); field.External {
		t.Error("a slice is reported as a type a method could be attached to")
	}
}

// The collision question is about a method name, and it is not the same
// question as whether an interface is satisfied: half a codec satisfies nothing
// and still collides.
func TestDeclaredMethodsAreRecorded(t *testing.T) {
	holder := build(t, "Holder")

	for _, reached := range holder.Closure {
		if got, want := reached.Methods, []string{"String"}; !slices.Equal(got, want) {
			t.Errorf("%s declares %v, want %v", reached, got, want)
		}
		if !reached.HasMethod("String") {
			t.Errorf("%s does not report the method it declares", reached)
		}
		if reached.HasMethod("MarshalJSONTo") {
			t.Errorf("%s reports a method nobody wrote", reached)
		}
	}

	// Holder embeds nothing and declares nothing, so it collides with nothing.
	if len(holder.Methods) != 0 {
		t.Errorf("Holder declares %v, want nothing", holder.Methods)
	}
}

// A subject that is still generic has no fields yet, only type parameters, and
// a model built from one would describe nothing in particular.
func TestOpenSubjectsAreRefused(t *testing.T) {
	loaded := session(t)
	pair := named(t, loaded, "Pair")
	value := pair.TypeParams().At(1)

	// A parameter can hide inside an argument as easily as it can be one, and
	// what comes out the other side is the same: a field with no shape.
	wrapped, err := types.Instantiate(nil, named(t, loaded, "Wrapping"), []types.Type{value}, false)
	if err != nil {
		t.Fatalf("instantiate Wrapping by a parameter: %v", err)
	}

	instantiate := func(arg types.Type) types.Type {
		t.Helper()

		out, err := types.Instantiate(nil, pair, []types.Type{types.Typ[types.String], arg}, false)
		if err != nil {
			t.Fatalf("instantiate Pair with %s: %v", arg, err)
		}
		return out
	}

	// One case per way an argument can be written around a parameter, because
	// every one of them ends in the same place: a field whose type is a
	// parameter, in a struct routed down the path that generates code for it.
	cases := map[string]types.Type{
		"a generic type":                  pair,
		"an instantiation by a parameter": instantiate(value),
		"a parameter behind a pointer":    instantiate(types.NewPointer(value)),
		"a parameter in a slice":          instantiate(types.NewSlice(value)),
		"a parameter in an array":         instantiate(types.NewArray(value, 3)),
		"a parameter in a channel":        instantiate(types.NewChan(types.SendRecv, value)),
		"a parameter in a map":            instantiate(types.NewMap(types.Typ[types.String], value)),
		"a parameter in a struct": instantiate(types.NewStruct(
			[]*types.Var{types.NewVar(token.NoPos, nil, "Held", value)}, nil)),
		"a parameter inside a name": instantiate(wrapped),
	}

	for name, subjectType := range cases {
		t.Run(name, func(t *testing.T) {
			built, diags := builder(t, loaded).Build(subjectType, token.Position{})

			if built != nil {
				t.Errorf("built %s from it", built)
			}
			if diags.Len() != 1 {
				t.Fatalf("reported %d diagnostics, want 1:\n%s", diags.Len(), diags.Render())
			}
			if got, want := diags.All()[0].Code.String(), "FRG2003"; got != want {
				t.Errorf("code is %s, want %s", got, want)
			}
		})
	}
}

// An instantiation cycle has no fixed point: every level is a different type,
// so the memo never catches and the walk never ends. The compiler rejects it,
// which is no help — forge reads packages that do not compile on purpose — so
// the walk has to end by itself, and say so.
func TestAnInstantiationCycleIsReportedRatherThanFollowed(t *testing.T) {
	dir, err := filepath.Abs(filepath.Join("testdata", "cyclic"))
	if err != nil {
		t.Fatalf("resolve fixture: %v", err)
	}

	loaded, err := load.Load(load.Config{Dir: dir})
	if err != nil {
		t.Fatalf("load fixture: %v", err)
	}
	// The package does not build, which is the point of it.
	if loaded.Diagnostics.Empty() {
		t.Fatal("the fixture compiles, so it is not the fixture this test needs")
	}

	pkg, ok := loaded.Package("cyclicfixture/broken")
	if !ok {
		t.Fatal("the fixture yielded no package")
	}
	start, ok := types.Unalias(pkg.Types.Scope().Lookup("Start").Type()).(*types.Named)
	if !ok {
		t.Fatal("Start is not a named type")
	}

	built, diags := subject.New(subject.Config{Fset: loaded.Fset, Module: "cyclicfixture"}).
		Build(start, token.Position{Filename: "spec.go", Line: 1, Column: 1})

	if built == nil {
		t.Fatal("Start modelled to nothing")
	}
	if diags.Empty() {
		t.Fatal("the walk ended without saying why")
	}
	if got, want := diags.All()[0].Code.String(), "FRG2006"; got != want {
		t.Errorf("code is %s, want %s", got, want)
	}
}
