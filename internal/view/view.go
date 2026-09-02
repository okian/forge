package view

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"slices"
	"strconv"
	"strings"

	"github.com/okian/forge/internal/emit"
	"github.com/okian/forge/internal/layer"
	"github.com/okian/forge/internal/model"
	"github.com/okian/forge/internal/shape"
)

// receiverName is what a view calls itself in its own methods.
const receiverName = "v"

// Asked is what writing a view needs to know.
type Asked struct {
	// Name is the view type's own name, and Doc the sentence written above it.
	Name string
	Doc  string

	// Held is the field the view keeps and Of is how that field's type is
	// written, which is whatever the decorator wraps.
	//
	// A field rather than an embedded type, because embedding would put every
	// method of what is wrapped onto the view whether this asked for it or not
	// — including the ones the decorator withdrew, which is the one thing a
	// view must not have.
	//
	// Of is a type the decorator has to arrange, and no layer produces one
	// today: a storage layer names its container after the declaration, so a
	// stack that is guarded has one type with the locked methods on it and
	// nothing underneath for a view to point at. A decorator that wants a view
	// has to have the stack beneath it emitted onto a type of its own — which
	// is that decorator's problem, and is why this takes the name rather than
	// working it out.
	Held string
	Of   string

	// Guards is the decorated type: what the decorator declares, and what a
	// caller reaches a scope through.
	//
	// A view must not mention it either. A method handing one back is a reach
	// from inside the scope to the value the scope was opened on —
	// v.Clone().Do(f) is a second scope opened through the first, and it never
	// mentions the view.
	//
	// Required rather than optional, because the generated type says in so many
	// words that no method on it names this — and a claim printed whether or not
	// it was checked is how the last version of this package came to promise
	// something it did not do.
	//
	// What is refused is the name anywhere in the signature rather than in the
	// result alone, which over-refuses: a Merge taking one, or an Equal
	// comparing against one, closes nothing, since a caller who can call it
	// already holds the thing. Narrowing it means deciding what a below-stack
	// surface actually spells, and that is settled by the arrangement whoever
	// wants a view has to build first.
	Guards string

	// Surface is the stack beneath the decorator, which is what the view
	// forwards to.
	//
	// Beneath, which is what nothing here can check: a surface that has already
	// had the decorator's withdrawals taken off it produces a view that cannot
	// iterate, which is a scope with no reason to exist and no complaint about
	// it. What that surface should hold is the decorator's own knowledge — it
	// withdrew those methods and can see whether the view kept them — so the
	// check belongs to whoever calls this rather than here.
	//
	// In practice a decorator that hands out a scope needs the walk for its own
	// reasons — a snapshot is one collected — so the check it already makes for
	// itself is the check this wants, and there is nothing left over for a
	// second one to catch.
	Surface []shape.Method

	// Imports are what the forwarded signatures may name.
	//
	// May, rather than do: what is written is decided per method, so a caller
	// who hands over everything the stack below imports is handing over more
	// than the view will name. What is not named is dropped, because an import
	// nothing in a file names is not a warning in Go but a file that does not
	// build.
	Imports []model.Import
}

// Write returns the view type and the methods that forward to what it wraps.
//
// Every method of the surface, with the signature it already has: a view whose
// methods differed from the ones they forward to would be a second API to
// learn, for no gain over the first.
//
// Every one of them on the value, whatever receiver the method below takes.
// That is not a copy of what is below and is not meant to be — a decorator
// hands its view to a function, and a decorator offering read access hands a
// value rather than a pointer to one. A view with any method on its own pointer
// would have a method set that a value of it did not, so the read scope would
// be missing whichever methods the stack below happened to declare on a
// pointer, for a reason that has nothing to do with reading.
//
// Nothing is lost by it. The view reaches what it wraps through a pointer, so a
// method declared on the pointer below is reachable from a value of the view
// exactly as one declared on the value is.
func Write(of Asked) (layer.Unit, error) {
	held, err := Source(of)
	if err != nil {
		return layer.Unit{}, err
	}

	return assembled(held, of)
}

// Source returns what Write would emit, as the text it is assembled from.
//
// For a caller that has declarations of its own to write beside it. Two parses
// cannot share one file set, and a comment is placed by position — so a caller
// that took the declarations and put them next to its own would be printing
// both against one set and finding the view's comments wherever its own
// positions happened to land. Text composes; syntax with positions in it does
// not.
//
// One check [Write] makes is not made here, and cannot be. A view whose
// signatures name a package nothing imported is a file that does not build —
// which is true of the unit [Write] produces, since that unit is the whole of
// what is written. It is not true of text somebody merges: a view forwards the
// methods of the layers beneath it, those layers emit those methods into the
// same file, and the import came in with them. Refusing here would refuse a
// view whose every name the file does bind.
//
// What that leaves is a caller who merges and has no way to check. Closing it
// needs a surface that can say what its signatures name — a method carries the
// text of its signature and nothing about where the types in it come from — and
// that is a change to what a layer describes rather than to what this writes.
func Source(of Asked) (string, error) {
	if of.Name == "" || of.Held == "" || of.Of == "" || of.Guards == "" {
		return "", fmt.Errorf(
			"a view needs a name, a field, a type and the type it guards, and was asked "+
				"for %q, %q, %q and %q",
			of.Name, of.Held, of.Of, of.Guards)
	}

	for _, one := range of.Imports {
		if one.Name == "" {
			return "", fmt.Errorf(
				"%s was given %s with no name to bind it to, and a view writes what it "+
					"imports by name", of.Name, one.Path)
		}
	}

	w := &strings.Builder{}

	declare(w, of)

	for _, one := range of.Surface {
		if err := forward(w, of, one); err != nil {
			return "", err
		}
	}

	return w.String(), nil
}

// importing returns what the view was given to import, as the emitter holds an
// import.
func importing(of Asked) []emit.Import {
	return slices.Clone(of.Imports)
}

// reaching returns the name a signature mentions that a view must not, or
// nothing.
//
// Two names: the view's own and the decorated type's. A method mentioning
// either is a way out of the scope — one hands back a second view, the other
// hands back the thing the scope was opened on, and a caller with either can
// open a scope inside the one they are in.
//
// Read off the signature rather than from a list the caller supplies, because a
// list is a thing to forget and a scope-opener is not always called what one
// would be called. It is a match on the identifier and not on the type, which
// makes it wide in one direction and narrow in the other: a package of somebody
// else's declaring a type of the same name is refused for a reason that is not
// theirs, and a scope reached through an interface, an alias or a generic
// spelling is not refused at all. Both are worth the trade — this is the cheap
// guard against handing over the wrong surface, not a proof about what a view
// can reach.
func reaching(held ast.Node, names ...string) string {
	var found string

	ast.Inspect(held, func(node ast.Node) bool {
		ident, is := node.(*ast.Ident)
		if is && found == "" && slices.Contains(names, ident.Name) {
			found = ident.Name
		}
		return found == ""
	})

	return found
}

// declare writes the view's own type.
func declare(w *strings.Builder, of Asked) {
	w.WriteString("// " + of.Name + " " + of.Doc + "\n")
	w.WriteString("//\n")
	w.WriteString("// No method on it names this type or the one it guards, so the call that\n")
	w.WriteString("// deadlocks by accident — reaching for the value you were just handed and\n")
	w.WriteString("// finding a way back in — cannot be written through this.\n")
	w.WriteString("//\n")
	w.WriteString("// Four things that leaves you, and every one of them is ordinary Go.\n")
	w.WriteString("//\n")
	w.WriteString("// The value the scope was opened on is still in scope inside the call, so\n")
	w.WriteString("// calling a method on that deadlocks as it always would. Nothing here can\n")
	w.WriteString("// hold a value inside the call it came from, so this kept past the call —\n")
	w.WriteString("// or a sequence taken from it and walked afterwards — reaches the same data\n")
	w.WriteString("// with nothing held. A way out spelled some other way, through an\n")
	w.WriteString("// interface or an alias, is a name this cannot recognise. And this type\n")
	w.WriteString("// is declared in your own package, so its field is reachable by hand and\n")
	w.WriteString("// its zero value exists — neither of which any arrangement of methods can\n")
	w.WriteString("// prevent.\n")
	w.WriteString("type " + of.Name + " struct {\n")
	w.WriteString("\t" + of.Held + " *" + of.Of + "\n")
	w.WriteString("}\n\n")
}

// forward writes one method of the view, which calls the same method on what it
// wraps.
func forward(w *strings.Builder, of Asked, one shape.Method) error {
	params, results, err := one.Rendered()
	if err != nil {
		return fmt.Errorf("%s cannot forward %s: %w", of.Name, one.Name, err)
	}

	// Read again rather than from the rendering, because what is looked for is
	// an identifier anywhere in the signature and a rendering is a list of
	// types. The parse cannot fail here, having just succeeded above.
	signature, _ := parser.ParseExpr("func" + one.Signature)

	if named := reaching(signature, of.Name, of.Guards); named != "" {
		return fmt.Errorf(
			"%s would forward %s, whose signature names %s: a view may not mention itself "+
				"or the type it guards, since a caller holding one could open a second scope "+
				"inside the first. Either the surface came from above the decorator rather "+
				"than below it, or %s is a name something else in this package already has",
			of.Name, one.Name, named, named)
	}

	w.WriteString("// " + one.Name + " " + docOf(one) + "\n")
	w.WriteString("func (" + receiverName + " " + of.Name + ") " + one.Name +
		"(" + named(params) + ")" + returning(results) + " {\n")

	call := receiverName + "." + of.Held + "." + one.Name + "(" + passed(params) + ")"
	if len(results) == 0 {
		w.WriteString("\t" + call + "\n}\n\n")
		return nil
	}

	w.WriteString("\treturn " + call + "\n}\n\n")
	return nil
}

// docOf returns the sentence written above a forwarded method.
//
// The surface's own where a layer wrote one, since it is the description of the
// method a reader wants and it is already written down. A method with none gets
// a sentence saying where it came from, which is more use than nothing and is
// what a reader would go and look up.
func docOf(one shape.Method) string {
	if one.Doc == "" {
		return "is the method of the same name on the stack below, reached through the view."
	}

	// Every line of it prefixed, not just the first. A surface's doc is
	// documented as one line and every layer writes one, but a doc assembled
	// from an option or a field name is a line somebody else decides the length
	// of — and a second line without the marker turns the whole unit into
	// something that does not parse, reported as forge's own mistake.
	held := strings.Split(one.Doc, "\n")
	for i := 1; i < len(held); i++ {
		held[i] = "// " + held[i]
	}

	return strings.Join(held, "\n") +
		"\n//\n// It is the same method the stack below declares, reached through the view."
}

// named returns the parameter list with a name per parameter, since a forwarded
// call has to pass them on.
func named(params []string) string {
	out := make([]string, 0, len(params))
	for i, one := range params {
		out = append(out, argument(i)+" "+one)
	}
	return strings.Join(out, ", ")
}

// passed returns the arguments the forwarded call hands over.
func passed(params []string) string {
	out := make([]string, 0, len(params))
	for i, one := range params {
		held := argument(i)
		if strings.HasPrefix(one, "...") {
			held += "..."
		}
		out = append(out, held)
	}
	return strings.Join(out, ", ")
}

// argument names one parameter of a forwarded method.
//
// Positionally rather than from the surface, because a surface writes a
// signature for a person to read and the names in one are not guaranteed to be
// there, to be distinct, or to be usable as identifiers.
func argument(at int) string { return "a" + strconv.Itoa(at) }

// returning writes the result list as a signature carries it.
func returning(results []string) string {
	switch len(results) {
	case 0:
		return ""
	case 1:
		return " " + results[0]
	default:
		return " (" + strings.Join(results, ", ") + ")"
	}
}

// assembled reads the written source back as declarations, and refuses a unit
// that names a package it does not carry.
//
// The check belongs here rather than beside the writing, because here is where
// the answer is knowable: what this returns is a whole unit, so what it names
// and what it imports are the same file's two halves. [Source] hands back a
// fragment somebody else will merge, and the file it lands in binds what every
// layer in it asked for.
func assembled(source string, of Asked) (layer.Unit, error) {
	fset := token.NewFileSet()

	file, err := parser.ParseFile(fset, "view.go", "package forge\n\n"+source, parser.ParseComments)
	if err != nil {
		return layer.Unit{}, fmt.Errorf("what was written for %s is not valid Go: %w", of.Name, err)
	}

	held := importing(of)

	if name, loose := unbound(file.Decls, held); loose {
		return layer.Unit{}, fmt.Errorf(
			"%s names %s and nothing given to it binds those: a view writes the "+
				"signatures the stack below declares, so it has to be given what those "+
				"signatures name", of.Name, name)
	}

	return layer.Unit{
		Decls:    file.Decls,
		Comments: file.Comments,
		Fset:     fset,
		Imports:  emit.Reaching(file.Decls, held),
	}, nil
}

// unbound returns a package name the written methods use that nothing imports,
// and whether there is one.
//
// The other half of pruning, and the half that fails loudly rather than
// quietly. Dropping an import nothing names keeps a file from holding one it
// cannot use; this keeps it from naming one it does not have — which is the
// same mistake from the other end and the more likely one, since what a view
// names is decided by the surface it was handed and what it imports is decided
// by whoever handed it over.
//
// The receiver is not a package. It is the one name in these bodies that is
// qualified and local, so it is the one exception rather than a class of them.
func unbound(decls []ast.Decl, held []emit.Import) (string, bool) {
	bound := make(map[string]bool, len(held)+1)
	bound[receiverName] = true

	for _, one := range held {
		bound[one.Name] = true
	}

	loose := make([]string, 0, len(bound))
	for name := range emit.Qualifiers(decls) {
		if !bound[name] {
			loose = append(loose, name)
		}
	}

	if len(loose) == 0 {
		return "", false
	}

	// Sorted, because a map is walked in whatever order it feels like and this
	// ends up in a message: two names missing would be reported as whichever
	// one the map offered first, so the same mistake would read differently
	// from one run to the next.
	slices.Sort(loose)

	return strings.Join(loose, ", "), true
}

// Named returns what the view over a declaration is called by default.
//
// After the declaration rather than after the decorator, because that is what a
// caller has in hand: they wrote Sessions and they are handed a SessionsView,
// and a name taken from the layer would make two declarations over one layer
// share it.
//
// By default, because a name a generator picks is a name a package may already
// have. A decorator that offers an option to rename it passes the answer to
// [Asked.Name] and never calls this — the same arrangement the collection uses
// for the view it names after a declaration, which is where the collision that
// makes the option necessary is reported.
func Named(declared string) string { return declared + "View" }
