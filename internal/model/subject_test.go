package model_test

import (
	"go/token"
	"go/types"
	"testing"

	"github.com/okian/forge/internal/model"
	"github.com/okian/forge/internal/tags"
)

const subjectPkg = "example.com/app/domain"

// namedStruct builds a named struct type without running a type-checker, which
// keeps these tests about the model rather than about loading.
func namedStruct(t *testing.T, pkgPath, pkgName, typeName string) *types.Named {
	t.Helper()
	pkg := types.NewPackage(pkgPath, pkgName)
	obj := types.NewTypeName(token.NoPos, pkg, typeName, nil)
	return types.NewNamed(obj, types.NewStruct(nil, nil), nil)
}

func personStruct(t *testing.T) *model.Struct {
	t.Helper()
	return &model.Struct{
		Named: namedStruct(t, subjectPkg, "domain", "Person"),
		Implements: []model.TypeRef{
			{Pkg: "encoding/json/v2", Name: "MarshalerTo"},
		},
		Fields: []model.Field{
			{
				Name:     "Name",
				Type:     model.Classified{Class: model.ClassBasic, Type: types.Typ[types.String]},
				Exported: true,
				Tags: []tags.Tag{
					{Key: "json", Raw: "name", Name: "name"},
					{Key: "db", Raw: "full_name", Name: "full_name"},
				},
			},
			{
				Name:     "Age",
				Type:     model.Classified{Class: model.ClassBasic, Type: types.Typ[types.Int]},
				Exported: true,
			},
			{
				Name: "secret",
				Type: model.Classified{Class: model.ClassBasic, Type: types.Typ[types.String]},
			},
		},
	}
}

func TestClassString(t *testing.T) {
	cases := map[model.Class]string{
		model.ClassInvalid:   "invalid",
		model.ClassBasic:     "basic",
		model.ClassNamed:     "named",
		model.ClassPointer:   "pointer",
		model.ClassSlice:     "slice",
		model.ClassArray:     "array",
		model.ClassMap:       "map",
		model.ClassStruct:    "struct",
		model.ClassInterface: "interface",
		model.ClassChan:      "chan",
		model.ClassFunc:      "func",
		model.Class(200):     "class(200)",
	}

	for class, want := range cases {
		if got := class.String(); got != want {
			t.Errorf("Class(%d).String() = %q, want %q", uint8(class), got, want)
		}
	}
}

func TestClassValid(t *testing.T) {
	cases := map[model.Class]bool{
		model.ClassInvalid:   false,
		model.ClassBasic:     true,
		model.ClassNamed:     true,
		model.ClassPointer:   true,
		model.ClassSlice:     true,
		model.ClassArray:     true,
		model.ClassMap:       true,
		model.ClassStruct:    true,
		model.ClassInterface: true,
		model.ClassChan:      true,
		model.ClassFunc:      true,
		model.Class(200):     false,
	}

	for class, want := range cases {
		if got := class.Valid(); got != want {
			t.Errorf("Class(%d).Valid() = %v, want %v", uint8(class), got, want)
		}
	}
}

// A rendered type is what a reader of a diagnostic sees, so it is qualified by
// package name rather than by the import path go/types prints by default.
func TestClassifiedStringQualifiesByPackageName(t *testing.T) {
	person := namedStruct(t, subjectPkg, "domain", "Person")

	cases := map[string]struct {
		classified model.Classified
		want       string
	}{
		"basic": {
			model.Classified{Class: model.ClassBasic, Type: types.Typ[types.Int]},
			"int",
		},
		"named": {
			model.Classified{Class: model.ClassNamed, Type: person, Ref: model.TypeRef{Pkg: subjectPkg, Name: "Person"}},
			"domain.Person",
		},
		"nested composite": {
			model.Classified{
				Class: model.ClassMap,
				Type:  types.NewMap(types.Typ[types.String], types.NewSlice(types.NewPointer(person))),
				Key:   &model.Classified{Class: model.ClassBasic, Type: types.Typ[types.String]},
				Elem: &model.Classified{
					Class: model.ClassSlice,
					Type:  types.NewSlice(types.NewPointer(person)),
					Elem:  &model.Classified{Class: model.ClassPointer, Type: types.NewPointer(person)},
				},
			},
			"map[string][]*domain.Person",
		},
		"unclassified": {
			model.Classified{},
			"<unclassified>",
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := tc.classified.String(); got != tc.want {
				t.Errorf("String() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestFieldTag(t *testing.T) {
	field := personStruct(t).Fields[0]

	tag, ok := field.Tag("json")
	if !ok {
		t.Fatal("Tag(json) reported absent")
	}
	if tag.Name != "name" {
		t.Errorf("Tag(json).Name = %q, want %q", tag.Name, "name")
	}

	if _, ok := field.Tag("validate"); ok {
		t.Error("Tag(validate) reported present")
	}

	// A field with no tags at all must answer the same way, not panic.
	if _, ok := (model.Field{}).Tag("json"); ok {
		t.Error("Tag on an untagged field reported present")
	}

	// Keys come back in the order they were written, so a diagnostic can quote
	// the tag the way the author sees it.
	keys := field.TagKeys()
	want := []string{"json", "db"}
	if len(keys) != len(want) {
		t.Fatalf("TagKeys() = %v, want %v", keys, want)
	}
	for i := range want {
		if keys[i] != want[i] {
			t.Errorf("TagKeys()[%d] = %q, want %q", i, keys[i], want[i])
		}
	}
}

// Two instantiations of one generic type are two types. If they shared an
// identity a closure holding both would silently keep one, and the generated
// code for the other would never be emitted.
func TestStructRefDistinguishesInstantiations(t *testing.T) {
	generic := genericPair(t)

	boxOfInt := instantiatedStruct(t, generic, types.Typ[types.String], types.Typ[types.Int])
	boxOfString := instantiatedStruct(t, generic, types.Typ[types.String], types.Typ[types.String])

	intRef, stringRef := boxOfInt.Ref(), boxOfString.Ref()
	if intRef == stringRef {
		t.Fatalf("Pair[string, int] and Pair[string, string] share the identity %v", intRef)
	}
	if intRef.Args == "" {
		t.Errorf("Ref() of an instantiation recorded no type arguments: %+v", intRef)
	}
	if intRef.Less(stringRef) == stringRef.Less(intRef) {
		t.Errorf("Less cannot separate %v from %v", intRef, stringRef)
	}

	// The origin is still reachable, because that is what a registry is keyed by.
	if got, want := intRef.Origin(), (model.TypeRef{Pkg: subjectPkg, Name: "Pair"}); got != want {
		t.Errorf("Origin() = %+v, want %+v", got, want)
	}
	if got, want := boxOfInt.String(), subjectPkg+".Pair[string,int]"; got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}

	// Arguments are spelled by import path, not by package name: two packages
	// may share a name, and two identities may not.
	named := instantiatedStruct(t, generic, types.Typ[types.String], namedStruct(t, subjectPkg, "domain", "Person"))
	if got, want := named.Ref().Args, "[string,"+subjectPkg+".Person]"; got != want {
		t.Errorf("Args = %q, want %q", got, want)
	}
}

// A layer that would generate an interface the type already implements must be
// able to see that and stay quiet, or the output does not compile.
func TestSatisfies(t *testing.T) {
	marshalerTo := model.TypeRef{Pkg: "encoding/json/v2", Name: "MarshalerTo"}
	binaryMarshaler := model.TypeRef{Pkg: "encoding", Name: "BinaryMarshaler"}

	person := personStruct(t)
	if !person.Satisfies(marshalerTo) {
		t.Error("Satisfies(MarshalerTo) = false, want true")
	}
	if person.Satisfies(binaryMarshaler) {
		t.Error("Satisfies(BinaryMarshaler) = true; one codec does not imply another")
	}

	var nilStruct *model.Struct
	if nilStruct.Satisfies(marshalerTo) {
		t.Error("Satisfies on a nil struct reported true")
	}

	field := model.Field{Implements: []model.TypeRef{marshalerTo}}
	if !field.Satisfies(marshalerTo) || field.Satisfies(binaryMarshaler) {
		t.Error("Field.Satisfies does not distinguish the interfaces it was given")
	}
}

// genericPair builds a generic named type with two type parameters.
func genericPair(t *testing.T) *types.Named {
	t.Helper()

	pkg := types.NewPackage(subjectPkg, "domain")
	obj := types.NewTypeName(token.NoPos, pkg, "Pair", nil)
	named := types.NewNamed(obj, types.NewStruct(nil, nil), nil)

	params := make([]*types.TypeParam, 0, 2)
	for _, name := range []string{"K", "V"} {
		params = append(params, types.NewTypeParam(
			types.NewTypeName(token.NoPos, pkg, name, nil),
			types.NewInterfaceType(nil, nil),
		))
	}
	named.SetTypeParams(params)

	return named
}

// instantiatedStruct instantiates a generic named type and models it.
func instantiatedStruct(t *testing.T, generic *types.Named, args ...types.Type) *model.Struct {
	t.Helper()

	inst, err := types.Instantiate(nil, generic, args, true)
	if err != nil {
		t.Fatalf("instantiate %s: %v", generic.Obj().Name(), err)
	}
	named, ok := inst.(*types.Named)
	if !ok {
		t.Fatalf("instantiation is a %T, want a named type", inst)
	}
	return &model.Struct{Named: named, Instantiated: true}
}

// A struct with no type yet has to hand out a types.Type that is nil, not a
// types.Type holding a nil pointer, which is a different value and renders as a
// panic.
func TestStructType(t *testing.T) {
	person := namedStruct(t, subjectPkg, "domain", "Person")

	if got := (&model.Struct{Named: person}).Type(); got != person {
		t.Errorf("Type() = %v, want %v", got, person)
	}
	if got := (&model.Struct{}).Type(); got != nil {
		t.Errorf("Type() of a struct with no type = %v, want nil", got)
	}

	var missing *model.Struct
	if got := missing.Type(); got != nil {
		t.Errorf("Type() of a nil struct = %v, want nil", got)
	}
}

// A rendered declaration spells its types the way its source does, while a
// [model.TypeRef] spells them the way an identity has to, qualified by import
// path. Confusing the two puts an import path inside a type argument in a
// diagnostic, which is the one place a reader is least equipped to skip it.
func TestTypeString(t *testing.T) {
	person := namedStruct(t, subjectPkg, "domain", "Person")
	pair := instantiatedStruct(t, genericPair(t), types.Typ[types.String], person)

	cases := map[string]struct {
		subject types.Type
		want    string
	}{
		"nothing":      {nil, "?"},
		"basic":        {types.Typ[types.Int], "int"},
		"named":        {person, "Person"},
		"pointer":      {types.NewPointer(person), "*Person"},
		"slice":        {types.NewSlice(person), "[]Person"},
		"instantiated": {pair.Named, "Pair[string, Person]"},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := model.TypeString(tc.subject); got != tc.want {
				t.Errorf("TypeString() = %q, want %q", got, tc.want)
			}
		})
	}
}

// The class says how a type is written; this says what it is. A named
// interface is ClassNamed, so a layer refusing what it cannot see through has
// to ask the question rather than read the class.
func TestClassifiedIsInterface(t *testing.T) {
	empty := types.NewInterfaceType(nil, nil).Complete()
	pkg := types.NewPackage(subjectPkg, "domain")
	behind := types.NewNamed(types.NewTypeName(token.NoPos, pkg, "Reader", nil), empty, nil)

	cases := map[string]struct {
		subject model.Classified
		want    bool
	}{
		"written in place": {model.Classified{Class: model.ClassInterface, Type: empty}, true},
		"behind a name":    {model.Classified{Class: model.ClassNamed, Type: behind}, true},
		"a struct":         {model.Classified{Class: model.ClassStruct, Type: types.NewStruct(nil, nil)}, false},
		"unclassified":     {model.Classified{}, false},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := tc.subject.IsInterface(); got != tc.want {
				t.Errorf("IsInterface() = %v, want %v", got, tc.want)
			}
		})
	}
}

// A method name already taken is a redeclaration, which is a different question
// from whether the type satisfies anything.
func TestStructHasMethod(t *testing.T) {
	subject := &model.Struct{Methods: []string{"MarshalJSONTo", "String"}}

	if !subject.HasMethod("String") {
		t.Error("HasMethod does not report a method the type declares")
	}
	if subject.HasMethod("UnmarshalJSONFrom") {
		t.Error("HasMethod reports a method nobody declared")
	}

	var missing *model.Struct
	if missing.HasMethod("String") {
		t.Error("HasMethod on a nil struct reports a method")
	}
}

func TestFieldString(t *testing.T) {
	field := personStruct(t).Fields[1]
	if got, want := field.String(), "Age int"; got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
}

func TestStructRef(t *testing.T) {
	person := personStruct(t)
	want := model.TypeRef{Pkg: subjectPkg, Name: "Person"}
	if got := person.Ref(); got != want {
		t.Errorf("Ref() = %+v, want %+v", got, want)
	}

	if got := (&model.Struct{}).Ref(); !got.IsZero() {
		t.Errorf("Ref() on an unresolved struct = %+v, want the zero value", got)
	}

	var nilStruct *model.Struct
	if got := nilStruct.Ref(); !got.IsZero() {
		t.Errorf("Ref() on a nil struct = %+v, want the zero value", got)
	}
}

// Whether a method can be attached to the subject decides whether an element
// layer emits methods or standalone functions, so the predicate has to be
// exactly right for each reason it can fail.
func TestWhereAMethodOnASubjectMayGo(t *testing.T) {
	held := personStruct(t)

	elsewhere := &model.Struct{
		Named: namedStruct(t, subjectPkg+"/other", "other", "Person"),
	}

	cases := map[string]struct {
		subject           *model.Struct
		attachable, reach bool
	}{
		// The package being written into, which is the only place a method on
		// the type can be declared.
		"in this package": {held, true, true},

		// Somewhere else in the same module: out of reach from here for the
		// language's reason, and reachable in the sense that generating into
		// that package would work.
		"in another package of this module": {elsewhere, false, true},

		"in another module": {
			&model.Struct{
				Named:    namedStruct(t, "other.example/lib", "lib", "Person"),
				External: true,
			},
			false, false,
		},

		// An instantiation has nowhere to put a method wherever it was
		// declared: the type a method would attach to is the generic one.
		"an instantiation": {
			&model.Struct{
				Named:        namedStruct(t, subjectPkg, "domain", "Pair"),
				Instantiated: true,
			},
			false, false,
		},

		"unresolved": {&model.Struct{}, false, false},
		"nil":        {nil, false, false},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := tc.subject.Attachable(subjectPkg); got != tc.attachable {
				t.Errorf("Attachable(%q) = %v, want %v", subjectPkg, got, tc.attachable)
			}
			if got := tc.subject.Reachable(); got != tc.reach {
				t.Errorf("Reachable() = %v, want %v", got, tc.reach)
			}
		})
	}
}

func TestStructField(t *testing.T) {
	person := personStruct(t)

	field, ok := person.Field("Age")
	if !ok {
		t.Fatal("Field(Age) reported absent")
	}
	if field.Name != "Age" {
		t.Errorf("Field(Age).Name = %q, want %q", field.Name, "Age")
	}

	// Unexported fields are still part of the model; whether a layer may touch
	// one is that layer's decision.
	if _, ok := person.Field("secret"); !ok {
		t.Error("Field(secret) reported absent")
	}

	if _, ok := person.Field("Missing"); ok {
		t.Error("Field(Missing) reported present")
	}

	var nilStruct *model.Struct
	if _, ok := nilStruct.Field("Age"); ok {
		t.Error("Field on a nil struct reported present")
	}
}

func TestStructFieldNamesPreservesDeclarationOrder(t *testing.T) {
	got := personStruct(t).FieldNames()
	want := []string{"Name", "Age", "secret"}

	if len(got) != len(want) {
		t.Fatalf("FieldNames() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("FieldNames()[%d] = %q, want %q", i, got[i], want[i])
		}
	}

	var nilStruct *model.Struct
	if got := nilStruct.FieldNames(); got != nil {
		t.Errorf("FieldNames() on a nil struct = %v, want nil", got)
	}
}

func TestStructString(t *testing.T) {
	if got, want := personStruct(t).String(), subjectPkg+".Person"; got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
	if got, want := (&model.Struct{}).String(), "<unresolved struct>"; got != want {
		t.Errorf("String() on an unresolved struct = %q, want %q", got, want)
	}
}

// The function a container calls to reach a subject is named after the subject,
// and two subjects never reach one name.
//
// Both halves matter. Naming it after the subject is what lets two declarations
// over one subject agree on one function; keeping two subjects apart is what
// stops two of them being given one. The second is not obvious for an
// instantiation, whose arguments are the only thing telling it from another
// instantiation of the same generic — and neither can carry a method, so both
// take this path and both are emitted.
func TestWhatAContainerCallsToReachASubject(t *testing.T) {
	person := &model.Struct{Named: namedStruct(t, subjectPkg, "domain", "Person")}

	if got, want := model.Through(person, "encode", "JSONTo"), "encodePersonJSONTo"; got != want {
		t.Errorf("the function is %q, want %q", got, want)
	}

	// Two instantiations of one generic, which are two types.
	first := instantiation(t, "Pair", types.Typ[types.String], types.Typ[types.Int])
	second := instantiation(t, "Pair", types.Typ[types.Int], types.Typ[types.String])

	if a, b := model.Through(first, "encode", "To"), model.Through(second, "encode", "To"); a == b {
		t.Errorf("Pair[string, int] and Pair[int, string] are both called %q", a)
	}

	// Nothing to name it after is no name, rather than a name made of the
	// pieces around the hole.
	for name, held := range map[string]*model.Struct{
		"nothing at all": nil,
		"unresolved":     {},
	} {
		if got := model.Through(held, "encode", "To"); got != "" {
			t.Errorf("a subject that is %s is reached through %q", name, got)
		}
	}
}

// instantiation builds a struct standing for one instantiation of a generic,
// which is what the arguments in a name are there to tell apart.
//
// A real instantiation rather than a struct with the arguments written on it,
// because what the name is built from is what the type says its arguments are
// — and a fixture that carried them separately would agree with the type only
// as long as somebody kept the two in step.
func instantiation(t *testing.T, name string, args ...types.Type) *model.Struct {
	t.Helper()

	pkg := types.NewPackage(subjectPkg, "domain")
	obj := types.NewTypeName(token.NoPos, pkg, name, nil)
	generic := types.NewNamed(obj, types.NewStruct(nil, nil), nil)

	params := make([]*types.TypeParam, len(args))
	for i := range args {
		held := types.NewTypeName(token.NoPos, pkg, string(rune('A'+i)), nil)
		params[i] = types.NewTypeParam(held, types.NewInterfaceType(nil, nil))
	}
	generic.SetTypeParams(params)

	held, err := types.Instantiate(nil, generic, args, false)
	if err != nil {
		t.Fatalf("instantiating %s: %v", name, err)
	}

	named, ok := held.(*types.Named)
	if !ok {
		t.Fatalf("instantiating %s gave %T", name, held)
	}
	return &model.Struct{Named: named, Instantiated: true}
}
