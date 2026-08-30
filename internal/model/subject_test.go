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
func TestStructLocal(t *testing.T) {
	cases := map[string]struct {
		subject *model.Struct
		want    bool
	}{
		"local":        {personStruct(t), true},
		"external":     {&model.Struct{Named: namedStruct(t, "other.example/lib", "lib", "Person"), External: true}, false},
		"instantiated": {&model.Struct{Named: namedStruct(t, subjectPkg, "domain", "Pair"), Instantiated: true}, false},
		"unresolved":   {&model.Struct{}, false},
		"nil":          {nil, false},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := tc.subject.Local(); got != tc.want {
				t.Errorf("Local() = %v, want %v", got, tc.want)
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
