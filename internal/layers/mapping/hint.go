package mapping

import (
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"strings"

	"golang.org/x/tools/go/ast/astutil"

	"github.com/okian/forge/plugin"
)

// Codes the hint grammar refuses with.
var (
	codeHintGrammar     = plugin.Register(3032, "a hint says more than a hint may")
	codeHintTwiceMember = plugin.Register(3033, "one member assigned twice in a hint")
)

// hinted reads the declaration's hint, if it has one, and returns the members
// it assigns with the parameter names the expressions are written against.
//
// One hint at most: the pipeline holds a declaration to that before the layer
// runs, so a second entry here is not a case to arbitrate.
func hinted(ctx *plugin.Context) (map[string]ast.Expr, string, string, error) {
	if len(ctx.Model.Hints) == 0 {
		return nil, "", "", nil
	}

	one := ctx.Model.Hints[0]
	src, dst := paramNames(one.Fn)
	assigned, err := grammar(one.Fn, ctx.Model.Subject, dst, one.Pos)
	return assigned, src, dst, err
}

// paramNames returns the hint's two parameter names in order, however the
// author grouped them in the signature.
func paramNames(fn *ast.FuncDecl) (src, dst string) {
	var names []string
	for _, field := range fn.Type.Params.List {
		for _, name := range field.Names {
			names = append(names, name.Name)
		}
	}
	if len(names) != 2 {
		// The matcher only hands over func(src *S, dst *T), so this is
		// unreachable; two placeholders keep a broken caller diagnosable.
		return "src", "dst"
	}
	return names[0], names[1]
}

// grammar holds a hint to the narrow shape the layer reads: plain assignments,
// one target member each. Narrow twice over — the left sides are how the layer
// knows what the hint settles, and a later stage inlines each right side into
// a fused writer, which only a pure expression survives.
func grammar(fn *ast.FuncDecl, target *plugin.Struct, dst string, pos token.Position) (map[string]ast.Expr, error) {
	out := make(map[string]ast.Expr)

	for _, stmt := range fn.Body.List {
		assign, ok := stmt.(*ast.AssignStmt)
		if !ok || assign.Tok != token.ASSIGN || len(assign.Lhs) != 1 || len(assign.Rhs) != 1 {
			return nil, plugin.New(codeHintGrammar, pos,
				"%s says more than a hint may: a hint is plain assignments, %s.Member = expression",
				fn.Name.Name, dst).
				WithHint("locals, branches and multiple assignment belong in ordinary code beside the mapping")
		}

		member, ok := dstField(assign.Lhs[0], dst)
		if !ok {
			return nil, plugin.New(codeHintGrammar, pos,
				"%s assigns to something that is not a member of %s", fn.Name.Name, dst).
				WithHint("the left side of every hint assignment is %s.<Member>", dst)
		}
		if !fieldOf(target, member) {
			return nil, plugin.New(codeHintGrammar, pos,
				"%s assigns %s, which %s does not declare as its own",
				fn.Name.Name, member, target.Named.Obj().Name()).
				WithHint("a promoted member belongs to the type that declares it; "+
					"the target's own members are %s", strings.Join(fieldNames(target), ", "))
		}
		if _, twice := out[member]; twice {
			return nil, plugin.New(codeHintTwiceMember, pos,
				"%s assigns %s twice", fn.Name.Name, member).
				WithHint("one assignment per member; the last word is nobody's in a mapping")
		}

		out[member] = assign.Rhs[0]
	}

	return out, nil
}

// dstField returns the member name of an assignment target shaped dst.<Member>.
func dstField(lhs ast.Expr, dst string) (string, bool) {
	sel, ok := lhs.(*ast.SelectorExpr)
	if !ok {
		return "", false
	}
	base, ok := sel.X.(*ast.Ident)
	if !ok || base.Name != dst {
		return "", false
	}
	return sel.Sel.Name, true
}

// fieldOf reports whether the target declares a field of its own under this
// name. Promoted members are not its own: the constructor assigns the fields
// the target declares, and a promoted assignment would settle the embedded
// member's field without saying so.
func fieldOf(target *plugin.Struct, name string) bool {
	for _, field := range target.Fields {
		if field.Name == name {
			return true
		}
	}
	return false
}

// fieldNames lists the target's own members, for the hint that misses them.
func fieldNames(target *plugin.Struct) []string {
	out := make([]string, len(target.Fields))
	for i, field := range target.Fields {
		out[i] = field.Name
	}
	return out
}

// respelled returns the hint expression with the author's parameter names
// rewritten to the identifiers the constructor binds, leaving selector members
// and composite keys alone — dst.Name = src.Name renames both bases and
// neither Name.
//
// The author's tree is never edited: the walk runs over a copy made by
// reprinting and reparsing the expression, because the original is the load
// session's syntax and [astutil.Apply] replaces nodes in place.
func respelled(expr ast.Expr, renamed map[string]string) string {
	fresh, err := parser.ParseExpr(types.ExprString(expr))
	if err != nil {
		// ExprString of a parsed expression reparses; failing here is forge's
		// own bug, and the unrenamed spelling at least names the right members.
		return types.ExprString(expr)
	}

	rewritten, ok := astutil.Apply(fresh, func(c *astutil.Cursor) bool {
		if sel, ok := c.Parent().(*ast.SelectorExpr); ok && c.Node() == sel.Sel {
			return false
		}
		if pair, ok := c.Parent().(*ast.KeyValueExpr); ok && c.Node() == pair.Key {
			return false
		}
		if ident, ok := c.Node().(*ast.Ident); ok {
			if to, rename := renamed[ident.Name]; rename {
				c.Replace(&ast.Ident{Name: to, NamePos: ident.NamePos})
			}
		}
		return true
	}, nil).(ast.Expr)
	if !ok {
		// Apply returns what it was given, and it was given an expression.
		return types.ExprString(expr)
	}

	return types.ExprString(rewritten)
}
