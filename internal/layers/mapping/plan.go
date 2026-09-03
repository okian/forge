package mapping

import (
	"errors"
	"go/types"
	"strings"

	"github.com/okian/forge/plugin"
)

// Codes this layer refuses with. The 2xxx ones are about the two types; the
// 3xxx one is about a directive.
var (
	codeNoMembers    = plugin.Register(2034, "a bridge's source has no members to read")
	codeUnsettled    = plugin.Register(2035, "a target member is settled no way")
	codeAmbiguous    = plugin.Register(2036, "two source members claim one target member")
	codeUnassignable = plugin.Register(2037, "a matched member's types do not assign")
	codeOutOfReach   = plugin.Register(2038, "a target's unexported fields are out of reach")

	codeIgnoreSaysNothing = plugin.Register(3031, "ignore names a member that is already settled")
)

// plan is everything the constructor is built from: the two types, and how
// each of the target's members is settled.
type plan struct {
	source types.Type
	target *plugin.Struct

	// members holds one binding per target field, in declaration order, which
	// is the order the constructor assigns in.
	members []binding
}

// settled says how one target member is filled in.
type settled uint8

const (
	settledInvalid settled = iota

	// settledField reads a source field: dst.X = src.From.
	settledField

	// settledMethod calls a source method: dst.X = src.From().
	settledMethod

	// settledIgnored stays the zero value, on purpose: the author wrote
	// ignore=X.
	settledIgnored
)

// binding is how one target field is settled.
type binding struct {
	field plugin.Field
	via   settled

	// from is the source member's name, for settledField and settledMethod.
	from string

	// folded records that the match was by folded spelling rather than exact,
	// which the ledger says out loud: a reader comparing the two names should
	// not have to notice the difference themselves.
	folded bool
}

// planned settles every member of the declaration's target against its source,
// and refuses the declaration the first time a member cannot be.
func planned(ctx *plugin.Context) (*plan, error) {
	if ctx == nil || ctx.Model == nil || ctx.Model.Subject == nil || ctx.Model.Source == nil {
		// Not a diagnostic: a diagnostic points at a declaration, and the
		// declaration is what is missing. Reaching here is forge calling
		// itself wrongly rather than anybody writing anything.
		return nil, errors.New("mapping: asked to plan without a bridged declaration")
	}

	out := &plan{source: ctx.Model.Source, target: ctx.Model.Subject}

	if err := reachable(ctx); err != nil {
		return nil, err
	}

	all := candidates(out.source)
	if len(all) == 0 {
		return nil, plugin.New(codeNoMembers, ctx.Model.Pos,
			"%s offers nothing to read: no exported field, and no exported method "+
				"taking nothing and returning one value", plugin.TypeString(out.source)).
			WithHint("export a field or a getter on the source, or bridge from a type that has one")
	}

	ignored := make(map[string]bool)
	for _, name := range ctx.Options.List("ignore") {
		ignored[name] = true
	}

	for _, field := range out.target.Fields {
		member, err := settle(ctx, field, all, ignored)
		if err != nil {
			return nil, err
		}
		if member.via == settledInvalid {
			// Every path out of settle either errors or says how the member
			// is filled in; a binding that says neither is forge's own bug.
			return nil, errors.New("mapping: a member was settled no way at all")
		}
		out.members = append(out.members, member)
	}

	return out, nil
}

// reachable refuses a target whose unexported fields the constructor could not
// set: the zero value they would keep is not a mapping of anything.
//
// A local target's unexported fields are not refused — they join the members
// and are settled like any other, because the constructor is generated into
// the package that may name them.
func reachable(ctx *plugin.Context) error {
	target := ctx.Model.Subject

	if ctx.Model.Pkg != nil && target.Named != nil && target.Named.Obj().Pkg() != nil &&
		target.Named.Obj().Pkg().Path() == ctx.Model.Pkg.PkgPath {
		return nil
	}

	for _, field := range target.Fields {
		if !field.Exported {
			return plugin.New(codeOutOfReach, ctx.Model.Pos,
				"%s has unexported fields (%s), which a constructor generated outside "+
					"its package cannot set", target.Named.Obj().Name(), field.Name).
				WithHint("declare the mapping in the target's own package, or export the field")
		}
	}

	return nil
}

// candidate is one thing a source offers: a field, or a method taking nothing
// and returning one value.
type candidate struct {
	name   string
	typ    types.Type
	method bool
}

// candidates lists what a source offers, fields before methods, so that the
// ladder's precedence is an ordering fact rather than a comparison.
func candidates(source types.Type) []candidate {
	var out []candidate

	if structure, ok := source.Underlying().(*types.Struct); ok {
		for i := range structure.NumFields() {
			if field := structure.Field(i); field.Exported() {
				out = append(out, candidate{name: field.Name(), typ: field.Type()})
			}
		}
	}

	// Methods through the pointer's method set, so a getter with either
	// receiver counts: the constructor holds *S, and *S reaches both. An
	// interface is its own method set and has no pointer to speak through.
	held := source
	if _, isInterface := source.Underlying().(*types.Interface); !isInterface {
		held = types.NewPointer(source)
	}

	set := types.NewMethodSet(held)
	for i := range set.Len() {
		fn, ok := set.At(i).Obj().(*types.Func)
		if !ok || !fn.Exported() {
			continue
		}
		sig, ok := fn.Type().(*types.Signature)
		if !ok || sig.Params().Len() != 0 || sig.Results().Len() != 1 {
			continue
		}
		out = append(out, candidate{name: fn.Name(), typ: sig.Results().At(0).Type(), method: true})
	}

	return out
}

// folded is the comparison a near-miss is recognised by: lowercased with
// underscores dropped, so Nick_Name and NickName both reach Nickname.
func folded(name string) string {
	return strings.ToLower(strings.ReplaceAll(name, "_", ""))
}

// match settles one target field against the candidates, walking the ladder:
// the field of the same name, the method of the same name, the unique fold.
// More than one hit on a rung is returned for the caller to report.
func match(field plugin.Field, all []candidate) (found candidate, fold, ok bool, clash []candidate) {
	var exact, folds []candidate

	for _, one := range all {
		switch {
		case one.name == field.Name:
			exact = append(exact, one)
		case folded(one.name) == folded(field.Name):
			folds = append(folds, one)
		}
	}

	// Fields come before methods in the candidate list, so the first exact hit
	// is the higher rung. Two exact hits cannot happen — a type cannot declare
	// a field and a method under one name — so exact needs no clash arm.
	if len(exact) > 0 {
		return exact[0], false, true, nil
	}

	switch len(folds) {
	case 0:
		return candidate{}, false, false, nil
	case 1:
		return folds[0], true, true, nil
	default:
		return candidate{}, false, false, folds
	}
}

// settle decides one member: matched on the ladder, ignored on purpose, or
// refused with the code that says which way it failed.
func settle(ctx *plugin.Context, field plugin.Field, all []candidate, ignored map[string]bool) (binding, error) {
	found, fold, ok, clash := match(field, all)

	if ok && types.AssignableTo(found.typ, field.Type.Type) {
		if ignored[field.Name] {
			return binding{}, plugin.New(codeIgnoreSaysNothing, ctx.Options.Pos,
				"ignore names %s, which %s settles anyway", field.Name, found.name).
				WithHint("drop %s from ignore, or rename the source member the mapping must not read", field.Name)
		}

		via := settledField
		if found.method {
			via = settledMethod
		}
		return binding{field: field, via: via, from: found.name, folded: fold}, nil
	}

	// The ways out of every refusal below, so each is settled before it is
	// refused: an ignore is the author saying the zero value is the mapping.
	if ignored[field.Name] {
		return binding{field: field, via: settledIgnored}, nil
	}

	switch {
	case len(clash) > 0:
		names := make([]string, len(clash))
		for i, one := range clash {
			names[i] = one.name
		}
		return binding{}, plugin.New(codeAmbiguous, field.Pos,
			"%s is claimed by %s, and the mapping cannot say which it means",
			field.Name, strings.Join(names, " and ")).
			WithHint("settle it with a //forge:map hint, or write ignore=%s", field.Name)

	case ok:
		return binding{}, plugin.New(codeUnassignable, field.Pos,
			"%s matches %s, and %s does not assign to %s", field.Name, found.name,
			plugin.TypeString(found.typ), plugin.TypeString(field.Type.Type)).
			WithHint("write a //forge:map hint that converts, or write ignore=%s", field.Name)

	default:
		return binding{}, plugin.New(codeUnsettled, field.Pos,
			"%s is settled no way: nothing on %s matches it",
			field.Name, plugin.TypeString(ctx.Model.Source)).
			WithHint("add a //forge:map hint that sets it, or write ignore=%s", field.Name)
	}
}
