package index

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"

	"github.com/okian/forge/plugin"
)

// What this layer builds per declaration is the half no template can hold:
// the container's struct, whose map fields are typed by the declaration's own
// key and index fields; the placing method, whose statements file an element
// under each of them; removal, which unfiles it; Reset, which empties maps
// only a declaration knows the names of; and one lookup method per dimension,
// each a single expression handing a map and a key to a helper the template
// supplies.
//
// Built rather than parsed from assembled text. A tree cannot be a syntax
// error, so the failure a builder can produce is a tree with a hole in it,
// which the emitter catches and reports against the declaration — where text
// assembled from a format string fails as a parse error in output nobody has
// on disk to look at.

// built is what the builders hand back: the container's own declaration, and
// the methods that follow the template's half in the file.
type built struct {
	container []ast.Decl
	methods   []ast.Decl
}

// build turns the plan into the declarations this layer builds.
func (p plan) build() (built, error) {
	subject, err := spelled(p.subject.Text)
	if err != nil {
		return built{}, fmt.Errorf("index: the subject is written as %q, which is not a type: %w",
			p.subject.Text, err)
	}
	key, err := spelled(p.key.typ.Text)
	if err != nil {
		return built{}, fmt.Errorf("index: %s is written as %q, which is not a type: %w",
			p.key.field, p.key.typ.Text, err)
	}

	out := built{container: []ast.Decl{p.structDecl(key)}}

	if p.unique {
		out.methods = append(out.methods, p.uniqueLookup(key, subject))
	} else {
		out.methods = append(out.methods, p.multiLookup(key, subject))
	}

	for _, one := range p.secondaries {
		typ, wrong := spelled(one.typ.Text)
		if wrong != nil {
			return built{}, fmt.Errorf("index: %s is written as %q, which is not a type: %w",
				one.field, one.typ.Text, wrong)
		}
		out.methods = append(out.methods, p.secondaryLookup(one, typ, subject))
	}

	out.methods = append(out.methods, p.remove(key), p.reset(), p.placer(subject))

	return out, nil
}

// structDecl builds the container's own declaration: the walk order the
// template's methods share, the primary map, and one secondary map per
// declared index field.
func (p plan) structDecl(key ast.Expr) ast.Decl {
	entry := &ast.StarExpr{X: ast.NewIdent(p.entry)}

	primary := ast.Expr(entry)
	if !p.unique {
		primary = &ast.ArrayType{Elt: entry}
	}

	fields := []*ast.Field{
		field(orderField, &ast.ArrayType{Elt: entry}),
		field(p.key.slot, &ast.MapType{Key: key, Value: primary}),
	}
	for _, one := range p.secondaries {
		typ, err := spelled(one.typ.Text)
		if err != nil {
			// build reports the same parse first; this keeps the builder
			// total rather than half-failing into a struct with a hole.
			continue
		}
		fields = append(fields, field(one.slot, &ast.MapType{Key: typ, Value: &ast.ArrayType{Elt: key}}))
	}

	doc := fmt.Sprintf(
		"%s holds elements beside lookup maps over their declared fields, so that finding one by %s "+
			"is a map access rather than a scan. The walk order is the order of addition, less what "+
			"removal has moved: taking an element out swaps the last one into its hole. The maps are "+
			"made on first use, so the zero value is ready to use.",
		p.declared, p.key.field)

	return &ast.GenDecl{
		Doc: comment(doc),
		Tok: token.TYPE,
		Specs: []ast.Spec{&ast.TypeSpec{
			Name: ast.NewIdent(p.declared),
			Type: &ast.StructType{Fields: &ast.FieldList{List: fields}},
		}},
	}
}

// uniqueLookup builds the primary lookup where one key reaches one element:
//
//	func (r Directory) ByID(k int) (*Person, bool) { return r.pick(r.byID, k) }
func (p plan) uniqueLookup(key, subject ast.Expr) ast.Decl {
	return p.method(
		p.key.method+" returns a pointer to the element held under this "+p.key.field+", and whether one is.",
		p.key.method,
		params(field(p.lookup, key)),
		results(&ast.StarExpr{X: subject}, ast.NewIdent("bool")),
		ret(p.helper("pick", p.slot(p.key), ast.NewIdent(p.lookup))),
	)
}

// multiLookup builds the primary lookup where a key may reach several:
//
//	func (r Directory) ByID(k int) iter.Seq[Person] { return r.spread(r.byID[k]) }
func (p plan) multiLookup(key, subject ast.Expr) ast.Decl {
	return p.method(
		p.key.method+" walks the elements held under this "+p.key.field+", oldest first.",
		p.key.method,
		params(field(p.lookup, key)),
		results(seqOf(subject)),
		ret(p.helper("spread", index(p.slot(p.key), ast.NewIdent(p.lookup)))),
	)
}

// secondaryLookup builds one lookup over an index field:
//
//	func (r Directory) ByName(k string) iter.Seq[Person] { return r.found(r.byName[k], r.byID) }
func (p plan) secondaryLookup(one column, typ, subject ast.Expr) ast.Decl {
	return p.method(
		one.method+" walks the elements whose "+one.field+" is this value, oldest first.",
		one.method,
		params(field(p.lookup, typ)),
		results(seqOf(subject)),
		ret(p.helper("found", index(p.slot(one), ast.NewIdent(p.lookup)), p.slot(p.key))),
	)
}

// remove builds the method that takes elements out by key.
func (p plan) remove(key ast.Expr) ast.Decl {
	if !p.unique {
		// bucket := r.byID[k]; every entry leaves the order; the bucket
		// leaves the map; how many there were is the answer.
		return p.method(
			"Remove takes every element held under this key out of the container, and reports how many were.",
			"Remove",
			params(field(p.lookup, key)),
			results(ast.NewIdent("int")),
			define(ast.NewIdent(p.bucket), index(p.slot(p.key), ast.NewIdent(p.lookup))),
			&ast.RangeStmt{
				Key: ast.NewIdent("_"), Value: ast.NewIdent(p.slotE), Tok: token.DEFINE,
				X: ast.NewIdent(p.bucket),
				Body: block(
					exprStmt(p.helper("cut", sel(ast.NewIdent(p.slotE), "at"))),
				),
			},
			exprStmt(call(ast.NewIdent("delete"), p.slot(p.key), ast.NewIdent(p.lookup))),
			ret(call(ast.NewIdent("len"), ast.NewIdent(p.bucket))),
		)
	}

	stmts := make([]ast.Stmt, 0, 5+len(p.secondaries))
	stmts = append(stmts,
		defineTwo(ast.NewIdent(p.slotE), ast.NewIdent(p.held), index(p.slot(p.key), ast.NewIdent(p.lookup))),
		&ast.IfStmt{
			Cond: not(ast.NewIdent(p.held)),
			Body: block(ret(ast.NewIdent("false"))),
		},
		exprStmt(call(ast.NewIdent("delete"), p.slot(p.key), ast.NewIdent(p.lookup))),
	)
	for _, one := range p.secondaries {
		stmts = append(stmts, p.delist(one, sel(sel(ast.NewIdent(p.slotE), "elem"), one.field), ast.NewIdent(p.lookup)))
	}
	stmts = append(stmts,
		exprStmt(p.helper("cut", sel(ast.NewIdent(p.slotE), "at"))),
		ret(ast.NewIdent("true")),
	)

	return p.method(
		"Remove takes the element held under this key out of the container, and reports whether one was. "+
			"It costs the buckets the element was filed in and nothing per element held: the last entry "+
			"in the walk order moves into the hole rather than everything shuffling down.",
		"Remove",
		params(field(p.lookup, key)),
		results(ast.NewIdent("bool")),
		stmts...,
	)
}

// reset builds the method that empties the container.
//
// Built rather than kept from the template because the maps are this
// declaration's own: the template's placeholder empties the order alone,
// which for any declaration with a map would leave lookups answering for
// elements no walk covers.
func (p plan) reset() ast.Decl {
	stmts := make([]ast.Stmt, 0, 3+len(p.secondaries))
	stmts = append(stmts,
		exprStmt(call(ast.NewIdent("clear"), p.slot(column{slot: orderField}))),
		assign(p.slot(column{slot: orderField}), &ast.SliceExpr{
			X:    p.slot(column{slot: orderField}),
			High: &ast.BasicLit{Kind: token.INT, Value: "0"},
		}),
		exprStmt(call(ast.NewIdent("clear"), p.slot(p.key))),
	)
	for _, one := range p.secondaries {
		stmts = append(stmts, exprStmt(call(ast.NewIdent("clear"), p.slot(one))))
	}

	return p.method(
		"Reset empties the container, keeping the memory it has already taken. "+
			"The entries that were held are let go, so the elements can be collected "+
			"while the order's own memory stays for the next fill.",
		resetMethod,
		params(),
		results(),
		stmts...,
	)
}

// placer builds the method every add goes through, in the shape this
// declaration chose: refusing a held key, replacing under one, or filing
// beside the others.
func (p plan) placer(subject ast.Expr) ast.Decl {
	switch {
	case p.refusing:
		return p.placeRefusing(subject)
	case p.unique:
		return p.placeReplacing(subject)
	default:
		return p.placeGrouping(subject)
	}
}

// filing is the insert path every placer shares: make the entry, grow the
// order, file the entry under the primary key and the primary key under every
// secondary value.
func (p plan) filing(v ast.Expr) []ast.Stmt {
	stmts := make([]ast.Stmt, 0, 3+len(p.secondaries))
	stmts = append(stmts,
		define(ast.NewIdent(p.slotE), &ast.UnaryExpr{Op: token.AND, X: &ast.CompositeLit{
			Type: ast.NewIdent(p.entry),
			Elts: []ast.Expr{
				kv("elem", v),
				kv("at", call(ast.NewIdent("len"), p.slot(column{slot: orderField}))),
			},
		}}),
		assign(p.slot(column{slot: orderField}),
			call(ast.NewIdent("append"), p.slot(column{slot: orderField}), ast.NewIdent(p.slotE))),
	)

	file := "noted"
	if !p.unique {
		file = "grouped"
	}
	stmts = append(stmts, assign(p.slot(p.key),
		p.helper(file, p.slot(p.key), sel(v, p.key.field), ast.NewIdent(p.slotE))))

	for _, one := range p.secondaries {
		stmts = append(stmts, assign(p.slot(one),
			p.helper("listed", p.slot(one), sel(v, one.field), sel(v, p.key.field))))
	}
	return stmts
}

// placeRefusing: a held key comes back as the sentinel and nothing changes.
func (p plan) placeRefusing(subject ast.Expr) ast.Decl {
	v := ast.NewIdent(p.element)

	stmts := make([]ast.Stmt, 0, 5+len(p.secondaries))
	stmts = append(stmts, &ast.IfStmt{
		Init: defineTwo(ast.NewIdent("_"), ast.NewIdent(p.held), index(p.slot(p.key), sel(v, p.key.field))),
		Cond: ast.NewIdent(p.held),
		Body: block(ret(ast.NewIdent(p.dup))),
	})
	stmts = append(stmts, p.filing(v)...)
	stmts = append(stmts, ret(ast.NewIdent("nil")))

	return p.method(
		placeRefusing+" adds one element to the order and files it under every key, unless its key is already held.",
		placeRefusing,
		params(field(p.element, subject)),
		results(ast.NewIdent("error")),
		stmts...,
	)
}

// placeReplacing: a held key keeps its entry and the entry takes the new
// element, in place, so a pointer a lookup answered before the replacement
// still names the element under that key.
func (p plan) placeReplacing(subject ast.Expr) ast.Decl {
	v := ast.NewIdent(p.element)

	swap := make([]ast.Stmt, 0, len(p.secondaries)*2+2)
	for _, one := range p.secondaries {
		swap = append(swap, p.delist(one,
			sel(sel(ast.NewIdent(p.slotE), "elem"), one.field), sel(v, p.key.field)))
	}
	swap = append(swap, assign(sel(ast.NewIdent(p.slotE), "elem"), v))
	for _, one := range p.secondaries {
		swap = append(swap, assign(p.slot(one),
			p.helper("listed", p.slot(one), sel(v, one.field), sel(v, p.key.field))))
	}
	swap = append(swap, &ast.ReturnStmt{})

	stmts := make([]ast.Stmt, 0, 4+len(p.secondaries))
	stmts = append(stmts, &ast.IfStmt{
		Init: defineTwo(ast.NewIdent(p.slotE), ast.NewIdent(p.held), index(p.slot(p.key), sel(v, p.key.field))),
		Cond: ast.NewIdent(p.held),
		Body: block(swap...),
	})
	stmts = append(stmts, p.filing(v)...)

	return p.method(
		placePlain+" adds one element to the order and files it under every key, replacing the element "+
			"its key already reaches — in place, so the entry stays where every lookup filed it.",
		placePlain,
		params(field(p.element, subject)),
		results(),
		stmts...,
	)
}

// placeGrouping: every element is filed, and the ones sharing a key share a
// bucket.
func (p plan) placeGrouping(subject ast.Expr) ast.Decl {
	return p.method(
		placePlain+" adds one element to the order and files it beside the ones sharing its key.",
		placePlain,
		params(field(p.element, subject)),
		results(),
		p.filing(ast.NewIdent(p.element))...,
	)
}

// delist is the statement that takes one primary key out of a secondary
// bucket: r.byName = r.delisted(r.byName, <value>, <key>).
func (p plan) delist(one column, value, key ast.Expr) ast.Stmt {
	return assign(p.slot(one), p.helper("delisted", p.slot(one), value, key))
}

// method builds one method of the container, on the pointer receiver every
// method of a struct holding maps takes.
func (p plan) method(doc, name string, params, results *ast.FieldList, body ...ast.Stmt) *ast.FuncDecl {
	return &ast.FuncDecl{
		Doc: comment(doc),
		Recv: &ast.FieldList{List: []*ast.Field{{
			Names: []*ast.Ident{ast.NewIdent(p.receiver)},
			Type:  &ast.StarExpr{X: ast.NewIdent(p.declared)},
		}}},
		Name: ast.NewIdent(name),
		Type: &ast.FuncType{Params: params, Results: results},
		Body: &ast.BlockStmt{List: body},
	}
}

// helper is a call to one of the template's methods through the receiver.
func (p plan) helper(name string, args ...ast.Expr) ast.Expr {
	return call(sel(ast.NewIdent(p.receiver), name), args...)
}

// slot is a map or the order reached through the receiver: r.byID.
func (p plan) slot(one column) ast.Expr {
	return sel(ast.NewIdent(p.receiver), one.slot)
}

// The small vocabulary the builders above share. Each is a shape the go/ast
// package spells longhand, written once so the bodies read as what they say.

func field(name string, typ ast.Expr) *ast.Field {
	return &ast.Field{Names: []*ast.Ident{ast.NewIdent(name)}, Type: typ}
}

func params(list ...*ast.Field) *ast.FieldList { return &ast.FieldList{List: list} }

func results(types ...ast.Expr) *ast.FieldList {
	if len(types) == 0 {
		return nil
	}
	list := make([]*ast.Field, 0, len(types))
	for _, one := range types {
		list = append(list, &ast.Field{Type: one})
	}
	return &ast.FieldList{List: list}
}

func seqOf(elem ast.Expr) ast.Expr {
	return &ast.IndexExpr{X: sel(ast.NewIdent("iter"), "Seq"), Index: elem}
}

func sel(x ast.Expr, name string) ast.Expr {
	return &ast.SelectorExpr{X: x, Sel: ast.NewIdent(name)}
}

func index(x, by ast.Expr) ast.Expr { return &ast.IndexExpr{X: x, Index: by} }

func call(fun ast.Expr, args ...ast.Expr) ast.Expr {
	return &ast.CallExpr{Fun: fun, Args: args}
}

func kv(name string, value ast.Expr) ast.Expr {
	return &ast.KeyValueExpr{Key: ast.NewIdent(name), Value: value}
}

func not(x ast.Expr) ast.Expr { return &ast.UnaryExpr{Op: token.NOT, X: x} }

func define(lhs ast.Expr, rhs ast.Expr) ast.Stmt {
	return &ast.AssignStmt{Lhs: []ast.Expr{lhs}, Tok: token.DEFINE, Rhs: []ast.Expr{rhs}}
}

func defineTwo(a, b ast.Expr, rhs ast.Expr) ast.Stmt {
	return &ast.AssignStmt{Lhs: []ast.Expr{a, b}, Tok: token.DEFINE, Rhs: []ast.Expr{rhs}}
}

func assign(lhs ast.Expr, rhs ast.Expr) ast.Stmt {
	return &ast.AssignStmt{Lhs: []ast.Expr{lhs}, Tok: token.ASSIGN, Rhs: []ast.Expr{rhs}}
}

func exprStmt(x ast.Expr) ast.Stmt { return &ast.ExprStmt{X: x} }

func ret(x ...ast.Expr) ast.Stmt { return &ast.ReturnStmt{Results: x} }

func block(stmts ...ast.Stmt) *ast.BlockStmt { return &ast.BlockStmt{List: stmts} }

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
	for _, line := range plugin.Wrapped(text, plugin.CommentWidth) {
		lines = append(lines, &ast.Comment{Text: "// " + line})
	}
	return &ast.CommentGroup{List: lines}
}

// spelled turns a type written as source into the syntax a built declaration
// needs.
//
// The types this layer names come from the model as text, because that is how
// a type is spelled for a particular package — qualified or not, aliased or
// not. Reading it back is a conversion between two ways of holding one thing
// rather than a template being expanded: what goes in came out of the type
// checker, so what fails here is forge having produced a spelling that is not
// a type.
func spelled(text string) (ast.Expr, error) {
	return parser.ParseExpr(text)
}

// orderField is what the template calls the walk order, which the built
// struct has to declare under the same name for the template's bodies to
// read.
const orderField = "order"
