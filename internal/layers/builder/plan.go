package builder

import (
	"github.com/okian/forge/internal/layers/validate"
	"github.com/okian/forge/plugin"
)

// codeUnsettable reports a field a builder cannot set and a rule saying it must
// be given.
//
// A 2xxx, because it is about the subject: what is wrong is that the field is
// unexported and the tag on it says a value has to be supplied, and only the
// author can decide which of the two to change.
var codeUnsettable = plugin.Register(2020, "a builder cannot be given a field it cannot set")

// codeTakenName reports a field whose setter would take a name the builder
// already uses.
//
// A 4xxx rather than a 2xxx: nothing is wrong with the field, and nothing is
// wrong with the builder. What is wrong is that the two would be one method,
// which is a decision about emission rather than about either.
var codeTakenName = plugin.Register(4019, "a builder's setter wants a name the builder already has")

// codeNothingToGive reports a subject a builder would offer nothing of.
//
// A 2xxx like the one above, and for the same reason: what is wrong is the
// subject, and what to do about it is the author's.
var codeNothingToGive = plugin.Register(2021, "a builder over a subject with nothing a caller can give")

// codeUnwritable reports a field whose type a builder cannot name, or must not
// copy.
//
// A 2xxx as well. Both are facts about the subject's fields that the setter's
// signature or body would run into, and both are the author's to change.

// unsettableHint says what to do about a required field a builder cannot set.
const unsettableHint = "export the field, so that a caller can give it, or drop the rule, " +
	"so that the value it keeps is the zero one — a builder offers what a caller can name"

// emptyHint says what to do about a subject a builder would offer nothing of.
const emptyHint = "export a field, or drop the layer: what a builder is for is naming the " +
	"fields at the call site, and one with no setters names nothing"

// unnameableHint says what to do about a field whose type a builder cannot
// write down.
const unnameableHint = "the setter's signature has to name the field's type, and an unexported " +
	"name belongs to the package that declared it — export the type, or move the declaration " +
	"into that package"

// uncopyableHint says what to do about a field a builder would have to copy and
// must not.
const uncopyableHint = "hold it behind a pointer, which is the usual advice for a lock and is " +
	"what makes the value copyable — a builder gives a field by assigning it, and Build hands " +
	"back the value it built"

// takenHint says what to do about a field named after the method that ends a
// builder.
const takenHint = "rename the field: a builder needs one name of its own, and it is the one " +
	"that hands the value back"

// The method that ends a builder, and what its type is called after the subject.
const (
	method = "Build"
	suffix = "Builder"
	verb   = "builder"
)

var codeUnwritable = plugin.Register(2022, "a builder cannot be given a field of this type")

// settable is one field a builder can be given.
type settable struct {
	// field is the field itself, which is what a diagnostic points at.
	field plugin.Field

	// name is the setter's own name, which is the field's.
	name string

	// spelled is how the field's type must be written in the file being
	// generated into.
	spelled plugin.Spelling

	// demanded records that the author said a value has to be given, and index
	// is which of the builder's record of what it was given this field is.
	demanded bool
	index    int
}

// plan is the whole of one builder.
type plan struct {
	// into is the package being generated into, which decides what a setter's
	// signature may name.
	into string

	// bound is what the file will bind, which every spelling is written
	// against so that a field's type from a package a layer of the stack
	// already imports is written under a name the file has not taken.
	bound []plugin.Import

	// of is the subject, and spelled how it is written in the file being
	// generated into.
	of      *plugin.Struct
	spelled plugin.Spelling

	// declared is the builder type's own name, and made the function that
	// returns one.
	declared string
	made     string

	// fields are the ones a caller can give, in declaration order.
	fields []settable

	// demanded is how many of them the author said have to be given, which is
	// the size of the builder's record of what it was given.
	demanded int

	diags plugin.Diagnostics
}

// planned works out what a subject's builder is made of.
func planned(held *plugin.Struct, into string, bound []plugin.Import) *plan {
	out := &plan{
		into:     into,
		bound:    bound,
		of:       held,
		spelled:  plugin.Spell(held.Type(), into, bound),
		declared: plugin.Through(held, "", suffix, into),
	}
	out.made = plugin.Around(true, "new", out.declared)

	for _, field := range held.Fields {
		out.consider(field)
	}

	// A builder with no setters is a constructor spelled the long way. Reported
	// rather than written, because what a builder is for is naming the fields
	// at the call site and one that names none of them is a type nobody could
	// have wanted.
	if len(out.fields) == 0 && out.diags.Empty() {
		out.diags.Add(plugin.New(codeNothingToGive, held.Pos,
			"%s has no field a caller could give", held.Ref().Name).
			WithHint("%s", emptyHint))
	}

	return out
}

// consider decides what one field of the subject means to the builder.
func (p *plan) consider(field plugin.Field) {
	demanded := validate.Demands(field)

	// What a builder offers is what a caller can name, and an unexported field
	// is not that — so a tag saying such a field has to be given is asking for
	// something a builder is not for, rather than a thing to work around.
	if !field.Exported {
		if demanded {
			p.diags.Add(plugin.New(codeUnsettable, field.Pos,
				"%s is unexported and is tagged as one a value has to carry", field.Name).
				WithHint("%s", unsettableHint))
		}
		return
	}

	if field.Name == method {
		p.diags.Add(plugin.New(codeTakenName, field.Pos,
			"a setter for %s would take the name %s ends a builder with", field.Name, method).
			WithHint("%s", takenHint))
		return
	}
	if !p.writable(field) {
		return
	}

	one := settable{
		field:    field,
		name:     field.Name,
		spelled:  plugin.Spell(field.Type.Type, p.into, p.bound),
		demanded: demanded,
	}
	if demanded {
		one.index = p.demanded
		p.demanded++
	}

	p.fields = append(p.fields, one)
}

// writable reports whether a setter for this field can be written at all, and
// says why where it cannot.
//
// Two ways it cannot. The signature names the field's type, and a name that is
// unexported belongs to the package that declared it; and the body assigns the
// value, which is a copy — so a field holding a lock would produce an
// assignment the vet everybody runs reports, in a file the caller cannot fix.
func (p *plan) writable(field plugin.Field) bool {
	if what, found := plugin.Unnameable(field.Type.Type, p.into); found {
		p.diags.Add(plugin.New(codeUnwritable, field.Pos,
			"%s is a %s, and %s cannot be named from the package being generated into",
			field.Name, field.Type, what).
			WithHint("%s", unnameableHint))
		return false
	}

	if what, found := plugin.Uncopyable(field.Type.Type); found {
		p.diags.Add(plugin.New(codeUnwritable, field.Pos,
			"%s holds a %s, which is a value that must not be copied", field.Name, what).
			WithHint("%s", uncopyableHint))
		return false
	}

	return true
}

// required returns the fields the author said have to be given, in the order
// Build reports them.
func (p *plan) required() []settable {
	out := make([]settable, 0, p.demanded)
	for _, one := range p.fields {
		if one.demanded {
			out = append(out, one)
		}
	}
	return out
}
