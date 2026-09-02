package contenthash

import (
	"fmt"
	"go/token"
	"go/types"
	"slices"
	"strings"

	"github.com/okian/forge/plugin"
)

// codeOpaqueField reports a value nothing can hash by its content.
//
// A 2xxx, because it is about the subject: the field's type is what cannot be
// hashed, and the author is the one who can say what should happen instead.
var codeOpaqueField = plugin.Register(2017, "field type cannot be hashed by its content")

// codeNotAHash reports a type that declares something called Hash which is not
// a hash of itself.
//
// Generating one would redeclare it, in a file the author cannot edit; not
// generating it would leave the type without the method every other type in the
// closure has, and the call sites would not compile either. Saying so is the
// only answer that leaves them somewhere to go.
var codeNotAHash = plugin.Register(2018, "a type declares a Hash that is not a content hash")

// codeHashOption reports a directive above a field that is not the one option a
// field takes.
//
// A 3xxx, because it is about something somebody wrote in a directive rather
// than about the type it was written above.
var codeHashOption = plugin.Register(3024, "hash directive on a field is not one")

// notAHashHint says what to do about a Hash that is not one.
const notAHashHint = "rename the method, or give it the signature a content hash has — " +
	"no arguments, and a uint64 as its result — and it will be called rather than written"

// subjectHint says what to do about a subject whose content this package cannot
// read, which is not the same advice a field gets: there is no field to write a
// directive above, and leaving the whole subject out of its own hash is not
// something anybody could mean.
const subjectHint = "a hash covers the whole of a value, and the unexported fields of a type " +
	"belong to the package that declares it — declare this one in the package being generated into, " +
	"or give the type a Hash of its own"

// ignoreHint says what to write above a field nothing can hash.
const ignoreHint = "write //forge:hash ignore above the field to leave it out of the hash, " +
	"which says at the field what its absence from the hash would say nowhere else"

// The method the subject carries and the prefix its call-through takes.
const (
	method = "Hash"
	verb   = "hash"
)

// optionIgnore leaves a field out of the hash, and is the only thing a field
// may say to this layer.
const optionIgnore = "ignore"

// how one value is mixed into a hash.
type how uint8

const (
	// howInvalid is the zero value: nothing has been decided.
	howInvalid how = iota

	// howWhole is any whole number — signed, unsigned, a rune, a byte — mixed
	// as the sixty-four bits its value occupies. A signed number converts two's
	// complement, which loses nothing, so no two of them arrive as one.
	howWhole

	// howBool, howString and howFloat are the rest of what the language gives a
	// value nothing has to be taken apart to reach.
	howBool
	howString
	howFloat

	// howComplex is a pair of floating-point numbers, mixed in that order.
	howComplex

	// howMethod is a value that hashes itself, which is either a type this run
	// writes a method for or one whose author wrote Hash.
	howMethod

	// howThrough is a type this run writes for and cannot put a method on,
	// because it is declared in another package. Its hash is a function in the
	// package being generated into, and the call names that instead.
	howThrough

	// The shapes that have to be walked into.
	howPointer
	howSlice
	howArray
	howMap

	// howStruct is a struct written in place, which has no name to hang a
	// method on and so is taken apart wherever it appears.
	howStruct

	// howOpaque is a value nothing can hash by its content, which is refused
	// rather than left out behind the author's back.
	howOpaque
)

// form is one type and how a hash of it is taken.
type form struct {
	// typ is the type itself, and spelled is how it is written in the file
	// being generated into.
	typ     types.Type
	spelled plugin.Spelling

	// how says which shape the hash takes.
	how how

	// call names the function a howThrough hash goes through, and is empty for
	// every other shape.
	call string

	// refusal is what was wrong with a howOpaque type, kept so that the second
	// field of that type is told the same thing as the first.
	//
	// A form is decided once and looked up thereafter, which is what stops a
	// type reached twice being worked out twice — and would, without this,
	// leave every field after the first of an unhashable type unreported. Being
	// told about one of three channels is being told a third of the story per
	// run.
	refusal string

	// elem is what a pointer, a slice, an array or a map holds, and key what a
	// map is keyed by. Both are nil for every other shape.
	elem *form
	key  *form

	// members are the members of a struct written in place, in declaration
	// order, and are empty for every other shape.
	members []member
}

// member is one member of a struct written in place.
type member struct {
	name string
	of   *form
}

// hashed is one field and how it is mixed in.
type hashed struct {
	// field is the field itself, which is what a diagnostic points at.
	field plugin.Field

	// path reaches the field from the value being hashed.
	path string

	// of is how the field's own value is hashed.
	of *form
}

// plan is one type's whole hash.
type plan struct {
	// of is the type being hashed, and spelled how it is written in the file
	// being generated into.
	of      *plugin.Struct
	spelled plugin.Spelling

	// fields are the ones mixed in, in declaration order. It is empty for a
	// type whose underlying type is not a struct, which has value instead.
	fields []hashed

	// value is how a name over something that is not a struct hashes: the
	// value itself, taken apart as whatever the name is a name for.
	//
	// Only ever the subject. The reachable set holds named structs and nothing
	// else, because a name over anything else is looked through where it is
	// used — but a declaration may name type Age int as its subject, and a
	// number is a thing with a hash.
	value *form

	// attach records that the type may carry the method, which is true only for
	// a type the package being generated into declares. why says what stops it
	// where it does not, for the comment the function is written with.
	attach bool
	why    string
}

// planner decides what a subject's hash is made of.
type planner struct {
	// into is the package being generated into, which decides both what a
	// method may be declared on and what an unexported field may be read from.
	into string

	// bound is what the file will bind, which every spelling is written
	// against so that a type from a package called math is not written under
	// the name the arithmetic's own import has.
	bound []plugin.Import

	// known holds every struct the subject reaches, by the identity that tells
	// two apart, so that a field whose type is one of them is hashed by calling
	// its own hash rather than by inlining what it holds. It is also what makes
	// the generator terminate: a type that reaches itself produces a method
	// that calls itself.
	known map[string]*plugin.Struct

	// plans holds what has been worked out, and order the identities in the
	// order they were reached, which is the order the methods are written.
	plans map[string]*plan
	order []string

	// excluded holds the types this run will not write a hash for because it
	// cannot read the whole of them, so that a field reaching one is refused
	// rather than sent to a method that was never written.
	excluded map[string]bool

	// forms memoises how a type is hashed, so that a type reached twice is
	// decided once.
	forms map[string]*form

	// filling holds the types being decided right now, which is what says a
	// type has reached itself rather than merely been reached twice. It is
	// emptied of nothing on the way into a struct, because a struct's hash is a
	// function and a call is where a cycle stops being infinite.
	filling map[string]bool

	diags plugin.Diagnostics
}

// key identifies a type across the whole plan, by the spelling that keeps two
// types of one name in two packages apart.
func key(t types.Type) string { return plugin.TypeIdentity(t) }

// plan works out what the subject and everything it reaches need hashing.
func (p *planner) plan(held *plugin.Struct) {
	p.known = make(map[string]*plugin.Struct)
	p.plans = make(map[string]*plan)
	p.excluded = make(map[string]bool)
	p.forms = make(map[string]*form)
	p.filling = make(map[string]bool)

	p.remember(held)
	for _, reached := range held.Closure {
		p.remember(reached)
	}

	p.delegate()
	p.exclude()

	for _, ref := range p.order {
		if one, writing := p.plans[ref]; writing {
			p.fill(one)
		}
	}
}

// delegate drops the types whose author already wrote the hash, and reports the
// ones that wrote something else under the name.
//
// A hand-written hash is the author overriding what would otherwise be
// generated, which is the rule the copy and the codec follow and for the same
// reason: a type whose identity forge cannot see is hashed properly by the
// person who can see it. A field holding one of them is hashed by calling Hash
// either way, so nothing downstream has to know which.
func (p *planner) delegate() {
	for _, ref := range p.order {
		held := p.known[ref]
		if held == nil || !held.HasMethod(method) {
			continue
		}

		delete(p.plans, ref)

		if !declares(held.Type()) {
			p.diags.Add(plugin.New(codeNotAHash, held.Pos,
				"%s declares %s, which does not answer with a number", held.Ref().Name, method).
				WithHint("%s", notAHashHint))
		}
	}
}

// exclude drops the types this run cannot read the whole of.
//
// A struct declared in another package keeps its unexported fields to itself,
// and generated code here cannot name them. A hash written over what is left is
// a hash of part of the value, which is worse than no hash at all: it would
// call two values the same on the strength of the half it could see. So the
// type is left without one, and whatever reaches it is refused where it is
// used.
func (p *planner) exclude() {
	for i, ref := range p.order {
		held := p.known[ref]
		if _, writing := p.plans[ref]; !writing || !partial(held, p.into) {
			continue
		}

		delete(p.plans, ref)
		p.excluded[ref] = true

		// The subject, which is the one of these with no use to point at: what
		// asked for the hash is the declaration rather than a field, and
		// leaving the declaration with nothing generated and nothing said would
		// be the silence this layer refuses everywhere else.
		if i == 0 {
			p.diags.Add(plugin.New(codeOpaqueField, held.Pos,
				"%s has unexported fields the package being generated into cannot read", held.Ref().Name).
				WithHint("%s", subjectHint))
		}
	}
}

// partial reports whether a struct holds anything the package being generated
// into cannot read, anywhere beneath it.
//
// Anywhere, rather than among its own fields, because the answer decides where
// a refusal points. A struct whose own fields are all exported but which holds
// one that is not would be planned for, and the refusal would then land on a
// field inside it — a line in a file the author cannot edit, telling them to
// write a directive there. Deciding it here moves the complaint to the field
// that reached the type, which is theirs.
//
// A struct of the package being generated into is never partial: unexported or
// not, its fields can be named from where the hash is written.
func partial(held *plugin.Struct, into string) bool {
	if held == nil || held.Local(into) {
		return false
	}
	if opaque(held, into) {
		return true
	}

	// The named structs beneath it, which the subject walk already found. Each
	// of them is either local — and so readable — or another one of these.
	for _, reached := range held.Closure {
		if !reached.Local(into) && opaque(reached, into) {
			return true
		}
	}
	return false
}

// opaque reports whether a struct has a field the package being generated into
// cannot name, looking through the structs written in place inside it.
//
// A struct written in place has no name and so no plan of its own, which means
// nothing else will answer for it. A named one does, and is left to do so.
func opaque(held *plugin.Struct, into string) bool {
	return slices.ContainsFunc(held.Fields, func(one plugin.Field) bool {
		return !one.Exported || unnameable(one.Type.Type, into, make(map[types.Type]bool))
	})
}

// unnameable reports whether a type written in place holds a member the package
// being generated into cannot name.
//
// It stops at a name, because a named type is either this package's — in which
// case everything in it can be named — or one the walk found and [partial] asks
// about separately. What is left is the composites: a struct written in place,
// and whatever a pointer, a slice, an array or a map holds.
func unnameable(t types.Type, into string, seen map[types.Type]bool) bool {
	if t == nil || seen[t] {
		return false
	}
	seen[t] = true

	if _, named := types.Unalias(t).(*types.Named); named {
		return false
	}

	if under, is := t.Underlying().(*types.Struct); is {
		return keepsBack(under, into, seen)
	}
	for _, held := range holds(t.Underlying()) {
		if unnameable(held, into, seen) {
			return true
		}
	}
	return false
}

// keepsBack reports whether a struct written in place has a member the package
// being generated into cannot name, or holds something that does.
func keepsBack(under *types.Struct, into string, seen map[types.Type]bool) bool {
	for field := range under.Fields() {
		if !field.Exported() && (field.Pkg() == nil || field.Pkg().Path() != into) {
			return true
		}
		if unnameable(field.Type(), into, seen) {
			return true
		}
	}
	return false
}

// holds returns what a composite written in place is made of, and nothing for
// anything else.
//
// A map's key as well as its value, because either half may be a struct written
// in place. A channel and a function hold nothing worth walking: neither is a
// value a hash could be taken of, and both are refused where they are used.
func holds(under types.Type) []types.Type {
	switch typed := under.(type) {
	case *types.Pointer:
		return []types.Type{typed.Elem()}
	case *types.Slice:
		return []types.Type{typed.Elem()}
	case *types.Array:
		return []types.Type{typed.Elem()}
	case *types.Map:
		return []types.Type{typed.Key(), typed.Elem()}
	default:
		return nil
	}
}

// remember records a type and reserves its plan, in the order reached.
func (p *planner) remember(held *plugin.Struct) {
	if held == nil || held.Named == nil {
		return
	}

	ref := key(held.Type())
	if _, seen := p.plans[ref]; seen {
		return
	}

	p.known[ref] = held
	p.plans[ref] = &plan{
		of:      held,
		spelled: plugin.Spell(held.Type(), p.into, p.bound),
		attach:  held.Attachable(p.into),
		why:     plugin.Unattachable(held, p.into),
	}
	p.order = append(p.order, ref)
}

// fill works out one type's hash.
//
// A struct is its fields; anything else is itself, taken apart as whatever its
// name is a name for.
func (p *planner) fill(held *plan) {
	if _, is := held.of.Named.Underlying().(*types.Struct); !is {
		held.value = p.decide(held.of.Named.Underlying(), blamed{at: held.of.Pos, what: held.spelled.Text})
		return
	}

	for _, field := range held.of.Fields {
		if p.ignored(field) {
			continue
		}

		held.fields = append(held.fields, hashed{
			field: field,
			path:  field.Name,
			of:    p.decide(field.Type.Type, blamed{at: field.Pos, what: field.Name}),
		})
	}
}

// ignored reports whether the author asked for this field to be left out, and
// reports anything else written above it.
//
// The value is checked rather than the key alone. ignore is the only thing this
// layer takes on a field, so anything else is a misspelling — and a misspelling
// that quietly left a field in the hash would be the one mistake this option
// exists to make visible.
func (p *planner) ignored(field plugin.Field) bool {
	held := false

	for _, directive := range plugin.Written(field.Directives, verb) {
		if written := strings.TrimSpace(directive.Args); written != optionIgnore {
			p.diags.Add(plugin.New(codeHashOption, directive.Pos,
				"%s is not an option this layer takes on a field", written).
				WithHint("a field takes %s and nothing else", optionIgnore))
			continue
		}
		held = true
	}

	return held
}

// blamed is where a refusal about a type is reported, and what it names.
//
// A type is refused where it is used rather than where it is declared: a field
// whose type is an interface is the author's decision to hash that field, and
// the interface is usually somebody else's type and not the mistake.
type blamed struct {
	at   token.Position
	what string

	// of is the form being decided, where there is one, so that what a refusal
	// said can be kept with it and said again about the next field of that
	// type. It is nil where nothing is being decided yet.
	of *form
}

// decide returns how a type is hashed, deciding it once.
func (p *planner) decide(t types.Type, where blamed) *form {
	if t == nil {
		// A field the model could not classify, which nothing here can hash and
		// nothing else will report. Refused rather than passed over: this is the
		// one branch where failing open would leave a value silently out of its
		// own hash.
		out := &form{how: howOpaque}
		p.refuse(blamed{at: where.at, what: where.what, of: out}, "of a type this run could not resolve")
		return out
	}

	ref := key(t)
	if found, seen := p.forms[ref]; seen {
		switch {
		// Reached again while it was still being decided, which is a type that
		// contains itself with no struct in between. A struct is what makes a
		// cycle finite — its hash is a function, and a call ends where the
		// value does — so a chain of pointers, slices or maps that closes on
		// itself has no form to write at all: every attempt at one is the same
		// attempt again.
		case p.filling[ref]:
			p.refuse(where, "a %s, which contains itself with no struct in between", found.spelled.Text)

		// Decided already, and decided to be something nothing can hash. Said
		// again, because it is being said about a different field.
		case found.how == howOpaque:
			p.refuse(where, "%s", found.refusal)
		}
		return found
	}

	out := &form{typ: t, spelled: plugin.Spell(t, p.into, p.bound)}
	p.forms[ref] = out

	where.of = out

	p.filling[ref] = true
	p.fillForm(out, where)
	delete(p.filling, ref)

	return out
}

// fillForm decides one form, having already recorded it.
func (p *planner) fillForm(out *form, where blamed) {
	ref := key(out.typ)

	// Asked before the type is taken apart, because a type with a hash of its
	// own is hashed by calling it whatever it is made of — which is the whole
	// point of a hand-written hash, and what lets a type whose identity forge
	// cannot see be hashed properly.
	if held, reached := p.known[ref]; reached {
		p.fillReached(out, held, where)
		return
	}
	if declares(out.typ) {
		out.how = howMethod
		return
	}

	switch under := out.typ.Underlying().(type) {
	case *types.Basic:
		p.fillBasic(out, under, where)

	case *types.Pointer:
		out.how, out.elem = howPointer, p.decide(under.Elem(), where)

	case *types.Slice:
		out.how, out.elem = howSlice, p.decide(under.Elem(), where)

	case *types.Array:
		out.how, out.elem = howArray, p.decide(under.Elem(), where)

	case *types.Map:
		out.how = howMap
		out.key = p.decide(under.Key(), where)
		out.elem = p.decide(under.Elem(), where)

	case *types.Struct:
		p.fillStruct(out, under, where)

	default:
		// An interface, a channel or a function. None of them has content a
		// hash could be taken of: what an interface holds is decided at run
		// time, and the other two are references whose identity is where
		// something is rather than what it is.
		out.how = howOpaque
		p.refuse(where, "a %s, whose identity is not its content", out.spelled.Text)
	}
}

// fillReached decides a named struct the subject walk found, which is one of
// three things.
func (p *planner) fillReached(out *form, held *plugin.Struct, where blamed) {
	ref := key(out.typ)

	switch one, writing := p.plans[ref]; {
	case p.excluded[ref]:
		out.how = howOpaque
		p.refuse(where, "a %s, whose unexported fields the package being generated into cannot read",
			out.spelled.Text)

	case !writing:
		// Its author wrote the hash, or wrote something else under the name —
		// which delegate has already reported. Either way the call is a method
		// call, and either way this run writes nothing for the type.
		out.how = howMethod

	case !one.attach:
		out.how, out.call = howThrough, plugin.Through(held, verb, "", p.into)

	default:
		out.how = howMethod
	}
}

// fillBasic decides one of the types the language gives a value directly.
func (p *planner) fillBasic(out *form, under *types.Basic, where blamed) {
	// Asked before anything else, because a uintptr is an integer as far as the
	// type checker is concerned and would otherwise be hashed as a number. What
	// it holds is an address: two runs of one program give one value two of
	// them, and the garbage collector may give one value two within a run — so
	// a hash built on it is not stable across the next call, let alone the next
	// build. unsafe.Pointer is the same thing without the arithmetic.
	if kind := under.Kind(); kind == types.Uintptr || kind == types.UnsafePointer {
		out.how = howOpaque
		p.refuse(where, "of type %s, which holds where a value is rather than what it is", out.spelled.Text)
		return
	}

	switch info := under.Info(); {
	case info&types.IsBoolean != 0:
		out.how = howBool

	case info&types.IsString != 0:
		out.how = howString

	case info&types.IsInteger != 0:
		out.how = howWhole

	case info&types.IsFloat != 0:
		out.how = howFloat

	case info&types.IsComplex != 0:
		out.how = howComplex

	default:
		// Nothing reaches here: the kinds above are every basic type but the
		// two answered before them and the untyped constants, which no field
		// has. The arm is here because a switch that assumed otherwise would
		// leave a form undecided and write nothing for it.
		out.how = howOpaque
		p.refuse(where, "of type %s, which is not a value a hash can be taken of", out.spelled.Text)
	}
}

// fillStruct decides a struct written in place, which is taken apart wherever
// it appears.
//
// A named struct never reaches here: the subject walk found every one of them,
// so each is either a type this run writes for or one whose author wrote the
// hash, and both were answered above. What is left is a struct with no name,
// which has nowhere to hang a method.
func (p *planner) fillStruct(out *form, under *types.Struct, where blamed) {
	out.how = howStruct

	for field := range under.Fields() {
		// A member this package cannot name is a member a hash written here
		// would leave out, and a hash of part of a value is not a hash of it.
		if !field.Exported() && !p.readable(field) {
			out.how, out.members = howOpaque, nil
			p.refuse(where, "a %s, whose member %s the package being generated into cannot read",
				out.spelled.Text, field.Name())
			return
		}

		out.members = append(out.members, member{name: field.Name(), of: p.decide(field.Type(), where)})
	}
}

// readable reports whether generated code in the package being written may name
// a field, which for an unexported one is a question about where it was
// declared.
func (p *planner) readable(field *types.Var) bool {
	pkg := field.Pkg()
	return pkg == nil || pkg.Path() == p.into
}

// refuse records that a value cannot be hashed, against whatever asked for it,
// and remembers what was said so that the next field of that type hears it too.
func (p *planner) refuse(where blamed, format string, args ...any) {
	said := fmt.Sprintf(format, args...)

	if where.of != nil {
		where.of.refusal = said
	}

	p.diags.Add(plugin.New(codeOpaqueField, where.at, "%s is %s", where.what, said).
		WithHint("%s", ignoreHint))
}

// declares reports whether a type has a hash of its own, which is a method
// called Hash taking nothing and answering with a uint64.
//
// The signature and not only the name. A method called Hash that takes an
// argument or answers with something else is somebody's method that happens to
// share a name, and calling it would not compile.
func declares(t types.Type) bool {
	named, is := types.Unalias(t).(*types.Named)
	if !is {
		return false
	}

	for one := range named.Methods() {
		if one.Name() != method {
			continue
		}

		signature, ok := one.Type().(*types.Signature)
		if !ok || signature.Params().Len() != 0 || signature.Results().Len() != 1 {
			continue
		}
		if basic, is := signature.Results().At(0).Type().Underlying().(*types.Basic); is && basic.Kind() == types.Uint64 {
			return true
		}
	}
	return false
}

// written returns the plans this run emits, in the order they were reached.
func (p *planner) written() []*plan {
	out := make([]*plan, 0, len(p.plans))
	for _, ref := range p.order {
		if held, kept := p.plans[ref]; kept {
			out = append(out, held)
		}
	}
	return out
}
