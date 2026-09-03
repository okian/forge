package mapping

import (
	"errors"
	"go/ast"
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
	codeUnnamedEnd   = plugin.Register(2039, "a bridge's end is not a named type")

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

	// srcParam and dstParam are the hint's parameter names, which its
	// expressions are spelled against; empty when the declaration has no hint.
	srcParam, dstParam string
}

// settled says how one target member is filled in.
type settled uint8

const (
	settledInvalid settled = iota

	// settledField reads a source field: dst.X = src.From.
	settledField

	// settledMethod calls a source method: dst.X = src.From().
	settledMethod

	// settledHint takes the hint's assignment: the author wrote the
	// expression, and the constructor carries it.
	settledHint

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

	// tagged records that the member was pinned by its from tag rather than
	// matched, so the ledger attributes it to the author.
	tagged bool

	// hint is the assignment's right side, for settledHint, spelled against
	// the hint's own parameter names until emission respells it.
	hint ast.Expr

	// overrode names the ladder match the hint displaced, recorded for the
	// ledger: a reader should learn the automatic answer existed and lost.
	overrode string
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

	assigned, srcName, dstName, err := hinted(ctx)
	if err != nil {
		return nil, err
	}
	out.srcParam, out.dstParam = srcName, dstName

	// The name a qualified from entry picks the mapping by. A source that is
	// not named — which the emitter refuses on its own — is reached only by
	// bare entries.
	sourceName := ""
	if named, ok := types.Unalias(out.source).(*types.Named); ok {
		sourceName = named.Obj().Name()
	}

	var unsettled []plugin.Field
	for _, field := range out.target.Fields {
		member, miss, err := settle(ctx, field, all, ignored, assigned, sourceName)
		if err != nil {
			return nil, err
		}
		if miss {
			unsettled = append(unsettled, field)
			continue
		}
		if member.via == settledInvalid {
			// Every path out of settle either errors or says how the member
			// is filled in; a binding that says neither is forge's own bug.
			return nil, errors.New("mapping: a member was settled no way at all")
		}
		out.members = append(out.members, member)
	}

	if len(unsettled) > 0 {
		return nil, unsettledDiag(ctx, unsettled)
	}

	return out, nil
}

// unsettledDiag reports every member nothing settles in one complaint, because
// the author's next edit answers them together: a hint holds any number of
// assignments, and ignore holds any number of names.
func unsettledDiag(ctx *plugin.Context, fields []plugin.Field) error {
	names := make([]string, len(fields))
	for i, field := range fields {
		names[i] = field.Name
	}

	verb := "is"
	if len(names) > 1 {
		verb = "are"
	}

	return plugin.New(codeUnsettled, fields[0].Pos,
		"%s %s settled no way: nothing on %s matches them by name",
		strings.Join(names, " and "), verb, plugin.TypeString(ctx.Model.Source)).
		WithHint("match them by name, assign them in a //forge:map hint, or list them in ignore=")
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

// settle decides one member: pinned by its tag, matched on the ladder, taken
// from the hint, ignored on purpose, or refused with the code that says which
// way it failed. A member nothing settles is reported by the caller, together
// with the others, so it comes back as a miss rather than an error.
func settle(ctx *plugin.Context, field plugin.Field, all []candidate,
	ignored map[string]bool, assigned map[string]ast.Expr, source string,
) (binding, bool, error) {
	if member, pinned, err := tagged(ctx, field, all, ignored, assigned, source); pinned || err != nil {
		return member, false, err
	}

	found, fold, ok, clash := match(field, all)

	if ok && types.AssignableTo(found.typ, field.Type.Type) {
		if expr, hinted := assigned[field.Name]; hinted {
			// The hint outranks the ladder: the author wrote the assignment
			// looking at the same match the ladder found, so silence about the
			// match belongs in the ledger rather than in a refusal.
			return binding{field: field, via: settledHint, hint: expr, overrode: found.name}, false, nil
		}
		if ignored[field.Name] {
			return binding{}, false, plugin.New(codeIgnoreSaysNothing, ctx.Options.Pos,
				"ignore names %s, which %s settles anyway", field.Name, found.name).
				WithHint("drop %s from ignore, or rename the source member the mapping must not read", field.Name)
		}

		via := settledField
		if found.method {
			via = settledMethod
		}
		return binding{field: field, via: via, from: found.name, folded: fold}, false, nil
	}

	// The ways out of every refusal below, so each is settled before anything
	// is refused: a hint is the author writing the member themselves, and an
	// ignore is the author saying the zero value is the mapping.
	if expr, hinted := assigned[field.Name]; hinted {
		return binding{field: field, via: settledHint, hint: expr}, false, nil
	}
	if ignored[field.Name] {
		return binding{field: field, via: settledIgnored}, false, nil
	}

	switch {
	case len(clash) > 0:
		names := make([]string, len(clash))
		for i, one := range clash {
			names[i] = one.name
		}
		return binding{}, false, plugin.New(codeAmbiguous, field.Pos,
			"%s is claimed by %s, and the mapping cannot say which it means",
			field.Name, strings.Join(names, " and ")).
			WithHint("settle it with a //forge:map hint, or write ignore=%s", field.Name)

	case ok:
		return binding{}, false, unassignable(field, found, "matches")

	default:
		return binding{}, true, nil
	}
}

// tagged settles one member against its from tag, if it carries one that
// answers this mapping, refusing the tag that contradicts the hint or the
// ignore beside it.
func tagged(ctx *plugin.Context, field plugin.Field, all []candidate,
	ignored map[string]bool, assigned map[string]ast.Expr, source string,
) (binding, bool, error) {
	held, pinned, err := fromTag(field, source)
	if err != nil || !pinned {
		return binding{}, false, err
	}

	if _, hinted := assigned[field.Name]; hinted {
		return binding{}, true, plugin.New(codeTagAndHint, field.Pos,
			"%s is settled twice over: the from tag names %s and the hint assigns it",
			field.Name, held.entry).
			WithHint("keep one of them; two explicit answers do not agree by accident")
	}
	if ignored[field.Name] {
		return binding{}, true, plugin.New(codeIgnoreSaysNothing, ctx.Options.Pos,
			"ignore names %s, which its from tag settles", field.Name).
			WithHint("drop %s from ignore, or drop the tag", field.Name)
	}

	member, err := pin(field, held, all)
	return member, true, err
}

// unassignable is the refusal a name match earns when the types do not agree,
// however the name was arrived at.
func unassignable(field plugin.Field, found candidate, how string) error {
	return plugin.New(codeUnassignable, field.Pos,
		"%s %s %s, and %s does not assign to %s", field.Name, how, found.name,
		plugin.TypeString(found.typ), plugin.TypeString(field.Type.Type)).
		WithHint("write a //forge:map hint that converts, or write ignore=%s", field.Name)
}

// assignable reports whether a candidate's value assigns to the field.
func assignable(found candidate, field plugin.Field) bool {
	return types.AssignableTo(found.typ, field.Type.Type)
}
