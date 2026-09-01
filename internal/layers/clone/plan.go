package clone

import (
	"go/types"
	"strings"

	"github.com/okian/forge/internal/diag"
	"github.com/okian/forge/internal/model"
)

// codeOpaqueField reports a field nothing can copy without being told what to
// do with it.
//
// A 2xxx, because it is about the subject: the field's type is what cannot be
// copied, and the author is the one who can say what should happen instead.
var codeOpaqueField = diag.Register(2015, "field type cannot be copied without being told to share it")

// codeCloneOption reports a directive above a field that is not the one option
// a field takes.
//
// A 3xxx, because it is about something somebody wrote in a directive rather
// than about the type it was written above.
var codeCloneOption = diag.Register(3018, "clone directive on a field is not one")

// codeNotACopy reports a type that declares something called Clone which is not
// a copy of itself.
//
// Generating the copy would redeclare it, in a file the author cannot edit; not
// generating it would leave the type without the method every other type in the
// closure has, and the call sites would not compile either. Saying so is the
// only answer that leaves them somewhere to go.
var codeNotACopy = diag.Register(2016, "a type declares a Clone that is not a copy of itself")

// notACopyHint says what to do about one.
const notACopyHint = "rename the method, or give it the signature a copy has — " +
	"no arguments, and the type itself as its result — and it will be called rather than written"

// The method the subject carries and the prefix its call-through takes.
const (
	method = "Clone"
	verb   = "clone"
)

// The option deciding whether a reference is copied or carried across, and its
// two answers.
const (
	optionAliasing = "aliasing"

	aliasingCopy  = "copy"
	aliasingShare = "share"
)

// shareHint says what to write above a field nothing can copy.
const shareHint = "write //forge:clone aliasing=share above the field to carry it across as it is, " +
	"which says at the field what a copy of it would have meant"

// how one value is copied.
type how uint8

const (
	// howAssign is a value that copying is: a number, a string, an array of
	// them, a struct with nothing underneath it. Assignment is the copy.
	howAssign how = iota

	// howShare is a value carried across as it is, because the declaration or
	// the field asked for that.
	howShare

	// howMethod is a value that copies itself, which is either a struct this
	// run writes a method for or a type whose author wrote Clone.
	howMethod

	// howThrough is a struct this run writes for and cannot put a method on,
	// because it is declared in another package. Its copy is a function in the
	// package being generated into, and the call names that instead.
	howThrough

	// The three the language gives a shape to and generated code has to build
	// again.
	howPointer
	howSlice
	howMap
	howArray

	// howOpaque is a value nothing can copy, which is refused rather than
	// shared behind the author's back.
	howOpaque
)

// form is one type and how a copy of it is made.
type form struct {
	// typ is the type itself, and spelled is how it is written in the file
	// being generated into.
	typ     types.Type
	spelled model.Spelling

	// how says which shape the copy takes.
	how how

	// call names the function a howThrough copy goes through, and is empty for
	// every other shape.
	call string

	// elem is what a pointer, a slice, an array or a map holds, already
	// decided. key is a map's key, which is copied by being assigned — a map
	// key is comparable and so holds nothing a copy could share.
	elem *form
}

// copied is one field and how it is copied.
type copied struct {
	// field is the field itself, which is what a diagnostic points at.
	field model.Field

	// path reaches the field from the value being copied.
	path string

	// of is how the field's own value is copied.
	of *form
}

// plan is one type's whole copy.
type plan struct {
	// of is the struct being copied, and spelled how it is written in the file
	// being generated into.
	of      *model.Struct
	spelled model.Spelling

	// fields are the ones that need more than assignment, in declaration
	// order. A field copied by assignment is not here: the copy starts as the
	// original, so a field that needs nothing has already been done.
	fields []copied

	// attach records that the type may carry the method, which is true only
	// for a struct the package being generated into declares. why says what
	// stops it where it does not, for the comment the function is written with.
	attach bool
	why    string

	// carried records that a field was left to the assignment because generated
	// code here cannot name it, which is the one case where a copy shares
	// something with what it was copied from.
	carried bool
}

// planner decides what a subject's copy is made of.
type planner struct {
	// into is the package being generated into, which decides whether a struct
	// can carry a method.
	into string

	// bound is what the file will bind, which every spelling is written
	// against so that a type from a package called slices is not written under
	// the name a copy's own import has.
	bound []model.Import

	// sharing records that the declaration asked for references to be carried
	// across rather than copied.
	sharing bool

	// known holds every struct the subject reaches, by the identity that tells
	// two apart, so that a field whose type is one of them is copied by calling
	// its own method rather than by inlining what it holds. It is also what
	// makes the generator terminate: a type that reaches itself produces a
	// method that calls itself.
	known map[string]*model.Struct

	// plans holds what has been worked out, and order the identities in the
	// order they were reached, which is the order the methods are written.
	plans map[string]*plan
	order []string

	// forms memoises how a type is copied, so that a type reached twice is
	// decided once and one that reaches itself terminates.
	forms map[string]*form

	diags diag.Set
}

// key identifies a type across the whole plan, by the spelling that keeps two
// types of one name in two packages apart.
func key(t types.Type) string { return model.TypeIdentity(t) }

// plan works out what the subject and everything it reaches need copying.
func (p *planner) plan(held *model.Struct) {
	p.known = make(map[string]*model.Struct)
	p.plans = make(map[string]*plan)
	p.forms = make(map[string]*form)

	p.remember(held)
	for _, reached := range held.Closure {
		p.remember(reached)
	}

	p.delegate()

	for _, ref := range p.order {
		if one, writing := p.plans[ref]; writing {
			p.fill(one)
		}
	}

	p.spread()
}

// spread marks a plan as carrying something across wherever anything it holds
// does.
//
// A copy that calls another type's copy is as shallow as that copy is, and what
// the comment on it claims is about the whole value rather than about the
// fields this type happens to own. So the answer travels up: a struct holding
// one whose unexported fields could not be read shares them too, and says so.
//
// To a fixpoint, because the holder of a holder is in the same position and the
// plans are in the order they were reached rather than in dependency order.
func (p *planner) spread() {
	for moved := true; moved; {
		moved = false

		for _, ref := range p.order {
			held := p.plans[ref]
			if held == nil || held.carried {
				continue
			}

			for _, one := range held.fields {
				if p.shallow(one.of, make(map[*form]bool)) {
					held.carried, moved = true, true
					break
				}
			}
		}
	}
}

// shallow reports whether copying a value goes through a type that carries
// something across rather than copying it.
//
// A type whose author wrote the copy answers nothing, because this run has no
// plan for it and no way to know what the author's copy reaches. Claiming
// either way about somebody else's method would be a guess.
func (p *planner) shallow(of *form, seen map[*form]bool) bool {
	if of == nil || seen[of] {
		return false
	}
	seen[of] = true

	switch of.how {
	case howMethod, howThrough:
		held := p.plans[key(of.typ)]
		return held != nil && held.carried
	default:
		return p.shallow(of.elem, seen)
	}
}

// delegate drops the structs whose author already wrote the copy, and reports
// the ones that wrote something else under the name.
//
// A hand-written copy is the author overriding what would otherwise be
// generated, which is the same rule the codec follows and for the same reason:
// a type whose invariants forge cannot see is copied properly by the person who
// can see them. What is left after this is the structs this run writes for; a
// field holding any of them is copied by calling Clone either way, so nothing
// downstream has to know which.
func (p *planner) delegate() {
	for _, ref := range p.order {
		held := p.known[ref]
		if held == nil || !held.HasMethod(method) {
			continue
		}

		delete(p.plans, ref)

		if !declares(held.Type()) {
			p.diags.Add(diag.New(codeNotACopy, held.Pos,
				"%s declares %s, which does not answer with a %s",
				held.Ref().Name, method, held.Ref().Name).
				WithHint("%s", notACopyHint))
		}
	}
}

// remember records a struct and reserves its plan, in the order reached.
func (p *planner) remember(held *model.Struct) {
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
		spelled: model.Spell(held.Type(), p.into, p.bound),
		attach:  held.Attachable(p.into),
		why:     model.Unattachable(held, p.into),
	}
	p.order = append(p.order, ref)
}

// fill works out one struct's copy.
func (p *planner) fill(held *plan) {
	for _, field := range held.of.Fields {
		// An unexported field of a struct declared in another package cannot be
		// read from here, so nothing can be done about it beyond the assignment
		// the copy already made. Silently, because the field is not the
		// author's to change and the assignment is the honest answer for it.
		//
		// The package rather than the module, which is the language's rule: a
		// name is unexported to everything outside the package that declares
		// it, and a copy written in this one that named it would not compile
		// whether or not the two are in the same module.
		if !field.Exported && !held.of.Local(p.into) {
			held.carried = true
			continue
		}

		one := copied{field: field, path: field.Name, of: p.decide(field.Type.Type, p.shared(field))}
		if !needs(one.of) {
			// Already done by the assignment the copy opens with, or asked to
			// be left alone. Either way there is nothing to write.
			continue
		}
		if one.of.how == howOpaque {
			p.diags.Add(diag.New(codeOpaqueField, field.Pos,
				"%s is a %s, which nothing can copy without being told what a copy of it means",
				field.Name, field.Type).
				WithHint("%s", shareHint))
			continue
		}

		held.fields = append(held.fields, one)
	}
}

// shared reports whether this field is to be carried across rather than copied.
//
// The field's own answer where it has one, and the declaration's otherwise. A
// field is where the decision belongs when only that field needs it — the
// pointer into a shared cache, the slice everybody reads and nobody writes —
// and the declaration is where it belongs when the whole subject is that kind
// of value.
//
// The value is checked rather than the key alone. aliasing is the only option
// this layer takes on a field and share is the only value worth writing there,
// so anything else is a misspelling — and a misspelling that quietly left a
// field shared would be the one mistake this option exists to make visible.
func (p *planner) shared(field model.Field) bool {
	held := p.sharing

	for _, directive := range model.Written(field.Directives, verb) {
		name, value, split := strings.Cut(directive.Args, "=")
		switch {
		case strings.TrimSpace(name) != optionAliasing:
			p.diags.Add(diag.New(codeCloneOption, directive.Pos,
				"%s is not an option this layer takes on a field", strings.TrimSpace(name)).
				WithHint("a field takes %s=%s and nothing else", optionAliasing, aliasingShare))
		case !split || strings.TrimSpace(value) != aliasingShare:
			p.diags.Add(diag.New(codeCloneOption, directive.Pos,
				"%s takes %s on a field and was given %q", optionAliasing, aliasingShare, strings.TrimSpace(value)).
				WithHint("%s is what a field says; the declaration is where %s is written, since it is the default",
					aliasingShare, aliasingCopy))
		default:
			held = true
		}
	}

	return held
}

// decide returns how a type is copied, deciding it once.
func (p *planner) decide(t types.Type, share bool) *form {
	if t == nil {
		return &form{how: howAssign}
	}

	// A share is about one field rather than about a type, so it is answered
	// before anything is memoised: the same type copied elsewhere is copied.
	if share {
		return &form{typ: t, spelled: p.spellingOf(t), how: howShare}
	}

	ref := key(t)
	if found, seen := p.forms[ref]; seen {
		return found
	}

	out := &form{typ: t, spelled: p.spellingOf(t)}
	p.forms[ref] = out
	p.fillForm(out)

	return out
}

// spellingOf writes a type as the generated file must spell it.
func (p *planner) spellingOf(t types.Type) model.Spelling {
	return model.Spell(t, p.into, p.bound)
}

// fillForm decides one form, having already recorded it.
func (p *planner) fillForm(out *form) {
	// Asked before the type is taken apart. A type that copies itself is copied
	// by calling it, whatever it is made of — which is the whole point of a
	// hand-written copy, and what lets a type whose invariants forge cannot see
	// be copied properly.
	//
	// A struct this run reached is one of the two: either this run writes its
	// copy or its author did. Which of the two decides how it is called, and
	// only for the structs this run writes for and cannot attach to: those get
	// a function in the package being generated into, because Go lets a method
	// be declared only where its type is. Everything else is asked whether it
	// copies itself, which is a question about a type forge has never written
	// anything for.
	if held, writing := p.plans[key(out.typ)]; writing && !held.attach {
		out.how, out.call = howThrough, model.Through(held.of, verb, "", p.into)
		return
	}
	if _, reached := p.known[key(out.typ)]; reached || declares(out.typ) {
		out.how = howMethod
		return
	}

	switch under := out.typ.Underlying().(type) {
	case *types.Pointer:
		out.how, out.elem = howPointer, p.decide(under.Elem(), false)

	case *types.Slice:
		out.how, out.elem = howSlice, p.decide(under.Elem(), false)

	case *types.Array:
		out.how, out.elem = howArray, p.decide(under.Elem(), false)

	case *types.Map:
		out.how, out.elem = howMap, p.decide(under.Elem(), false)

	case *types.Interface, *types.Chan, *types.Signature:
		out.how = howOpaque

	case *types.Struct:
		// A struct written in place, or one from another module whose model
		// this run does not hold. Copying it is assigning it, which reaches
		// every field the language will let generated code reach.
		out.how = howAssign

	default:
		out.how = howAssign
	}
}

// declares reports whether a type has a copy of its own, which is a method
// called Clone taking nothing and answering with the type itself.
//
// The signature and not only the name. A method called Clone that takes an
// argument or answers with something else is somebody's method that happens to
// share a name, and calling it would not compile.
func declares(t types.Type) bool {
	named, is := types.Unalias(t).(*types.Named)
	if !is {
		return false
	}

	for i := range named.NumMethods() {
		one := named.Method(i)
		if one.Name() != method {
			continue
		}

		signature, ok := one.Type().(*types.Signature)
		if !ok || signature.Params().Len() != 0 || signature.Results().Len() != 1 {
			continue
		}
		if types.Identical(signature.Results().At(0).Type(), named) {
			return true
		}
	}
	return false
}

// written returns the plans this run emits, in the order they were reached.
//
// Every struct the subject reaches, including one with nothing to copy: a copy
// that is an assignment is still a copy, and a caller holding a value of that
// type wants to write the same call for it as for every other. It costs a
// method that returns its receiver, which the compiler will inline away.
func (p *planner) written() []*plan {
	out := make([]*plan, 0, len(p.plans))
	for _, ref := range p.order {
		if held, kept := p.plans[ref]; kept {
			out = append(out, held)
		}
	}
	return out
}
