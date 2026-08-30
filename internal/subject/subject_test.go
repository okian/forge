package subject_test

import (
	"go/token"
	"go/types"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/okian/forge/internal/load"
	"github.com/okian/forge/internal/model"
	"github.com/okian/forge/internal/subject"
)

// fixtureModule is the module the fixture packages belong to, which is what
// decides whether a type is one forge could attach a method to.
const fixtureModule = "subjectsfixture"

// domainPkg is the package the subjects are declared in.
const domainPkg = fixtureModule + "/domain"

// session loads the fixture module once for the whole suite.
func session(t *testing.T) *load.Session {
	t.Helper()

	dir, err := filepath.Abs(filepath.Join("testdata", "subjects"))
	if err != nil {
		t.Fatalf("resolve fixture: %v", err)
	}

	loaded, err := load.Load(load.Config{Dir: dir})
	if err != nil {
		t.Fatalf("load fixture: %v", err)
	}
	if !loaded.Diagnostics.Empty() {
		t.Fatalf("fixture does not load clean:\n%s", loaded.Diagnostics.Render())
	}
	return loaded
}

// named returns the named type declared in the domain package under this name.
func named(t *testing.T, loaded *load.Session, name string) *types.Named {
	t.Helper()

	pkg, ok := loaded.Package(domainPkg)
	if !ok {
		t.Fatalf("fixture has no package %s", domainPkg)
	}

	obj := pkg.Types.Scope().Lookup(name)
	if obj == nil {
		t.Fatalf("%s declares no %s", domainPkg, name)
	}
	typ, ok := types.Unalias(obj.Type()).(*types.Named)
	if !ok {
		t.Fatalf("%s is a %T, want a named type", name, obj.Type())
	}
	return typ
}

// builder returns a builder over the fixture module.
func builder(t *testing.T, loaded *load.Session, interfaces ...subject.Interface) *subject.Builder {
	t.Helper()

	return subject.New(subject.Config{
		Fset:       loaded.Fset,
		Module:     fixtureModule,
		Interfaces: interfaces,
	})
}

// build models one fixture subject and fails if anything was reported.
func build(t *testing.T, name string) *model.Struct {
	t.Helper()

	loaded := session(t)
	built, diags := builder(t, loaded).Build(named(t, loaded, name), token.Position{})
	if !diags.Empty() {
		t.Fatalf("%s does not model clean:\n%s", name, diags.Render())
	}
	if built == nil {
		t.Fatalf("%s modelled to nothing", name)
	}
	return built
}

// fieldNames returns a struct's own field names, in order.
func fieldNames(s *model.Struct) []string { return s.FieldNames() }

// closureNames returns the names of the structs in a closure, in order.
func closureNames(s *model.Struct) []string {
	out := make([]string, len(s.Closure))
	for i, reached := range s.Closure {
		out[i] = reached.Ref().Name
	}
	return out
}

// Fields are modelled in the order they were written, because generated output
// follows that order and a field added in the middle of a struct should produce
// a diff in the middle of a generated file.
func TestFieldsAreModelledInDeclarationOrder(t *testing.T) {
	person := build(t, "Person")

	want := []string{"Name", "Age", "Address", "Since", "secret"}
	if got := fieldNames(person); !slices.Equal(got, want) {
		t.Fatalf("fields = %v, want %v", got, want)
	}

	if person.Ref() != (model.TypeRef{Pkg: domainPkg, Name: "Person"}) {
		t.Errorf("Ref() = %v", person.Ref())
	}
	if person.External || person.Instantiated || person.Cyclic {
		t.Errorf("Person is external=%v instantiated=%v cyclic=%v, want all false",
			person.External, person.Instantiated, person.Cyclic)
	}
	if !person.Local() {
		t.Error("Person is not local, so nothing could attach a method to it")
	}
}

// A field that cannot be read from outside its package cannot be read by
// generated code in another one either, so whether it is exported is part of
// the model rather than something a layer works out again.
func TestExportIsRecorded(t *testing.T) {
	person := build(t, "Person")

	cases := map[string]bool{"Name": true, "Age": true, "secret": false}
	for name, want := range cases {
		field, ok := person.Field(name)
		if !ok {
			t.Fatalf("Person has no field %s", name)
		}
		if field.Exported != want {
			t.Errorf("%s is exported=%v, want %v", name, field.Exported, want)
		}
	}
}

// Tags are parsed once, here, and every layer reads the same interpretation.
func TestTagsAreParsedOntoTheField(t *testing.T) {
	person := build(t, "Person")

	name, _ := person.Field("Name")
	if got, want := name.TagKeys(), []string{"json", "db"}; !slices.Equal(got, want) {
		t.Fatalf("Name carries keys %v, want %v", got, want)
	}
	if tag, _ := name.Tag("json"); tag.Name != "name" {
		t.Errorf("the json tag names %q, want %q", tag.Name, "name")
	}
	if tag, _ := name.Tag("db"); tag.Name != "full_name" {
		t.Errorf("the db tag names %q, want %q", tag.Name, "full_name")
	}

	age, _ := person.Field("Age")
	if tag, _ := age.Tag("json"); !tag.Has("omitzero") {
		t.Errorf("Age's json tag carries %v, want omitzero", tag.Options)
	}
	// Under a convention the leading element is a rule like any other, so the
	// first one lands in the name and the rest in the options. Reassembling
	// them is the reading layer's business, not the parser's.
	validate, _ := age.Tag("validate")
	if got, want := validate.Name, "required"; got != want {
		t.Errorf("Age's validate tag opens with %q, want %q", got, want)
	}
	if got, want := validate.Value("min"), "0"; got != want {
		t.Errorf("Age's validate tag has min=%q, want %q", got, want)
	}

	since, _ := person.Field("Since")
	if len(since.Tags) != 0 {
		t.Errorf("Since carries tags %v, want none", since.Tags)
	}
}

// A tag that will not parse is the author's mistake, and it is reported where
// the author wrote it rather than where the declaration is: a subject may be
// reached from several declarations and is written in exactly one place.
func TestAMalformedTagIsReportedAtItsField(t *testing.T) {
	loaded := session(t)

	built, diags := builder(t, loaded).Build(named(t, loaded, "Tagged"), token.Position{})
	if built == nil {
		t.Fatal("a malformed tag stopped the model being built")
	}
	if got := diags.Len(); got != 1 {
		t.Fatalf("reported %d diagnostics, want 1:\n%s", got, diags.Render())
	}

	reported := diags.All()[0]
	if got, want := reported.Code.String(), "FRG2004"; got != want {
		t.Errorf("code is %s, want %s", got, want)
	}
	if !strings.Contains(reported.Message, "Broken") {
		t.Errorf("message %q does not name the field", reported.Message)
	}
	if filepath.Base(reported.Pos.Filename) != "domain.go" || reported.Pos.Line == 0 {
		t.Errorf("position is %s, want the field's own", reported.Pos)
	}

	// The rest of the model is still true, so a layer that reads none of the
	// tags still has something to work from.
	if got, want := fieldNames(built), []string{"Broken"}; !slices.Equal(got, want) {
		t.Errorf("fields = %v, want %v", got, want)
	}
}

// An embedded field carries the name Go gives it, which is its type's name, and
// the fields it promotes belong to the type it names rather than to this one.
func TestEmbeddingIsRecordedWithoutBeingFlattened(t *testing.T) {
	contact := build(t, "Contact")

	want := []string{"Person", "Address", "Preferred"}
	if got := fieldNames(contact); !slices.Equal(got, want) {
		t.Fatalf("fields = %v, want %v", got, want)
	}

	for _, name := range []string{"Person", "Address"} {
		field, _ := contact.Field(name)
		if !field.Embedded {
			t.Errorf("%s is not marked embedded", name)
		}
	}
	if field, _ := contact.Field("Preferred"); field.Embedded {
		t.Error("Preferred is marked embedded")
	}

	// A pointer to an embedded struct is still an embedded struct.
	if field, _ := contact.Field("Address"); field.Type.Class != model.ClassPointer {
		t.Errorf("Address is classified %s, want a pointer", field.Type.Class)
	}
}

// The reachable set is what lets generation emit a codec for a struct's fields
// as well as for the struct, and emit each of them once.
func TestClosureHoldsEveryStructReachable(t *testing.T) {
	person := build(t, "Person")

	// Depth first in field order: Address before the Unit it reaches, and
	// time.Time last because Since is written last.
	want := []string{"Address", "Unit", "Time"}
	if got := closureNames(person); !slices.Equal(got, want) {
		t.Fatalf("closure = %v, want %v", got, want)
	}

	// Every member is the same value the builder would hand out for that type,
	// which is what stops two declarations generating two codecs for one
	// struct.
	address := person.Closure[0]
	if got, want := closureNames(address), []string{"Unit"}; !slices.Equal(got, want) {
		t.Errorf("Address's closure = %v, want %v", got, want)
	}
	if address.Closure[0] != person.Closure[1] {
		t.Error("Unit is two values, so it would be generated for twice")
	}
}

// A type from outside the module is recorded, because a layer has to decide
// what to do about it, and is not opened: its unexported fields cannot be read
// from here at all, so what it reaches is not a set anything here could
// generate for.
func TestExternalTypesAreRecordedButNotFollowed(t *testing.T) {
	external := build(t, "External")

	want := []string{"Time", "Place"}
	if got := closureNames(external); !slices.Equal(got, want) {
		t.Fatalf("closure = %v, want %v", got, want)
	}

	when, _ := external.Field("When")
	if !when.External {
		t.Error("time.Time is not marked external")
	}
	where, _ := external.Field("Where")
	if where.External {
		t.Error("a type in a second package of the same module is marked external")
	}

	stdlib := external.Closure[0]
	if !stdlib.External || stdlib.Local() {
		t.Errorf("time.Time is external=%v local=%v", stdlib.External, stdlib.Local())
	}
	if len(stdlib.Closure) != 0 {
		t.Errorf("time.Time's closure is %v, want nothing followed", closureNames(stdlib))
	}
}

// A type that reaches itself is a linked list or a tree, not a mistake. It is
// recorded so that a layer walking the closure terminates, and it is kept out
// of its own closure so that nothing counts it twice.
func TestCyclesAreRecordedRatherThanRefused(t *testing.T) {
	node := build(t, "Node")

	if !node.Cyclic {
		t.Error("Node does not reach itself")
	}
	if len(node.Closure) != 0 {
		t.Errorf("Node's closure is %v, want nothing but itself, which is excluded", closureNames(node))
	}
}

// Two types that reach each other are both cyclic, and each holds the other.
func TestMutualCyclesAreRecordedOnBothSides(t *testing.T) {
	loaded := session(t)
	shared := builder(t, loaded)

	ring, _ := shared.Build(named(t, loaded, "Ring"), token.Position{})
	spoke, _ := shared.Build(named(t, loaded, "Spoke"), token.Position{})

	if !ring.Cyclic || !spoke.Cyclic {
		t.Errorf("cyclic: Ring=%v Spoke=%v, want both", ring.Cyclic, spoke.Cyclic)
	}
	if got, want := closureNames(ring), []string{"Spoke"}; !slices.Equal(got, want) {
		t.Errorf("Ring's closure = %v, want %v", got, want)
	}
	if got, want := closureNames(spoke), []string{"Ring"}; !slices.Equal(got, want) {
		t.Errorf("Spoke's closure = %v, want %v", got, want)
	}
}

// A name over something that is not a struct is looked through rather than
// followed, and a name that mentions itself would be looked through forever.
func TestANameThatLoopsThroughItselfTerminates(t *testing.T) {
	registry := build(t, "Registry")

	if got, want := closureNames(registry), []string{"Unit"}; !slices.Equal(got, want) {
		t.Fatalf("closure = %v, want %v", got, want)
	}
	if all, _ := registry.Field("All"); all.Type.Class != model.ClassNamed {
		t.Errorf("All is classified %s, want named", all.Type.Class)
	}
}

// Two declarations specialised to one subject get one model, which is what lets
// generation emit one codec for it rather than one per declaration.
func TestOneTypeIsModelledOnce(t *testing.T) {
	loaded := session(t)
	shared := builder(t, loaded)

	first, _ := shared.Build(named(t, loaded, "Person"), token.Position{})
	second, _ := shared.Build(named(t, loaded, "Person"), token.Position{})

	if first != second {
		t.Error("one type modelled twice")
	}

	// And a subject reached from another subject is the same value again.
	address, _ := shared.Build(named(t, loaded, "Address"), token.Position{})
	if address != first.Closure[0] {
		t.Error("Address is one value as a subject and another as a field")
	}
}

// Go cannot attach a method to an instantiation, so a subject that is one takes
// the standalone path — and the model has to say which it is.
func TestInstantiationsAreRecorded(t *testing.T) {
	keyed := build(t, "Keyed")

	entry, _ := keyed.Field("Entry")
	if entry.Type.Class != model.ClassNamed {
		t.Fatalf("Entry is classified %s, want named", entry.Type.Class)
	}
	if entry.Type.Ref.Args == "" {
		t.Errorf("Entry's reference %v records no type arguments", entry.Type.Ref)
	}

	if got, want := closureNames(keyed), []string{"Pair", "Unit"}; !slices.Equal(got, want) {
		t.Fatalf("closure = %v, want %v", got, want)
	}
	pair := keyed.Closure[0]
	if !pair.Instantiated {
		t.Error("Pair[string, Unit] is not marked instantiated")
	}
	if pair.Local() {
		t.Error("Pair[string, Unit] reports that a method could attach to it")
	}
	// The instantiation's fields are substituted, so the model describes the
	// type that was written and not the generic it came from.
	if key, _ := pair.Field("Key"); key.Type.Class != model.ClassBasic {
		t.Errorf("Key is classified %s, want basic", key.Type.Class)
	}
}

// Generating twice from identical inputs has to produce identical output, and
// nothing downstream can restore an order this stage lost.
func TestModellingIsDeterministic(t *testing.T) {
	loaded := session(t)

	var first []string
	for range 5 {
		built, diags := builder(t, loaded).Build(named(t, loaded, "Person"), token.Position{})
		if !diags.Empty() {
			t.Fatalf("Person does not model clean:\n%s", diags.Render())
		}

		got := closureNames(built)
		if first == nil {
			first = got
			continue
		}
		if !slices.Equal(got, first) {
			t.Fatalf("closure = %v, want %v on every run", got, first)
		}
	}
}

// A struct reached only through a closure is described as completely as one
// built as a subject, or a layer walking the closure would meet a type that
// claims to reach nothing and recurse into it forever.
func TestAStructReachedOnlyThroughAClosureIsCompleteToo(t *testing.T) {
	ring := build(t, "Ring")

	spoke := ring.Closure[0]
	if got, want := spoke.Ref().Name, "Spoke"; got != want {
		t.Fatalf("Ring reaches %s, want %s", got, want)
	}
	if !spoke.Cyclic {
		t.Error("Spoke does not reach itself, though it reaches Ring which reaches it")
	}
	if got, want := closureNames(spoke), []string{"Ring"}; !slices.Equal(got, want) {
		t.Errorf("Spoke's closure = %v, want %v", got, want)
	}
	if len(spoke.Fields) == 0 {
		t.Error("Spoke has no fields, so it was never opened")
	}
}

// Go cannot attach a method to an instantiation, and a subject that is one has
// to say so rather than leave a layer to work it out.
func TestAnInstantiatedSubjectSaysSo(t *testing.T) {
	loaded := session(t)

	keyed, _ := builder(t, loaded).Build(named(t, loaded, "Keyed"), token.Position{})
	entry, _ := keyed.Field("Entry")

	built, diags := builder(t, loaded).Build(entry.Type.Type, token.Position{})
	if !diags.Empty() {
		t.Fatalf("Pair[string, Unit] does not model clean:\n%s", diags.Render())
	}
	if !built.Instantiated || built.Local() {
		t.Errorf("instantiated=%v local=%v, want true and false", built.Instantiated, built.Local())
	}
}

// A named type over something that is not a struct is a subject like any other.
// It simply has no fields.
func TestANamedScalarIsASubject(t *testing.T) {
	celsius := build(t, "Celsius")

	if len(celsius.Fields) != 0 {
		t.Errorf("fields = %v, want none", fieldNames(celsius))
	}
	if !celsius.Local() {
		t.Error("Celsius reports that no method could attach to it")
	}
}
