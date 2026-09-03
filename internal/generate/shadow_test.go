package generate_test

import (
	"fmt"
	"go/token"
	"go/types"
	"testing"

	"golang.org/x/tools/go/packages"

	"github.com/okian/forge/internal/discover"
	"github.com/okian/forge/internal/generate"
	"github.com/okian/forge/internal/goldentest"
	"github.com/okian/forge/internal/model"
	"github.com/okian/forge/internal/tags"
)

// bound is one layer and a name its generated bodies bind, with the stack the
// layer needs beneath it.
type bound struct {
	// layer is the directive an author writes, and stack the whole stack the
	// declaration is over, outermost first. A decorator goes above its storage
	// and an element below its container, so the order is spelled per case
	// rather than derived.
	layer string
	stack []string

	// inline records that a declaration may name this stack as its underlying
	// type. Where it may not, forge owns the declared type and the fixture must
	// not also declare it.
	inline bool

	// args are the options the directive carries, where the case needs them to
	// reach the emitter it is about.
	args string

	// names are the identifiers the layer's bodies bind.
	names []string
}

// The layers whose bodies bound a name that a subject could share, and the
// names each one bound.
//
// Six of these produced a file that did not compile — collection/c, json/c,
// json/dec, json/v, guarded/g and builder/b — because each was a receiver or a
// parameter written down as a constant, in scope over a body that also had to
// spell the subject.
//
// The other two are coverage rather than regressions. collection/v and json/enc
// were checked against a generator built before the change and compiled: a
// closure's own parameter is not in scope over its signature, so `func(v v)`
// resolves the type in the enclosing scope, and nothing a codec writes names
// its encoder as a type. They are here because the allocation covers them
// anyway and a name that moves for one case should not stand still for another.
//
// The layers not here were checked the same way and were never at risk. Clone,
// Hash, Patch, Redact and Validate bind names too, but none of them binds one
// in a scope that spells a type — so a subject sharing one is legal Go, and a
// case for it would assert nothing about this.
var theBound = []bound{
	{
		layer: "collection", stack: []string{"Collection"}, inline: true,
		args:  "sort=Name index=ID",
		names: []string{"c", "v"},
	},
	{layer: "json", stack: []string{"Collection", "Json"}, names: []string{
		// The append codec's signatures and the entry points' locals.
		"c", "v", "dst", "b", "i", "depth", "borrow", "data", "w", "r",
		// The scanning bodies' locals, every one in scope where a member's
		// type is spelled.
		"err", "err2", "err3", "held", "next", "ok", "names", "scratch",
		"first", "done", "lo", "hi", "at", "esc", "open",
		// The container's streaming halves.
		"n", "counted", "failed", "ended", "kind", "feed", "yield",
	}},
	{layer: "guarded", stack: []string{"Guarded", "Slice"}, names: []string{"g"}},
	{layer: "builder", stack: []string{"Builder"}, names: []string{"b"}},
	{layer: "ring", stack: []string{"Ring"}, args: "cap=8", names: []string{"r"}},
	{layer: "slice", stack: []string{"Slice"}, inline: true, names: []string{"s"}},

	// The bridge's target is the subject here, so it is the type the
	// constructor spells in its signature and its literal. dst is bound only
	// when a hint is respelled, and this case carries none — it asserts the
	// allocation moves the name anyway.
	{layer: "map", stack: []string{"Map"}, names: []string{"src", "dst", "held"}},
}

// A subject named after something a layer's bodies bind still generates code
// that compiles.
//
// The failure this stops is the worst-shaped one forge has: the declaration is
// accepted, the file is written with no diagnostic, and the compiler refuses it
// — in a file the author is told not to edit. A subject called c gave
// `func (c Held) Seq() HeldSeq { return HeldSeq{Seq[c](c.All())} }`, where the
// receiver shadows the very type the body has to name.
//
// Type-checked rather than merely emitted, because emission is not where this
// goes wrong. Every one of these cases produced output its layer was perfectly
// happy with.
//
// Two of the fixtures earn their shape. The collection case writes sort and
// index options, because those are what pull in the template helpers whose
// bodies spell the subject — the built methods alone would leave half the
// layer's output untested. The subject's Name field is tagged required, because
// the builder writes the body that spells the subject only where there is a
// field to demand; without it Build returns the held value and names no type.
// Take either away and the case still passes with the allocation removed.
func TestASubjectNamedAfterWhatALayerBinds(t *testing.T) {
	for _, held := range theBound {
		for _, name := range held.names {
			t.Run(held.layer+"/"+name, func(t *testing.T) {
				files, diags := generate.Package(local, "model",
					[]generate.Request{shadowing(held, name)}, config())

				if !diags.Empty() {
					t.Fatalf("generating over a subject called %s was refused:\n%s",
						name, diags.Render())
				}

				sources := []goldentest.Source{
					{Name: "subject.go", Content: []byte(shadowSource(held, name))},
				}
				for _, file := range files {
					sources = append(sources, goldentest.Source{
						Name: file.Name, Content: file.Content, Generated: true,
					})
				}

				// Compiled rather than recorded. What is asked is whether the
				// output type-checks at all, and a golden file per case would be
				// a dozen files nobody reads asserting one thing between them.
				if err := goldentest.Compiles(goldentest.Package{Path: "model", Files: sources}); err != nil {
					t.Errorf("what was generated over a subject called %s does not compile: %v", name, err)
				}
			})
		}
	}
}

// shadowSource is the fixture the generated output is compiled against.
//
// The declared type is written here only where a declaration may name its own
// underlying type. Everywhere else forge owns it, and a fixture declaring it as
// well would collide with what was generated rather than testing anything.
func shadowSource(of bound, named string) string {
	held := "package model\n\n" +
		fmt.Sprintf("// %s is the subject, named after an identifier a layer binds.\n", named) +
		fmt.Sprintf("type %s struct {\n\tID int\n\tName string\n}\n", named)

	if of.inline {
		held += "\n// Held is a collection of them.\n" +
			fmt.Sprintf("type Held []%s\n", named)
	}

	if of.layer == "map" {
		// The bridge reads a source, which is the author's own type exactly
		// as the subject is.
		held += "\n// Origin is the source the bridge reads.\n" +
			"type Origin struct {\n\tID int\n\tName string\n}\n"
	}

	return held
}

// shadowing builds a declaration over a subject of the given name, through the
// given layer and whatever the layer needs beneath it.
func shadowing(of bound, named string) generate.Request {
	pkg := types.NewPackage(local, "model")
	obj := types.NewTypeName(token.NoPos, pkg, named, nil)

	// Name is required, which is what makes the builder emit the half that
	// spells the subject in a body: without a field the author demanded, Build
	// returns the held value directly and names no type. So the tag is what
	// gives the builder case anything to catch.
	subject := &model.Struct{
		Named: types.NewNamed(obj, types.NewStruct(nil, nil), nil),
		Fields: []model.Field{
			{Name: "ID", Exported: true, Type: model.Classified{Type: types.Typ[types.Int]}},
			{
				Name: "Name", Exported: true,
				Type: model.Classified{Type: types.Typ[types.String]},
				Tags: []tags.Tag{{Key: "validate", Raw: "required"}},
			},
		},
	}

	stack := make([]model.LayerRef, 0, len(of.stack))
	for _, one := range of.stack {
		stack = append(stack, model.LayerRef{Origin: model.TypeRef{Pkg: model.MarkerPkg, Name: one}})
	}

	form := model.FormSpec
	if of.inline {
		form = model.FormInline
	}

	// The options reach a layer through the directive rather than through the
	// model, so a case that needs them has to write them there. Sorting and
	// indexing are what pull in the template helpers whose bodies spell the
	// subject, which is the half a built-methods-only case cannot reach.
	text := "//forge:" + of.layer
	if of.args != "" {
		text += " " + of.args
	}

	// A bridge reads a source beside its subject: the same shape, so the
	// ladder settles both members by name and the constructor is written.
	var source types.Type
	if of.layer == "map" {
		origin := types.NewTypeName(token.NoPos, pkg, "Origin", nil)
		source = types.NewNamed(origin, types.NewStruct([]*types.Var{
			types.NewField(token.NoPos, pkg, "ID", types.Typ[types.Int], false),
			types.NewField(token.NoPos, pkg, "Name", types.Typ[types.String], false),
		}, nil), nil)
	}

	return generate.Request{
		Model: &model.Model{
			Name: "Held", Form: form, Subject: subject, Stack: stack,
			Source: source,
			Pkg:    &packages.Package{PkgPath: local},
			Pos:    declaredAt,
		},
		Directives: []discover.Directive{{
			Layer: of.layer, Text: text, Args: of.args,
			ArgsOffset: len(text) - len(of.args),
			Pos: token.Position{
				Filename: declaredAt.Filename, Line: declaredAt.Line - 1, Column: 1,
			},
		}},
	}
}
