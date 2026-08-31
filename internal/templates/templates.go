package templates

import (
	"go/ast"
	"go/parser"
	"go/token"
	"maps"
	"slices"
	"strconv"

	"github.com/okian/forge/internal/diag"
	"github.com/okian/forge/internal/emit"
)

// What can be wrong with a template, or with what it was asked to become.
//
// These are failures of forge's own code rather than of anybody's declaration,
// which is why they carry the position of the declaration that asked: an author
// reading one can do nothing about it but report it, and the declaration is
// what they were doing at the time.
var (
	codeTemplateUnreadable = diag.Register(4910, "template does not parse")
	codeTemplateShape      = diag.Register(4911, "template is not shaped like a template")
	codeTemplateCollision  = diag.Register(4912, "template name collides with what it was rewritten into")
)

// Template is a layer's method bodies, as the layer ships them.
type Template struct {
	// Name is what the template is called in a diagnostic. A layer's own name
	// is the useful answer, since that is what an author would report.
	Name string

	// Source is the template package's source, one file of it.
	//
	// One file, because comments travel by position in a list belonging to a
	// file, and declarations gathered from two would carry offsets counted
	// against two different origins. A layer whose bodies outgrow one file
	// specialises each of them separately.
	Source []byte
}

// Rewrite says what a template is being specialised into.
type Rewrite struct {
	// Param is the template's type parameter, and Subject the type it stands
	// for: "T" becoming "Person".
	Param   string
	Subject string

	// Container is the template's own type, and Declared what the author called
	// it: "Collection" becoming "Persons".
	Container string
	Declared  string

	// Prefix is prepended to every other name the template declares at package
	// level.
	//
	// Generated code lands in somebody else's package, where a helper called
	// "each" would collide with theirs — and the collision is a build failure in
	// a file they did not write, about a name they never chose. The prefix is
	// the caller's because it has to be stable: the same declaration specialised
	// twice must produce the same helper names, or every run rewrites the file.
	Prefix string
}

// Result is a specialised template, in the shape a unit carries.
type Result struct {
	// Decls are the declarations to emit, in the order the template wrote them.
	Decls []ast.Decl

	// Comments and Fset are what the printer needs to put the comments back
	// where they were. They travel with the declarations because a comment is
	// not reachable from what it documents.
	Comments []*ast.CommentGroup
	Fset     *token.FileSet

	// Imports are what the template's declarations need, in the order they were
	// written.
	//
	// Each carries the name it was bound to as well as its path, because the
	// bodies call it by that name: a template importing "iter" as seq writes
	// seq.Seq, and a file that imported the path without the name would hold a
	// body naming a package that is not there.
	Imports []emit.Import
}

// Apply specialises a template, or says why it cannot be.
//
// The position is the declaration that asked, since that is what an author was
// doing when a template of forge's own turned out to be wrong.
func Apply(t Template, r Rewrite, at token.Position) (Result, diag.Set) {
	var diags diag.Set

	if wrong := r.check(); wrong != "" {
		diags.Add(diag.New(codeTemplateShape, at,
			"the %s template cannot be specialised: %s", t.Name, wrong).
			WithHint("%s", reportHint))
		return Result{}, diags
	}

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, t.Name+".go", t.Source, parser.ParseComments|parser.SkipObjectResolution)
	if err != nil {
		diags.Add(diag.New(codeTemplateUnreadable, at,
			"the %s template does not parse: %v", t.Name, err).
			WithHint("%s", reportHint))
		return Result{}, diags
	}

	imports, decls := separate(file.Decls)

	renamed, wrong := names(decls, r)
	if wrong != "" {
		diags.Add(diag.New(codeTemplateShape, at,
			"the %s template cannot be specialised: %s", t.Name, wrong).
			WithHint("%s", reportHint))
		return Result{}, diags
	}

	if clash := shadowed(decls, renamed); clash != "" {
		diags.Add(diag.New(codeTemplateCollision, at,
			"the %s template declares %s and uses that name for something else too", t.Name, clash).
			WithHint("%s", reportHint))
		return Result{}, diags
	}

	if wrong := specialise(decls, r, renamed); wrong != "" {
		diags.Add(diag.New(codeTemplateShape, at,
			"the %s template cannot be specialised: %s", t.Name, wrong).
			WithHint("%s", reportHint))
		return Result{}, diags
	}

	comments := kept(file, decls)
	reword(comments, renamed)

	settled, groups, positions, err := settle(decls, comments, fset)
	if err != nil {
		diags.Add(diag.New(codeTemplateUnreadable, at,
			"the %s template does not read back after rewriting: %v", t.Name, err).
			WithHint("%s", reportHint))
		return Result{}, diags
	}

	return Result{
		Decls:    settled,
		Comments: groups,
		Fset:     positions,
		Imports:  imports,
	}, diags
}

// reportHint says what an author can do about a template that is wrong, which
// is nothing except say so.
const reportHint = "this is a fault in forge rather than in the declaration; report it with the declaration that produced it"

// check reports what is wrong with a rewrite, or nothing.
//
// Every name is checked for being one. The printer writes an identifier exactly
// as it was given, so a declared name with a space in it becomes a file that
// does not parse, reported against a declaration whose author wrote nothing
// wrong — and the two names that must differ are checked because the output of
// confusing them is a type declared in terms of itself, which the compiler
// reports about the generated file rather than about this.
func (r Rewrite) check() string {
	named := []struct {
		what string
		name string
	}{
		{"type parameter", r.Param},
		{"subject", r.Subject},
		{"container type", r.Container},
		{"declared name", r.Declared},
	}

	for _, one := range named {
		switch {
		case one.name == "":
			return "no " + one.what + " was named"
		case !identifier(one.name):
			return "the " + one.what + " is written as " + strconv.Quote(one.name) + ", which is not an identifier"
		}
	}

	switch {
	case r.Prefix != "" && !identifier(r.Prefix):
		return "the prefix is written as " + strconv.Quote(r.Prefix) + ", which is not an identifier"
	case r.Param == r.Container:
		return "the type parameter and the container have the same name"
	case r.Declared == r.Subject:
		return "the declared type and the subject have the same name, so the one would be declared in terms of itself"
	case r.Subject == r.Container:
		return "the subject and the container have the same name"
	case r.Param == "_":
		return "the type parameter is blank, which names nothing to replace"
	}
	return ""
}

// identifier reports whether a name is one Go would accept.
//
// ASCII only, which is narrower than the language and wide enough for every
// name forge builds: a declared name comes from the author's own type, and a
// prefix is chosen by the layer. A name outside it is refused rather than
// written into a file to see what happens.
func identifier(name string) bool {
	if name == "" || !letter(name[0]) {
		return false
	}
	for i := 1; i < len(name); i++ {
		if !letter(name[i]) && !digit(name[i]) {
			return false
		}
	}
	return true
}

// separate lifts the import paths out of a template's declarations.
//
// Out rather than rewritten in place: the file that is eventually written
// gathers imports from every layer that contributed to it and writes one block,
// so an import declaration carried through here would be a second block in a
// file that already has one.
func separate(decls []ast.Decl) (imports []emit.Import, rest []ast.Decl) {
	for _, decl := range decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.IMPORT {
			rest = append(rest, decl)
			continue
		}

		for _, spec := range gen.Specs {
			imported, ok := spec.(*ast.ImportSpec)
			if !ok || imported.Path == nil {
				continue
			}

			path, err := strconv.Unquote(imported.Path.Value)
			if err != nil || path == "" {
				continue
			}

			one := emit.Import{Path: path}
			if imported.Name != nil {
				one.Name = imported.Name.Name
			}
			imports = append(imports, one)
		}
	}
	return imports, rest
}

// names decides what every name the rewrite touches becomes.
//
// One map, holding the type parameter along with everything the template
// declares. The parameter could be handled separately — it is not a
// package-level name and nothing collides with it — but then two pieces of code
// would each have an answer for one identifier, and whichever ran first would
// win. That is not a hypothetical: a template declaring a type called T would
// have it renamed to the subject, redeclaring the author's own type in their
// own package.
//
// The container takes the declared name; everything else takes the prefix. A
// template declaring nothing but its container needs no prefix, which is why
// the requirement is checked here rather than alongside the rest of the
// rewrite.
func names(decls []ast.Decl, r Rewrite) (map[string]string, string) {
	renamed := make(map[string]string)

	for _, decl := range decls {
		for _, name := range declared(decl) {
			switch {
			case name == r.Param:
				// Legal Go — the parameter shadows it inside the container —
				// and unrewritable: one of the two would have to keep a name
				// that means the other.
				return nil, "it declares " + name + ", which is also the name of its type parameter"
			case name == r.Container:
				renamed[name] = r.Declared
			case name == "_" || name == "init":
				// Neither is a name anything can refer to, so neither can
				// collide with one in the package this lands in.
			case r.Prefix == "":
				return nil, "it declares " + name + " and no prefix was given to keep that from colliding"
			default:
				renamed[name] = r.Prefix + upper(name)
			}
		}
	}

	if _, has := renamed[r.Container]; !has {
		return nil, "it declares no type called " + r.Container
	}

	if clash := collides(renamed); clash != "" {
		return nil, clash
	}

	renamed[r.Param] = r.Subject
	return renamed, ""
}

// collides reports two names that become one, which is a package holding two
// declarations of it. The prefix is what makes that possible: counted and
// Counted differ, and personsCounted does not differ from personsCounted.
//
// Sorted, because this is a message somebody reads and a map's order is
// whatever it is this run. A template with four such pairs would otherwise
// report a different one of them each time it was asked.
func collides(renamed map[string]string) string {
	taken := make(map[string]string, len(renamed))

	for _, from := range slices.Sorted(maps.Keys(renamed)) {
		to := renamed[from]
		if first, twice := taken[to]; twice {
			return "it declares both " + first + " and " + from + ", which become the same name"
		}
		taken[to] = from
	}
	return ""
}

// declared returns the package-level names a declaration introduces.
func declared(decl ast.Decl) []string {
	switch d := decl.(type) {
	case *ast.FuncDecl:
		if d.Recv != nil {
			// A method's name lives on its receiver's type rather than in the
			// package, so it collides with nothing and is left alone.
			return nil
		}
		return []string{d.Name.Name}

	case *ast.GenDecl:
		var found []string
		for _, spec := range d.Specs {
			switch s := spec.(type) {
			case *ast.TypeSpec:
				found = append(found, s.Name.Name)
			case *ast.ValueSpec:
				for _, name := range s.Names {
					found = append(found, name.Name)
				}
			}
		}
		return found
	}
	return nil
}

// shadowed reports a name the template both declares at package level and binds
// to something else somewhere inside it.
//
// Rewriting is done by name, without type information, because type-checking
// every layer's template on every run would mean resolving its imports on every
// run. The price is this rule: a template may not reuse one of its own
// package-level names for a parameter, a local or a field. It is forge's own
// code and can hold to that; a template that does not is refused here rather
// than rewritten into something that silently means something else.
func shadowed(decls []ast.Decl, renamed map[string]string) string {
	var found string

	for _, decl := range decls {
		// A type parameter's own declaration is not a second use of its name,
		// and it is the one place the name is meant to appear as a binding.
		parameters := declaring(decl)

		ast.Inspect(decl, func(n ast.Node) bool {
			if found != "" {
				return false
			}
			if field, is := n.(*ast.Field); is && parameters[field] {
				return false
			}

			for _, name := range binds(n) {
				// The type parameter is in this map too, and it is the name most
				// worth catching: it is rewritten wherever it appears, so a
				// second thing called T becomes a second thing called Person —
				// a field the subject does not have, or a variable shadowing
				// the subject's own type for a whole body.
				if _, declares := renamed[name.Name]; declares && !declaresIt(decl, name) {
					found = name.Name
					return false
				}
			}
			return true
		})
	}

	return found
}

// declaring returns the fields that declare a declaration's type parameters.
func declaring(decl ast.Decl) map[*ast.Field]bool {
	found := make(map[*ast.Field]bool)

	add := func(list *ast.FieldList) {
		if list == nil {
			return
		}
		for _, field := range list.List {
			found[field] = true
		}
	}

	switch d := decl.(type) {
	case *ast.FuncDecl:
		add(d.Type.TypeParams)
	case *ast.GenDecl:
		for _, spec := range d.Specs {
			if typ, ok := spec.(*ast.TypeSpec); ok {
				add(typ.TypeParams)
			}
		}
	}
	return found
}

// binds returns the identifiers a node binds to something new.
func binds(n ast.Node) []*ast.Ident {
	switch b := n.(type) {
	case *ast.Field:
		return b.Names
	case *ast.ValueSpec:
		return b.Names
	case *ast.AssignStmt:
		if b.Tok != token.DEFINE {
			return nil
		}
		var found []*ast.Ident
		for _, expr := range b.Lhs {
			if name, ok := expr.(*ast.Ident); ok {
				found = append(found, name)
			}
		}
		return found
	case *ast.LabeledStmt:
		return []*ast.Ident{b.Label}
	case *ast.RangeStmt:
		var found []*ast.Ident
		for _, expr := range []ast.Expr{b.Key, b.Value} {
			if name, ok := expr.(*ast.Ident); ok && b.Tok == token.DEFINE {
				found = append(found, name)
			}
		}
		return found
	}
	return nil
}

// declaresIt reports whether this identifier is the package-level declaration
// itself rather than a use of its name for something else.
func declaresIt(decl ast.Decl, name *ast.Ident) bool {
	gen, ok := decl.(*ast.GenDecl)
	if !ok {
		return false
	}

	for _, spec := range gen.Specs {
		value, ok := spec.(*ast.ValueSpec)
		if !ok {
			continue
		}
		if slices.Contains(value.Names, name) {
			return true
		}
	}
	return false
}

// upper returns a name with its first letter in upper case, so that a prefixed
// helper reads as one word.
func upper(name string) string {
	if name == "" {
		return name
	}

	runes := []rune(name)
	if lower := runes[0]; lower >= 'a' && lower <= 'z' {
		runes[0] = lower - ('a' - 'A')
	}
	return string(runes)
}

// kept returns the comment groups that belong to the declarations being
// emitted.
//
// The package comment is left behind: it documents the template rather than
// what the template becomes, and a generated file carrying "Package tmpl holds
// the bodies of the slice layer" would be describing something that is not
// there.
func kept(file *ast.File, decls []ast.Decl) []*ast.CommentGroup {
	if len(decls) == 0 {
		return nil
	}

	from := decls[0].Pos()
	if doc := docOf(decls[0]); doc != nil {
		from = doc.Pos()
	}

	var found []*ast.CommentGroup
	for _, group := range file.Comments {
		if group.Pos() >= from && group != file.Doc {
			found = append(found, group)
		}
	}
	return found
}

// docOf returns a declaration's own doc comment.
func docOf(decl ast.Decl) *ast.CommentGroup {
	switch d := decl.(type) {
	case *ast.FuncDecl:
		return d.Doc
	case *ast.GenDecl:
		return d.Doc
	}
	return nil
}
