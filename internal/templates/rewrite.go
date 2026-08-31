package templates

import (
	"go/ast"

	"golang.org/x/tools/go/ast/astutil"
)

// specialise rewrites a template's declarations in place, or says what about
// them cannot be rewritten.
//
// Three passes, in an order that matters. The type parameters go first, because
// the result is not generic and everything after would otherwise keep stepping
// around them. Then every place a name is used with type arguments, because
// those are single nodes whose parts would be rewritten separately and wrongly.
// Only then are the remaining identifiers renamed.
func specialise(decls []ast.Decl, r Rewrite, renamed map[string]string) string {
	for _, decl := range decls {
		concrete(decl, r)
	}

	for i, decl := range decls {
		rewritten, wrong := instantiations(decl, r, renamed)
		if wrong != "" {
			return wrong
		}
		decls[i] = rewritten
	}

	for i := range decls {
		decls[i] = identifiers(decls[i], renamed)
	}
	return ""
}

// concrete removes the template's own type parameter from what it declared.
//
// A specialised template is one type over one subject, so that parameter has
// nothing left to stand for. A parameter that is not it stays: a function
// generic over some other type is generic over something the subject does not
// answer for, and a view over U is a view over U whatever the element is.
func concrete(decl ast.Decl, r Rewrite) {
	switch d := decl.(type) {
	case *ast.FuncDecl:
		// A generic method is not legal Go, so anything here belongs to a
		// package-level function.
		d.Type.TypeParams = without(d.Type.TypeParams, r.Param)

	case *ast.GenDecl:
		for _, spec := range d.Specs {
			if typ, ok := spec.(*ast.TypeSpec); ok {
				typ.TypeParams = without(typ.TypeParams, r.Param)
			}
		}
	}
}

// without returns a parameter list with one parameter removed, and nothing at
// all when that empties it.
//
// Nothing rather than an empty list: the printer writes an empty list as "[]",
// which is not a type parameter list and does not parse.
func without(list *ast.FieldList, param string) *ast.FieldList {
	if list == nil {
		return nil
	}

	var kept []*ast.Field
	for _, field := range list.List {
		var names []*ast.Ident
		for _, name := range field.Names {
			if name.Name != param {
				names = append(names, name)
			}
		}

		if len(names) > 0 {
			field.Names = names
			kept = append(kept, field)
		}
	}

	if len(kept) == 0 {
		return nil
	}
	list.List = kept
	return list
}

// instantiations rewrites every use of a template name with type arguments.
//
// The template's own parameter goes, because the declaration it referred to no
// longer has it; anything else stays, because the declaration it referred to
// still does. Collection[T] becomes Persons, zero[T]() becomes personsZero(),
// and pick[T, int] becomes personsPick[int].
//
// Handled as whole nodes because they are whole nodes: rewriting the name and
// the arguments separately would leave Persons[Person], which is a type nobody
// declared, and would leave the arguments of a call that no longer takes any.
func instantiations(decl ast.Decl, r Rewrite, renamed map[string]string) (ast.Decl, string) {
	var wrong string

	rewritten := astutil.Apply(decl, func(c *astutil.Cursor) bool {
		name, args := indexed(c.Node())
		if name == nil {
			return true
		}
		if _, declares := renamed[name.Name]; !declares {
			// Something else generic, instantiated with something of its own.
			return true
		}

		kept := except(args, r.Param)
		if name.Name == r.Container && len(kept) == len(args) {
			// The container over something that is not the element. There is
			// one element and one declared name, so there is nowhere for a
			// second specialisation of it to go.
			wrong = "it uses " + r.Container + " over something other than " + r.Param
			return false
		}

		c.Replace(applied(name, kept))
		return false
	}, nil)

	return declOf(decl, rewritten), wrong
}

// indexed returns the name being instantiated and the type arguments it was
// given, for either shape the syntax takes.
//
// One argument is an IndexExpr and more than one is an IndexListExpr. They are
// separate node types with no common interface, which is why a rewrite that
// knows only the first silently passes over every two-parameter template.
func indexed(n ast.Node) (*ast.Ident, []ast.Expr) {
	switch e := n.(type) {
	case *ast.IndexExpr:
		if name, ok := e.X.(*ast.Ident); ok {
			return name, []ast.Expr{e.Index}
		}
	case *ast.IndexListExpr:
		if name, ok := e.X.(*ast.Ident); ok {
			return name, e.Indices
		}
	}
	return nil, nil
}

// except returns the type arguments other than the one named.
func except(args []ast.Expr, param string) []ast.Expr {
	var kept []ast.Expr
	for _, arg := range args {
		if name, ok := arg.(*ast.Ident); ok && name.Name == param {
			continue
		}
		kept = append(kept, arg)
	}
	return kept
}

// applied rebuilds a use of a name with whatever type arguments it still needs.
func applied(name *ast.Ident, args []ast.Expr) ast.Expr {
	switch len(args) {
	case 0:
		return &ast.Ident{NamePos: name.Pos(), Name: name.Name}
	case 1:
		return &ast.IndexExpr{X: name, Lbrack: name.End(), Index: args[0], Rbrack: name.End()}
	default:
		return &ast.IndexListExpr{X: name, Lbrack: name.End(), Indices: args, Rbrack: name.End()}
	}
}

// identifiers renames what is left, from the one map that says what every name
// becomes — the type parameter among them, so that nothing here has a second
// opinion about an identifier the map already answers for.
//
// Three things are left alone, each because it is a name that lives somewhere
// other than the package. The selected half of a selector, because a field
// called Len and a function called Len are different things spelled the same
// way. The key of a composite literal, for the same reason and more sharply: a
// key names a field of whatever type is being built, very often somebody
// else's. And a method's own name, because it lives on its receiver's type —
// renaming it would rewrite the declaration and leave every call to it saying
// the name it used to have.
func identifiers(decl ast.Decl, renamed map[string]string) ast.Decl {
	method := methodName(decl)

	rewritten := astutil.Apply(decl, func(c *astutil.Cursor) bool {
		if untouchable(c, method) {
			return false
		}

		name, ok := c.Node().(*ast.Ident)
		if !ok {
			return true
		}

		if to, rename := renamed[name.Name]; rename {
			name.Name = to
		}
		return true
	}, nil)

	return declOf(decl, rewritten)
}

// untouchable reports whether a node names something that does not live in the
// package this is being written into.
func untouchable(c *astutil.Cursor, method *ast.Ident) bool {
	if c.Node() == method {
		return true
	}
	if selector, ok := c.Parent().(*ast.SelectorExpr); ok && c.Node() == selector.Sel {
		return true
	}

	pair, ok := c.Parent().(*ast.KeyValueExpr)
	return ok && c.Node() == pair.Key
}

// methodName returns a declaration's own name when it is a method, which is a
// name the package does not hold.
func methodName(decl ast.Decl) *ast.Ident {
	fn, ok := decl.(*ast.FuncDecl)
	if !ok || fn.Recv == nil {
		return nil
	}
	return fn.Name
}

// declOf returns the rewritten node as a declaration, or the original when a
// walk somehow replaced one with something that is not.
//
// Nothing here replaces a root, so the walk gives back what it was given — but
// a type assertion that could panic is one that will, eventually, in somebody
// else's build. Keeping the declaration that went in leaves the caller with
// something to emit rather than a stack trace.
func declOf(original ast.Decl, rewritten ast.Node) ast.Decl {
	if out, ok := rewritten.(ast.Decl); ok {
		return out
	}
	return original
}
