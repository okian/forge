package collection

import (
	"fmt"
	"go/ast"
	"go/token"
	"slices"
	"strings"

	"github.com/okian/forge/internal/diag"
	"github.com/okian/forge/internal/layer"
	"github.com/okian/forge/internal/model"
	"github.com/okian/forge/internal/shape"
	"github.com/okian/forge/internal/shared/seq"
)

// plan is what a declaration asked for, worked out before anything is built.
//
// Deciding first and building second keeps the two apart: what a declaration
// gets is a reading of its subject and its options, and building is turning
// that into syntax. A pass that did both would report half a surface and emit
// the other half.
type plan struct {
	// declared is the type the methods are on, pkg the package they land in,
	// and subject the element spelled as that package has to spell it.
	declared string
	pkg      string
	subject  model.Spelling

	// view is what the lazy view over the elements is called.
	view string

	// projections, sorts and indexes are the fields each of those is built
	// from, in the order the subject declares them or the option names them.
	projections []column
	sorts       []column
	indexes     []column

	// at is where the declaration was written, which is where anything that
	// cannot be built is reported.
	at token.Position

	// beneath is what the layers under this one put on the declared type, which
	// is what a name this layer generates must not already be.
	beneath shape.Shape
}

// column is one field a generated method is built from: its name on the
// subject, the method's own name, and the field's type spelled for the package
// being written into.
type column struct {
	field  string
	method string
	typ    model.Spelling
}

// planned reads a declaration and says what this layer will generate for it.
func planned(ctx *layer.Context, below shape.Shape) (plan, diag.Set) {
	var diags diag.Set

	subject := ctx.Model.Subject

	// Every type this layer names is spelled against what the file already
	// binds, and each spelling adds what it bound to that. Spelling them
	// independently is how one package ends up imported twice under two names,
	// or two packages of one name both claiming it.
	bound := taken()

	spelled := ctx.Model.SubjectSpelling(bound)
	bound = spelled.Bound(bound)

	out := plan{
		declared: ctx.Model.Name,
		subject:  spelled,
		view:     viewName(ctx.Model.Name, named(ctx.Options, "seq")),
		at:       ctx.Model.Pos,
		beneath:  below,
	}
	if ctx.Model.Pkg != nil {
		out.pkg = ctx.Model.Pkg.PkgPath
	}

	out.projections, bound = projections(subject, spelled, bound)
	out.sorts, bound = columns(ctx, subject, spelled, bound, sorting, &diags)
	out.indexes, _ = columns(ctx, subject, spelled, bound, indexing, &diags)

	return out, diags
}

// projections is one column per exported field, named as the plural of the
// field.
//
// Every field rather than the ones an option names, because a column of a
// collection is not a choice the way an order or a key is: a collection you
// cannot take a column out of is half a collection, and there is no reading of
// a struct under which one field is projectable and the next is not.
//
// Unexported fields are left out. Generated code lands in the package that
// declares the collection, which is not always the one that declares the
// subject, so a projection of an unexported field is one that compiles for some
// declarations and not others — and a surface that depends on where the
// declaration happens to live is worse than one that stops at the export
// boundary everywhere.
func projections(subject *model.Struct, spelled model.Spelling, bound []model.Import) ([]column, []model.Import) {
	var out []column

	for _, field := range subject.Fields {
		if !field.Exported {
			continue
		}

		typ := model.Spell(field.Type.Type, spelled.Local, bound)
		bound = typ.Bound(bound)

		out = append(out, column{field: field.Name, method: plural(field.Name), typ: typ})
	}
	return out, bound
}

// asked is one option that names fields and what this layer makes of each: the
// prefix its methods take, and what stops a field from being one of them.
type asked struct {
	option string
	prefix string
	usable func(model.Field) (diag.Code, string)
}

// The two options that name fields. Both are choices a declaration makes rather
// than things a struct implies, which is why they are options at all.
var (
	sorting  = asked{"sort", "SortedBy", orderableField}
	indexing = asked{"index", "By", keyableField}
)

// columns reads one option that names fields and turns each into a column,
// reporting the ones this layer cannot generate from.
//
// The field exists — validation checked that — so what is left is whether it
// will do: an unexported one cannot be read from a package that is not the
// subject's, one that cannot be ordered cannot be sorted by, and one that
// cannot be a map key cannot be indexed by. All three are reported at the
// option rather than at the declaration, because the option is what has to
// change.
//
// A field named twice is one column, and one complaint. Validation reports the
// repeat, so what is left here is not to act on it twice — which means counting
// every name read rather than every column kept, or a field that is both
// repeated and unusable is refused once per mention.
func columns(
	ctx *layer.Context, subject *model.Struct, spelled model.Spelling, bound []model.Import,
	of asked, diags *diag.Set,
) ([]column, []model.Import) {
	var out []column

	said := make(map[string]bool)

	written, _ := ctx.Options.Lookup(of.option)
	for _, name := range ctx.Options.List(of.option) {
		if said[name] {
			continue
		}
		said[name] = true

		field, has := subject.Field(name)
		if !has {
			// Validation resolves an option that names a field against the
			// subject's fields, so reaching here means the two disagree about
			// what the subject is.
			continue
		}

		if code, why := usable(field, of); why != "" {
			diags.Add(diag.New(code, written.Pos,
				"%s=%s: %s %s", of.option, name, name, why).
				WithHint("%s", "drop it from the option, or name a field this layer can generate from"))
			continue
		}

		typ := model.Spell(field.Type.Type, spelled.Local, bound)
		bound = typ.Bound(bound)

		out = append(out, column{field: field.Name, method: of.prefix + field.Name, typ: typ})
	}
	return out, bound
}

// usable says what stops a field from being generated from, or nothing.
//
// Export comes first and is asked of both options, because it is the one answer
// that does not depend on which option named the field: generated code that
// cannot read a field cannot do anything with it, and a method called Bysecret
// is not a name anybody would write even where it compiles.
func usable(field model.Field, of asked) (diag.Code, string) {
	if !field.Exported {
		return codeFieldUnexported, "is not exported, and generated code cannot read it"
	}
	return of.usable(field)
}

// orderableField and keyableField say what stops a field from being sorted by
// or indexed by, or nothing.
func orderableField(field model.Field) (diag.Code, string) {
	if orderable(field.Type.Type) {
		return 0, ""
	}
	return codeSortNotOrdered, "is " + field.Type.String() + ", which cannot be compared for order"
}

func keyableField(field model.Field) (diag.Code, string) {
	if keyable(field.Type.Type) {
		return 0, ""
	}
	return codeIndexNotKeyable, "is " + field.Type.String() + ", which cannot be a map key"
}

// named returns the value written for an option, and nothing when it was not
// written. Every option this layer takes is optional, so absence is an answer
// rather than something to report.
func named(options model.Options, key string) string {
	value, _ := options.Get(key)
	return value
}

// imports is everything the generated methods name, which is the subject's own
// packages and every field's.
//
// A field is as likely to come from somewhere else as the subject is —
// time.Time is the ordinary case — and a projection returning one names it.
// Gathering the subject's alone was the shape of mistake that produces a file
// which is right in every line and does not compile.
func (p plan) imports() []model.Import {
	out := slices.Clone(p.subject.Imports)

	for _, columns := range [][]column{p.projections, p.sorts, p.indexes} {
		for _, one := range columns {
			for _, needed := range one.typ.Imports {
				if !slices.ContainsFunc(out, func(held model.Import) bool { return held.Path == needed.Path }) {
					out = append(out, needed)
				}
			}
		}
	}

	slices.SortFunc(out, func(a, b model.Import) int { return strings.Compare(a.Path, b.Path) })
	return out
}

// clashes reports every generated name that is already a method of the declared
// type, whether because two of this layer's own names came out the same or
// because a layer beneath it got there first.
//
// Pluralising is what makes the first possible: Address and Addresse are two
// fields and Addresses is one name. The second needs no coincidence at all — a
// field called Len projects to Lens, and a subject with a field called Al gives
// SortedByAl no trouble but a field called L and an index give ByLen exactly
// that. Either way the output is a method declared twice in a file the author
// cannot edit, so both are caught here, where the two names can be shown
// together.
//
// Every clash rather than the first, because a subject that produced one has
// very likely produced two and reporting them one build at a time is the thing
// this project's diagnostics are written not to do.
func (p plan) clashes() diag.Set {
	var diags diag.Set

	// The view's own type is not a method and is checked apart from them: it
	// is the one name this layer takes in the package rather than on the type,
	// and the shared view it is declared over is the one thing already there.
	if p.view == seq.Name {
		diags.Add(diag.New(codeNamesCollide, p.at,
			"the view is called %s, which is the name of the shared view it is declared over", p.view).
			WithHint("%s", "name it something else with the seq option"))
	}

	seen := make(map[string]string, len(p.projections)+len(p.sorts)+len(p.indexes)+1)
	for _, method := range p.beneath.Names() {
		seen[method] = "the storage beneath it"
	}

	for _, one := range p.generated() {
		if first, twice := seen[one.method]; twice {
			diags.Add(diag.New(codeNamesCollide, p.at,
				"%s is generated for %s and is already %s", one.method, one.field, first).
				WithHint("%s", "the two cannot both be reached; drop the option that named one of them, "+
					"or rename the field a projection was built from"))
			continue
		}
		seen[one.method] = "generated for " + one.field
	}

	return diags
}

// generated is every method this layer will put on the declared type, in the
// order it will write them.
func (p plan) generated() []column {
	out := make([]column, 0, len(p.projections)+len(p.sorts)+len(p.indexes)+1)

	out = append(out, column{field: "the elements", method: "Seq"})
	out = append(out, p.projections...)
	out = append(out, p.sorts...)
	out = append(out, p.indexes...)

	return out
}

// build turns the plan into the declarations this layer emits.
//
// The order is the order the file reads in: the view's own type, the method
// that returns it, then the projections, the sorted views and the lookups. Not
// sorted — a reader wants the type beside the method that produces it, which is
// the one thing sorting by name would separate.
func (p plan) build() ([]ast.Decl, error) {
	// Once, and used everywhere the subject is named. The two halves of a built
	// method would otherwise hold it two ways, and the half that held it as a
	// bare name would be lying about it for every subject that is not one.
	subject, err := spelled(p.subject.Text)
	if err != nil {
		return nil, fmt.Errorf("collection: the subject is written as %q, which is not a type: %w",
			p.subject.Text, err)
	}

	out := []ast.Decl{
		view(p.view, fmt.Sprintf("%s is a lazy view over the elements of %s.", p.view, p.declared),
			seq.Name, subject),
		viewer(built{
			doc: fmt.Sprintf(
				"Seq returns a lazy view over the elements. Its combinators are the shared %s[%s] view's.",
				seq.Name, p.subject.Text),
			receiver: receiverName, on: p.declared,
			name: "Seq", result: ast.NewIdent(p.view),
			helper: walking,
		}, seq.Name, subject),
	}

	for _, kind := range []struct {
		columns []column
		doc     func(column) string
		helper  string
		result  func(key, subject ast.Expr) ast.Expr
	}{
		{p.projections, projected, projecting, sliceOfKey},
		{p.sorts, sortedBy, ordering, sliceOfSubject},
		{p.indexes, keyedBy, keying, mapOfKey},
	} {
		for _, one := range kind.columns {
			key, wrong := spelled(one.typ.Text)
			if wrong != nil {
				return nil, fmt.Errorf("collection: %s is written as %q, which is not a type: %w",
					one.field, one.typ.Text, wrong)
			}

			out = append(out, method(built{
				doc:      kind.doc(one),
				receiver: receiverName, on: p.declared,
				name: one.method, result: kind.result(key, subject),
				helper:  kind.helper,
				element: elementName, subject: subject, field: one.field, key: key,
			}))
		}
	}

	// Last, because it is the one thing here that is not per field: the sorted
	// views are a method each and this is a single order over the whole.
	return append(out, sortable(p)...), nil
}

// surface is the plan as the layers above it see it: every method this layer
// will put on the declared type, named and with its result spelled.
//
// Written from the plan rather than read back from what build produced, and the
// two are held together by a test rather than by construction. Reading the
// syntax back would report whatever the builder happened to emit — a helper it
// keeps to itself included — where what belongs in a surface is the contract,
// and a layer above written against a helper would break the first time one was
// renamed.
//
// Every one takes a value receiver, because every one answers a question rather
// than changing the collection.
func (p plan) surface(owner model.TypeRef) []shape.Method {
	out := make([]shape.Method, 0, 1+len(p.projections)+len(p.sorts)+len(p.indexes))

	out = append(out, shape.Method{
		Name: "Seq", Signature: "() " + p.view, Owner: owner,
		Doc: fmt.Sprintf("Seq returns a lazy view over the elements, as %s.", p.view),
	})

	for _, kind := range []struct {
		columns []column
		doc     func(column) string
		result  func(key string) string
	}{
		{p.projections, projected, func(key string) string { return "[]" + key }},
		{p.sorts, sortedBy, func(string) string { return "[]" + p.subject.Text }},
		{p.indexes, keyedBy, func(key string) string { return "map[" + key + "]" + p.subject.Text }},
	} {
		for _, one := range kind.columns {
			out = append(out, shape.Method{
				Name: one.method, Signature: "() " + kind.result(one.typ.Text), Owner: owner,
				Doc: kind.doc(one),
			})
		}
	}

	return out
}

// The one-line summaries the generated methods carry. They say what the method
// answers rather than how, because how is in the helper it hands the work to
// and a reader of the generated file can see that from here.
func projected(one column) string {
	return fmt.Sprintf("%s returns the %s of every element, in order.", one.method, one.field)
}

func sortedBy(one column) string {
	return fmt.Sprintf("%s returns the elements ordered by %s, leaving equal ones as they were.",
		one.method, one.field)
}

func keyedBy(one column) string {
	return fmt.Sprintf("%s returns the elements keyed by %s, keeping the last of any that share one.",
		one.method, one.field)
}

// The result each shape of method returns: a slice of the field for a
// projection, the elements themselves for a sorted view, and a map from the
// field to the elements for a lookup.
func sliceOfKey(key, _ ast.Expr) ast.Expr { return &ast.ArrayType{Elt: key} }

func sliceOfSubject(_, subject ast.Expr) ast.Expr { return &ast.ArrayType{Elt: subject} }

func mapOfKey(key, subject ast.Expr) ast.Expr { return &ast.MapType{Key: key, Value: subject} }
