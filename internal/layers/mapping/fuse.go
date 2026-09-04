package mapping

import (
	"go/ast"

	"github.com/okian/forge/internal/layers/jsoncodec"
	"github.com/okian/forge/plugin"
)

// fusedInStack reports whether the declaration writes the codec beneath the
// bridge, which is what asks for the fused writers.
func fusedInStack(ctx *plugin.Context) bool {
	for _, ref := range ctx.Model.Stack {
		if ref.Origin.Pkg == plugin.MarkerPkg && ref.Origin.Name == "Json" {
			return true
		}
	}
	return false
}

// fused asks the codec for the writers that put the target's document on the
// wire straight from the source, reading each member from what the settle
// table says it holds.
func fused(ctx *plugin.Context, built *plan) (plugin.Unit, error) {
	if member, held := hintReadsDst(built); held {
		return plugin.Unit{}, plugin.New(codeHintGrammar, ctx.Model.Hints[0].Pos,
			"%s reads %s, and a fused mapping has no target value to read while the document is written",
			ctx.Model.Hints[0].Fn.Name.Name, built.dstParam+"."+member).
			WithHint("spell the member's value from %s alone, or drop Json from the declaration", built.srcParam)
	}

	sourceSpelled := plugin.Spell(built.source, ctx.Model.Pkg.PkgPath, ctx.Bound())
	target := ctx.Model.SubjectSpelling(sourceSpelled.Bound(ctx.Bound())).Text

	return jsoncodec.Fused(ctx, func(src string) map[string]string {
		out := make(map[string]string, len(built.members))
		for _, member := range built.members {
			switch member.via {
			case settledField:
				out[member.field.Name] = src + "." + member.from
			case settledMethod:
				out[member.field.Name] = src + "." + member.from + "()"
			case settledHint:
				out[member.field.Name] = respelled(member.hint,
					map[string]string{built.srcParam: src})
			case settledIgnored, settledInvalid:
				// The member stays what a built target would have held: its
				// zero value, read off a zero literal so the codec's own
				// omission logic sees exactly what construct-then-encode sees.
				out[member.field.Name] = target + "{}." + member.field.Name
			}
		}
		return out
	})
}

// hintReadsDst reports whether any hint expression reads the target parameter,
// naming the member it reads. The constructor allows it — the literal exists
// by the time the hint's assignments run — and the fusion cannot: there is no
// target value while the document is written.
func hintReadsDst(built *plan) (string, bool) {
	for _, member := range built.members {
		if member.via != settledHint {
			continue
		}

		found := ""
		ast.Inspect(member.hint, func(node ast.Node) bool {
			if ident, ok := node.(*ast.Ident); ok && ident.Name == built.dstParam && found == "" {
				found = member.field.Name
			}
			return found == ""
		})
		if found != "" {
			return found, true
		}
	}
	return "", false
}
