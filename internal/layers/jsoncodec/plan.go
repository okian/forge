package jsoncodec

import (
	"fmt"
	"go/token"
	"go/types"
	"strings"

	"github.com/okian/forge/internal/diag"
	"github.com/okian/forge/internal/model"
)

// Diagnostics this layer reports.
//
// All three are about a field the codec cannot be written for, which is the
// only kind of failure a codec has: everything else it does is decided by the
// field's type and the tag written on it. They are refusals rather than
// warnings because the alternative is output that compiles and encodes the
// wrong thing — a member missing from an object is not something a round-trip
// test through the same codec would ever notice.
var (
	codeOpaqueField  = diag.Register(2007, "field type cannot be encoded without reflection")
	codeTagOption    = diag.Register(2008, "json tag option is not supported")
	codeNameCollides = diag.Register(2009, "two fields claim one name on the wire")
	codeCannotOmit   = diag.Register(2010, "a member cannot be tested for the value it would be omitted at")
	codeHalfCodec    = diag.Register(2011, "a type declares one half of a codec")
)

// halfHint says what to do about a type carrying one half of a codec.
const halfHint = "write the other half, or rename the one that is there; " +
	"a generated pair would redeclare it"

// zeroHint says what to do about a zero test that cannot be written.
//
// A type can answer the question for itself, and declaring IsZero is how: this
// asks it before comparing anything, so a type whose parts cannot be compared
// has a way to be omitted anyway. The other way is omitempty, which is answered
// by writing the member and looking at it — a different question, and often the
// one that was meant.
const zeroHint = "remove the option, give the field's type an IsZero method, " +
	"or use omitempty, which asks what the member wrote rather than what it holds"

// fallbackHint says how to encode a field forge cannot see through.
const fallbackHint = "write //forge:json fallback=stdlib above the field to hand it to the reflective encoder"

// written is how one type's value is put on the wire and read back.
type written uint8

const (
	// writtenInvalid is the zero value: nothing decided how to write it.
	writtenInvalid written = iota

	// The scalars, each named for the jsontext token that carries it.
	writtenBool
	writtenString
	writtenInt
	writtenUint
	writtenFloat

	// writtenBytes is a slice or array of bytes, which JSON carries as a
	// base64 string rather than as an array of numbers. It is the one place a
	// Go slice is not a JSON array, and it is the standard library's rule
	// rather than a choice available here.
	writtenBytes

	// writtenStruct is a struct whose codec this layer also writes.
	writtenStruct

	// writtenDelegate is a type that already declares a codec of its own,
	// which is called rather than duplicated.
	writtenDelegate

	// The composites, each written in terms of what it holds.
	writtenPointer
	writtenSlice
	writtenArray
	writtenMap

	// writtenFallback is a field handed to the reflective encoder, which is
	// what a directive above the field asks for.
	writtenFallback
)

// form is one type and how its value is written.
type form struct {
	// typ is the type itself, and spelled is how it is written in the file
	// being generated for.
	typ     types.Type
	spelled model.Spelling

	// how says which form the value takes.
	how written

	// key and elem are the parts of a composite, each already decided.
	key  *form
	elem *form

	// members are a struct's, flattened: an embedded struct's members appear
	// here as the enclosing struct's, which is what JSON embedding means.
	members []member

	// attach records that the type may carry the two methods, which is true
	// only for a struct declared in the package being generated into.
	attach bool

	// whole records that the members are the whole struct: nothing was left off
	// the wire, so what the members say about the value is what the value is.
	//
	// It is what lets a zero test be built out of the members. An unexported
	// field and one tagged out are not written and are still part of the value,
	// so a struct missing either has members that answer a narrower question
	// than the one being asked.
	whole bool
}

// guard is an embedded pointer a member is reached through.
//
// Both halves are needed and neither is derivable from the other here. The path
// is what the condition and the allocation are written against, and the elem is
// what is allocated — spelled for the file being generated into, which is a
// question about imports rather than about types and is answered where the
// spelling is done.
type guard struct {
	// path reaches the pointer field from the outermost value.
	path string

	// elem is how the type it points at is spelled, and imports are what that
	// spelling binds: the allocation names the type, and a file that names a
	// package it does not import does not compile.
	elem    string
	imports []model.Import
}

// member is one field as it appears on the wire.
type member struct {
	// name is what the member is called in the JSON object.
	name string

	// path is how the field is reached from the enclosing value, without the
	// leading receiver: "Name", or "Address.City" for a promoted one.
	path string

	// guards lists the pointer fields the path passes through, outermost
	// first. An embedded pointer that is nil contributes no members when
	// writing and is allocated when reading, which is what these are for.
	guards []guard

	// of is how the field's own value is written.
	of form

	// omitZero and omitEmpty are the two ways a member may be left out.
	omitZero  bool
	omitEmpty bool

	// tagged records that the member's name was written in a tag rather than
	// derived from the field's, which is what breaks a tie between two fields
	// at one depth claiming one name.
	tagged bool

	// field is the field this came from, which is what a diagnostic points at.
	field model.Field
}

// planner decides what a subject's codec is made of.
//
// It carries the structs the subject reaches, because a field whose type is one
// of them is written by calling that type's codec rather than by inlining it,
// and because the walk has to stop somewhere: a struct that reaches itself
// would otherwise be flattened forever.
type planner struct {
	// into is the package being generated into, which decides whether a struct
	// can carry a method.
	into string

	// known holds every struct the subject reaches, by the spelling that
	// identifies it.
	known map[string]*model.Struct

	// style is what an untagged field's name is written in, and omitZero says
	// whether every member is left out when it holds its zero value. Both are
	// written on the declaration and apply to the whole subject; a tag on one
	// field can ask for omission where the declaration did not, and nothing
	// asks for the opposite.
	style    string
	omitZero bool

	// dropped records that a field of the struct being flattened was left off
	// the wire, which is what [form.whole] is built from.
	dropped bool

	// filling holds the types being decided right now, which is what says a
	// type has reached itself rather than merely been reached twice. It is
	// emptied on the way into a struct, because a struct's codec is a function
	// and a call is where a cycle stops being infinite.
	filling map[string]bool

	// forms memoises by spelling, so that a type reached twice is decided
	// once and a type that reaches itself terminates.
	forms map[string]*form

	// order lists the spellings in the order they were first decided, which is
	// the order their codecs are written.
	order []string

	diags diag.Set
}

// spellingOf writes a type as the generated file must spell it.
func (p *planner) spellingOf(t types.Type) model.Spelling {
	return model.Spell(t, p.into, nil)
}

// key identifies a type across the whole plan.
//
// The fully qualified spelling rather than the one the file uses: two types
// from two packages of the same name are one string under the second and two
// under this, and merging them would write one codec for two types.
func key(t types.Type) string { return model.TypeString(t) }

// plan decides the codec for a subject and everything it reaches.
func (p *planner) plan(held *model.Struct) *form {
	p.known = make(map[string]*model.Struct)
	p.forms = make(map[string]*form)
	p.filling = make(map[string]bool)

	p.remember(held)
	for _, reached := range held.Closure {
		p.remember(reached)
	}

	return p.decide(held.Type(), blamed{pos: held.Pos})
}

// remember records a struct under the spelling its type is identified by.
func (p *planner) remember(held *model.Struct) {
	if held == nil || held.Named == nil {
		return
	}
	p.known[key(held.Type())] = held
}

// blamed is the field a refusal about a type is reported against.
//
// A type is refused where it is used rather than where it is declared: a field
// of type []any is the author's to fix, and the declaration of any is not. The
// name is carried beside the position because a diagnostic is read twice — once
// in an editor, which follows the position, and once in a build log, which has
// only the text.
type blamed struct {
	name string
	pos  token.Position
}

// decide returns how a type is written, deciding it once.
func (p *planner) decide(t types.Type, where blamed) *form {
	held := key(t)
	if found, seen := p.forms[held]; seen {
		// Reached again while it is still being decided, which means it
		// contains itself with no struct in between. A struct is what makes a
		// cycle finite — its codec is a function, and a call ends where the
		// value does — so a chain of pointers, slices or maps that closes on
		// itself has no form to write at all: every attempt at one is the same
		// attempt again.
		if p.filling[held] {
			p.refuse(where, "a %s, which contains itself with no struct in between", found.spelled.Text)
		}
		return found
	}

	// Recorded before it is decided, so that a type reaching itself finds the
	// entry rather than recursing into it. What is found is filled in by the
	// time anything reads it, because nothing reads a form until the whole
	// plan is built.
	out := &form{typ: t, spelled: p.spellingOf(t)}
	p.forms[held] = out
	p.order = append(p.order, held)

	p.filling[held] = true
	p.fill(out, where)
	delete(p.filling, held)

	return out
}

// fill decides one form, having already recorded it.
func (p *planner) fill(out *form, where blamed) {
	// Asked before the type is taken apart. A type with a codec of its own is
	// written by calling it, whatever it is made of — which is the whole point
	// of a hand-written codec, and the reason time.Time does not have to be
	// understood here.
	//
	// A pointer is not asked, because a pointer's method set is its element's
	// and answering yes would lose the only thing a pointer adds: that it may
	// be absent. Delegating straight through one writes a call on a nil
	// receiver, which for a value-receiver method is a dereference at run time
	// rather than a null on the wire. What the pointer points at is asked
	// instead, once the pointer itself has been taken apart below.
	if _, indirect := out.typ.(*types.Pointer); !indirect {
		if p.declaresCodec(out.typ) {
			out.how = writtenDelegate
			return
		}

		// One half is not a codec, and it is not nothing either. Generating the
		// pair would redeclare the half that is there, in a file the author
		// cannot edit; generating neither would leave the type written by a
		// marshaler nobody wrote a reader for. Saying so is the only answer
		// that leaves them somewhere to go.
		if half := p.halfCodec(out.typ); half != "" {
			p.diags.Add(diag.New(codeHalfCodec, where.pos,
				"%s declares %s and not the other half", out.spelled.Text, half).
				WithHint("%s", halfHint))
			return
		}
	}

	switch under := out.typ.Underlying().(type) {
	case *types.Basic:
		out.how = basic(under)
		if out.how == writtenInvalid {
			p.refuse(where, "a %s, which has no JSON form", under.Name())
		}

	case *types.Struct:
		p.fillStruct(out, where)

	case *types.Pointer:
		out.how = writtenPointer
		out.elem = p.decide(under.Elem(), where)

	case *types.Slice:
		if isByte(under.Elem()) {
			out.how = writtenBytes
			return
		}
		out.how = writtenSlice
		out.elem = p.decide(under.Elem(), where)

	case *types.Array:
		if isByte(under.Elem()) {
			out.how = writtenBytes
			return
		}
		out.how = writtenArray
		out.elem = p.decide(under.Elem(), where)

	case *types.Map:
		p.fillMap(out, under, where)

	default:
		p.refuse(where, "a %s, which cannot be encoded without knowing at run time what it holds", out.spelled.Text)
	}
}

// fillStruct decides a struct, which is the case with members.
func (p *planner) fillStruct(out *form, where blamed) {
	held, modelled := p.known[key(out.typ)]
	switch {
	case !modelled:
		// Reached through something the subject walk did not follow. Nothing
		// here knows its fields' tags or the options written above them, and
		// guessing at either produces a codec that disagrees with the one the
		// same struct gets elsewhere.
		p.refuse(where, "a %s, a struct this codec was not given the fields of", out.spelled.Text)
		return

	case held.External:
		// Its unexported fields cannot be read from here at all, so a codec
		// written member by member would silently leave them out — and for a
		// type like time.Time, whose whole content is unexported, that is an
		// empty object rather than a timestamp.
		p.refuse(where, "a %s, declared in another module, whose unexported fields generated code cannot read",
			out.spelled.Text)
		return
	}

	out.how = writtenStruct
	out.attach = held.Attachable(p.into)

	// Saved and restored, because a member's own type may be a struct whose
	// fields are dropped, and what is being recorded is about this one.
	//
	// The chain of types being decided is emptied rather than saved: what it
	// records is a type reaching itself without a function in between, and this
	// struct's codec is that function. A member that reaches back to it is
	// reaching a call, which ends.
	outer, chain := p.dropped, p.filling
	p.dropped, p.filling = false, make(map[string]bool)

	out.members = p.flatten(held, "", nil, make(map[string]bool))
	out.whole = !p.dropped

	p.dropped, p.filling = outer, chain
}

// fillMap decides a map, which JSON can carry only when its keys are strings.
func (p *planner) fillMap(out *form, under *types.Map, where blamed) {
	basic, ok := under.Key().Underlying().(*types.Basic)
	if !ok || basic.Info()&types.IsString == 0 {
		p.refuse(where, "a %s, keyed by something a JSON object member cannot be named by",
			out.spelled.Text)
		return
	}

	// A key that brought its own codec writes itself, and what it writes is a
	// value rather than a member name — so a map keyed by one is not written as
	// this would write it.
	if p.declaresCodec(under.Key()) {
		p.refuse(where, "a %s, whose key type writes itself and so cannot be a member name",
			out.spelled.Text)
		return
	}

	out.how = writtenMap
	out.key = p.decide(under.Key(), where)
	out.elem = p.decide(under.Elem(), where)
}

// basic returns how a predeclared type is written, or writtenInvalid.
//
// Complex numbers and unsafe.Pointer have no JSON form, and neither does an
// untyped constant's type, which a field can never have anyway. Everything else
// is one of five tokens.
func basic(held *types.Basic) written {
	info := held.Info()
	switch {
	case info&types.IsBoolean != 0:
		return writtenBool
	case info&types.IsString != 0:
		return writtenString
	case info&types.IsUnsigned != 0:
		return writtenUint
	case info&types.IsInteger != 0:
		return writtenInt
	case info&types.IsFloat != 0:
		return writtenFloat
	default:
		return writtenInvalid
	}
}

// isByte reports whether a type is the one Go spells both byte and uint8, which
// is what makes a slice of it a string on the wire.
func isByte(t types.Type) bool {
	held, ok := t.Underlying().(*types.Basic)
	return ok && held.Kind() == types.Uint8
}

// refuse records that a field cannot be written, and how to write it anyway.
// The field's name opens the sentence, so that the complaint reads as being
// about something the author wrote. Where a refusal is about the subject itself
// rather than about one of its fields there is no name to open with, and the
// sentence starts with what the type is.
func (p *planner) refuse(where blamed, format string, args ...any) {
	said := fmt.Sprintf(format, args...)
	if where.name != "" {
		said = where.name + " is " + said
	}

	p.diags.Add(diag.New(codeOpaqueField, where.pos, "%s", said).
		WithHint("%s", fallbackHint))
}

// The json tag options this layer understands, beyond the leading name.
const (
	optionOmitZero  = "omitzero"
	optionOmitEmpty = "omitempty"
	optionString    = "string"
	optionEmbed     = "embed"
	optionFormat    = "format"
	optionCase      = "case"
)

// flatten returns a struct's members as they appear in one JSON object.
//
// Embedding is what makes this a walk rather than a loop. An embedded struct's
// members are the enclosing struct's members — that is what Go embedding means
// and what encoding/json/v2 does with it — so they are collected here, at the
// depth they were reached, rather than being written as a nested object.
//
// The precedence rules are the standard library's, and they are not forge's to
// choose: a name claimed at two depths belongs to the shallower, and a name
// claimed twice at one depth belongs to whichever field named it in a tag. What
// is left over — one name, one depth, two tags — is a struct that encoding/json
// also refuses, and it is refused here so that the refusal arrives when the code
// is written rather than when it runs.
//
// prefix is how the struct being walked is reached from the outermost value,
// and guards are the pointer fields on the way to it.
func (p *planner) flatten(held *model.Struct, prefix string, guards []guard, walked map[string]bool) []member {
	if held == nil || walked[key(held.Type())] {
		// A struct embedding itself has no finite set of members. Go allows it
		// only through a pointer, which is why it has to be stopped by what has
		// been walked rather than by what a type is.
		return nil
	}
	walked[key(held.Type())] = true
	defer delete(walked, key(held.Type()))

	var out []member
	for _, field := range held.Fields {
		out = append(out, p.member(field, prefix, guards, walked)...)
	}

	return p.resolve(out)
}

// member returns what one field contributes: its own member, or the members of
// the struct it embeds.
func (p *planner) member(field model.Field, prefix string, guards []guard, walked map[string]bool) []member {
	if wrong, found := p.unsupported(field); found {
		p.diags.Add(wrong)
		return nil
	}

	name, written := wireName(field, p.style)
	if !written {
		// Not on the wire, and still part of the value. What that costs is
		// recorded rather than shrugged at: a zero test built from the members
		// of a struct with a field missing is a test of something else.
		p.dropped = true
		return nil
	}

	path := prefix + field.Name
	if p.promotes(field) {
		return p.promoted(field, path, guards, walked)
	}

	return []member{{
		name:      name,
		path:      path,
		guards:    guards,
		of:        *p.fieldForm(field),
		omitZero:  has(field, optionOmitZero) || p.omitZero,
		omitEmpty: has(field, optionOmitEmpty),
		field:     field,
		tagged:    tagged(field),
	}}
}

// promotes reports whether a field's own members become the enclosing struct's.
//
// An embedded field is promoted unless it is given a name, which is the rule
// encoding/json/v2 states: embedding is implicit for a Go embedded field, and a
// name written in a tag is what asks for a nested object instead. A named field
// is promoted only when it asks to be.
func (p *planner) promotes(field model.Field) bool {
	if has(field, optionEmbed) {
		return true
	}

	tag, ok := field.Tag(jsonKey)
	return field.Embedded && (!ok || tag.Name == "")
}

// promoted returns the members an embedded field contributes.
//
// A pointer to a struct is embedded through, and the pointer becomes a guard:
// its members are written only when it is non-nil, and reading one allocates
// it. Anything else that cannot be walked into is refused, because a field
// asking to be embedded and not written at all is silence about a whole group
// of members.
func (p *planner) promoted(field model.Field, path string, guards []guard, walked map[string]bool) []member {
	under := field.Type.Type
	if pointer, is := under.Underlying().(*types.Pointer); is {
		under = pointer.Elem()
		spelled := p.spellingOf(under)
		guards = append(append([]guard(nil), guards...),
			guard{path: path, elem: spelled.Text, imports: spelled.Imports})
	}

	embedded, modelled := p.known[key(under)]
	if !modelled {
		p.dropped = true
		p.refuse(blamed{field.Name, field.Pos}, "embedded, and this codec was not given its fields")
		return nil
	}
	if _, is := under.Underlying().(*types.Struct); !is {
		p.refuse(blamed{field.Name, field.Pos}, "embedded, and only a struct's members can be promoted")
		return nil
	}
	if embedded.External {
		p.refuse(blamed{field.Name, field.Pos}, "embedded from another module, whose unexported fields cannot be read")
		return nil
	}

	// A type with a codec of its own is written by that codec, which writes an
	// object — so its members are not available to be promoted into this one.
	if p.declaresCodec(under) {
		p.refuse(blamed{field.Name, field.Pos},
			"embedded and declares a codec of its own, which writes an object rather than members")
		return nil
	}

	return p.flatten(embedded, path+".", guards, walked)
}

// fieldForm decides how a field's value is written, honouring a directive
// above it that asks for the reflective encoder.
func (p *planner) fieldForm(field model.Field) *form {
	if p.fallback(field) {
		return &form{typ: field.Type.Type, spelled: p.spellingOf(field.Type.Type), how: writtenFallback}
	}
	return p.decide(field.Type.Type, blamed{field.Name, field.Pos})
}

// fallback reports whether a directive above the field hands it to the
// reflective encoder.
//
// The value is checked rather than the key alone. fallback is the only option
// this layer takes on a field and stdlib is its only value, so anything else
// written there is a misspelling, and a misspelling that quietly turned
// reflection on would be the one mistake this option exists to make visible.
func (p *planner) fallback(field model.Field) bool {
	found := false

	for _, directive := range model.Written(field.Directives, markerName) {
		key, value, split := strings.Cut(directive.Args, "=")
		switch {
		case strings.TrimSpace(key) != optionFallback:
			p.diags.Add(diag.New(codeTagOption, directive.Pos,
				"%s is not an option this layer takes on a field", strings.TrimSpace(key)).
				WithHint("a field takes %s=%s and nothing else", optionFallback, fallbackValue))
		case !split || strings.TrimSpace(value) != fallbackValue:
			p.diags.Add(diag.New(codeTagOption, directive.Pos,
				"%s takes %s and was given %q", optionFallback, fallbackValue, strings.TrimSpace(value)).
				WithHint("%s is the only reflective boundary there is", fallbackValue))
		default:
			found = true
		}
	}

	return found
}

// The field-scoped option, and the one value it takes.
const (
	optionFallback = "fallback"
	fallbackValue  = "stdlib"
)

// unsupported returns what is wrong with a field's json tag, if anything.
//
// One option and one case. format parses and generates nothing: the Go release
// this targets withdrew its support behind an internal flag no build can reach,
// so a field carrying one would be encoded in a format nobody asked for. Saying
// so is the whole of the handling — silently ignoring an option that names a
// date layout produces timestamps in the wrong format, which is exactly the
// kind of wrong that reaches production.
func (p *planner) unsupported(field model.Field) (diag.Diagnostic, bool) {
	if option, ok := tagOption(field, optionFormat); ok {
		return diag.New(codeTagOption, field.Pos,
			"%s is written with %s, which this Go release does not support", field.Name, option.Raw).
			WithHint("%s", formatHint), true
	}

	if option, ok := tagOption(field, optionCase); ok {
		return diag.New(codeTagOption, field.Pos,
			"%s is written with %s, which decides how a name is matched when reading", field.Name, option.Raw).
			WithHint("%s", caseHint), true
	}

	if option, ok := tagOption(field, optionString); ok {
		return diag.New(codeTagOption, field.Pos,
			"%s is tagged %q, which puts its number inside a JSON string", field.Name, ","+option.Raw).
			WithHint("%s", quotedHint), true
	}

	return diag.Diagnostic{}, false
}

// formatHint and caseHint say what to do about an option this layer does not
// generate for.
const (
	formatHint = "the format option is withdrawn behind an internal flag in this Go release; " +
		"remove it, or write //forge:json fallback=stdlib above the field to encode it reflectively"
	caseHint = "generated code matches a name exactly; remove the option, or write " +
		"//forge:json fallback=stdlib above the field to match it reflectively"
	quotedHint = "this layer does not generate the quoted form; remove the option, or write " +
		"//forge:json fallback=stdlib above the field to encode it reflectively"
)

// has reports whether a field's json tag carries a bare option.
func has(field model.Field, name string) bool {
	tag, ok := field.Tag(jsonKey)
	return ok && tag.Has(name)
}

// tagged reports whether a field's name on the wire was written rather than
// derived, which is what breaks a tie between two fields at one depth.
func tagged(field model.Field) bool {
	tag, ok := field.Tag(jsonKey)
	return ok && tag.Name != ""
}

// resolve applies the precedence rules to members claiming one name.
//
// They arrive in depth order because the walk is depth-first through a struct's
// own fields before its embedded ones — so the first member under a name is the
// shallowest, and a later one at the same depth is a tie. A tie that a tag
// breaks is resolved; a tie that nothing breaks is a name with no answer.
//
// Refused rather than dropped. The standard library drops it — a name two
// embedded structs both claim is written by neither — which is a member going
// missing for a reason nothing in the source says out loud. A refusal costs the
// author one tag and tells them why.
//
// The one that wins keeps its own place in the object rather than taking the
// place of the one it beat. The two are different positions and the standard
// library writes the winner's, so a struct whose own field shadows an embedded
// one writes the embedded members first and the shadowing field where it was
// declared.
//
// Both of those need the winner, and the winner is not known when its rival is
// reached — a member can be beaten by one that has not been walked to yet. So
// the winners are chosen first and everything else asked afterwards, the
// complaint included: raising it while walking would refuse a struct where a
// shallower field settles the tie a moment later, which is a struct the
// standard library encodes without complaint.
func (p *planner) resolve(held []member) []member {
	won := make(map[string]int, len(held))

	for i, one := range held {
		at, claimed := won[one.name]
		if !claimed {
			won[one.name] = i
			continue
		}

		standing := held[at]
		switch {
		case one.depth() < standing.depth():
			won[one.name] = i
		case one.tagged && !standing.tagged && one.depth() == standing.depth():
			won[one.name] = i
		}
	}

	out := make([]member, 0, len(won))
	for i, one := range held {
		if won[one.name] != i {
			continue
		}
		if rival, tied := p.ambiguous(held, i, one); tied {
			p.diags.Add(diag.New(codeNameCollides, one.field.Pos,
				"%s and %s are both written as %q", one.path, rival.path, one.name).
				WithHint("%s", "give one of them a different name in its json tag, or leave it out with json:\"-\""))
			continue
		}
		out = append(out, one)
	}
	return out
}

// ambiguous returns a member the winner cannot be told from, if there is one.
//
// A rival at the winner's own depth with the same claim on the name — both
// tagged, or neither — is a name the precedence rules do not settle. Nothing
// below the winner's depth can be one, because depth settles those.
func (p *planner) ambiguous(held []member, at int, winner member) (member, bool) {
	for i, one := range held {
		if i == at || one.name != winner.name {
			continue
		}
		if one.depth() == winner.depth() && one.tagged == winner.tagged {
			return one, true
		}
	}
	return member{}, false
}

// depth is how many structs deep the member was found, which is what decides
// precedence between two claiming one name.
func (m member) depth() int { return strings.Count(m.path, ".") }
