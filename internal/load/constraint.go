package load

import (
	"go/ast"
	"go/build/constraint"
	"go/token"
)

// SpecTag is the build tag that guards spec files.
//
// A spec file is type-checked but never linked, and forge owns the real
// declaration in a complementary //go:build !forgespec file. The two
// constraints are complements, so exactly one declaration of a generated type
// is in scope in either configuration.
const SpecTag = "forgespec"

// SpecFile reports whether the file is a spec file: one the compiler builds
// only when [SpecTag] is set.
//
// The test is that the file's constraint cannot be satisfied without the tag
// but can be satisfied with it, so //go:build forgespec and
// //go:build forgespec && linux both qualify while //go:build linux does not.
// A file with no constraint at all is an ordinary file, which is where inline
// declarations live.
//
// Constraints derived from a file's name — the _linux of app_linux.go — never
// mention the spec tag, so they hold under both evaluations and cannot change
// the answer. They are not consulted.
func SpecFile(fset *token.FileSet, file *ast.File) bool {
	expr, ok := buildConstraint(fset, file)
	if !ok {
		return false
	}

	withoutTag := expr.Eval(func(tag string) bool { return tag != SpecTag })
	withTag := expr.Eval(func(string) bool { return true })

	return withTag && !withoutTag
}

// buildConstraint returns the file's build constraint, and whether it has one.
//
// Only comments before the package clause can constrain a file. A //go:build
// line wins outright when there is one, matching the go command; otherwise the
// // +build lines are combined, since each is a separate condition and all of
// them must hold.
func buildConstraint(fset *token.FileSet, file *ast.File) (constraint.Expr, bool) {
	var (
		plus      constraint.Expr
		plusGroup *ast.CommentGroup
	)

	for _, group := range file.Comments {
		if group.Pos() >= file.Package {
			break
		}

		for _, comment := range group.List {
			goBuild := constraint.IsGoBuild(comment.Text)
			if !goBuild && !constraint.IsPlusBuild(comment.Text) {
				continue
			}

			expr, err := constraint.Parse(comment.Text)
			if err != nil {
				// A malformed constraint is the go command's to report. For
				// forge's purposes the file simply is not a spec file, which is
				// the safe answer: it means forge does not claim the
				// declarations in it.
				return nil, false
			}

			if goBuild {
				return expr, true
			}

			plusGroup = group
			if plus == nil {
				plus = expr
			} else {
				plus = &constraint.AndExpr{X: plus, Y: expr}
			}
		}
	}

	// The legacy form only counts when a blank line separates it from the
	// documentation below, and a file that gets this wrong is built in every
	// configuration. Treating it as a constraint anyway would have forge claim
	// a declaration the compiler still has, and the two would collide.
	if plus != nil && !followedByBlankLine(fset, file, plusGroup) {
		return nil, false
	}

	return plus, plus != nil
}

// followedByBlankLine reports whether a blank line separates the comment group
// from whatever follows it.
//
// A comment group ends at a blank line, so another group between this one and
// the package clause is itself proof of the separation. Only the last group
// has to be measured against the package clause.
func followedByBlankLine(fset *token.FileSet, file *ast.File, group *ast.CommentGroup) bool {
	for _, other := range file.Comments {
		if other.Pos() > group.End() && other.Pos() < file.Package {
			return true
		}
	}
	return fset.Position(file.Package).Line > fset.Position(group.End()).Line+1
}
