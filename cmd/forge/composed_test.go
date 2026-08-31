package main

import (
	"go/token"
	"go/types"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"

	"github.com/okian/forge/internal/discover"
	"github.com/okian/forge/internal/model"
	"github.com/okian/forge/internal/resolve"
)

// specialising builds a declaration with a subject that can be generated from,
// which is what a layer needs before it will say what it would emit.
func specialising(name string, directives []discover.Directive, markers ...string) request {
	stack := make([]model.LayerRef, len(markers))
	for i, marker := range markers {
		stack[i] = model.LayerRef{Origin: model.TypeRef{Pkg: model.MarkerPkg, Name: marker}}
	}

	return request{
		Declaration: resolve.Declaration{
			Candidate: discover.Candidate{
				Name: name, Form: model.FormInline, Directives: directives,
				Pkg: &packages.Package{PkgPath: "example.com/model", Name: "model"},
				Pos: token.Position{Filename: "model/person.go", Line: 8, Column: 6},
			},
			Stack:   stack,
			Subject: subjectType,
		},
		Model: &model.Struct{
			Named: subjectType,
			Fields: []model.Field{
				{Name: "ID", Exported: true, Type: model.Classified{Type: types.Typ[types.Int]}},
				{Name: "Name", Exported: true, Type: model.Classified{Type: types.Typ[types.String]}},
			},
		},
	}
}

// spec puts a declaration in a file forge owns the type in, which is where a
// stack naming a layer that cannot live with a raw underlying type belongs.
func spec(one request) request {
	one.Declaration.Candidate.Form = model.FormSpec
	return one
}

// wrote is one directive above the declaration, split the way the scanner
// splits one: the layer it addresses, and the rest of the line.
func wrote(layer, args string) discover.Directive {
	text := "//forge:" + layer + " " + args

	return discover.Directive{
		Layer: layer, Args: args, Text: text,
		ArgsOffset: len(text) - len(args),
		Pos:        token.Position{Filename: "model/person.go", Line: 7, Column: 1},
	}
}

// The explanation describes the stack that will be generated, which is not
// always the stack that was written.
//
// A refining layer written over no storage has one filled in beneath it. An
// explanation that left it out would describe a type with the collection's
// methods and none of the container's, and a reader counting on it would find
// four more in the file.
func TestExplainingTheStackThatWillBeGenerated(t *testing.T) {
	got := asking(t, []request{specialising("Persons", nil, "Collection")}, "-t", "Persons")

	if got.status != 0 {
		t.Fatalf("explaining ended with %d:\n%s", got.status, got.err)
	}
	if !strings.Contains(got.out, "Slice") {
		t.Errorf("the answer does not name the storage forge fills in:\n%s", got.out)
	}
	for _, want := range []string{"Len", "All", "Backward", "AppendSeq"} {
		if !strings.Contains(got.out, want) {
			t.Errorf("the answer does not say the type gains %s:\n%s", want, got.out)
		}
	}
}

// The methods named after the declaration's own options are in the answer, so
// what is explained is what the file will hold.
func TestExplainingTheMethodsTheOptionsAskedFor(t *testing.T) {
	asked := specialising("Persons",
		[]discover.Directive{wrote("collection", "sort=Name index=ID")}, "Collection")

	got := asking(t, []request{asked}, "-t", "Persons")

	if got.status != 0 {
		t.Fatalf("explaining ended with %d:\n%s", got.status, got.err)
	}
	for _, want := range []string{"SortedByName", "ByID", "Names", "IDs"} {
		if !strings.Contains(got.out, want) {
			t.Errorf("the answer does not say the type gains %s:\n%s", want, got.out)
		}
	}
}

// A stack that does not compose is explained as it was written, with the reason
// beside it.
//
// Composition stops at the layer that refused, so what it has built by then is
// the inner part of the stack and nothing else — an answer that showed it would
// have dropped the layers above without saying so, which is the half of the
// declaration somebody in trouble is usually asking about.
func TestExplainingAStackThatDoesNotCompose(t *testing.T) {
	// A codec over a subject with no fields. There is nothing to generate a
	// codec from, so the innermost layer refuses and the walk stops there,
	// having composed one entry of the two.
	// In a spec file, because an element layer is never transparent and an
	// inline declaration naming one is refused before the stack is walked —
	// which is a different complaint from the one under test.
	asked := spec(specialising("Persons", nil, "Collection", "Json"))
	asked.Model.Fields = nil

	got := asking(t, []request{asked}, "-t", "Persons")

	for _, want := range []string{"Collection", "Json"} {
		if !strings.Contains(got.out, want) {
			t.Errorf("the answer drops %s, which is above the layer that refused:\n%s", want, got.out)
		}
	}
	if got.status == 0 {
		t.Errorf("a stack that does not compose was explained without a complaint:\n%s", got.err)
	}
	if !strings.Contains(got.err, "Structured") {
		t.Errorf("the complaint does not say what was missing:\n%s", got.err)
	}
}

// What generation would refuse is what explaining says, word for word.
//
// The two used to disagree, and disagreed silently: explaining ran the option
// reader and the layer's planner and threw away everything both said, so a
// declaration the generator refused was described cheerfully and the run ended
// with a status of nought. Somebody debugging a refusal ran the verb written
// for debugging refusals and was told nothing was wrong.
//
// Each case here is refused by a different stage, and no stage but the
// generator reports any of them. A verb that repeated the checks it felt like
// repeating would pass this on the day it was written and drift the first time
// either side changed.
func TestExplainingSaysWhatGeneratingWouldRefuse(t *testing.T) {
	cases := map[string]struct {
		directive string
		fields    []model.Field
		code      string
	}{
		// The option reader: a field the subject has not got. Spelled the way
		// somebody who meant Name and typed it wrong would spell it, which is
		// the whole of how this arises.
		"a field that is not there": {
			directive: "sort=Nmae", //nolint:misspell // the typo is the fixture
			code:      "FRG3010",
		},

		// The layer: a field it has, that this option cannot be built from.
		"a field that cannot be ordered": {
			directive: "sort=Tags",
			fields: []model.Field{
				{Name: "Tags", Exported: true, Type: model.Classified{
					Type: types.NewSlice(types.Typ[types.String]),
				}},
			},
			code: "FRG3013",
		},

		// The layer again, later: two methods that want one name.
		"two projections of one name": {
			fields: []model.Field{
				{Name: "Address", Exported: true, Type: model.Classified{Type: types.Typ[types.String]}},
				{Name: "Addresse", Exported: true, Type: model.Classified{Type: types.Typ[types.String]}},
			},
			code: "FRG4101",
		},
	}

	for name, held := range cases {
		t.Run(name, func(t *testing.T) {
			var written []discover.Directive
			if held.directive != "" {
				written = []discover.Directive{wrote("collection", held.directive)}
			}

			asked := specialising("Persons", written, "Collection")
			if len(held.fields) > 0 {
				asked.Model.Fields = held.fields
			}

			got := asking(t, []request{asked}, "-t", "Persons")

			if !strings.Contains(got.err, held.code) {
				t.Errorf("explaining said %q, want %s", got.err, held.code)
			}
			if got.status == 0 {
				t.Error("a declaration the generator would refuse was explained without a complaint")
			}
		})
	}
}

// A layer whose generator is not written is not a fault in the declaration.
//
// Most of the catalog is markers whose layers are not built yet, and exploring
// one is what this verb is for. Reporting "generates nothing yet" as something
// wrong would make every such run end in a complaint about forge's roadmap; the
// report has a column that says it instead, and says it as pending work rather
// than as a mistake.
func TestExplainingALayerWhoseGeneratorIsNotWritten(t *testing.T) {
	// A ring keeps invariants a slice operation would corrupt, so it says it is
	// not transparent and belongs in a spec file. That is a rule about where the
	// declaration is written rather than about the layer's generator.
	got := asking(t, []request{spec(specialising("Persons", nil, "Ring"))}, "-t", "Persons")

	if got.status != 0 {
		t.Errorf("explaining a layer forge has not written ended with %d:\n%s", got.status, got.err)
	}
	if strings.Contains(got.err, "generates nothing yet") {
		t.Errorf("the unwritten layer was reported as a fault:\n%s", got.err)
	}
	if !strings.Contains(got.out, "pending") {
		t.Errorf("the answer does not say the work is pending:\n%s", got.out)
	}
}
