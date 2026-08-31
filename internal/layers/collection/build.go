package collection

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
)

// A generated method of this layer is one expression: it hands one of the
// subject's fields to a helper the template supplies and returns what comes
// back. The helper is where anything could go wrong, and it is compiled where
// it was written; what is built here is the sentence that names the field.
//
// Built rather than parsed from assembled text. A tree cannot be a syntax
// error, so the failure a builder can produce is a tree with a hole in it,
// which the emitter catches and reports against the declaration — where text
// assembled from a format string fails as a parse error in output nobody has on
// disk to look at.

// method builds one of this layer's methods:
//
//	// <doc>
//	func (c <recv>) <name>() <result> { return c.<helper>(func(v <subject>) <key> { return v.<field> }) }
//
// Every one of the three shapes is this, and writing them as one thing is what
// keeps them from drifting into three ways of saying the same sentence.
func method(m built) *ast.FuncDecl {
	return &ast.FuncDecl{
		Doc:  comment(m.doc),
		Recv: receiver(m.receiver, m.on),
		Name: ast.NewIdent(m.name),
		Type: &ast.FuncType{
			Params:  &ast.FieldList{},
			Results: results(m.result),
		},
		Body: &ast.BlockStmt{List: []ast.Stmt{
			&ast.ReturnStmt{Results: []ast.Expr{
				&ast.CallExpr{
					Fun:  &ast.SelectorExpr{X: ast.NewIdent(m.receiver), Sel: ast.NewIdent(m.helper)},
					Args: []ast.Expr{selecting(m.element, m.subject, m.field, m.key)},
				},
			}},
		}},
	}
}

// built is what one generated method is made of.
type built struct {
	// doc is the one-line summary written above it, without the name it opens
	// with — that is added, so it cannot disagree with the method's own.
	doc string

	// receiver is the name the body calls the collection by, and on is the type
	// the method is declared on.
	receiver string
	on       string

	// name is the method's own name, and result the type it returns.
	name   string
	result ast.Expr

	// helper is the method of the collection this one hands its work to.
	helper string

	// element is the name the built closure calls one element by, subject the
	// type of that element, field the field it takes, and key the type of that
	// field.
	//
	// The subject is an expression rather than a name, because it is not always
	// one: a type from another package is a selector and an instantiation is an
	// index. Written as an identifier it would print correctly and be a node
	// that says it is something it is not, which anything reading the tree
	// afterwards — the walk that decides which imports survive, for one —
	// believes.
	element string
	subject ast.Expr
	field   string
	key     ast.Expr
}

// selecting builds the closure that takes one field out of one element:
//
//	func(v Person) int { return v.Age }
//
// A closure that captures nothing, which the compiler makes once and reuses, so
// naming a field this way costs nothing a hand-written selector would not.
func selecting(element string, subject ast.Expr, field string, key ast.Expr) *ast.FuncLit {
	return &ast.FuncLit{
		Type: &ast.FuncType{
			Params: &ast.FieldList{List: []*ast.Field{{
				Names: []*ast.Ident{ast.NewIdent(element)},
				Type:  subject,
			}}},
			Results: results(key),
		},
		Body: &ast.BlockStmt{List: []ast.Stmt{
			&ast.ReturnStmt{Results: []ast.Expr{
				&ast.SelectorExpr{X: ast.NewIdent(element), Sel: ast.NewIdent(field)},
			}},
		}},
	}
}

// view builds the declaration of the type this layer's lazy view is:
//
//	// <doc>
//	type <name> struct{ <shared>[<subject>] }
//
// Embedded rather than defined from it, and that is the difference between a
// view with combinators on it and one with none. A defined type takes its
// representation from what it was defined from and none of its methods, so a
// view declared that way would be a sequence nothing could be done to; an
// embedded one promotes every combinator and still has room for the methods
// that read the subject, which is the whole reason it is named at all.
func view(name, doc, shared string, subject ast.Expr) *ast.GenDecl {
	return &ast.GenDecl{
		Doc: comment(doc),
		Tok: token.TYPE,
		Specs: []ast.Spec{&ast.TypeSpec{
			Name: ast.NewIdent(name),
			Type: &ast.StructType{Fields: &ast.FieldList{List: []*ast.Field{{
				Type: &ast.IndexExpr{X: ast.NewIdent(shared), Index: subject},
			}}}},
		}},
	}
}

// viewer builds the method that returns the view:
//
//	// <doc>
//	func (c <on>) <name>() <view> { return <view>{<shared>[<subject>](c.All())} }
func viewer(m built, shared string, subject ast.Expr) *ast.FuncDecl {
	held := &ast.CallExpr{
		Fun: &ast.IndexExpr{X: ast.NewIdent(shared), Index: subject},
		Args: []ast.Expr{&ast.CallExpr{
			Fun: &ast.SelectorExpr{X: ast.NewIdent(m.receiver), Sel: ast.NewIdent(m.helper)},
		}},
	}

	return &ast.FuncDecl{
		Doc:  comment(m.doc),
		Recv: receiver(m.receiver, m.on),
		Name: ast.NewIdent(m.name),
		Type: &ast.FuncType{
			Params:  &ast.FieldList{},
			Results: results(m.result),
		},
		Body: &ast.BlockStmt{List: []ast.Stmt{
			&ast.ReturnStmt{Results: []ast.Expr{&ast.CompositeLit{
				Type: m.result,
				Elts: []ast.Expr{held},
			}}},
		}},
	}
}

// receiver builds a value receiver. A value, because everything this layer
// generates reads the collection and returns something new — so a value of the
// declared type has all of it, and one returned by a function can be asked a
// question without being stored first.
func receiver(name, on string) *ast.FieldList {
	return &ast.FieldList{List: []*ast.Field{{
		Names: []*ast.Ident{ast.NewIdent(name)},
		Type:  ast.NewIdent(on),
	}}}
}

// results builds a single-result list.
func results(of ast.Expr) *ast.FieldList {
	return &ast.FieldList{List: []*ast.Field{{Type: of}}}
}

// comment builds the doc comment a built declaration carries with it.
//
// With it rather than beside it: a built declaration has no position, and the
// printer places a comment by position — so a comment left in a file's list
// would be written wherever position zero lands, which is above everything.
func comment(text string) *ast.CommentGroup {
	if text == "" {
		return nil
	}

	var lines []*ast.Comment
	for _, line := range wrapped(text, commentWidth) {
		lines = append(lines, &ast.Comment{Text: "// " + line})
	}
	return &ast.CommentGroup{List: lines}
}

// commentWidth is how wide a built comment's text may be before the two slashes
// and the space in front of it, so that a wrapped line is eighty columns.
const commentWidth = 77

// wrapped breaks a line of prose at the last space that fits.
//
// The file a built comment lands in also holds comments the template supplied,
// and those are wrapped because somebody wrote them that way. A generated
// method whose one-line summary ran to ninety columns beside them would look
// like the machine-written half of a file that is meant to read as one thing.
//
// A word longer than the width is left long rather than broken: a type name is
// one word, and hyphenating it would produce something that is not the name.
func wrapped(text string, width int) []string {
	var out []string

	line := ""
	for _, word := range strings.Fields(text) {
		switch {
		case line == "":
			line = word
		case len(line)+1+len(word) <= width:
			line += " " + word
		default:
			out = append(out, line)
			line = word
		}
	}

	if line != "" {
		out = append(out, line)
	}
	return out
}

// spelled turns a type written as source into the syntax a built declaration
// needs.
//
// The types this layer names come from the model as text, because that is how a
// type is spelled for a particular package — qualified or not, aliased or not.
// Reading it back is a conversion between two ways of holding one thing rather
// than a template being expanded: what goes in came out of the type checker, so
// what fails here is forge having produced a spelling that is not a type.
func spelled(text string) (ast.Expr, error) {
	return parser.ParseExpr(text)
}
