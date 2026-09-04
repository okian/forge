package index

import (
	"go/token"
	"go/types"
	"slices"
	"strings"

	"github.com/okian/forge/plugin"
)

// plan is what a declaration asked for, worked out before anything is built.
//
// Deciding first and building second keeps the two apart: what a declaration
// gets is a reading of its subject and its options, and building is turning
// that into syntax. A pass that did both would report half a surface and emit
// the other half.
type plan struct {
	// declared is the type the methods are on, and subject the element spelled
	// as the package being written into has to spell it.
	declared string
	subject  plugin.Spelling

	// key is the primary dimension, and secondaries the extra lookups the
	// declaration named. A key nothing resolved is left empty, which Generate
	// treats as the pipeline having skipped validation.
	key         column
	secondaries []column

	// unique says whether one key reaches at most one element, and refusing
	// what adding a held key does when it is: refuse with an error, or replace
	// the element in place. Both are false where unique is.
	unique   bool
	refusing bool

	// at is where the declaration was written, which is where anything that
	// cannot be built is reported.
	at token.Position

	// beneath is what the layers under this one expose, which the lookups'
	// names must stay clear of.
	beneath plugin.Shape

	// receiver is what every generated method calls the container, element
	// what the placing methods call the element they were handed, and lookup
	// what the lookup methods call the key they were asked. slotE, held and
	// bucket are the locals the built bodies bind — an entry, a comma-ok, a
	// primary bucket. entry and dup are what the entry type and the sentinel
	// error are called in the file, which the built bodies name.
	//
	// All allocated rather than spelled, because each is in scope over a body
	// that also names the subject and the key types: a subject called e gave
	// a removal `e := r.byID[k]` over a type called e, which is a file this
	// layer wrote and the compiler refused — and the author cannot edit it.
	receiver string
	element  string
	lookup   string
	slotE    string
	held     string
	bucket   string
	entry    string
	dup      string
}

// column is one field a lookup is built from: its name on the subject, the
// method's own name, the struct field the map lives in, and the field's type
// spelled for the package being written into.
type column struct {
	field  string
	method string
	slot   string
	typ    plugin.Spelling
}

// planned reads a declaration and says what this layer will generate for it.
func planned(ctx *plugin.Context, below plugin.Shape) (plan, plugin.Diagnostics) {
	var diags plugin.Diagnostics

	subject := ctx.Model.Subject

	// Every type this layer names is spelled against what the file already
	// binds, and each spelling adds what it bound to that. Spelling them
	// independently is how one package ends up imported twice under two names,
	// or two packages of one name both claiming it.
	bound := ctx.Bound()

	spelled := ctx.Model.SubjectSpelling(bound)
	bound = spelled.Bound(bound)

	out := plan{
		declared: ctx.Declared(),
		subject:  spelled,
		unique:   uniqueKeys(ctx),
		at:       ctx.Model.Pos,
		beneath:  below,
		entry:    entryFor(ctx.Declared()),
		dup:      errorFor(ctx.Declared()),
	}
	out.refusing = out.unique && !replacing(ctx)

	// The policy is only a question where a key can already be held, so
	// writing one beside unique=false is asking for an answer nothing will
	// consult — a dead option, which this project's diagnostics exist to
	// refuse rather than carry.
	if written, has := ctx.Options.Lookup(optionConflict); has && !out.unique {
		diags.Add(plugin.New(codePolicyUnneeded, written.Pos,
			"%s=%s is written beside %s=false, and a key that may be held many times has no add to refuse",
			optionConflict, written.Value, optionUnique).
			WithHint("%s", "drop "+optionConflict+", or let the keys be unique"))
	}

	out.key, bound = keyed(ctx, subject, spelled, bound, &diags)
	out.secondaries, bound = indexed(ctx, subject, spelled, bound, out, &diags)

	// Last, because the columns are half of what the bodies spell and the
	// spellings are what these have to stay clear of.
	named(&out, bound)

	return out, diags
}

// uniqueKeys reports whether one key reaches at most one element, which is the
// schema's default where the declaration says nothing.
//
// The default is applied here as well as declared, because a shape is asked
// for before anything has filled defaults in — a layer that read the option
// raw would describe an unwritten declaration as one arrangement and generate
// it as the other.
func uniqueKeys(ctx *plugin.Context) bool {
	if ctx == nil {
		return true
	}

	held, written := ctx.Options.Get(optionUnique)
	return !written || held != "false"
}

// replacing reports whether this declaration asked for a held key to be
// replaced rather than refused.
func replacing(ctx *plugin.Context) bool {
	if ctx == nil {
		return false
	}

	held, written := ctx.Options.Get(optionConflict)
	return written && held == conflictReplace
}

// keyed reads the key option and turns it into the primary dimension,
// reporting a field this layer cannot generate from.
//
// The field exists — validation resolved it against the subject — so what is
// left is whether it will do: an unexported one cannot be read from a package
// that is not the subject's, and one that is not comparable cannot be a map
// key. Both are reported at the option rather than at the declaration,
// because the option is what has to change.
func keyed(
	ctx *plugin.Context, subject *plugin.Struct, spelled plugin.Spelling, bound []plugin.Import,
	diags *plugin.Diagnostics,
) (column, []plugin.Import) {
	written, has := ctx.Options.Lookup(optionKey)
	if !has || written.Value == "" {
		return column{}, bound
	}

	field, resolved := subject.Field(written.Value)
	if !resolved {
		// Validation resolves an option that names a field against the
		// subject's fields, so reaching here means the two disagree about what
		// the subject is.
		return column{}, bound
	}

	if code, why := usable(field); why != "" {
		diags.Add(plugin.New(code, written.Pos,
			"%s=%s: %s %s", optionKey, field.Name, field.Name, why).
			WithHint("%s", "name a field this layer can look elements up by"))
		return column{}, bound
	}

	typ := plugin.Spell(field.Type.Type, spelled.Local, bound)
	bound = typ.Bound(bound)

	return dimension(field, typ), bound
}

// indexed reads the index option and turns each named field into a secondary
// dimension, reporting the ones this layer cannot generate from.
//
// A field named twice is one dimension and one complaint — validation reports
// the repeat — and a field that is the key is refused: the key already
// answers for it, and a secondary lookup resolving through itself would walk
// every element once per time it was filed.
func indexed(
	ctx *plugin.Context, subject *plugin.Struct, spelled plugin.Spelling, bound []plugin.Import,
	of plan, diags *plugin.Diagnostics,
) ([]column, []plugin.Import) {
	written, has := ctx.Options.Lookup(optionIndex)
	named := ctx.Options.List(optionIndex)
	if !has || len(named) == 0 {
		return nil, bound
	}

	// A secondary bucket holds keys and resolves each through the primary
	// map, so a key that reaches several elements would have a bucket walk an
	// element once per filing. Refused rather than represented differently,
	// because the representation that would carry it is one nothing has asked
	// for yet.
	if !of.unique {
		diags.Add(plugin.New(codeSecondariesNeedUnique, written.Pos,
			"%s=%s is written beside %s=false, and a secondary lookup resolves through a key that has to reach one element",
			optionIndex, written.Value, optionUnique).
			WithHint("%s", "let the keys be unique, or look elements up by the other field as the key"))
		return nil, bound
	}

	var out []column

	said := make(map[string]bool, len(named))
	for _, name := range named {
		if said[name] {
			continue
		}
		said[name] = true

		if name == of.key.field {
			diags.Add(plugin.New(codeSecondaryIsKey, written.Pos,
				"%s=%s names %s, which is the key", optionIndex, written.Value, name).
				WithHint("%s", "drop it from "+optionIndex+"; the key already answers for it"))
			continue
		}

		field, resolved := subject.Field(name)
		if !resolved {
			continue
		}

		if code, why := usable(field); why != "" {
			diags.Add(plugin.New(code, written.Pos,
				"%s=%s: %s %s", optionIndex, name, name, why).
				WithHint("%s", "drop it from the option, or name a field this layer can generate from"))
			continue
		}

		typ := plugin.Spell(field.Type.Type, spelled.Local, bound)
		bound = typ.Bound(bound)

		out = append(out, dimension(field, typ))
	}
	return out, bound
}

// dimension turns one usable field into the column a lookup is built from.
//
// The method takes the field's name through [plugin.Join], which is where
// every generated name is spelled; the struct field takes it through
// [plugin.Around], so the author's own spelling comes through and the seam is
// the one every seam forge writes is.
func dimension(field plugin.Field, typ plugin.Spelling) column {
	return column{
		field:  field.Name,
		method: plugin.Join("By", field.Name),
		slot:   plugin.Around(false, "by", field.Name),
		typ:    typ,
	}
}

// usable says what stops a field from being generated from, or nothing.
//
// Export comes first, because it is the one answer that does not depend on
// which option named the field: generated code that cannot read a field
// cannot do anything with it.
func usable(field plugin.Field) (plugin.Code, string) {
	if !field.Exported {
		return codeFieldUnexported, "is not exported, and generated code cannot read it"
	}
	if !keyable(field) {
		return codeNotKeyable, "is " + field.Type.String() + ", which cannot be a map key"
	}
	return 0, ""
}

// keyable reports whether a field can be a map key, which is what every
// dimension needs. A slice, a map and a function cannot; a struct of them
// cannot either, which is why this asks the type rather than its shape.
func keyable(field plugin.Field) bool {
	return field.Type.Type != nil && types.Comparable(field.Type.Type)
}

// named fills in what the generated methods call the container, an element, a
// key and their own locals, out of the way of every name their bodies also
// spell.
//
// Seeded with what the file binds and with the spellings themselves. The
// packages cover a subject declared somewhere else; one declared in the
// package being generated into imports nothing, so its name reaches this only
// through the spelling. The entry type and the sentinel are seeded too, since
// a placing body names both.
func named(of *plan, bound []plugin.Import) {
	taken := make([]string, 0, len(bound)+len(of.secondaries)+8)
	for _, one := range bound {
		taken = append(taken, one.Name)
	}

	taken = append(taken, plugin.Mentioned(of.subject.Text)...)
	taken = append(taken, plugin.Mentioned(of.declared)...)
	taken = append(taken, of.entry, of.dup)

	for _, one := range append([]column{of.key}, of.secondaries...) {
		if one.field == "" {
			continue
		}
		taken = append(taken, plugin.Mentioned(one.typ.Text)...)
	}

	block := plugin.Locals(taken...)

	of.receiver = block.Declare("r")
	of.element = block.Declare("v")
	of.lookup = block.Declare("k")
	of.slotE = block.Declare("e")
	of.held = block.Declare("held")
	of.bucket = block.Declare("bucket")
}

// clashes reports every pair of lookups whose generated names came out the
// same.
//
// [plugin.Join] spells an initialism in full case wherever it falls, which is
// what makes the pair possible: Id and ID are two fields and ByID is one
// name. What that would produce is a method declared twice in a file the
// author cannot edit, so it is caught here, where the two names can be shown
// together. The fixed methods cannot collide with a lookup — every lookup
// opens with By and none of them do — and a name the author's own code
// already holds is the generic collision pass's to report.
func (p plan) clashes() plugin.Diagnostics {
	var diags plugin.Diagnostics

	seen := make(map[string]string, len(p.secondaries)+1)
	if p.key.field != "" {
		seen[p.key.method] = p.key.field
	}

	for _, one := range p.secondaries {
		if first, twice := seen[one.method]; twice {
			diags.Add(plugin.New(codeLookupsCollide, p.at,
				"%s is generated for %s and is already generated for %s", one.method, one.field, first).
				WithHint("%s", "the two cannot both be reached; drop one of the fields from the option, or rename it"))
			continue
		}
		seen[one.method] = one.field
	}

	return diags
}

// surface is the plan as the layers above it see it: every method this layer
// will put on the declared type, named and with its signature spelled.
//
// Written from the plan rather than read back from what build produced, and
// the two are held together by a test rather than by construction. Reading
// the syntax back would report whatever the builder happened to emit — the
// template's helpers included — where what belongs in a surface is the
// contract, and a layer above written against a helper would break the first
// time one was renamed.
//
// Every method takes a pointer. The container is a struct holding a slice and
// its maps, and a copy of one is a second container sharing the first's
// entries — so even the methods that only read take the original, and a
// decorator wrapping them has to know that before it writes the call.
func (p plan) surface(owner plugin.TypeRef, elem plugin.TypeRef) []plugin.Method {
	var (
		bare = spellElem(elem)
		seq  = "iter.Seq[" + bare + "]"
	)

	fails := ""
	appends := "adds every element a sequence yields"
	if p.refusing {
		fails = " error"
		appends = "adds every element a sequence yields, and stops at the first whose key is already held"
	}

	out := make([]plugin.Method, 0, 6+len(p.secondaries))
	out = append(out,
		plugin.Method{Name: "Len", Signature: "() int", Owner: owner, Pointer: true, Doc: "how many elements the container holds"},
		plugin.Method{Name: "All", Signature: "() " + seq, Owner: owner, Pointer: true, Doc: "walks the elements in the order they were added, less any that removal has moved"},
		plugin.Method{Name: appendPlain, Signature: "(seq " + seq + ")" + fails, Owner: owner, Pointer: true, Doc: appends},
		plugin.Method{Name: resetMethod, Signature: "()", Owner: owner, Pointer: true, Doc: "empties the container, keeping the memory it has taken"},
	)

	if p.key.field == "" {
		return out
	}

	if p.unique {
		out = append(out,
			plugin.Method{
				Name: "Remove", Signature: "(" + p.lookup + " " + p.typText(p.key) + ") bool",
				Owner: owner, Pointer: true,
				Doc: "takes the element held under a key out of the container, and reports whether one was",
			},
			plugin.Method{
				Name: p.key.method, Signature: "(" + p.lookup + " " + p.typText(p.key) + ") (*" + p.subjectText(bare) + ", bool)",
				Owner: owner, Pointer: true,
				Doc: p.key.method + " returns a pointer to the element held under a key, and whether one is.",
			})
	} else {
		out = append(out,
			plugin.Method{
				Name: "Remove", Signature: "(" + p.lookup + " " + p.typText(p.key) + ") int",
				Owner: owner, Pointer: true,
				Doc: "takes every element held under a key out of the container, and reports how many were",
			},
			plugin.Method{
				Name: p.key.method, Signature: "(" + p.lookup + " " + p.typText(p.key) + ") iter.Seq[" + p.subjectText(bare) + "]",
				Owner: owner, Pointer: true,
				Doc: p.key.method + " walks the elements held under a key, oldest first.",
			})
	}

	for _, one := range p.secondaries {
		out = append(out, plugin.Method{
			Name: one.method, Signature: "(" + p.lookup + " " + p.typText(one) + ") iter.Seq[" + p.subjectText(bare) + "]",
			Owner: owner, Pointer: true,
			Doc: one.method + " walks the elements whose " + one.field + " is this value, oldest first.",
		})
	}

	return out
}

// typText spells a column's type for a signature, and subjectText the element.
//
// Both prefer the spelling the file will hold, which for a subject or a field
// from the package being written into is the bare name — and fall back to the
// bare element where a plan was made without one, which is the shape question
// asked with no declaration.
func (p plan) typText(one column) string {
	return one.typ.Text
}

func (p plan) subjectText(bare string) string {
	if p.subject.Text != "" {
		return p.subject.Text
	}
	return bare
}

// imports is everything the generated methods name, which is the subject's own
// packages and every dimension's.
//
// A key is as likely to come from somewhere else as the subject is —
// time.Time is the ordinary case — and a lookup taking one names it.
// Gathering the subject's alone is the shape of mistake that produces a file
// which is right in every line and does not compile.
func (p plan) imports() []plugin.Import {
	out := slices.Clone(p.subject.Imports)

	for _, one := range append([]column{p.key}, p.secondaries...) {
		for _, needed := range one.typ.Imports {
			if !slices.ContainsFunc(out, func(held plugin.Import) bool { return held.Path == needed.Path }) {
				out = append(out, needed)
			}
		}
	}

	slices.SortFunc(out, func(a, b plugin.Import) int { return strings.Compare(a.Path, b.Path) })
	return out
}
