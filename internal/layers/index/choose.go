package index

import (
	"fmt"
	"go/ast"
	"strings"

	"github.com/okian/forge/plugin"
)

// choice is what one declaration's options make of the template: what to call
// the names it keeps, which declarations to leave out, and which of the ones
// it keeps to rename once the name is free.
//
// The three are one decision and are made together, because they have to
// agree. Naming a declaration that is then dropped leaves a name nothing
// carries; dropping one that was named leaves the file without it.
type choice struct {
	// names is what the rewrite is told, for the names it must not simply
	// prefix: the constructor and the error a caller writes out, and the
	// entry type the built struct names.
	names map[string]string

	// drop and rename are keyed by what a declaration is called *after* the
	// rewrite, since that is what the tree holds by the time they are read. A
	// method keeps the template's name through the rewrite, so for methods
	// the two spellings are one.
	drop   map[string]bool
	rename map[string]string

	// methods is every method the template may declare, and whether this
	// declaration emits it. A method the template grows that is not here is
	// refused rather than silently carried or dropped.
	methods map[string]bool
}

// chosen decides what this declaration makes of the template.
func chosen(p plan) choice {
	held := choice{
		names: map[string]string{
			dupError:  p.dup,
			entryType: p.entry,
		},
		drop:   map[string]bool{},
		rename: map[string]string{},
	}

	// The template's own container declaration goes: it holds the walk order
	// alone, declared so the template's bodies compile, and the built struct
	// — the order beside this declaration's maps — is what the file gets
	// under the name.
	held.drop[p.declared] = true

	// Both constructors cannot carry one name into a file, and only one of
	// them reaches a file. The one that does is named here; the other keeps
	// whatever the prefix rule gives it, and is dropped before anything is
	// written.
	kept, unused := constructorPlain, constructorRefusing
	if p.refusing {
		kept, unused = constructorRefusing, constructorPlain
	}
	held.names[kept] = constructorFor(p.declared)
	held.drop[held.spelled(unused, p.declared)] = true

	if !p.refusing {
		held.drop[held.spelled(dupError, p.declared)] = true
	}

	// A method is not a package-level name, so the rewrite leaves it alone
	// and both halves of the pair arrive under the names the template gave
	// them. The kept one is renamed to the contract's name here, once the
	// other is gone and the name is free.
	if p.refusing {
		held.drop[appendPlain] = true
		held.rename[appendRefusing] = appendPlain
	} else {
		held.drop[appendRefusing] = true
	}

	// The placeholders every run rebuilds, and the helpers this declaration
	// does not reach. A helper nothing calls would be a method on the
	// author's type that nothing uses — small, and the kind of thing that
	// accumulates once twelve layers are doing it.
	held.methods = map[string]bool{
		"Len":          true,
		"All":          true,
		appendPlain:    !p.refusing,
		appendRefusing: p.refusing, // renamed to the contract's name above
		placePlain:     false,
		placeRefusing:  false,
		resetMethod:    false,

		"cut":      true,
		"pick":     p.unique,
		"noted":    p.unique,
		"spread":   !p.unique,
		"grouped":  !p.unique,
		"found":    len(p.secondaries) > 0,
		"listed":   len(p.secondaries) > 0,
		"delisted": len(p.secondaries) > 0,
	}
	for name, wanted := range held.methods {
		if !wanted {
			held.drop[name] = true
		}
	}

	return held
}

// spelled returns what the rewrite will call a name the template declares.
//
// It is the rewrite's own rule rather than a guess at it: a name the choice
// gives an answer for becomes that answer, and every other package-level name
// takes the declaration's prefix. Working the name out this way rather than
// taking the prefix back off afterwards is what keeps the two from
// disagreeing about case.
func (c choice) spelled(name, declared string) string {
	if answer, asked := c.names[name]; asked {
		return answer
	}
	return plugin.Around(false, "", declared, name)
}

// applied returns the declarations this run keeps, with the ones it kept named
// the way the contract names them.
//
// The template writes every answer to every option, so that each is compiled
// and vetted by the ordinary build rather than living as a string nobody
// reads until it fails in somebody else's package. What a run emits is one
// answer per option, and the rest never reach a file.
//
// Renaming is the second half of it and cannot be done by the rewrite, which
// works on the names a package declares: a method is not one of those, and
// two methods cannot carry the name AppendSeq in a template that has to
// compile. So the kept one is renamed here, once the other is gone and the
// name is free.
func (c choice) applied(decls []ast.Decl, declared string) ([]ast.Decl, error) {
	kept := make([]ast.Decl, 0, len(decls))

	for _, decl := range decls {
		name := declaredAs(decl)

		// A method the template declares that this file has never heard of is
		// a template that grew one and a layer that would carry or drop it
		// without a word. What either produces is a file missing something it
		// calls, or holding something nothing does.
		if fn, is := decl.(*ast.FuncDecl); is && fn.Recv != nil {
			if _, known := c.methods[name]; !known {
				return nil, fmt.Errorf("index: the template declares %s, which nothing here emits or leaves out", name)
			}
		}

		if c.drop[name] {
			continue
		}
		if to, asked := c.rename[name]; asked {
			fn, is := decl.(*ast.FuncDecl)
			if !is {
				return nil, fmt.Errorf("index: %s is not a function, and this expects to rename one", name)
			}
			fn.Name = ast.NewIdent(to)
			redocument(fn.Doc, name, to)
		}
		kept = append(kept, decl)
	}

	// And everywhere the renamed methods are called, which is not the same
	// thing as where they are declared: a rename that moved only the
	// declaration would leave a body calling a method the file no longer has.
	calls(kept, c.rename)

	// Every run drops the placeholders at least, so a run that dropped
	// nothing means a name this file expects the template to declare is not
	// the name the template declares any more.
	if len(kept) == len(decls) {
		return nil, fmt.Errorf("index: nothing was left out of %s, so the template no longer declares what this expects", declared)
	}

	return kept, nil
}

// redocument renames a declaration in the comment documenting it.
//
// A doc comment opens with the name of the thing it documents, which is the
// convention every Go reader and every documentation tool relies on, so a
// method renamed without its comment is one whose documentation is about a
// method that is not there. Only the opening word is touched: the rest of the
// comment is prose about what the method does, and what it does did not
// change.
func redocument(doc *ast.CommentGroup, from, to string) {
	if doc == nil || len(doc.List) == 0 {
		return
	}

	first, opening := doc.List[0], "// "+from
	if !strings.HasPrefix(first.Text, opening) {
		return
	}

	if rest := first.Text[len(opening):]; rest == "" || rest[0] == ' ' {
		first.Text = "// " + to + rest
	}
}

// calls renames the method calls in these declarations.
//
// Only what is selected on something: a method is reached through a receiver,
// so a bare identifier of the same name is a different thing entirely and is
// left alone. The receiver itself is not examined, because the only methods
// renamed here are this template's own and nothing in it calls a method of
// that name on anything else.
func calls(decls []ast.Decl, rename map[string]string) {
	if len(rename) == 0 {
		return
	}

	for _, decl := range decls {
		ast.Inspect(decl, func(node ast.Node) bool {
			selector, is := node.(*ast.SelectorExpr)
			if !is || selector.Sel == nil {
				return true
			}
			if to, asked := rename[selector.Sel.Name]; asked {
				selector.Sel = ast.NewIdent(to)
			}
			return true
		})
	}
}

// declaredAs returns the one name a declaration introduces, or nothing where
// it introduces none or more than one.
//
// It is the name the tree holds now, after the rewrite: a package-level name
// is carrying the declaration's prefix or the answer the choice gave it, and
// a method is carrying what the template called it, because the prefix is for
// the names a generated file adds to somebody else's package and a method is
// not one of those.
func declaredAs(decl ast.Decl) string {
	switch typed := decl.(type) {
	case *ast.FuncDecl:
		if typed == nil || typed.Name == nil {
			return ""
		}
		return typed.Name.Name

	case *ast.GenDecl:
		if typed == nil || len(typed.Specs) != 1 {
			return ""
		}
		switch spec := typed.Specs[0].(type) {
		case *ast.ValueSpec:
			if len(spec.Names) == 1 {
				return spec.Names[0].Name
			}
		case *ast.TypeSpec:
			if spec.Name != nil {
				return spec.Name.Name
			}
		}
		return ""

	default:
		return ""
	}
}
