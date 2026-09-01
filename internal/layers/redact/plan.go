package redact

import (
	"go/token"
	"go/types"

	"github.com/okian/forge/internal/diag"
	"github.com/okian/forge/internal/model"
	"github.com/okian/forge/internal/tags"
)

// What can be wrong with a subject asked to say how it may be logged.
//
// Both are refusals rather than omissions, for the reason the package doc
// gives: a value redacted in the places somebody looked and printed in the
// places they did not reads as a safe one, and that is worse than no method at
// all.
var (
	codeNothingHidden = diag.Register(2025, "redaction was asked for and nothing is marked secret")
	codeUnmaskable    = diag.Register(2026, "a secret sits behind something a log value cannot mask")
)

// tag is what marks a field as one that does not belong in a log.
//
// The same key the log value earned from a tag alone reads, because it is the
// same question asked of the same field. A key of this layer's own would be a
// second way to say one thing, and the two would disagree the first time
// somebody wrote only one of them.
const tag = "redact"

// hidden reports whether a field asked to be kept out of logs.
//
// A tag written as "-" is an author opting out, which is what the ignore form
// means everywhere else a tag is read and what the emitters that write the log
// value a tag earns on its own already honour. Reading it as a request would
// mask a field somebody said to leave alone — and, worse, would refuse
// generation over one that could not be masked and did not need to be.
func hidden(field model.Field) bool {
	held, carries := field.Tag(tag)
	return carries && !held.Ignored
}

// hiddenBy answers the same question of a struct tag the model has not parsed.
//
// Only a struct written in place needs it. Every named struct the subject
// reaches is modelled, tags and all, and read through [hidden]; an anonymous
// one is not modelled at all, so this is where its tag is read or nowhere.
func hiddenBy(held string) bool {
	// The problems are dropped rather than reported, because a tag this cannot
	// read is a tag whose field is refused anyway: an in-place struct is never
	// maskable, so nothing turns on which of its keys parsed.
	found, _ := tags.Parse(held)

	for _, one := range found {
		if one.Key == tag {
			return !one.Ignored
		}
	}
	return false
}

// logged is one field and how the value prints it.
type logged struct {
	// field is the field itself, which is what a diagnostic points at.
	field model.Field

	// spelled is how its type is written in the file being generated into.
	spelled model.Spelling

	// masked records that the field's own tag asked for it. Nothing below a
	// masked field is looked at, because nothing below it is printed.
	masked bool

	// guarded records that the field is a pointer to something this run writes
	// a log value for, so the value has to be worked out before it is handed
	// over rather than in the call.
	//
	// A log value is written with a value receiver, because a pointer receiver
	// would leave a plain value of the type not implementing the interface at
	// all — which is the leak the whole layer exists to prevent, since slog
	// would print such a value's fields. What that costs is a nil pointer: slog
	// resolves one by calling the method on it, and a value method reached
	// through nothing panics. slog recovers it and logs the stack trace in
	// place of the field, which is a worse thing to find in a log than the
	// value would have been.
	guarded bool
}

// plan is one type's whole log value.
type plan struct {
	// of is the struct being written for, and spelled how it is written in the
	// file being generated into.
	of      *model.Struct
	spelled model.Spelling

	// fields are the ones the value prints, in declaration order.
	//
	// Unexported ones are not among them, and leaving them out is what the
	// method is for as much as masking is: a handler that cannot resolve a
	// value formats it with %+v, which prints them, so a type with no log value
	// prints more than a caller of its package can read. One with a log value
	// prints what its author chose to offer.
	fields []logged
}

// planner works out which of the types a subject reaches have anything to hide.
type planner struct {
	// into is the package being generated into, which decides how a type is
	// spelled, and bound what the package's files will bind.
	into  string
	bound []model.Import

	// at is where the declaration was written, which is where a refusal about
	// the subject as a whole points.
	at token.Position

	// known holds every struct the subject reaches, by the identity that tells
	// two apart, so that a field whose type is one of them can be looked up.
	known map[string]*model.Struct

	// hides records which of them reach a secret, worked out before any of them
	// is planned. It has to be, because whether a field is printed as itself
	// depends on whether its own type will be given a method.
	hides map[string]bool

	// plans holds what has been worked out, and order the identities in the
	// order the subject reaches them, which is the order they are planned in
	// rather than the order they are written in: what a file holds is sorted by
	// the key each contribution is made under.
	plans map[string]*plan
	order []string

	diags diag.Set
}

// key identifies a type across the whole plan, by the identity that keeps two
// types of one name in two packages apart.
func key(t types.Type) string { return model.TypeIdentity(t) }

// plan works out what the subject and everything it reaches need.
//
// Three passes, and they cannot be folded into one. What a struct is written
// depends on which of the structs it holds will be written, which depends on
// what they hold — so everything is gathered, then settled, then planned.
func (p *planner) plan(held *model.Struct) {
	p.gather(held)
	p.settle()
	p.write(held)
}

// gather records every struct the subject reaches, the subject included.
func (p *planner) gather(held *model.Struct) {
	p.known = make(map[string]*model.Struct, len(held.Closure)+1)
	p.hides = make(map[string]bool, len(held.Closure)+1)
	p.plans = make(map[string]*plan)

	p.known[key(held.Type())] = held
	for _, reached := range held.Closure {
		p.known[key(reached.Type())] = reached
	}
}

// settle works out which of the known structs reach a secret.
//
// To a fixed point rather than in one walk. What a struct reaches is decided by
// what the structs it holds reach, so one walk answers correctly only in the
// order the answers happen to be needed — and there is no such order for a
// struct that reaches itself, which is an ordinary thing for an author to
// write. Each round can only turn a false into a true and there are finitely
// many, so it ends, and what it ends at does not depend on the order the map
// was walked in.
//
// A struct whose author wrote the method is not among them, whatever its tags
// say. Theirs is what a handler will call, so there is nothing to write for it
// and nothing above it needs writing for on its account.
func (p *planner) settle() {
	for ref, held := range p.known {
		p.hides[ref] = !held.HasMethod(method) && tagged(held)
	}

	for changed := true; changed; {
		changed = false

		for ref, held := range p.known {
			if !p.hides[ref] && !held.HasMethod(method) && p.holds(held) {
				p.hides[ref], changed = true, true
			}
		}
	}
}

// tagged reports whether a struct has a secret of its own.
//
// An unexported field counts, where it is not printed. A handler formats a
// value it cannot resolve with %+v, which prints unexported fields — so a
// secret in one is printed by a type with no log value, and a type given one
// leaves it out along with every other field slog could not have read. Tagging
// an unexported field is therefore worth something, and what it is worth is the
// method existing at all.
func tagged(held *model.Struct) bool {
	for _, field := range held.Fields {
		if hidden(field) {
			return true
		}
	}
	return false
}

// holds reports whether a struct has a field reaching one that hides, against
// what has been settled so far.
func (p *planner) holds(held *model.Struct) bool {
	for _, field := range held.Fields {
		// A masked field hides whatever is under it, so what is under it is not
		// a reason for this struct to be written.
		//
		// An unexported one is not skipped, for the reason [tagged] gives about
		// its own: a handler formats a value it cannot resolve with %+v, which
		// reaches unexported fields and everything under them. A struct whose
		// only route to a secret runs through one still needs a method, and
		// what the method does about the field is leave it out.
		if hidden(field) {
			continue
		}
		if _, through := p.beneath(field.Type.Type); through {
			return true
		}
	}
	return false
}

// beneath reports whether a type reaches a struct with something to hide, and
// whether a log value can do anything about it.
//
// The second answer is the one that matters. slog resolves a LogValuer for the
// value of an attribute and for what a pointer points at, and stops there: it
// does not walk into a slice of them and ask each element. So a secret behind a
// slice, an array or a map is one this cannot mask, and the walk says which of
// the two routes it took to find it.
//
// The type-checker's own graph rather than the model's classification of it. A
// classification says a field is a named type and stops, which is the right
// depth for almost everything and one short here: a field typed IDs, where IDs
// is declared as a slice of something secret, is a named type whose element the
// classification does not carry — and it is exactly a route a secret can be
// reached along.
func (p *planner) beneath(t types.Type) (maskable, found bool) {
	return p.route(t, false, make(map[string]bool))
}

// named answers for a defined type, which is the only kind of type a log value
// can be attached to and so the only one where the walk can stop happily.
func (p *planner) named(held *types.Named, through bool, seen map[string]bool) (maskable, found bool) {
	ref := key(held)
	if seen[ref] {
		return false, false
	}
	seen[ref] = true

	known, modelled := p.known[ref]

	// A method the author wrote is the answer to what the value prints, and is
	// not an answer to whether it can be reached. Whatever theirs prints is what
	// a handler sees where slog resolves it, so nothing here has to be written
	// for the type — but slog only asks where it would have asked for one of
	// this layer's, so behind a collection theirs goes unconsulted exactly as
	// one written here would. Found, and maskable on the same terms.
	if modelled && known.HasMethod(method) {
		return !through, true
	}

	if p.hides[ref] {
		// Maskable only where a method can be put on it. A type this package
		// cannot declare on — one from another package, one that is an
		// instantiation of a generic — hides a secret nothing here can reach,
		// which is the same answer a slice gives arrived at from the other
		// side.
		return !through && modelled && known.Attachable(p.into), true
	}

	// A struct this run has a model for was settled already, and what it
	// reaches was settled with it. Walking into it again would answer the same
	// question a second way, which is a second answer waiting to disagree with
	// the first.
	if modelled {
		return false, false
	}

	// One it has no model for is walked into as whatever it is declared as,
	// which is how a named slice of something secret is reached at all.
	return p.route(held.Underlying(), through, seen)
}

// route walks a type looking for a struct that hides, remembering whether it
// has passed through something slog will not resolve through.
func (p *planner) route(t types.Type, through bool, seen map[string]bool) (maskable, found bool) {
	switch held := types.Unalias(t).(type) {
	case *types.Named:
		return p.named(held, through, seen)

	case *types.Pointer:
		// slog resolves a pointer like the value it points at, so passing
		// through one changes nothing about what can be masked.
		return p.route(held.Elem(), through, seen)

	case *types.Slice:
		return p.route(held.Elem(), true, seen)

	case *types.Array:
		return p.route(held.Elem(), true, seen)

	case *types.Map:
		if _, is := p.route(held.Key(), true, seen); is {
			return false, true
		}
		return p.route(held.Elem(), true, seen)

	case *types.Struct:
		// Written in place, so it has no name — and a type with no name cannot
		// be given a method. Whatever is found under one is found behind
		// something no log value can be attached to, so it is reported the same
		// way a secret behind a slice is: never maskable, whatever the route in
		// was.
		//
		// Its own tags are read here rather than only its field types, because
		// this is the only place they are read at all. Everything else settles
		// a struct under its name and looks the answer up; one written in place
		// is never settled, so a tag inside it would otherwise be a secret
		// nothing ever saw.
		for i := range held.NumFields() {
			if hiddenBy(held.Tag(i)) {
				return false, true
			}
			if _, is := p.route(held.Field(i).Type(), true, seen); is {
				return false, true
			}
		}
		return false, false

	default:
		return false, false
	}
}

// write plans each struct that has something to hide, and reports the subject
// that has nothing.
func (p *planner) write(held *model.Struct) {
	// Walked in the order the subject reaches them rather than over the map, so
	// that what is planned does not depend on the order a map was walked in.
	for _, one := range append([]*model.Struct{held}, held.Closure...) {
		ref := key(one.Type())
		if !p.hides[ref] || p.plans[ref] != nil {
			continue
		}

		// A type with nowhere to put a method is passed over rather than
		// written for, and passed over in silence.
		//
		// It arrives here at all because it has something to hide, and it is
		// reached by routes the walk does not take: a masked field stops the
		// walk, since nothing below one is printed, and an unexported one is
		// not printed either. Both of those are the answer rather than the
		// problem — the field replaces the whole of what it holds, or leaves it
		// out — so there is nothing for this type's method to have done, and
		// refusing would turn a safe program into an error whose remedy the
		// author has already taken.
		//
		// Every route that does print one is refused where it is printed, by
		// [planner.refuse], which is the report that names a field somebody can
		// act on. The subject is the exception and is reported below: nothing
		// points at it, so nothing else would say anything at all.
		if !one.Attachable(p.into) {
			continue
		}

		p.plans[ref] = p.planned(one)
		p.order = append(p.order, ref)
	}

	// The subject with nowhere to put a method, which nothing else reports. A
	// field reaching one is refused at the field, and the subject is what no
	// field points at — so without this it falls through to the complaint below
	// and is told that nothing it reaches is tagged, which is false.
	if !held.Attachable(p.into) && p.hides[key(held.Type())] {
		p.unattachable(held)
		return
	}

	// A subject whose author wrote the method is one this had nothing to do for,
	// which is not the same as one there was nothing to do about. It is the
	// override every closure layer offers, silent for the reason theirs are
	// silent: somebody who wrote the method meant to, and a complaint about it
	// would be a complaint about doing the thing the design invites.
	if held.HasMethod(method) {
		return
	}

	// A declaration that asked for redaction and reaches nothing marked secret
	// asked for a method that says exactly what slog says without it. Refused
	// rather than written, because a layer that quietly did nothing would leave
	// an author believing a value is protected.
	if len(p.order) == 0 {
		p.diags.Add(diag.New(codeNothingHidden, p.at,
			"%s asks for redaction and nothing it reaches carries a %s tag",
			held.Ref().Name, tag).
			WithHint("%s", "mark the fields that must not be logged, as in "+
				"Token string `"+tag+`:""`+"`; a value with nothing to hide already logs "+
				"correctly without this layer"))
	}
}

// planned works out one struct's log value.
func (p *planner) planned(held *model.Struct) *plan {
	out := &plan{of: held, spelled: model.Spell(held.Type(), p.into, p.bound)}

	for _, field := range held.Fields {
		if !field.Exported {
			continue
		}

		one := logged{
			field:   field,
			spelled: model.Spell(field.Type.Type, p.into, p.bound),
			masked:  hidden(field),
		}

		if !one.masked {
			one.guarded = p.pointsAt(field.Type.Type)
			p.refuse(held, field)
		}

		out.fields = append(out.fields, one)
	}

	return out
}

// pointsAt reports whether a field is a pointer to something this run writes a
// log value for.
//
// A pointer and nothing else. Everything a log value can be reached through is
// either handed over as itself — where nil is the type's own zero and nothing
// is called on it — or is behind a collection, which is refused. Only a pointer
// gets as far as a method call that may have nothing to call it on.
func (p *planner) pointsAt(t types.Type) bool {
	held, indirect := types.Unalias(t).(*types.Pointer)
	if !indirect {
		return false
	}

	named, is := types.Unalias(held.Elem()).(*types.Named)
	if !is {
		return false
	}
	return p.hides[key(named)]
}

// refuse reports a field whose secret this cannot reach.
//
// The one thing that must not be written silently. A field holding a slice of
// something with a secret in it is printed by the handler as the struct it is,
// LogValue and all — so a value that redacted everything else and left that
// alone would be a value redacted where the author looked and open where they
// did not.
func (p *planner) refuse(held *model.Struct, field model.Field) {
	maskable, found := p.beneath(field.Type.Type)
	if !found || maskable {
		return
	}

	at := field.Pos
	if at.Filename == "" {
		at = p.at
	}

	p.diags.Add(diag.New(codeUnmaskable, at,
		"%s.%s holds a secret %s", held.Ref().Name, field.Name, p.why(field.Type.Type)).
		WithHint("%s", maskHint))
}

// why says which of the ways a secret got out of reach this field took.
//
// Three reasons, and a message that gave one for all of them would be plainly
// untrue most of the time. A secret behind a collection has a log value slog
// will not ask for; one in a type from another package or in an instantiation
// of a generic has none to ask for, and for different reasons; one inside a
// struct written in place has nowhere for a method to go at all. Telling an
// author that slog does not walk into collections, about a field holding no
// collection, sends them looking for one that is not there.
func (p *planner) why(t types.Type) string {
	if held := p.nowhere(t, make(map[string]bool)); held != "" {
		return held
	}
	return "behind a slice, an array or a map, which a log value cannot mask"
}

// nowhere says where a secret is held that cannot carry a method, or nothing
// where the route out of reach was a collection instead.
func (p *planner) nowhere(t types.Type, seen map[string]bool) string {
	switch held := types.Unalias(t).(type) {
	case *types.Named:
		ref := key(held)
		if seen[ref] {
			return ""
		}
		seen[ref] = true

		known, modelled := p.known[ref]
		if p.hides[ref] && modelled && !known.Attachable(p.into) {
			return "in " + model.TypeString(known.Type()) + ", and " + model.Unattachable(known, p.into)
		}
		if modelled {
			return ""
		}
		return p.nowhere(held.Underlying(), seen)

	case *types.Pointer:
		return p.nowhere(held.Elem(), seen)

	case *types.Struct:
		// Written in place, so there is no model to name and no name to put a
		// method on. Said here rather than through [model.Unattachable], which
		// answers for a type that has a name and is only in the wrong place.
		return "in a struct written in place, which has no name to declare a method on"

	default:
		return ""
	}
}

// maskHint says what to do about a secret this cannot reach, which is the same
// thing whichever way it got out of reach: say so at the field.
const maskHint = "slog resolves a log value for an attribute and for what a pointer points " +
	"at, and asks nothing of a type that cannot carry one — so tag this field itself, " +
	"which replaces the whole of it and is what marking its contents secret meant"

// unattachable reports the subject when it has something to hide and nowhere to
// put the method that would hide it.
//
// The subject alone, because it is the only such type nothing else speaks for.
// A field reaching one is refused where the field is, which is a report naming
// something an author can change; a closure member nothing prints is passed
// over, since the field that would have printed it has already dealt with it.
// The subject is what no field points at, so without this it falls through to
// the complaint about nothing being tagged — which is false, and sends its
// reader to add a tag that is already there.
func (p *planner) unattachable(held *model.Struct) {
	// The declaration rather than the type, where the type is somebody else's.
	// A caret in a module cache is one nothing can be done at, and the reader
	// is the author of the declaration either way — so a position they can open
	// beats one that is nearer the fault and out of reach.
	at := held.Pos
	if held.External || at.Filename == "" {
		at = p.at
	}

	p.diags.Add(diag.New(codeUnmaskable, at,
		"%s has something to hide and %s", model.TypeString(held.Type()),
		model.Unattachable(held, p.into)).
		WithHint("%s", moving(held)))
}

// moving says what an author can do about a subject with nowhere to put a
// method, which depends on whose type it is.
//
// Moving the declaration is the answer for a type this module owns and no
// answer at all for one in a dependency, so it is offered only where it can be
// taken. What is left in the other case is honest and short: the subject cannot
// be redacted where it is, and something the author does own has to stand in
// front of it.
func moving(held *model.Struct) string {
	const opening = "a log value has to be a method, and there is nowhere to put one — "

	if held.External {
		return opening + "this type belongs to a dependency, so declare the stack over one of " +
			"your own that holds it and tag the field"
	}
	return opening + "declare the stack in the package that declares the type, or over one of " +
		"your own that holds it with the field tagged"
}

// written returns the plans in the order the subject reaches them.
func (p *planner) written() []*plan {
	out := make([]*plan, 0, len(p.order))
	for _, ref := range p.order {
		out = append(out, p.plans[ref])
	}
	return out
}
