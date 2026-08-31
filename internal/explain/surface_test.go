package explain_test

import (
	"go/token"
	"go/types"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"

	"github.com/okian/forge/internal/explain"
	"github.com/okian/forge/internal/layers"
	"github.com/okian/forge/internal/model"
)

// specialised is the declaration a layer is asked what it exposes against.
//
// It carries the option that decides half the answer, so that an explanation
// built from it is the one a reader of the generated file would recognise.
func specialised(entries ...model.Option) *model.Model {
	pkg := types.NewPackage("example.com/model", "model")
	obj := types.NewTypeName(token.NoPos, pkg, "Person", nil)

	// The same fields the documented subject has, given the types a layer
	// reading them needs. Explaining a stack takes their count and their tags;
	// generating from one takes what each of them is, and a layer asked what it
	// would emit is doing the second.
	subject := &model.Struct{
		Named:  types.NewNamed(obj, types.NewStruct(nil, nil), nil),
		Fields: make([]model.Field, len(person.Fields)),
	}
	for i, field := range person.Fields {
		held := types.Typ[types.String]
		if field.Name == "ID" || field.Name == "Age" {
			held = types.Typ[types.Int]
		}

		subject.Fields[i] = field
		subject.Fields[i].Type = model.Classified{Type: held}
	}

	held := &model.Model{
		Name:    "Persons",
		Form:    model.FormInline,
		Subject: subject,
		Stack:   stack("Collection", "Slice"),
		Pkg:     &packages.Package{PkgPath: "example.com/model"},
	}
	if len(entries) > 0 {
		held.Options = []model.Options{{Layer: "collection", Entries: entries}}
	}
	return held
}

// asked builds the declaration those options were written on.
func asked(entries ...model.Option) explain.Declaration {
	decl := documented()
	decl.Form = model.FormInline
	decl.Stack = stack("Collection", "Slice")
	decl.Layout.Text = "Collection[Slice[Person]]"
	decl.Model = specialised(entries...)
	return decl
}

// A layer whose methods are named after the declaration reports them, so the
// explanation lists what the generated file will hold rather than the part of
// it that is the same for everybody.
//
// This is the whole reason a layer is handed the declaration when it is asked
// what it exposes. Without it a collection reports one method — the lazy view —
// and an author reading the explanation would find seven in the file.
func TestTheMethodsNamedAfterTheDeclaration(t *testing.T) {
	got := explain.Of(asked(model.Option{Key: "sort", Value: "Name"},
		model.Option{Key: "index", Value: "ID"}), layers.Builtins())

	if len(got.Steps) != 3 {
		t.Fatalf("walked %d steps, want the subject and two layers", len(got.Steps))
	}

	query := got.Steps[2]
	want := "Seq, IDs, Names, Ages, Emails, SortedByName, ByID"
	if joined := strings.Join(query.Methods, ", "); joined != want {
		t.Errorf("the collection emits %s, want %s", joined, want)
	}
}

// The same layer over the same subject with nothing asked of it emits the
// projections and neither a sorted view nor a lookup, because those are what
// the options are for.
func TestTheMethodsFollowWhatWasAsked(t *testing.T) {
	got := explain.Of(asked(), layers.Builtins())

	query := got.Steps[len(got.Steps)-1]
	want := "Seq, IDs, Names, Ages, Emails"
	if joined := strings.Join(query.Methods, ", "); joined != want {
		t.Errorf("a collection asked for nothing emits %s, want %s", joined, want)
	}
}

// An option written for another layer is not this one's.
//
// Each layer is handed its own set rather than the whole of what was written,
// so a sort declared on a storage layer cannot reach the collection above it
// and add a method nobody asked that layer for.
func TestALayerReadsOnlyItsOwnOptions(t *testing.T) {
	decl := asked()
	decl.Model.Options = []model.Options{
		{Layer: "slice", Entries: []model.Option{{Key: "sort", Value: "Name"}}},
	}

	got := explain.Of(decl, layers.Builtins())

	query := got.Steps[len(got.Steps)-1]
	for _, method := range query.Methods {
		if method == "SortedByName" {
			t.Errorf("the collection emits %v, and the sort was written for the layer beneath it", query.Methods)
		}
	}
}

// A declaration with no model still explains, with the part of each layer's
// surface that does not depend on one.
//
// That is the run somebody whose subject was refused makes, and it is the one
// where an explanation is worth the most: the stack is still theirs, the layers
// still say what they are for, and only the field-derived names are missing.
func TestExplainingWithoutAModel(t *testing.T) {
	decl := asked()
	decl.Model = nil

	got := explain.Of(decl, layers.Builtins())

	query := got.Steps[len(got.Steps)-1]
	if want := []string{"Seq"}; strings.Join(query.Methods, ", ") != strings.Join(want, ", ") {
		t.Errorf("a collection explained without a declaration emits %v, want %v", query.Methods, want)
	}
}
