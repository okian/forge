package generate

import (
	"go/ast"
	"go/token"
	"go/types"
	"slices"
	"strings"

	"golang.org/x/tools/go/packages"

	"github.com/okian/forge/internal/diag"
	"github.com/okian/forge/internal/emit"
	"github.com/okian/forge/internal/merge"
	"github.com/okian/forge/internal/shape"
)

// What a package's own declarations can do to what is generated into it.
//
// All five are about the package rather than about one layer, which is why they
// are reported here: a layer is handed a subject and knows nothing about what
// else is written beside it, and two layers know nothing about each other.
var (
	codeOverrideWrong = diag.Register(4011, "a method the author declared does not satisfy the contract the stack needs")
	codeTwoClaimants  = diag.Register(4012, "two layers want one method name")
	codeNameTaken     = diag.Register(4013, "a generated declaration collides with one the package already has")
	codePackageClash  = diag.Register(4016, "two import paths bind one package name")
	codeNameTwice     = diag.Register(4018, "two generated declarations want one package-level name")
)

// declared is what the package already holds, which is what generated code may
// neither redeclare nor contradict.
//
// The author's own, and not a previous run's. Generated files are loaded with
// the package they belong to — they have to be, or a call site naming a
// generated type would stop the load — so what go/types reports includes
// whatever forge wrote last time. Counting that would make every second run
// report the whole of its own output as a collision.
type declared struct {
	// names holds the package-level identifiers the author declared, and where.
	names map[string]token.Position

	// methods holds the author's methods, by the name of the type they are
	// declared on and then by their own name.
	methods map[string]map[string]*types.Func
}

// holds reads what a package already declares.
func holds(pkg *packages.Package, generated func(token.Pos) bool) declared {
	out := declared{
		names:   make(map[string]token.Position),
		methods: make(map[string]map[string]*types.Func),
	}
	if pkg == nil || pkg.Types == nil {
		return out
	}

	fset := pkg.Fset
	scope := pkg.Types.Scope()

	for _, name := range scope.Names() {
		obj := scope.Lookup(name)
		if obj == nil || wrote(generated, obj.Pos()) {
			continue
		}

		out.names[name] = position(fset, obj.Pos())

		named, is := types.Unalias(obj.Type()).(*types.Named)
		if !is {
			continue
		}

		for i := range named.NumMethods() {
			one := named.Method(i)
			if wrote(generated, one.Pos()) {
				continue
			}
			if out.methods[name] == nil {
				out.methods[name] = make(map[string]*types.Func)
			}
			out.methods[name][one.Name()] = one
		}
	}

	return out
}

// wrote reports whether a previous run wrote what is at a position.
func wrote(generated func(token.Pos) bool, at token.Pos) bool {
	return generated != nil && generated(at)
}

// position resolves a position, or reports none where there is no file set to
// resolve it against.
func position(fset *token.FileSet, at token.Pos) token.Position {
	if fset == nil || !at.IsValid() {
		return token.Position{}
	}
	return fset.Position(at)
}

// policing is what deciding a declaration's collisions needs to know.
type policing struct {
	// held is what the package already declares.
	held declared

	// exposed is what the stack offers, which is what an overriding method has
	// to go on satisfying.
	exposed shape.Shape

	// at is where a diagnostic about the declaration points, and declared is
	// the name of the type being generated for.
	at       token.Position
	declared string
}

// policed applies the package's rules to what a stack generated, and returns
// what is left to write.
//
// Four things happen here and nowhere else, because this is the first stage
// that can see a package rather than a layer. A method the author already
// declared is dropped, which is how an override works. Two layers claiming one
// name are reported, because a generator that picked one would be choosing for
// them. A generated name the package already has is reported, because
// overwriting it is not this program's to do. And the imports are checked
// against each other, which needs every layer's at once.
func policed(held merge.Unit, of policing, diags *diag.Set) merge.Unit {
	held.Sections = overrides(held.Sections, of, diags)

	claimed(held.Sections, of, diags)
	taken(held.Sections, of, diags)
	bound(held.Imports, of, diags)

	return held
}

// overrides drops every method the author already declared, and reports the
// ones that no longer do what the stack needs.
//
// Dropped silently, because that is what an override is: a method somebody
// wrote themselves is the answer, and generating a second one would not
// compile. What is not silent is an override that stops the layers above it
// working — a Len returning a string under a shape that promises a number is
// not an override, it is a stack that does not hold together, and the compiler
// would report it against generated code rather than against the method.
func overrides(sections []emit.Section, of policing, diags *diag.Set) []emit.Section {
	out := make([]emit.Section, 0, len(sections))

	for _, section := range sections {
		kept := make([]ast.Decl, 0, len(section.Decls))

		for _, decl := range section.Decls {
			on, name, is := methodOf(decl)
			if !is {
				kept = append(kept, decl)
				continue
			}

			author, overridden := of.held.methods[on][name]
			if !overridden {
				kept = append(kept, decl)
				continue
			}

			if wrong := contradicts(of, name, author); wrong != "" {
				diags.Add(diag.New(codeOverrideWrong, of.at,
					"%s declares %s, and %s", on, name, wrong).
					WithHint("%s", "a method written in place of a generated one still has to be the "+
						"method the layers above it were written against"))
			}
		}

		section.Decls = kept
		if !section.Empty() {
			out = append(out, section)
		}
	}

	return out
}

// contradicts says how an overriding method fails the contract the stack
// exposes, or nothing.
//
// Compared against the surface, which is what the layers above were written
// against, and only where the comparison means the same thing on both sides.
// A surface is written for a person to read — the element in it is spelled by
// its bare name whatever the file would have to call it — so a type there
// holding a dot or a bracket is a spelling this cannot line up against
// go/types, and the arity is what both agree on. A plain identifier is
// unambiguous, which is exactly the case the rule was written about: Len()
// string under a shape that promises Len() int.
func contradicts(of policing, name string, author *types.Func) string {
	held, offered := of.exposed.Method(name)
	if !offered {
		return ""
	}

	signature, is := author.Type().(*types.Signature)
	if !is {
		return ""
	}

	params, results, err := held.Rendered()
	if err != nil {
		return ""
	}

	switch {
	case signature.Params().Len() != len(params):
		return "the stack was written against " + name + held.Signature
	case signature.Results().Len() != len(results):
		return "the stack was written against " + name + held.Signature
	}

	for i, want := range results {
		if !plain(want) {
			continue
		}
		if got := types.TypeString(signature.Results().At(i).Type(), nil); got != want {
			return name + " answers with " + got + " where the stack was written against " + want
		}
	}

	return ""
}

// plain reports whether a type as a surface spells it means the same thing to
// go/types: a predeclared name, with no package qualifier and no arguments.
func plain(written string) bool {
	return !strings.ContainsAny(written, ".[]*") && types.Universe.Lookup(written) != nil
}

// claimed reports two layers writing one method on one type.
//
// Never resolved here. The rule is that a designated layer keeps the plain name
// and the others take qualified ones, and only a layer knows what its own
// method should be called when it cannot have the plain one — WriteTo becomes
// WriteCSVTo and not WriteToAsCsv. So this reports the ambiguity and leaves the
// naming to whoever writes the second layer that wants a name, because a naming
// scheme designed against no real case is designed against nothing.
func claimed(sections []emit.Section, of policing, diags *diag.Set) {
	seen := make(map[string]bool)
	var twice []string

	for _, section := range sections {
		for _, decl := range section.Decls {
			on, name, is := methodOf(decl)
			if !is {
				continue
			}

			held := on + "." + name
			if seen[held] && !slices.Contains(twice, held) {
				twice = append(twice, held)
			}
			seen[held] = true
		}
	}

	slices.Sort(twice)
	for _, held := range twice {
		diags.Add(diag.New(codeTwoClaimants, of.at,
			"two layers of %s write %s", of.declared, held).
			WithHint("%s", "drop one of them, or write the method yourself — a method the author "+
				"declares is the one that is kept"))
	}
}

// redeclared reports two generated declarations that want one package-level
// name, against the position given.
//
// Asked of a whole build's worth of files rather than of one, because two
// generated names meet across files as readily as within one: what an element
// layer writes goes into the file a package shares, and what a container layer
// writes goes into the declaration's own.
//
// It is a package's question rather than a layer's, and no layer can answer it:
// a layer is handed one subject and names what it writes after that, so two
// layers over two subjects — or one layer over two subjects reached from two
// declarations — are the only place two names can meet. The names are built to
// keep them apart, and building is not proving: a name is a fold of a package
// and a type into one identifier, and a fold of two things into one has
// collisions somewhere however it is written.
//
// So the fold stays readable and this is what makes it safe. Reported rather
// than resolved, because renaming one of two things forge chose the names for
// would leave a caller unable to guess either — and the case is rare enough
// that being told to rename a type is a better answer than a scheme nobody can
// predict.
func redeclared(sections []emit.Section, at token.Position, diags *diag.Set) {
	seen := make(map[string]bool)
	var twice []string

	for _, section := range sections {
		for _, decl := range section.Decls {
			for _, name := range declares(decl) {
				if seen[name] && !slices.Contains(twice, name) {
					twice = append(twice, name)
				}
				seen[name] = true
			}
		}
	}

	slices.Sort(twice)
	for _, name := range twice {
		diags.Add(diag.New(codeNameTwice, at,
			"two of the declarations generated for this package are called %s", name).
			WithHint("%s", "a generated name is built from the type it is for and the package that "+
				"declares it, and two types have folded onto one — rename one of them"))
	}
}

// taken reports a generated declaration whose name the package already has.
//
// Never an overwrite. A file forge writes is a file forge owns, and a name in
// it that the author also declared is two declarations in one package: the
// build fails, and the failure names the generated file, which is the one place
// nobody can fix it.
func taken(sections []emit.Section, of policing, diags *diag.Set) {
	var found []string

	for _, section := range sections {
		for _, decl := range section.Decls {
			for _, name := range declares(decl) {
				if name == of.declared {
					// The declared type itself, which a spec declaration's
					// author writes under the tag and forge writes under its
					// complement. That is the arrangement rather than a clash.
					continue
				}
				if _, has := of.held.names[name]; has && !slices.Contains(found, name) {
					found = append(found, name)
				}
			}
		}
	}

	slices.Sort(found)
	for _, name := range found {
		diags.Add(diag.New(codeNameTaken, of.at,
			"generating %s writes %s, which the package already declares at %s",
			of.declared, name, of.held.names[name]).
			WithHint("%s", "rename what the package declares, or the declaration forge names it "+
				"after; forge will not write over something somebody else wrote"))
	}
}

// bound reports two import paths that each want the same name.
//
// Together is the point, and is why this is not among the things one layer is
// asked about on its own: a layer knows what it binds and cannot know what the
// layer beside it binds, so two paths that each want to be spelled `cmp` is a
// question only a stage holding every unit can ask. The emitter refuses one
// path bound to two names, which is the same mistake from the other side; this
// is the one it cannot see.
func bound(imports []emit.Import, of policing, diags *diag.Set) {
	paths := make(map[string]string, len(imports))
	var clashes []string

	for _, one := range imports {
		first, seen := paths[one.Name]
		if seen && first != one.Path && !slices.Contains(clashes, one.Name) {
			clashes = append(clashes, one.Name)
		}
		if !seen {
			paths[one.Name] = one.Path
		}
	}

	slices.Sort(clashes)
	for _, name := range clashes {
		diags.Add(diag.New(codePackageClash, of.at,
			"generating %s binds two packages to %s", of.declared, name).
			WithHint("%s", "this is a fault in the layers rather than in the declaration: each spells "+
				"its own types against what it imports and neither can see the other's; "+
				"report it with the declaration"))
	}
}

// methodOf returns the type a declaration is a method on and the method's name,
// and whether it is one.
//
// The receiver's type without its pointer, since a method on the pointer and
// one on the value are both methods of the type and neither may be declared
// twice.
func methodOf(decl ast.Decl) (on, name string, is bool) {
	fn, on, is := methodOn(decl)
	if !is {
		return "", "", false
	}
	return on, fn.Name.Name, true
}

// methodOn answers the same question and hands back the declaration itself,
// for a caller that goes on to read the signature.
//
// Worth having beside [methodOf] because the alternative is a second type
// assertion at the caller, on a value this one already decided the type of —
// and an assertion whose answer is known is one nobody checks.
func methodOn(decl ast.Decl) (fn *ast.FuncDecl, on string, is bool) {
	fn, ok := decl.(*ast.FuncDecl)
	if !ok || fn == nil || fn.Name == nil || fn.Recv == nil || len(fn.Recv.List) != 1 {
		return nil, "", false
	}

	held := fn.Recv.List[0].Type
	if star, indirect := held.(*ast.StarExpr); indirect {
		held = star.X
	}
	if index, generic := held.(*ast.IndexExpr); generic {
		held = index.X
	}

	ident, named := held.(*ast.Ident)
	if !named {
		return nil, "", false
	}
	return fn, ident.Name, true
}

// declares returns the package-level names a declaration introduces, which is
// none for a method: a method's name lives on its type rather than in the
// package.
func declares(decl ast.Decl) []string {
	switch typed := decl.(type) {
	case *ast.FuncDecl:
		if typed == nil || typed.Name == nil || typed.Recv != nil {
			return nil
		}
		return []string{typed.Name.Name}

	case *ast.GenDecl:
		if typed == nil || typed.Tok == token.IMPORT {
			return nil
		}

		var out []string
		for _, spec := range typed.Specs {
			out = append(out, specifies(spec)...)
		}
		return out

	default:
		return nil
	}
}

// specifies returns the names one specification of a grouped declaration
// introduces.
//
// The blank one is not among them. A file may declare _ as many times as it
// likes, so it is not a name anything can collide with.
func specifies(spec ast.Spec) []string {
	switch held := spec.(type) {
	case *ast.TypeSpec:
		if held.Name == nil {
			return nil
		}
		return []string{held.Name.Name}

	case *ast.ValueSpec:
		var out []string
		for _, name := range held.Names {
			if name != nil && name.Name != "_" {
				out = append(out, name.Name)
			}
		}
		return out

	default:
		return nil
	}
}
