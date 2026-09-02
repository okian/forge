package collection

import (
	"fmt"
	"go/ast"
	"go/token"

	"github.com/okian/forge/plugin"
)

// The two positions Less and Swap are written in terms of.
const (
	firstIndex  = "i"
	secondIndex = "j"
)

// sorts reports whether the collection gets the three methods sort.Sort takes.
//
// One key and not several, because these put the collection itself in order and
// a declaration naming two orders has no reason to prefer either. Indexed
// because they address elements by position, which today means the declared
// type's underlying form is a slice.
//
// Read by what writes them and by what declares them on the surface, which is
// the whole reason it is a function rather than a condition written twice: a
// surface that disagreed with the output would leave a method a decorator does
// not wrap and a name collision detection does not see.
func sorts(p plan) bool {
	return len(p.sorts) == 1 && p.beneath.Caps.Has(plugin.Indexed)
}

// sortable returns the two methods that, with the length the storage already
// answers, make the declared type sortable in place.
//
// Emitted for one declared sort key and no other number of them. With none
// there is no order to be in; with several there are several, and which of them
// sort.Sort should use is a question the author never answered. Guessing would
// be picking an order for somebody's data because they named it first — and the
// sorted views are still generated for each, so what is missing is only the
// unqualified one.
//
// It is worth having beyond those views because it is what the standard library
// takes: sort.Sort, sort.Stable and sort.IsSorted all ask for these three, and
// so does anything written against them. A view hands back a copy, which is the
// right answer for reading and the wrong one for a caller who wants the
// collection itself put in order.
//
// Indexed is what says the elements can be reached by position, and today that
// means the declared type's underlying form is a slice — the language does the
// indexing and no method backs the capability. A storage that claims Indexed
// some other way would need to put the two positional operations on the surface
// first, and this would have to be written against those instead.
func sortable(p plan) []ast.Decl {
	if !sorts(p) {
		return nil
	}

	by := p.sorts[0]
	held := receiverName

	// Built rather than parsed, like everything else this layer emits. A parsed
	// declaration carries positions from a file set nobody keeps, and a comment
	// hung off one of those is printed wherever those positions happen to land
	// — which is how a method ships without the documentation written for it.
	return []ast.Decl{
		&ast.FuncDecl{
			Doc: comment(fmt.Sprintf(
				"Less reports whether the element at %s sorts before the one at %s, by %s. "+
					"It is one of the three sort.Sort takes: the storage answers the length, "+
					"and Swap below is the other. "+
					"The order is %s's because it is the one sort key this declaration names — "+
					"one naming several has several orders and no reason to prefer any of them, "+
					"and gets its sorted views without these. "+
					"Unlike %s, sorting through this rearranges the collection rather than "+
					"answering with a copy of it in order.",
				firstIndex, secondIndex, by.field, by.field, by.method)),
			Recv: receiver(held, p.declared),
			Name: ast.NewIdent("Less"),
			Type: &ast.FuncType{
				Params:  positions(),
				Results: results(ast.NewIdent("bool")),
			},
			Body: &ast.BlockStmt{List: []ast.Stmt{
				&ast.ReturnStmt{Results: []ast.Expr{&ast.BinaryExpr{
					X:  field(held, firstIndex, by.field),
					Op: token.LSS,
					Y:  field(held, secondIndex, by.field),
				}}},
			}},
		},
		&ast.FuncDecl{
			Doc: comment(fmt.Sprintf(
				"Swap exchanges the elements at %s and %s, which is the last of the three "+
					"sort.Sort takes.",
				firstIndex, secondIndex)),
			Recv: receiver(held, p.declared),
			Name: ast.NewIdent("Swap"),
			Type: &ast.FuncType{Params: positions()},
			Body: &ast.BlockStmt{List: []ast.Stmt{
				&ast.AssignStmt{
					Lhs: []ast.Expr{at(held, firstIndex), at(held, secondIndex)},
					Tok: token.ASSIGN,
					Rhs: []ast.Expr{at(held, secondIndex), at(held, firstIndex)},
				},
			}},
		},
	}
}

// positions builds the parameter list both methods take.
func positions() *ast.FieldList {
	return &ast.FieldList{List: []*ast.Field{{
		Names: []*ast.Ident{ast.NewIdent(firstIndex), ast.NewIdent(secondIndex)},
		Type:  ast.NewIdent("int"),
	}}}
}

// at builds the element at a position: c[i].
func at(held, index string) ast.Expr {
	return &ast.IndexExpr{X: ast.NewIdent(held), Index: ast.NewIdent(index)}
}

// field builds one field of the element at a position: c[i].Age.
func field(held, index, name string) ast.Expr {
	return &ast.SelectorExpr{X: at(held, index), Sel: ast.NewIdent(name)}
}
