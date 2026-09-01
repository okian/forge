package validate

import (
	"go/types"
	"strconv"

	"github.com/okian/forge/internal/diag"
	"github.com/okian/forge/internal/model"
)

// What can be wrong with the rules written on a subject.
//
// All three are about a tag, because a tag is the only thing this layer reads
// that somebody wrote. They are refusals rather than warnings for the reason
// the package doc gives: a rule that says something and does nothing leaves an
// author believing a value is checked, and nothing downstream ever says
// otherwise.
var (
	codeUnknownRule = diag.Register(2012, "validate tag names a rule that is not one")
	codeRuleValue   = diag.Register(2013, "validate rule was given a value it does not take")
	codeRuleShape   = diag.Register(2014, "validate rule cannot be asked of this type")
)

// codeNotACheck reports a type that declares something called Validate which is
// not a check of itself.
//
// Generating one would redeclare it, in a file the author cannot edit; not
// generating it would leave the type without the method every other type in the
// closure has, and the call sites would not compile either. Saying so is the
// only answer that leaves them somewhere to go.
var codeNotACheck = diag.Register(2019, "a type declares a Validate that is not a check")

// notACheckHint says what to do about one.
const notACheckHint = "rename the method, or give it the signature a check has — " +
	"no arguments, and an error as its result — and it will be called rather than written"

// The method the subject carries, and the prefix a field's own check is
// written under.
const (
	method = "Validate"
	verb   = "validate"
)

// What to do about a value a rule will not take. Each says the thing that is
// actually actionable rather than repeating the complaint: what a length is,
// and what oneof's values have to be written as.
const (
	lengths = "a length counts what a value holds, so it is a whole number that is not negative; " +
		"the rules and what each of them takes are documented in the layer"

	members = "oneof compares the value against each of the ones listed, so every one of them has to be " +
		"written as the field's own type; the rules are documented in the layer"
)

// checked is one field and everything asked of it.
type checked struct {
	// field is the field itself, which is what a diagnostic points at.
	field model.Field

	// path reaches the field from the value being checked, which for a field
	// of the subject is its own name.
	path string

	// rules are what its tag asked for, in the order they were written. Order
	// is kept because a value with three things wrong reports them in the order
	// the author would read the tag.
	rules []rule

	// form is what the field's type is, in the terms the rules are written in,
	// and spelled is how that type must be written in the file being generated
	// into — which a zero written out as a composite literal names.
	form    form
	spelled model.Spelling

	// hook records that the subject declares ValidateName, so the author's own
	// check is called where the tags' are.
	hook bool

	// nested records that the field's type has a Validate of its own to call,
	// whether this run is writing it or somebody already had, and indirect
	// that it is reached through a pointer — which has to be asked about
	// before it is followed.
	nested   bool
	indirect bool

	// through names the function the nested check goes through, for a type
	// this run writes for and cannot put a method on. It is empty where the
	// type carries one, which is where the call is a method call.
	through string

	// pattern names the package-level variable a regexp rule is compiled into.
	pattern string
}

// plan is one type's whole check.
type plan struct {
	// of is the struct being checked, and spelled is how it is written in the
	// file being generated into.
	of      *model.Struct
	spelled model.Spelling

	// bound is what that file will bind, carried so that a type spelled after
	// the plan was made is spelled against the same set as the ones spelled
	// while it was being made.
	bound []model.Import

	// fields are its checks, in declaration order.
	fields []checked

	// attach records that the type may carry the method, which is true only
	// for a struct the package being generated into declares. why says what
	// stops it where it does not, for the comment the function is written with.
	attach bool
	why    string
}

// wanted reports whether the plan asks anything at all.
//
// A struct with no rules, no hook and no field that has either is one whose
// Validate would return nil however it was called, and a file holding such a
// method is a file with a method in it that says nothing. The subject gets one
// regardless, because the declaration asked for it.
func (p plan) wanted() bool {
	for _, one := range p.fields {
		if len(one.rules) > 0 || one.hook || one.nested {
			return true
		}
	}
	return false
}

// planner works out what a subject and everything it reaches need checking.
type planner struct {
	// into is the package being generated into, which decides whether a struct
	// can carry the method.
	into string

	// bound is what the file will bind, which every spelling is written
	// against so that a type from a package called regexp is not written under
	// the name this layer's own import has.
	bound []model.Import

	// known holds every struct the subject reaches, by the identity that tells
	// two apart, so that a field whose type is one of them is checked by
	// calling its own method rather than by inlining its rules.
	known map[string]*model.Struct

	// plans holds what has been worked out, and order the identities in the
	// order they were reached, which is the order the methods are written.
	plans map[string]*plan
	order []string

	// delegated holds the types whose author wrote the check, which is why this
	// run has no plan for them. It is what tells a plan that was taken away
	// because nothing needed writing from one that was taken away because
	// somebody had already written it.
	delegated map[string]bool

	diags diag.Set
}

// plan works out what the subject and everything it reaches need.
func (p *planner) plan(held *model.Struct) *plan {
	p.known = make(map[string]*model.Struct)
	p.plans = make(map[string]*plan)
	p.delegated = make(map[string]bool)

	p.remember(held)
	for _, reached := range held.Closure {
		p.remember(reached)
	}

	p.delegate()

	// Every struct is planned before any is asked whether it needs a method,
	// because whether one needs it depends on whether the structs it holds do.
	for _, ref := range p.order {
		if one, writing := p.plans[ref]; writing {
			p.fill(one)
		}
	}
	p.settle()

	return p.plans[key(held.Type())]
}

// delegate drops the structs whose author already wrote the check, and reports
// the ones that wrote something else under the name.
//
// A hand-written check is the author overriding what would otherwise be
// generated, which is the rule the copy and the codec follow and for the same
// reason: a type whose invariants forge cannot see is checked properly by the
// person who can see them — and for a struct in another package those
// invariants are exactly the ones forge cannot see, since its unexported fields
// cannot be read from here at all.
//
// A field holding one of them is checked by calling Validate either way, so
// nothing downstream has to know which.
func (p *planner) delegate() {
	for _, ref := range p.order {
		held := p.known[ref]
		if held == nil || !held.HasMethod(method) {
			continue
		}

		delete(p.plans, ref)
		p.delegated[ref] = true

		if !declares(held.Type()) {
			p.diags.Add(diag.New(codeNotACheck, held.Pos,
				"%s declares %s, which does not answer with an error alone", held.Ref().Name, method).
				WithHint("%s", notACheckHint))
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
		bound:   p.bound,
		attach:  held.Attachable(p.into),
		why:     model.Unattachable(held, p.into),
	}
	p.order = append(p.order, ref)
}

// key identifies a type across the whole plan, by the spelling that keeps two
// types of one name in two packages apart.
func key(t types.Type) string { return model.TypeIdentity(t) }

// fill works out one struct's checks.
func (p *planner) fill(held *plan) {
	for _, field := range held.of.Fields {
		one := checked{
			field: field, path: field.Name,
			form:    formOf(field.Type.Type),
			spelled: model.Spell(field.Type.Type, p.into, p.bound),
		}

		// An unexported field of a struct declared in another package cannot be
		// read from here at all, so nothing about it can be checked. Silently,
		// because it is not the author's tag that is wrong: the field is not
		// theirs and the rule may be enforced where it is.
		//
		// The package rather than the module, which is the language's rule: a
		// name is unexported to everything outside the package that declares
		// it, whether or not the two are in the same module.
		if !field.Exported && !held.of.Local(p.into) {
			continue
		}

		p.rules(&one, held)
		one.hook = held.of.HasMethod(method + field.Name)
		one.nested, one.indirect = p.nested(field)
		one.through = p.throughFor(field)

		held.fields = append(held.fields, one)
	}
}

// rules reads a field's tag and records what it asked for.
func (p *planner) rules(one *checked, held *plan) {
	tag, tagged := one.field.Tag(tagKey)
	if !tagged || tag.Raw == "" {
		return
	}

	found, problems := written(tag.Raw)
	for _, wrong := range problems {
		p.diags.Add(diag.New(codeUnknownRule, one.field.Pos,
			"%s carries %s", one.field.Name, wrong.says).
			WithHint("%s", "the rules are documented in the layer, and a tag that names something else is a check nobody performs"))
	}

	for _, asked := range found {
		if !p.applicable(one, asked) {
			continue
		}
		if asked.name == ruleRegexp {
			one.pattern = model.Through(held.of, "pattern", one.field.Name, p.into)
		}
		one.rules = append(one.rules, asked)
	}
}

// applicable reports whether a rule may be asked of this field, reporting it
// where it may not.
func (p *planner) applicable(one *checked, asked rule) bool {
	needs, described := applies[asked.name]
	if !described {
		// A rule the grammar accepted and this table has no row for is this
		// file having drifted from the one beside it.
		p.diags.Add(diag.New(codeUnknownRule, one.field.Pos,
			"%s carries %s", one.field.Name, unknown(asked.name)))
		return false
	}

	if !needs.accepts(one.form) {
		p.diags.Add(diag.New(codeRuleShape, one.field.Pos,
			"%s is a %s, and %s needs %s",
			one.field.Name, one.field.Type, asked.name, needs.needs).
			WithHint("%s", advice(needs)))
		return false
	}

	// A length is a count, and a count is a whole number that is not negative.
	// The rule's own grammar accepts any number, because min on a float is a
	// number and min on a slice is a count, and only the type says which.
	if counted(asked) && !one.form.numeric {
		if !asked.digits {
			p.diags.Add(diag.New(codeRuleValue, one.field.Pos,
				"%s asks for a length of %s, and a length is a whole number",
				one.field.Name, asked.number).
				WithHint("%s", lengths))
			return false
		}
		if held, err := strconv.ParseInt(asked.number, 10, 64); err == nil && held < 0 {
			p.diags.Add(diag.New(codeRuleValue, one.field.Pos,
				"%s asks for a length of %s, and nothing is shorter than nothing",
				one.field.Name, asked.number).
				WithHint("%s", lengths))
			return false
		}
	}

	// A whole number compared against a fraction is a comparison the language
	// will not write, and rounding it would enforce a rule nobody asked for.
	if counted(asked) && one.form.numeric && !one.form.float && !asked.digits {
		p.diags.Add(diag.New(codeRuleValue, one.field.Pos,
			"%s is a %s and %s asks for %s, which is not a whole number",
			one.field.Name, one.field.Type, asked.name, asked.number).
			WithHint("%s", "write a whole number, or declare the field as a float"))
		return false
	}

	if asked.name == ruleOneOf {
		return p.members(one, asked)
	}
	return true
}

// counted reports whether a rule carries a number.
func counted(asked rule) bool {
	return asked.name == ruleMin || asked.name == ruleMax || asked.name == ruleLen
}

// members checks that what oneof accepts can be written as the field's type.
func (p *planner) members(one *checked, asked rule) bool {
	if one.form.text {
		return true
	}

	for _, member := range asked.members {
		if one.form.float {
			if _, err := strconv.ParseFloat(member, 64); err != nil {
				p.diags.Add(diag.New(codeRuleValue, one.field.Pos,
					"%s is a %s and oneof lists %s, which is not a number",
					one.field.Name, one.field.Type, member).
					WithHint("%s", members))
				return false
			}
			continue
		}
		if _, err := strconv.ParseInt(member, 10, 64); err != nil {
			p.diags.Add(diag.New(codeRuleValue, one.field.Pos,
				"%s is a %s and oneof lists %s, which is not a whole number",
				one.field.Name, one.field.Type, member).
				WithHint("%s", members))
			return false
		}
	}
	return true
}

// advice says what to write instead of a rule that does not apply.
func advice(needs wants) string {
	if needs.instead == "" {
		return "the rules and what each of them needs are documented in the layer"
	}
	return "write " + needs.instead + ", which asks the question this type has an answer to"
}

// nested reports whether a field's type has a check of its own to call.
//
// A struct this run is writing for is asked of the plan rather than of
// go/types, for the reason the codec asks the model: a generated file is loaded
// with the package it belongs to, so a method written by the last run looks
// like one the author wrote. Every other type is one forge has never written
// anything for, and what go/types says about it is the author's.
func (p *planner) nested(field model.Field) (nested, indirect bool) {
	held := field.Type.Type
	if held == nil {
		return false, false
	}

	// Through a pointer, since a struct held by pointer is checked by the same
	// method — but the pointer is reported, because a nil one is a value that
	// is not there and following it would stop the program rather than report
	// anything. Whether it may be nil at all is what required is for.
	if pointer, behind := held.Underlying().(*types.Pointer); behind {
		held, indirect = pointer.Elem(), true
	}

	if _, ours := p.known[key(held)]; ours {
		return true, indirect
	}
	return declares(held), indirect
}

// throughFor names the function a nested check goes through, and nothing where
// the call is a method call.
//
// A struct this run writes for and cannot attach to gets a function in the
// package being generated into, because Go puts a method only where its type
// is. A struct that carries the method — its author's or this run's — is called
// through it, which is what every check written before this one did.
func (p *planner) throughFor(field model.Field) string {
	held := field.Type.Type
	if held == nil {
		return ""
	}
	if pointer, behind := held.Underlying().(*types.Pointer); behind {
		held = pointer.Elem()
	}

	one, writing := p.plans[key(held)]
	if !writing || one.attach {
		return ""
	}
	return model.Through(one.of, verb, "", p.into)
}

// declares reports whether a type has a check of its own, whichever receiver it
// was declared with.
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
		if types.Identical(signature.Results().At(0).Type(), errorType) {
			return true
		}
	}
	return false
}

// errorType is what a check answers with, looked up once.
var errorType = types.Universe.Lookup("error").Type()

// settle drops the plans that ask nothing, and keeps dropping until nothing
// changes.
//
// A struct is worth a method when it has a rule, a hook, or a field whose type
// has a method — and that last one is a question about the struct's fields,
// which may themselves have been dropped. So it is answered by asking again
// until the answer stops moving, rather than in one pass that would keep a
// method whose only reason was another method that is not there.
//
// The subject is not dropped. A declaration that named this layer asked for the
// method, and one that returns nothing is the honest answer to a subject with
// nothing to check.
func (p *planner) settle() {
	kept := p.order[0]

	for moved := true; moved; {
		moved = false

		for _, ref := range p.order {
			held := p.plans[ref]
			if held == nil || ref == kept || held.wanted() {
				continue
			}

			delete(p.plans, ref)
			moved = true
		}

		if moved {
			p.forget()
		}
	}
}

// forget clears the calls into checks that are no longer written.
func (p *planner) forget() {
	for _, ref := range p.order {
		held := p.plans[ref]
		if held == nil {
			continue
		}

		for i := range held.fields {
			one := &held.fields[i]
			if !one.nested {
				continue
			}
			if p.dropped(one.field) {
				one.nested, one.through = false, ""
			}
		}
	}
}

// dropped reports whether a field's type was one this run planned for and has
// since decided needs nothing.
//
// Two things take a plan away and they mean opposite things. Settling takes one
// away because nobody will write that check, and a call into it has to go.
// Delegating takes one away because somebody already wrote it, and the call is
// the whole point — clearing it would leave the author's own check unreached,
// in code that compiles and quietly stops enforcing whatever it enforced.
func (p *planner) dropped(field model.Field) bool {
	held := field.Type.Type
	if held == nil {
		return false
	}
	if pointer, indirect := held.Underlying().(*types.Pointer); indirect {
		held = pointer.Elem()
	}

	ref := key(held)
	if _, ours := p.known[ref]; !ours || p.delegated[ref] {
		return false
	}
	_, still := p.plans[ref]

	return !still
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
