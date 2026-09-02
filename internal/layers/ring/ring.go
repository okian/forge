package ring

import (
	_ "embed"
	"errors"
	"fmt"
	"go/ast"
	"go/token"
	"math"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/okian/forge/internal/diag"
	"github.com/okian/forge/internal/emit"
	"github.com/okian/forge/internal/layer"
	"github.com/okian/forge/internal/model"
	"github.com/okian/forge/internal/shape"
	"github.com/okian/forge/internal/templates"
)

// codeCapacityNotPositive reports a declared capacity that could hold nothing.
//
// A 3xxx, because it is about an option somebody wrote rather than about how
// the stack composes: the layer is fine and the number is not.
var codeCapacityNotPositive = diag.Register(3017, "declared capacity holds nothing")

// bodies is the template this layer emits, embedded from the package beside it.
//
// Embedded rather than quoted, so that what is emitted is a Go file the
// ordinary build compiles, the ordinary vet reads and this package's own tests
// exercise. A body that is only ever a string is a body nothing checks until
// somebody's generated file fails to build.
//
//go:embed tmpl/tmpl.go
var bodies []byte

// container is the type the template declares, and param the element it is
// generic over. They are the two names the rewrite is written in terms of, and
// they are written here rather than passed in because they are facts about the
// file beside this one and change only when it does.
const (
	container = "Ring"
	param     = "T"
)

// The names the template gives the declarations a run chooses between.
//
// Each pair is one option's two answers. The layer keeps one of each and drops
// the other, and what it keeps it renames to the name the contract uses — so a
// caller writes Push whichever policy the declaration chose, and finds out
// which by whether there is an error to handle.
const (
	constructorTaking = "New"
	constructorFixed  = "NewFixed"

	pushOverwriting = "Push"
	pushRefusing    = "PushChecked"

	appendOverwriting = "AppendSeq"
	appendRefusing    = "AppendSeqChecked"

	capacityConst = "fixedCap"
	fullError     = "errFull"
)

// The options this layer reads, and the policies the second of them names.
const (
	optionCap      = "cap"
	optionOverflow = "overflow"

	overflowOverwrite = "overwrite"
	overflowError     = "error"
)

// templateImports names every package the template imports, and what a file
// importing it binds that package to.
//
// Written down rather than read off the paths, because a path does not say what
// it binds: encoding/json/v2 binds json and math/rand/v2 binds rand, so taking
// the last element under-reports exactly the names most worth knowing. And
// under-reporting is the harmful direction — a name this does not mention is a
// name the subject is not moved out of the way of, which is the collision the
// spelling exists to prevent.
var templateImports = map[string]string{
	"errors": "errors",
	"iter":   "iter",
}

// taken returns what the template's imports bind, sorted so that a spelling
// built from them does not depend on a map.
func taken() []model.Import {
	out := make([]model.Import, 0, len(templateImports))
	for path, name := range templateImports {
		out = append(out, model.Import{Path: path, Name: name})
	}

	slices.SortFunc(out, func(a, b model.Import) int { return strings.Compare(a.Path, b.Path) })
	return out
}

// Layer generates fixed-capacity circular storage.
//
// It carries no state. What a declaration decides reaches it through the
// context it is asked to generate against, so one value serves every
// declaration in a run and there is nothing to reset between them.
type Layer struct{}

// New returns the ring storage layer.
func New() Layer { return Layer{} }

// Origin identifies the marker this layer claims.
func (Layer) Origin() model.TypeRef { return model.TypeRef{Pkg: model.MarkerPkg, Name: container} }

// Binds names what this layer's output imports, so that every layer of the
// stack spells its types against the same set.
//
// Every import the template has, including the one a given declaration may not
// keep. What this decides is which names the subject is moved out of the way
// of, and moving it out of the way of a name the file turns out not to bind
// costs nothing; not moving it out of the way of one the file does bind is a
// package imported twice under one name.
func (Layer) Binds() []model.Import { return taken() }

// Writes names nothing, because everything this layer writes is about the
// buffer rather than about what is in it.
func (Layer) Writes() []string { return nil }

// Kind says where in a stack the layer may appear.
func (Layer) Kind() model.Kind { return model.KindStorage }

// Stage says how far along the layer is.
func (Layer) Stage() layer.Stage { return layer.StageReady }

// Doc returns the one-line summary the list command prints.
func (Layer) Doc() string {
	return "fixed-capacity circular buffer, so a long-running producer cannot grow memory without bound"
}

// Transparent reports that the raw underlying type does not uphold this layer's
// invariants.
//
// The representation is a buffer, a head and a count, and the three are only
// meaningful together: a head past the end of the buffer, or a count larger
// than it, is a value every method reads wrongly. The language offers no way to
// stop somebody writing one, so the type is forge's rather than the author's,
// and a declaration over this storage belongs in a spec file.
func (Layer) Transparent() bool { return false }

// OptionSchema declares every option the layer accepts.
func (Layer) OptionSchema() []layer.OptionDef {
	return []layer.OptionDef{
		// Not required: a ring whose capacity is not declared takes it at
		// construction, which is what a caller sizing a buffer from
		// configuration needs.
		{
			Key: optionCap, Value: layer.ValueInt,
			Doc: "how many elements the buffer holds, when that is fixed at build time rather than passed to the constructor",
		},
		{
			Key: optionOverflow, Value: layer.ValueEnum,
			Values:  []string{overflowOverwrite, overflowError},
			Default: overflowOverwrite,
			Doc:     "what a push does when the buffer is full",
		},
	}
}

// Accepts reports whether the layer can sit on the shape beneath it.
//
// It always can. Storage is the bottom of a stack's representation: what is
// beneath it is the subject and whatever element layers attached to the
// subject, and none of that is something a container has to be able to do
// anything with.
func (Layer) Accepts(shape.Shape) error { return nil }

// Shape returns what the layer exposes to the layer above it.
//
// Bounded is the one that distinguishes it from the other storage: a layer
// above can ask whether what it sits on has a ceiling, and behave differently
// if it does. Indexed is not among them, because the elements are not where
// their positions say they are — reaching the third one means knowing where the
// oldest is, which is what All is for.
func (l Layer) Shape(ctx *layer.Context, below shape.Shape) shape.Shape {
	below.Caps = below.Caps.With(shape.Sized, shape.Ordered, shape.Streamable, shape.Bounded)
	return below.WithMethods(l.methods(ctx, below.Elem)...)
}

// methods is the surface this layer emits, described for the layers above it.
//
// It is written out rather than read back from the template, and it has to be:
// the template declares more than any declaration gets, so a surface derived
// from a parse would report both answers to every option as though a caller
// could reach either.
//
// What the options change is here rather than hidden, because it is the surface
// they change. A refusing container's Push returns an error and a layer above
// wrapping it has to know that before it writes the call.
func (l Layer) methods(ctx *layer.Context, elem model.TypeRef) []shape.Method {
	var (
		seq    = "iter.Seq[" + spellElem(elem) + "]"
		refuse = refusing(ctx)
		fails  = ""
	)
	if refuse {
		fails = " error"
	}

	out := make([]shape.Method, 0, 6)
	out = append(out,
		shape.Method{Name: "Cap", Signature: "() int", Owner: l.Origin(), Pointer: true, Doc: "how many elements the container can hold"},
		shape.Method{Name: "Len", Signature: "() int", Owner: l.Origin(), Pointer: true, Doc: "how many elements the container holds"},
		shape.Method{Name: "All", Signature: "() " + seq, Owner: l.Origin(), Pointer: true, Doc: "walks from the oldest element to the newest"},
		shape.Method{Name: "Backward", Signature: "() " + seq, Owner: l.Origin(), Pointer: true, Doc: "walks from the newest element to the oldest"},
		shape.Method{Name: "Reset", Signature: "()", Owner: l.Origin(), Pointer: true, Doc: "empties the container, keeping the buffer it was constructed with"})

	pushes := "adds an element, dropping the oldest to make room"
	appends := "adds every element a sequence yields, dropping older ones as it fills"
	if refuse {
		pushes = "adds an element, and reports that it did not when the container is full"
		appends = "adds every element a sequence yields, and stops at the first that does not fit"
	}

	return append(out,
		shape.Method{
			Name: pushOverwriting, Signature: "(v " + spellElem(elem) + ")" + fails,
			Owner: l.Origin(), Pointer: true, Doc: pushes,
		},
		shape.Method{
			Name: appendOverwriting, Signature: "(seq " + seq + ")" + fails,
			Owner: l.Origin(), Pointer: true, Doc: appends,
		})
}

// refusing reports whether this declaration asked to be told rather than to
// lose its oldest element.
//
// The default is the one the schema declares, and is applied here as well
// because a shape is asked for before anything has filled defaults in — a layer
// that read the option raw would describe an unwritten declaration as one
// policy and generate it as the other.
func refusing(ctx *layer.Context) bool {
	if ctx == nil {
		return false
	}

	held, written := ctx.Options.Get(optionOverflow)
	return written && held == overflowError
}

// spellElem names the element for a signature a person reads.
//
// The bare name rather than the qualified one: a shape is printed in a table
// beside the declaration it belongs to, where the package is already known. A
// stack whose subject could not be modelled has no element at all, and is
// spelled as the template spells it, which is the honest answer to a question
// with no answer yet.
func spellElem(elem model.TypeRef) string {
	if elem.Name == "" {
		return param
	}
	return elem.Name
}

// Generate returns the declarations this layer contributes.
func (l Layer) Generate(ctx *layer.Context, _ shape.Shape) (layer.Unit, error) {
	if ctx == nil || ctx.Model == nil || ctx.Model.Subject == nil {
		// Not a diagnostic: a diagnostic points at a declaration, and the
		// declaration is the thing that is missing. The pipeline never asks a
		// layer to generate for a model it does not have, so reaching here is
		// forge calling itself wrongly rather than anybody writing anything.
		return layer.Unit{}, errors.New("ring: asked to generate without a modelled declaration")
	}

	fixed, err := declaredCapacity(ctx)
	if err != nil {
		return layer.Unit{}, err
	}

	// Spelled against what the file will already bind, so that a subject from a
	// package called errors is written as something else rather than as a
	// second import under a name the template has. The whole stack's bindings
	// rather than this layer's, because the file is the whole stack's.
	subject := ctx.Model.SubjectSpelling(ctx.Bound())

	held := planned(ctx, fixed)

	out, diags := l.apply(ctx, subject, held)
	if refused := diags.Err(); refused != nil {
		return layer.Unit{}, refused
	}

	if wrong := accounted(out.Imports); wrong != "" {
		return layer.Unit{}, fmt.Errorf("ring: %s", wrong)
	}

	decls, err := chosen(out.Decls, held, ctx, fixed)
	if err != nil {
		return layer.Unit{}, err
	}

	return layer.Unit{
		Decls:    decls,
		Comments: out.Comments,
		Fset:     out.Fset,
		Imports:  append(emit.Reaching(decls, out.Imports), imported(subject)...),
	}, nil
}

// Constructor returns the function that makes one of these containers.
//
// A ring always has one. The buffer is allocated once and never grows, which is
// the whole of what the type is for, so a zero value has no buffer and can hold
// nothing — and adding to one says so rather than carrying on. A decorator
// holding a ring as a field therefore has to offer a way to make the ring, or
// what it holds is a container nobody can fill.
//
// It takes a size where the declaration did not write one and nothing where it
// did, which is the same pair of constructors the layer emits: a capacity in
// the declaration is part of the type, and one left out is the caller's.
//
// A capacity the option validator would refuse is reported as no constructor
// rather than as a wrong one. The refusal itself belongs to generation, where
// it points at the declaration; answering here with a signature built from a
// number this layer is about to reject would have a decorator above writing a
// call against it.
//
// Asked without a declaration it answers with none, for the reason [Layer.Shape]
// does: a caller asking that way is asking what the layer is rather than what
// it would emit here, and what a constructor is called is a fact about a
// declaration that has not been given.
func (Layer) Constructor(ctx *layer.Context) (layer.Constructor, bool) {
	if ctx == nil || ctx.Model == nil {
		return layer.Constructor{}, false
	}

	fixed, err := declaredCapacity(ctx)
	if err != nil {
		return layer.Constructor{}, false
	}

	out := layer.Constructor{Name: constructorFor(ctx.Declared()), Pointer: true}
	if fixed == 0 {
		out.Params, out.Args = []string{sizeParam + " int"}, []string{sizeParam}
	}

	return out, true
}

// sizeParam is what the constructor taking a capacity calls it, which is the
// name the template gives it and the name a decorator forwarding to it has to
// write.
const sizeParam = "size"

// declaredCapacity returns the capacity written for this declaration, and zero
// where none was.
//
// A number that is not positive is refused here rather than passed on. The
// option's kind only says it is a whole number, and a container declared to
// hold none of something is not a smaller container: it is one whose every push
// is a mistake, discovered at the first one rather than at the declaration that
// made it inevitable.
func declaredCapacity(ctx *layer.Context) (int, error) {
	written, ok := ctx.Options.Lookup(optionCap)
	if !ok {
		return 0, nil
	}

	size, err := strconv.Atoi(written.Value)
	if err != nil {
		// The option's own validation has already refused anything that is not
		// a number, so reaching here means it did not run.
		return 0, fmt.Errorf("ring: %s=%q is not a number", optionCap, written.Value)
	}

	// Reported at the option rather than at the directive it sits in, so the
	// caret lands under the number somebody wrote.
	switch {
	case size <= 0:
		return 0, diag.New(codeCapacityNotPositive, written.Pos,
			"%s=%d, and a container that holds nothing has nothing to be", optionCap, size).
			WithHint("%s", "write a positive capacity, or leave "+optionCap+
				" out and pass one to the constructor")

	// A constant larger than a 32-bit int is one the generated file cannot
	// hold: it is written into the output as an untyped constant assigned to an
	// int, so a package that builds where forge ran would not build for a
	// smaller word. Committed output has to compile everywhere the module does.
	case size > math.MaxInt32:
		return 0, diag.New(codeCapacityNotPositive, written.Pos,
			"%s=%d, which is more than an int holds on every platform this could be built for", optionCap, size).
			WithHint("%s", "write a capacity below "+strconv.Itoa(math.MaxInt32)+
				", or leave "+optionCap+" out and pass one to the constructor")
	}
	return size, nil
}

// plan is what one declaration's options make of the template: what to call the
// names it keeps, which declarations to leave out, and which of the ones it
// keeps to rename once the name is free.
//
// The three are one decision and are made together, because they have to agree.
// Naming a declaration that is then dropped leaves a name nothing carries;
// dropping one that was named leaves the file without it.
type plan struct {
	// names is what the rewrite is told, for the names it must not simply
	// prefix: a constructor and an error a caller writes out.
	names map[string]string

	// drop and rename are keyed by what a declaration is called *after* the
	// rewrite, since that is what the tree holds by the time they are read.
	drop   map[string]bool
	rename map[string]string
}

// planned decides what this declaration makes of the template.
func planned(ctx *layer.Context, fixed int) plan {
	declared := ctx.Declared()

	held := plan{
		names:  map[string]string{fullError: errorFor(declared)},
		drop:   map[string]bool{},
		rename: map[string]string{},
	}

	// Both constructors cannot carry one name into a file, and only one of them
	// reaches a file. The one that does is named here; the other keeps whatever
	// the prefix rule gives it, and is dropped before anything is written.
	kept, unused := constructorTaking, constructorFixed
	if fixed > 0 {
		kept, unused = constructorFixed, constructorTaking
	}
	held.names[kept] = constructorFor(declared)
	held.drop[held.spelled(unused, declared)] = true

	if fixed == 0 {
		held.drop[held.spelled(capacityConst, declared)] = true
	}

	// A method is not a package-level name, so the rewrite leaves it alone and
	// both halves of the pair arrive under the names the template gave them.
	if refusing(ctx) {
		held.drop[pushOverwriting], held.drop[appendOverwriting] = true, true
		held.rename[pushRefusing] = pushOverwriting
		held.rename[appendRefusing] = appendOverwriting
	} else {
		held.drop[pushRefusing], held.drop[appendRefusing] = true, true
		held.drop[held.spelled(fullError, declared)] = true
	}

	return held
}

// spelled returns what the rewrite will call a name the template declares.
//
// It is the rewrite's own rule rather than a guess at it: a name the plan gives
// an answer for becomes that answer, and every other package-level name takes
// the declaration's prefix. Working the name out this way rather than taking
// the prefix back off afterwards is what keeps the two from disagreeing about
// case — persons + FixedCap is one rule applied forwards, and nothing has to
// know that undoing it would have to lower a letter the template wrote upper.
func (p plan) spelled(name, declared string) string {
	if answer, asked := p.names[name]; asked {
		return answer
	}
	return lower(declared) + upper(name)
}

// apply specialises the template for one declaration.
//
// The names the plan carries are the ones the prefix rule must not touch: a
// constructor and an error a caller writes out. Everything else the template
// declares is a helper, and takes the declaration's prefix so that it cannot
// collide with something the author wrote.
func (Layer) apply(ctx *layer.Context, subject model.Spelling, held plan) (templates.Result, diag.Set) {
	return templates.Apply(
		templates.Template{Name: "ring", Source: bodies},
		templates.Rewrite{
			Param:     param,
			Subject:   subject.Text,
			Container: container,
			Declared:  ctx.Declared(),
			Names:     held.names,
			Prefix:    lower(ctx.Declared()),
		},
		ctx.Model.Pos)
}

// chosen returns the declarations this run keeps, with the ones it kept named
// the way the contract names them.
//
// The template writes every answer to every option, so that each is compiled
// and vetted by the ordinary build rather than living as a string nobody reads
// until it fails in somebody else's package. What a run emits is one answer per
// option, and the rest never reach a file.
//
// Renaming is the second half of it and cannot be done by the rewrite, which
// works on the names a package declares: a method is not one of those, and two
// methods cannot carry the name Push in a template that has to compile. So the
// kept one is renamed here, once the other is gone and the name is free.
func chosen(decls []ast.Decl, held plan, ctx *layer.Context, fixed int) ([]ast.Decl, error) {
	kept := make([]ast.Decl, 0, len(decls))

	for _, decl := range decls {
		name := declaredAs(decl)
		if held.drop[name] {
			continue
		}
		// Only a function is ever renamed: what the pairs are is two methods
		// and two constructors, and the constructors are named by the rewrite
		// rather than here. Anything else carrying one of those names is the
		// template having changed under this file.
		if to, asked := held.rename[name]; asked {
			fn, is := decl.(*ast.FuncDecl)
			if !is {
				return nil, fmt.Errorf("ring: %s is not a function, and this expects to rename one", name)
			}
			fn.Name = ast.NewIdent(to)
			redocument(fn.Doc, name, to)
		}
		kept = append(kept, decl)
	}

	// And everywhere the renamed methods are called, which is not the same
	// thing as where they are declared: one of them calls the other, and a
	// rename that moved only the declaration would leave a body calling a
	// method the file no longer has.
	calls(kept, held.rename)

	// Every option has two answers and each drops one of them, so a run that
	// dropped nothing chose nothing — which means a name this file expects the
	// template to declare is not the name the template declares any more.
	if len(kept) == len(decls) {
		return nil, fmt.Errorf("ring: nothing was left out of %s, so the template no longer declares what this expects", ctx.Declared())
	}

	if fixed > 0 {
		if err := size(kept, held.spelled(capacityConst, ctx.Declared()), fixed); err != nil {
			return nil, err
		}
	}
	return kept, nil
}

// redocument renames a declaration in the comment documenting it.
//
// A doc comment opens with the name of the thing it documents, which is the
// convention every Go reader and every documentation tool relies on, so a
// method renamed without its comment is one whose documentation is about a
// method that is not there. Only the opening word is touched: the rest of the
// comment is prose about what the method does, and what it does did not change.
func redocument(doc *ast.CommentGroup, from, to string) {
	if doc == nil || len(doc.List) == 0 {
		return
	}

	first, opening := doc.List[0], "// "+from
	if !strings.HasPrefix(first.Text, opening) {
		return
	}

	// The name and not merely its beginning. A comment opening with
	// PushCheckedTwice documents something else, and rewriting it would leave
	// prose about a method under a name that is not the one it describes.
	if rest := first.Text[len(opening):]; rest == "" || rest[0] == ' ' {
		first.Text = "// " + to + rest
	}
}

// calls renames the method calls in these declarations.
//
// Only what is selected on something: a method is reached through a receiver,
// so a bare identifier of the same name is a different thing entirely and is
// left alone. The receiver itself is not examined, because the only methods
// renamed here are this template's own and nothing in it calls a method of that
// name on anything else.
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

// declaredAs returns the one name a declaration introduces, or nothing where it
// introduces none or more than one.
//
// It is the name the tree holds now, after the rewrite: a package-level name is
// carrying the declaration's prefix or the answer the plan gave it, and a
// method is carrying what the template called it, because the prefix is for the
// names a generated file adds to somebody else's package and a method is not
// one of those.
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

// size writes the declared capacity into the constant that carries it.
//
// The rewrite renames declarations and does not change what they are equal to,
// and this is the one value in the template that a declaration decides. It is
// written as the number the author wrote, so that reading the generated file
// answers the question the directive asked without going back to the directive.
func size(decls []ast.Decl, named string, fixed int) error {
	for _, decl := range decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.CONST || declaredAs(decl) != named {
			continue
		}

		spec, is := gen.Specs[0].(*ast.ValueSpec)
		if !is || len(spec.Values) != 1 {
			return fmt.Errorf("ring: the template's %s is not one value, so %d has nowhere to go", named, fixed)
		}

		spec.Values[0] = &ast.BasicLit{
			ValuePos: spec.Values[0].Pos(),
			Kind:     token.INT,
			Value:    strconv.Itoa(fixed),
		}
		return nil
	}

	return fmt.Errorf("ring: a fixed capacity was declared and the template has no %s to put it in", named)
}

// accounted reports a template import nothing wrote down, or nothing.
//
// The subject is spelled before the template is read, against the names the
// list above says the file will bind. An import that is not in it is one the
// subject was not moved out of the way of, so the check is not bookkeeping: it
// is the thing that keeps the list from being a comment about a file that has
// since changed. It fails on the first run of this package's tests, which is
// where an import added to the template is cheapest to notice.
func accounted(imports []emit.Import) string {
	for _, one := range imports {
		if _, known := templateImports[one.Path]; !known {
			return "the template imports " + one.Path + ", which nothing recorded a bound name for"
		}
	}
	return ""
}

// imported returns a spelling's imports in the shape a unit carries.
func imported(spelled model.Spelling) []emit.Import {
	out := make([]emit.Import, 0, len(spelled.Imports))
	for _, one := range spelled.Imports {
		out = append(out, emit.Import{Path: one.Path, Name: one.Name, Aliased: one.Aliased})
	}
	return out
}

// constructorFor names the constructor after the type it builds, and errorFor
// names the refusal after the type that returns it. Both take the visibility of
// that type: neither has business being reachable from outside a package the
// type is unexported in.
func constructorFor(declared string) string { return exported(declared, "New", "") }

func errorFor(declared string) string { return exported(declared, "Err", "Full") }

// exported joins a name around the declaration, in the case the declaration has.
func exported(declared, before, after string) string {
	if first, _ := utf8.DecodeRuneInString(declared); unicode.IsUpper(first) {
		return before + declared + after
	}
	return lower(before) + upper(declared) + after
}

// lower returns a name with its first letter in lower case, and upper with it
// in upper case. Between them they turn a declared name into the prefix its
// helpers take and into the tail of the names built around it.
func lower(name string) string { return model.Lower(name) }

func upper(name string) string { return model.Upper(name) }
