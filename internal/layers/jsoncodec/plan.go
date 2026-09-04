package jsoncodec

import (
	"fmt"
	"go/token"
	"go/types"
	"strings"

	"github.com/okian/forge/plugin"
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
	codeOpaqueField  = plugin.Register(2007, "field type cannot be encoded without reflection")
	codeTagOption    = plugin.Register(2008, "json tag option is not supported")
	codeNameCollides = plugin.Register(2009, "two fields claim one name on the wire")
	codeCannotOmit   = plugin.Register(2010, "a member cannot be tested for the value it would be omitted at")
	codeHalfCodec    = plugin.Register(2011, "a type declares one half of a codec")

	// Three the standard library refuses and this layer used to let past.
	codeTaggedUnexported = plugin.Register(2031, "an unexported field carries a json tag")
	codeNoMembers        = plugin.Register(2032, "a struct has no members to write")
	codeNoRepresentation = plugin.Register(2033, "a type has no default JSON representation")
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

	// writtenText is a type carrying a text codec, which JSON carries as a
	// string written through it.
	//
	// What the standard library does with such a type, and the reason to do the
	// same: a named integer counted through a set of constants means one of
	// their names, and the number behind it is an implementation detail that a
	// document has no way to explain and a reader has no way to check. Writing
	// the name also refuses a value the set has no name for, since that is what
	// the text codec was written to do.
	writtenText

	// writtenTime is time.Time itself, recognised by identity the way a
	// duration is and written first-class rather than through the boundary its
	// own codec would otherwise cross.
	//
	// The identity is what buys the shortcut. Its MarshalJSON and AppendText
	// produce the same bytes — one strict RFC 3339 formatter behind both, with
	// the quotes the only difference — so the value can be appended straight
	// into the caller's buffer instead of being handed to the standard library
	// and spliced, which is where every other MarshalJSON writer goes because
	// nothing else about such a method promises its answer is valid JSON.
	// Reading stays with UnmarshalJSON over the value's own span, exactly as a
	// delegate reads, so the verdicts on the way in are the method's own.
	writtenTime

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
	spelled plugin.Spelling

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

	// text names the half of a text codec the value is written through, and is
	// set only where [form.how] is [writtenText].
	//
	// Decided here rather than at the point of writing, because deciding is
	// what a plan is for: which half a type has is a question about the type,
	// and the writer's business is turning an answer into lines.
	text string

	// writes and reads name the methods a delegate is written and read
	// through, and borrows records that it offers the borrowing reader as
	// well. Set only where [form.how] is [writtenDelegate] — except reads,
	// which [writtenTime] sets too, because a time is read exactly as a
	// delegate is: its own UnmarshalJSON over the value's span.
	//
	// AppendJSON and UnmarshalJSON are called straight. Either name from the
	// streaming pair means the call goes through the standard library instead,
	// because calling those needs an encoder generated output does not carry —
	// and MarshalJSON goes the same way, since the library validates what the
	// method answers and this codec's own output must stay valid whatever an
	// author's method returns.
	writes  string
	reads   string
	borrows bool

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
	imports []plugin.Import
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
	field plugin.Field
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

	// bound is what the file will bind, which every spelling is written
	// against so that a type from a package called json is not written under
	// the name this layer's own import has.
	bound []plugin.Import

	// willWrite reports whether the run will put a method on a type, and
	// authored whether the author already declared one. Between them they are
	// how this layer learns that one of its field types carries a text codec,
	// without believing what a previous run wrote. Either may be nil, which is
	// what a test asking one question gets.
	willWrite func(types.Type, string) bool
	authored  func(types.Type, string) bool

	// known holds every struct the subject reaches, by the spelling that
	// identifies it.
	known map[string]*plugin.Struct

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

	diags plugin.Diagnostics
}

// spellingOf writes a type as the generated file must spell it.
func (p *planner) spellingOf(t types.Type) plugin.Spelling {
	return plugin.Spell(t, p.into, p.bound)
}

// key identifies a type across the whole plan.
//
// The fully qualified spelling rather than the one the file uses: two types
// from two packages of the same name are one string under the second and two
// under this, and merging them would write one codec for two types.
func key(t types.Type) string { return plugin.TypeIdentity(t) }

// plan decides the codec for a subject and everything it reaches.
func (p *planner) plan(held *plugin.Struct) *form {
	p.known = make(map[string]*plugin.Struct)
	p.forms = make(map[string]*form)
	p.filling = make(map[string]bool)

	p.remember(held)
	for _, reached := range held.Closure {
		p.remember(reached)
	}

	return p.decide(held.Type(), blamed{pos: held.Pos})
}

// remember records a struct under the spelling its type is identified by.
func (p *planner) remember(held *plugin.Struct) {
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

// owned reports whether the type says for itself how it goes onto the wire, and
// records the answer where it does.
//
// Asked before the type is taken apart. A type with a codec of its own is
// written by calling it, whatever it is made of — which is the whole point of a
// hand-written codec, and the reason time.Time does not have to be understood
// here.
//
// A pointer is not asked, because a pointer's method set is its element's and
// answering yes would lose the only thing a pointer adds: that it may be
// absent. Delegating straight through one writes a call on a nil receiver,
// which for a value-receiver method is a dereference at run time rather than a
// null on the wire. What the pointer points at is asked instead, once the
// pointer itself has been taken apart.
func (p *planner) owned(out *form, where blamed) bool {
	if _, indirect := out.typ.(*types.Pointer); indirect {
		return false
	}

	if writes, reads := p.codecHalves(out.typ); writes != "" && reads != "" {
		out.how = writtenDelegate
		out.writes, out.reads = writes, reads
		out.borrows = p.declares(out.typ, borrowedMethod)
		return true
	}

	// One half is not a codec, and it is not nothing either. Generating the
	// pair would redeclare the half that is there, in a file the author cannot
	// edit; generating neither would leave the type written by a marshaler
	// nobody wrote a reader for. Saying so is the only answer that leaves them
	// somewhere to go.
	if half := p.halfCodec(out.typ); half != "" {
		p.diags.Add(plugin.New(codeHalfCodec, where.pos,
			"%s declares %s and not the other half", out.spelled.Text, half).
			WithHint("%s", halfHint))
		return true
	}

	// Then a text codec, for a type this layer would not write member by member
	// anyway. After the JSON codec, because a type that says how it goes into
	// JSON has answered the question this one is asking. Before the type is
	// taken apart, because a named integer with a text codec is still an
	// integer underneath and taking it apart would write the number — which is
	// the one thing the text codec was written to stop.
	//
	// A struct is left alone here. Its members are what this layer exists to
	// write, and a struct whose members it cannot read is offered the text
	// codec in [planner.fillStruct] instead, where the reason it cannot is
	// already in hand.
	if _, held := out.typ.Underlying().(*types.Struct); !held && p.declaresText(out.typ) {
		out.how, out.text = writtenText, p.textWriter(out.typ)
		return true
	}

	return false
}

// isDuration reports whether a type is time.Duration itself.
//
// Not whether it is an int64, and not whether it is named over one: a duration
// is one type, and the standard library recognises it by identity for the same
// reason this does — a count of nanoseconds is what a duration holds and not
// what every int64 means.
func isDuration(t types.Type) bool {
	named, ok := types.Unalias(t).(*types.Named)
	if !ok {
		return false
	}
	obj := named.Obj()
	return obj != nil && obj.Name() == "Duration" &&
		obj.Pkg() != nil && obj.Pkg().Path() == "time"
}

// isTime reports whether a type is time.Time itself.
//
// By identity for the reason [isDuration] is: what is known about the type is
// known about that one type, not about every struct that resembles it. A type
// defined over time.Time carries none of its methods and is decided the way
// any other struct is.
func isTime(t types.Type) bool {
	named, ok := types.Unalias(t).(*types.Named)
	if !ok {
		return false
	}
	obj := named.Obj()
	return obj != nil && obj.Name() == "Time" &&
		obj.Pkg() != nil && obj.Pkg().Path() == "time"
}

// named returns what a complaint about a type should call it: the field's name
// where the type was reached through one, and the type's own where the subject
// itself is what is wrong.
func named(where blamed, out *form) string {
	if where.name != "" {
		return where.name
	}
	return out.spelled.Text
}

// fill decides one form, having already recorded it.
func (p *planner) fill(out *form, where blamed) {
	// A duration is asked about before anything else, because the standard
	// library asks about it before anything else: time.Duration outranks every
	// method a type could carry, and it has no default representation at all.
	// encoding/json/v2 refuses rather than choose between a count of
	// nanoseconds and a string like "1h30m", and this layer wrote the count —
	// which is what v1 wrote, so a document written here and read by anything
	// current disagrees about what the number meant.
	//
	// By identity rather than by underlying kind: every int64 is not a
	// duration.
	if isDuration(out.typ) {
		p.diags.Add(plugin.New(codeNoRepresentation, where.pos,
			"%s is a time.Duration, which has no one JSON form", named(where, out)).
			WithHint("say which form the document carries: format:units for \"1h30m\", " +
				"or format:sec, format:milli, format:micro or format:nano for a number"))
		out.how = writtenInvalid
		return
	}

	// A time is asked about next, and by identity for the same reason. Asked
	// before [planner.owned] because owned would answer: time.Time declares
	// MarshalJSON, and the general rule sends every such type through the
	// standard library, which validates what a method nobody here has read
	// returns. This one has been read — its MarshalJSON is its AppendText in
	// quotes — so the value goes straight into the buffer, and reading keeps
	// the method's own verdicts by delegating the span to UnmarshalJSON.
	if isTime(out.typ) {
		out.how = writtenTime
		out.reads = unmarshalMethod
		return
	}

	if p.owned(out, where) {
		return
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
//
// A struct this layer cannot write member by member is offered its own text
// codec before it is refused. That is the answer for a type like netip.Addr,
// whose whole content is unexported and whose text form is the one everybody
// already reads it in. It is offered here rather than before the type is taken
// apart, because a struct whose members *can* be read is written from them:
// member by member is what this layer is for, and a local struct that also
// carries a text codec is one whose author has two answers for one question.
//
// Not time.Time, which carries the codec that came before this one and is
// refused for that reason rather than this one. Refused and not handed over: a
// refusal names the field and says to write fallback=stdlib above it, which is
// what hands that one value to the reflective encoder — and the encoder then
// reaches the same method everything else does.
func (p *planner) fillStruct(out *form, where blamed) {
	held, modelled := p.known[key(out.typ)]
	switch {
	case !modelled:
		// Reached through something the subject walk did not follow. Nothing
		// here knows its fields' tags or the options written above them, and
		// guessing at either produces a codec that disagrees with the one the
		// same struct gets elsewhere.
		if p.declaresText(out.typ) {
			out.how, out.text = writtenText, p.textWriter(out.typ)
			return
		}
		p.refuse(where, "a %s, a struct this codec was not given the fields of", out.spelled.Text)
		return

	case held.External:
		// Its unexported fields cannot be read from here at all, so a codec
		// written member by member would silently leave them out — and for a
		// type like time.Time, whose whole content is unexported, that is an
		// empty object rather than a timestamp.
		if p.declaresText(out.typ) {
			out.how, out.text = writtenText, p.textWriter(out.typ)
			return
		}
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

	// A struct with fields and nothing to write is refused rather than written
	// as an empty object, which is what the standard library does and for the
	// reason that matters here: {} read back into the same type gives the same
	// value, so a codec that wrote it would satisfy every round trip while
	// carrying none of the value. Nothing that tests a codec against itself
	// could ever see it.
	//
	// A struct with no fields at all is a different thing and is written: {} is
	// what it holds rather than what was lost. A field carrying a json tag is
	// one the author meant something by, so a struct with one of those has its
	// own complaint made elsewhere and is not made to answer this one.
	if len(out.members) == 0 && len(held.Fields) > 0 && !anyTagged(held) {
		p.diags.Add(plugin.New(codeNoMembers, where.pos,
			"%s has no members to write", named(where, out)).
			WithHint("export a field, or give one a json tag, so that there is " +
				"something for the document to carry"))
		out.how = writtenInvalid
	}
}

// anyTagged reports whether any of a struct's fields carries a json tag, which
// is what the standard library takes as the author having meant something by a
// struct that otherwise has nothing to write.
func anyTagged(held *plugin.Struct) bool {
	for _, field := range held.Fields {
		if _, ok := field.Tag(jsonKey); ok {
			return true
		}
	}
	return false
}

// fillMap decides a map, which JSON can carry only when its keys are strings.
func (p *planner) fillMap(out *form, under *types.Map, where blamed) {
	basic, ok := under.Key().Underlying().(*types.Basic)
	if !ok || basic.Info()&types.IsString == 0 {
		p.refuse(where, "a %s, keyed by something a JSON object member cannot be named by",
			out.spelled.Text)
		return
	}

	// A key that says how it is written says it about a value rather than about
	// a member name, so a map keyed by one is not written as this would write
	// it. A JSON codec writes a whole value, which is not a name at all. A text
	// codec writes something a name could be — and the members would come out
	// in the order the Go keys sort rather than the order the names do, which
	// is an order nothing in the document explains and one a reader comparing
	// two documents cannot rely on.
	//
	// Refused rather than sorted by the text, because sorting by it means
	// writing every name before choosing an order, and this layer writes a
	// member as it reaches it. The reflective encoder holds the whole object
	// and can afford to.
	if p.declaresCodec(under.Key()) || p.declaresText(under.Key()) {
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

	p.diags.Add(plugin.New(codeOpaqueField, where.pos, "%s", said).
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

// knownOptions are the options a json tag may carry, which is the set a
// misspelling is recognised against.
var knownOptions = []string{
	optionCase, optionEmbed, optionFormat,
	optionOmitEmpty, optionOmitZero, optionString,
}

// normalizedOption is the standard library's comparison for an option that
// might be a known one written some other way: lowercased, with underscores
// dropped.
func normalizedOption(name string) string {
	return strings.ToLower(strings.ReplaceAll(name, "_", ""))
}

// misspelledOption reports an option that is a known one in every respect
// except how it is written.
//
// An option nobody recognises at all is left alone, because the standard
// library leaves it alone: a tag may carry a word this grammar has no meaning
// for and still be a tag. What it may not carry is omitEmpty, because a reader
// seeing that word expects the member to be left out and both this codec and
// the standard library would write it.
func (p *planner) misspelledOption(field plugin.Field) (plugin.Diagnostic, bool) {
	tag, ok := field.Tag(jsonKey)
	if !ok {
		return plugin.Diagnostic{}, false
	}

	for _, option := range tag.Options {
		if option.Name == "" {
			continue
		}
		normalized := normalizedOption(option.Name)
		for _, known := range knownOptions {
			if option.Name == known || normalized != known {
				continue
			}
			return plugin.New(codeTagOption, field.Pos,
				"%s is written with %q, which is %q spelled another way",
				field.Name, option.Name, known).
				WithHint("the standard library reads this option only as %q; "+
					"write it that way, or remove it", known), true
		}
	}

	return plugin.Diagnostic{}, false
}

// repeatedOption reports an option written more than once.
//
// Which occurrence a lookup returns is a lookup's question and internal/tags
// answers it by taking the first, because that is the only answer a lookup can
// give. Whether the tag was allowed to say it twice is a different question and
// this is where it is asked: a tag carrying two values for one option describes
// two wire formats, and choosing between them is not a generator's to make
// quietly.
func (p *planner) repeatedOption(field plugin.Field) (plugin.Diagnostic, bool) {
	tag, ok := field.Tag(jsonKey)
	if !ok {
		return plugin.Diagnostic{}, false
	}

	for _, known := range knownOptions {
		if tag.Count(known) < 2 {
			continue
		}
		return plugin.New(codeTagOption, field.Pos,
			"%s writes %q more than once", field.Name, known).
			WithHint("an option may be written once; keep the one that says " +
				"what the wire format is and remove the other"), true
	}

	return plugin.Diagnostic{}, false
}

// impureEmbed reports an embed written with a name or with company.
//
// embed says the field's members are the enclosing struct's members. A name
// would be the name of an object that is never written, and an option deciding
// whether to write that object decides about the same nothing. The standard
// library refuses both rather than picking one to ignore, and so does this.
func (p *planner) impureEmbed(field plugin.Field) (plugin.Diagnostic, bool) {
	tag, ok := field.Tag(jsonKey)
	if !ok || !tag.Has(optionEmbed) {
		return plugin.Diagnostic{}, false
	}

	switch {
	case tag.Name != "":
		return plugin.New(codeTagOption, field.Pos,
			"%s is embedded under the name %q, which nothing is written under",
			field.Name, tag.Name).
			WithHint("%s", impureEmbedHint), true
	case len(tag.Options) > 1:
		return plugin.New(codeTagOption, field.Pos,
			"%s is embedded and asked for %q as well", field.Name, tag.Raw).
			WithHint("%s", impureEmbedHint), true
	}

	return plugin.Diagnostic{}, false
}

const impureEmbedHint = "embed promotes a field's members into the enclosing object, " +
	`so it is written on its own: json:",embed"`

// taggedUnexported reports a tag on a field generated code cannot read.
//
// The field is left out either way, so the tag describes a member that will
// never be written. Left alone it reads as an instruction that was followed,
// which is worse than an error: the author's own source says the member is
// there. json:"-" is the one tag that stays legal on one, because it asks for
// exactly what happens.
func (p *planner) taggedUnexported(field plugin.Field) (plugin.Diagnostic, bool) {
	if field.Exported {
		return plugin.Diagnostic{}, false
	}

	tag, ok := field.Tag(jsonKey)
	if !ok || tag.Ignored {
		return plugin.Diagnostic{}, false
	}

	return plugin.New(codeTaggedUnexported, field.Pos,
		"%s is unexported and carries %s, which asks for a member nothing writes",
		field.Name, tag.String()).
		WithHint("generated code cannot read an unexported field from outside its " +
			`package; export the field, or write json:"-" to say it is left out on purpose`), true
}

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
func (p *planner) flatten(held *plugin.Struct, prefix string, guards []guard, walked map[string]bool) []member {
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
func (p *planner) member(field plugin.Field, prefix string, guards []guard, walked map[string]bool) []member {
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
func (p *planner) promotes(field plugin.Field) bool {
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
func (p *planner) promoted(field plugin.Field, path string, guards []guard, walked map[string]bool) []member {
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
func (p *planner) fieldForm(field plugin.Field) *form {
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
func (p *planner) fallback(field plugin.Field) bool {
	found := false

	for _, directive := range plugin.Written(field.Directives, markerName) {
		key, value, split := strings.Cut(directive.Args, "=")
		switch {
		case strings.TrimSpace(key) != optionFallback:
			p.diags.Add(plugin.New(codeTagOption, directive.Pos,
				"%s is not an option this layer takes on a field", strings.TrimSpace(key)).
				WithHint("a field takes %s=%s and nothing else", optionFallback, fallbackValue))
		case !split || strings.TrimSpace(value) != fallbackValue:
			p.diags.Add(plugin.New(codeTagOption, directive.Pos,
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
func (p *planner) unsupported(field plugin.Field) (plugin.Diagnostic, bool) {
	// A tag that is wrong about itself is reported before a tag that asks for
	// something this layer does not generate. The order matters for one case
	// in particular: case:ignore written beside case:strict is a contradiction
	// rather than a request for loose matching, and reporting it as the latter
	// would tell an author to remove an option they wrote twice on purpose.
	for _, check := range []func(plugin.Field) (plugin.Diagnostic, bool){
		p.taggedUnexported,
		p.misspelledOption,
		p.repeatedOption,
		p.impureEmbed,
	} {
		if wrong, found := check(field); found {
			return wrong, true
		}
	}

	if option, ok := tagOption(field, optionFormat); ok {
		return plugin.New(codeTagOption, field.Pos,
			"%s is written with %s, which this Go release does not support", field.Name, option.Raw).
			WithHint("%s", formatHint), true
	}

	if option, ok := tagOption(field, optionCase); ok {
		return plugin.New(codeTagOption, field.Pos,
			"%s is written with %s, which decides how a name is matched when reading", field.Name, option.Raw).
			WithHint("%s", caseHint), true
	}

	if option, ok := tagOption(field, optionString); ok {
		return plugin.New(codeTagOption, field.Pos,
			"%s is tagged %q, which puts its number inside a JSON string", field.Name, ","+option.Raw).
			WithHint("%s", quotedHint), true
	}

	return plugin.Diagnostic{}, false
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
func has(field plugin.Field, name string) bool {
	tag, ok := field.Tag(jsonKey)
	return ok && tag.Has(name)
}

// tagged reports whether a field's name on the wire was written rather than
// derived, which is what breaks a tie between two fields at one depth.
func tagged(field plugin.Field) bool {
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
			p.diags.Add(plugin.New(codeNameCollides, one.field.Pos,
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
