package cli

import (
	"go/types"

	"github.com/okian/forge/internal/diag"
	"github.com/okian/forge/internal/discover"
	"github.com/okian/forge/internal/model"
)

// Diagnostics the hint matcher reports. All four are about a hint nobody will
// read, which is worth saying out loud: a mapping generated without the hint
// its author wrote is quietly missing the one member the hint existed to
// settle.
var (
	codeHintUnknown   = diag.Register(3025, "a function directive the map layer does not take")
	codeHintShape     = diag.Register(3026, "a map hint is not shaped like one")
	codeHintTwice     = diag.Register(3028, "two hints for one mapping")
	codeHintUnmatched = diag.Register(3029, "a map hint matches no declaration")
	codeHintNotSpec   = diag.Register(3030, "a map hint lives outside the spec file")
)

// matched pairs each map hint with the declaration it is for, by the types its
// parameters name: a hint func(src *User, dst *Person) belongs to the
// declaration whose source is User and whose subject is Person.
//
// Matching runs here rather than in the layer because it is the half the
// layer cannot do: a layer sees one declaration, and a hint that matches none
// of them is exactly the case worth reporting.
func matched(hints []discover.Hint, requests []request, diags *diag.Set) {
	for _, hint := range hints {
		// Discovery claims only map directives, so the layer is never in
		// question — what may still be wrong is everything after the verb.
		if hint.Args != "hint" {
			diags.Add(diag.New(codeHintUnknown, hint.Pos,
				"%q is not something the map layer takes on a function", hint.Args).
				WithHint("a function is marked //forge:map hint and nothing else"))
			continue
		}
		if hint.Form != model.FormSpec {
			diags.Add(diag.New(codeHintNotSpec, hint.Pos,
				"a map hint lives in the spec file, and %s is not one", hint.Pos.Filename).
				WithHint("move the function into a file guarded by //go:build forgespec; " +
					"it is type-checked there and never linked"))
			continue
		}

		src, dst, ok := hintTypes(hint)
		if !ok {
			diags.Add(diag.New(codeHintShape, hint.Pos,
				"%s is not shaped like a hint", hint.Fn.Name.Name).
				WithHint("a hint is func(src *S, dst *T) for the S and T of one Map declaration"))
			continue
		}

		if !claimHint(requests, hint, src, dst, diags) {
			diags.Add(diag.New(codeHintUnmatched, hint.Pos,
				"%s matches no Map declaration in its package", hint.Fn.Name.Name).
				WithHint("declare type X Map[S, T] with the S and T the hint's parameters name"))
		}
	}
}

// hintTypes reads the source and target a hint's parameters name, and reports
// whether the function is shaped like a hint at all: two pointer parameters,
// no results, no type parameters.
func hintTypes(hint discover.Hint) (src, dst types.Type, ok bool) {
	if hint.Pkg == nil || hint.Pkg.TypesInfo == nil {
		return nil, nil, false
	}

	fn, ok := hint.Pkg.TypesInfo.Defs[hint.Fn.Name].(*types.Func)
	if !ok {
		return nil, nil, false
	}

	sig, ok := fn.Type().(*types.Signature)
	if !ok || sig.TypeParams().Len() > 0 || sig.Results().Len() > 0 || sig.Params().Len() != 2 {
		return nil, nil, false
	}

	from, ok := sig.Params().At(0).Type().(*types.Pointer)
	if !ok {
		return nil, nil, false
	}
	to, ok := sig.Params().At(1).Type().(*types.Pointer)
	if !ok {
		return nil, nil, false
	}

	return from.Elem(), to.Elem(), true
}

// claimHint hands the hint to the one request whose source and subject its
// parameters name, and reports whether any did.
//
// The search is package-scoped: a hint names types, and two packages may each
// declare a Person, so the declaration a hint settles is the one beside it.
func claimHint(requests []request, hint discover.Hint, src, dst types.Type, diags *diag.Set) bool {
	for i := range requests {
		req := &requests[i]
		decl := req.Declaration

		if decl.Source == nil || decl.Candidate.Pkg != hint.Pkg {
			continue
		}
		if !types.Identical(decl.Source, src) || !types.Identical(decl.Subject, dst) {
			continue
		}

		if len(req.Hints) > 0 {
			// The first hint stands; a second is a contradiction to report
			// rather than an order to guess at.
			diags.Add(diag.New(codeHintTwice, hint.Pos,
				"%s is the second hint for %s", hint.Fn.Name.Name, decl.Candidate.Name).
				WithHint("one hint settles a mapping; fold the assignments into %s",
					req.Hints[0].Fn.Name.Name))
			return true
		}

		req.Hints = append(req.Hints, model.Hint{Fn: hint.Fn, Pkg: hint.Pkg, Pos: hint.Pos})
		return true
	}

	return false
}
