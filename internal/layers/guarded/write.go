package guarded

import (
	"fmt"
	"go/parser"
	"go/token"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/okian/forge/internal/emit"
	"github.com/okian/forge/internal/layer"
	"github.com/okian/forge/internal/model"
	"github.com/okian/forge/internal/shape"
	"github.com/okian/forge/internal/view"
)

// The names the generated code gives itself.
const (
	// lockField is the lock, and heldField what it guards.
	lockField = "mu"
	heldField = "held"

	// receiverName is what the guarded type calls itself in its own methods.
	receiverName = "g"
)

// Generate returns the declarations this layer contributes.
func (l Layer) Generate(ctx *layer.Context, below shape.Shape) (layer.Unit, error) {
	if ctx == nil || ctx.Model == nil || ctx.Model.Subject == nil {
		// Not a diagnostic: a diagnostic points at a declaration, and a call
		// with no declaration in it has none to point at.
		return layer.Unit{}, fmt.Errorf("%s: asked to generate against no declaration", container)
	}

	// A surface spells its element bare, and the view forwards those spellings
	// as they are written. That is right for a subject the file can name that
	// way and wrong for one it cannot — so the one it cannot is refused here,
	// rather than written out as a signature naming a type this package has no
	// name for.
	//
	// Asked as whether a method could be attached to the subject, which is the
	// question of whether it is declared in this package. Whether it is in this
	// module is a different question with a different answer: a subject two
	// packages over is one forge can generate for and is not one this file can
	// write down bare, and a check that asked the looser question would pass it
	// through and emit a scope forwarding a name nothing here declares.
	if !ctx.Model.Subject.Attachable(local(ctx)) {
		return layer.Unit{}, fmt.Errorf(
			"%s: %s holds a subject the package being generated into cannot name, and the "+
				"methods a scope forwards are written with the name the stack below uses",
			container, ctx.Model.Name)
	}

	held := plan{
		declared: ctx.Declared(),
		inner:    l.Encloses(ctx.Declared()),
		view:     viewName(ctx),
		elem:     elem(ctx),
		below:    below,
		sized:    below.Caps.Has(shape.Sized),
		locker:   locker(ctx),
		locked:   lockedWrite(ctx),
		encodes:  encodes(below),
		holds:    holds(ctx),
	}

	if err := offered(held); err != nil {
		return layer.Unit{}, err
	}

	scope, err := scoped(held)
	if err != nil {
		return layer.Unit{}, err
	}

	w := &strings.Builder{}
	held.declare(w)

	if held.holds != nil {
		held.making(w)
	}

	held.scopes(w)
	held.copying(w)

	if held.sized {
		held.counting(w)
	}
	if held.locker {
		held.exposing(w)
	}
	if held.encodes {
		held.encoding(w)
	}

	// The scope's own declarations go in the same text rather than beside the
	// parsed ones. Two parses cannot share a file set, a comment is placed by
	// position, and a unit carries one set — so declarations from two parses in
	// one unit would print the second lot's comments wherever the first lot's
	// positions happened to fall.
	w.WriteString(scope)

	return assembled(w.String(), held)
}

// plan is what the writing needs to know, worked out once.
type plan struct {
	// declared is the type the lock is on, inner what it holds, view what a
	// scope hands over, and elem what a snapshot is a slice of.
	declared string
	inner    string
	view     string
	elem     string

	// below is the stack beneath the lock, which is what the view forwards to.
	below shape.Shape

	// holds is how the container beneath the lock is made, and is nothing where
	// its zero value is already one.
	holds *layer.Constructor

	// sized records that the stack beneath can be counted, locker that the
	// author asked for the lock itself, locked that they asked for encoding to
	// hold it rather than copy first, and encodes that the elements have a
	// codec for the container's own to be written out of.
	sized   bool
	locker  bool
	locked  bool
	encodes bool
}

// offered checks that the stack beneath declares the methods this layer is
// about to write against, in the shape it writes them in.
//
// A capability says what a stack can do and a method says how it is spelled,
// and everything here is written by name: a snapshot collects a walk, a count
// forwards a count. Composition already refused a stack that cannot be walked,
// so what is left is a stack whose walk is not the walk — which is a layer
// beneath disagreeing with the contract rather than an author writing anything,
// and is worth saying so before it becomes a file that does not compile.
func offered(held plan) error {
	walk, walks := held.below.Method(walkMethod)
	if !walks {
		return missing(held, walkMethod, "a snapshot is a walk collected and a scope is a walk lent out")
	}
	if err := walked(held, walk); err != nil {
		return err
	}

	if !held.sized {
		return nil
	}

	count, counts := held.below.Method(length)
	if !counts {
		return missing(held, length, "a count behind a lock is the count beneath it")
	}
	return counted(held, count)
}

// walked checks the method a snapshot is collected from and a scope forwards.
//
// Matched whole, against the declaration's own element. A snapshot is written
// as a slice of the subject and filled by collecting this walk, so a walk over
// anything else is a snapshot whose two halves disagree — and the disagreement
// would arrive as a generated file that does not compile rather than as a
// complaint about the layer that caused it.
//
// The element is spelled bare on both sides: a surface writes it that way, and
// this layer refuses a subject the package cannot write that way at all.
func walked(held plan, one shape.Method) error {
	want := sequenceOpens + held.elem + "]"

	params, results, err := one.Rendered()
	if err != nil || len(params) != 0 || len(results) != 1 || results[0] != want {
		return notTheContract(held, one, walkMethod+"() "+want)
	}
	return nil
}

// counted checks the method a count is forwarded to, which is matched whole:
// there is one spelling of a count, and int64 is not it.
func counted(held plan, one shape.Method) error {
	params, results, err := one.Rendered()
	if err != nil || len(params) != 0 || len(results) != 1 || results[0] != countResult {
		return notTheContract(held, one, length+"() "+countResult)
	}
	return nil
}

// missing reports a stack that does not offer a method a lock is written over.
func missing(held plan, name, because string) error {
	return fmt.Errorf("%s: %s is %s and offers no %s, and %s",
		container, held.declared, held.below.Caps, name, because)
}

// notTheContract reports a method offered under a name a lock writes over, in a
// shape it cannot write over.
func notTheContract(held plan, one shape.Method, want string) error {
	return fmt.Errorf(
		"%s: %s cannot be locked: the %s layer offers %s%s, and a lock is written over %s",
		container, held.declared, one.Owner.Name, one.Name, one.Signature, want)
}

// scoped writes the value a scope hands over.
//
// Its own package rather than this one, because what it is is not a fact about
// locks: a decorator that owns anything a caller must not hold needs the same
// value, and the reasoning about what such a value may and may not have is the
// same reasoning wherever it is written.
//
// What that package cannot check is whether the scope kept what the lock took
// away. It is handed a surface and has no way of telling whether the methods
// missing from it were taken off by this layer or were never there — and a
// scope that cannot walk is a scope with no reason to exist and no complaint
// about it. This layer can tell, and [offered] is where it does: the walk it
// insists on because a snapshot is collected from one is the same walk the
// scope forwards, so both halves of that question are answered by one check
// rather than by two that could disagree.
func scoped(held plan) (string, error) {
	made, err := view.Source(view.Asked{
		Name:    held.view,
		Doc:     "is what a scope over " + held.declared + " hands the function it runs.",
		Held:    heldField,
		Of:      held.inner,
		Guards:  held.declared,
		Surface: held.below.Surface,
		Imports: naming(),
	})
	if err != nil {
		return "", fmt.Errorf("%s: %w", container, err)
	}

	return made, nil
}

// declare writes the guarded type itself.
func (p plan) declare(w *strings.Builder) {
	w.WriteString("// " + p.declared + " is " + p.elem + " behind a read-write lock.\n")
	w.WriteString("//\n")
	w.WriteString("// Everything the stack below offers is on " + p.inner + ", which this holds\n")
	w.WriteString("// and nothing else can reach. What reaches it is a scope: " + writeScope + " under the\n")
	w.WriteString("// write lock, " + readScope + " under the read lock, each handed a " + p.view + " for\n")
	w.WriteString("// as long as the call lasts.\n")
	w.WriteString("//\n")
	for _, line := range p.ready() {
		w.WriteString("// " + line + "\n")
	}
	w.WriteString("//\n")
	w.WriteString("// The lock must not be copied once it has been used, which is why every\n")
	w.WriteString("// method here is on the pointer.\n")
	w.WriteString("type " + p.declared + " struct {\n")
	w.WriteString("\t" + lockField + "   sync.RWMutex\n")
	w.WriteString("\t" + heldField + " " + p.inner + "\n")
	w.WriteString("}\n\n")
}

// making writes the way in for a lock over a container whose zero value is not
// one.
//
// The lock does not decide how what it holds is made, and it does not have to:
// a bounded container is told how many elements it holds and an unbounded one
// has nothing to be told, and the layer that declared the container answered
// that question before this ran. What is written here is a call forwarding to
// that answer, with the parameters it takes, so that a caller outside the
// package can make one — which they otherwise could not, since the container
// and its own constructor are unexported.
func (p plan) making(w *strings.Builder) {
	name := constructorFor(p.declared)

	w.WriteString("// " + name + " returns a lock around a new " + p.inner + ".\n")
	w.WriteString("//\n")
	w.WriteString("// The container beneath a lock is made rather than declared — it has to be\n")
	w.WriteString("// told how much it holds — so this is the way in, and the zero value of\n")
	w.WriteString("// this type is a lock around a container that will refuse every element.\n")
	w.WriteString("//\n")
	w.WriteString("// What is made here is reachable through this lock and through nothing\n")
	w.WriteString("// else, which is the whole of what the lock is for.\n")
	w.WriteString("func " + name + "(" + p.holds.Signature() + ") *" + p.declared + " {\n")
	w.WriteString("\treturn &" + p.declared + "{" + heldField + ": " + p.made() + "}\n")
	w.WriteString("}\n\n")
}

// made returns the call that builds what the lock holds, as a value: the lock
// holds a container rather than a pointer to one, so a constructor answering
// with a pointer is dereferenced here.
func (p plan) made() string {
	if p.holds.Pointer {
		return "*" + p.holds.Call()
	}
	return p.holds.Call()
}

// constructorFor names the constructor after the type it builds, and gives it
// the visibility of that type: a constructor for an unexported container has no
// business being reachable from outside the package it is unexported in.
func constructorFor(declared string) string {
	if first, _ := utf8.DecodeRuneInString(declared); unicode.IsUpper(first) {
		return "New" + declared
	}
	return "new" + model.Upper(declared)
}

// ready says what the zero value of the declared type is good for, which is
// decided by the container beneath it rather than by the lock.
func (p plan) ready() []string {
	if p.holds != nil {
		return []string{
			"The zero value holds a container that was never made and can hold",
			"nothing, so use " + constructorFor(p.declared) + ".",
		}
	}
	return []string{
		"The zero value is ready to use, because the zero value of what it holds",
		"is.",
	}
}

// scopes writes the two ways in.
func (p plan) scopes(w *strings.Builder) {
	w.WriteString("// " + writeScope + " runs f with the write lock held.\n")
	w.WriteString("//\n")
	w.WriteString("// Everything beneath the lock is reachable through the view for as long as\n")
	w.WriteString("// the call lasts, and no longer: a view kept past it reaches the same data\n")
	w.WriteString("// with nothing held. The lock is not reentrant, so a call back into this\n")
	w.WriteString("// value from inside f deadlocks — which the view cannot be used to write,\n")
	w.WriteString("// and the value f closed over still can.\n")
	w.WriteString("func (" + receiverName + " *" + p.declared + ") " + writeScope +
		"(f func(v " + p.view + ")) {\n")
	w.WriteString("\t" + receiverName + "." + lockField + ".Lock()\n")
	w.WriteString("\tdefer " + receiverName + "." + lockField + ".Unlock()\n\n")
	w.WriteString("\tf(" + p.view + "{" + heldField + ": &" + receiverName + "." + heldField + "})\n")
	w.WriteString("}\n\n")

	w.WriteString("// " + readScope + " runs f with the read lock held.\n")
	w.WriteString("//\n")
	w.WriteString("// Several readers run at once and a writer waits for all of them, so a long\n")
	w.WriteString("// read is a writer held up.\n")
	w.WriteString("//\n")
	w.WriteString("// What f is handed reaches the whole of what is below, including whatever\n")
	w.WriteString("// changes it — the read lock is what this holds and not what the view\n")
	w.WriteString("// enforces. Changing anything through it is a data race against the other\n")
	w.WriteString("// readers this let in, and " + writeScope + " is what to use instead. Nothing here\n")
	w.WriteString("// stops it: telling a method that reads from one that writes needs\n")
	w.WriteString("// something the layers do not currently say about themselves.\n")
	w.WriteString("//\n")
	w.WriteString("// Nor is a read lock reentrant, which is the less obvious half: a second\n")
	w.WriteString("// RLock taken while a writer is waiting blocks behind that writer, so\n")
	w.WriteString("// " + length + ", " + snapshot + " and the codec called from inside f deadlock under\n")
	w.WriteString("// contention and pass every test that runs without it. " + writeScope + " from inside f\n")
	w.WriteString("// deadlocks outright, since it waits for the read lock f is holding.\n")
	w.WriteString("// Read what you need through the view.\n")
	w.WriteString("func (" + receiverName + " *" + p.declared + ") " + readScope +
		"(f func(v " + p.view + ")) {\n")
	w.WriteString("\t" + receiverName + "." + lockField + ".RLock()\n")
	w.WriteString("\tdefer " + receiverName + "." + lockField + ".RUnlock()\n\n")
	w.WriteString("\tf(" + p.view + "{" + heldField + ": &" + receiverName + "." + heldField + "})\n")
	w.WriteString("}\n\n")
}

// copying writes the snapshot.
func (p plan) copying(w *strings.Builder) {
	w.WriteString("// " + snapshot + " returns the elements, copied under the read lock.\n")
	w.WriteString("//\n")
	w.WriteString("// The slice is nobody else's, so it can be walked, sorted and held with\n")
	w.WriteString("// nothing locked. It costs a copy of the elements, which is the price of\n")
	w.WriteString("// walking something a writer may be changing — and the alternative is not\n")
	w.WriteString("// a cheaper walk but a walk that races.\n")
	w.WriteString("//\n")
	w.WriteString("// The elements are copied the way Go copies anything, which is to say\n")
	w.WriteString("// shallowly: a field that is a slice, a map or a pointer still refers to\n")
	w.WriteString("// what the element in the container refers to. Writing through one of\n")
	w.WriteString("// those from here is a write to memory another goroutine reads under the\n")
	w.WriteString("// lock, which the lock cannot see and will not stop. Treat what comes\n")
	w.WriteString("// back as readable rather than as yours.\n")

	if p.sized {
		w.WriteString("//\n")
		w.WriteString("// One allocation, because the container can say how much it holds before\n")
		w.WriteString("// the walk starts. A copy that grew as it went would allocate once per\n")
		w.WriteString("// doubling and copy what it had each time — which is what collecting a\n")
		w.WriteString("// sequence of unknown length has to do, and is not what this is.\n")
	}

	w.WriteString("func (" + receiverName + " *" + p.declared + ") " + snapshot + "() []" + p.elem + " {\n")
	w.WriteString("\t" + receiverName + "." + lockField + ".RLock()\n")
	w.WriteString("\tdefer " + receiverName + "." + lockField + ".RUnlock()\n\n")

	if p.sized {
		w.WriteString("\treturn slices.AppendSeq(\n")
		w.WriteString("\t\tmake([]" + p.elem + ", 0, " + receiverName + "." + heldField + "." + length + "()),\n")
		w.WriteString("\t\t" + receiverName + "." + heldField + "." + walkMethod + "())\n")
	} else {
		w.WriteString("\treturn slices.Collect(" + receiverName + "." + heldField + "." + walkMethod + "())\n")
	}

	w.WriteString("}\n\n")
}

// counting writes the length.
func (p plan) counting(w *strings.Builder) {
	w.WriteString("// " + length + " returns how many elements there are.\n")
	w.WriteString("//\n")
	w.WriteString("// Reached directly rather than through a scope, because it is one number\n")
	w.WriteString("// read and handed back: there is nothing a caller can hold open, and asking\n")
	w.WriteString("// them to write a closure for it would teach them that scopes are ceremony.\n")
	w.WriteString("//\n")
	w.WriteString("// It is a fact about the past by the time it is read, like every count of\n")
	w.WriteString("// something another goroutine may be changing.\n")
	w.WriteString("func (" + receiverName + " *" + p.declared + ") " + length + "() int {\n")
	w.WriteString("\t" + receiverName + "." + lockField + ".RLock()\n")
	w.WriteString("\tdefer " + receiverName + "." + lockField + ".RUnlock()\n\n")
	w.WriteString("\treturn " + receiverName + "." + heldField + "." + length + "()\n")
	w.WriteString("}\n\n")
}

// exposing writes the lock itself, for a declaration that asked for it.
func (p plan) exposing(w *strings.Builder) {
	w.WriteString("// Lock and Unlock take and release the write lock, making this a\n")
	w.WriteString("// sync.Locker.\n")
	w.WriteString("//\n")
	w.WriteString("// Written because the declaration asked for them. They are what the rest of\n")
	w.WriteString("// this type exists to make unnecessary: a caller holding the lock directly\n")
	w.WriteString("// can reach nothing the lock guards — everything beneath it is behind a\n")
	w.WriteString("// scope, and a scope takes the lock itself.\n")
	w.WriteString("//\n")
	w.WriteString("// Which is the same sentence read the other way round: every method that\n")
	w.WriteString("// reaches the data takes the lock, so calling one while holding it\n")
	w.WriteString("// deadlocks. What these are for is handing this value to something that\n")
	w.WriteString("// asks for a sync.Locker and does not otherwise touch it — a sync.Cond is\n")
	w.WriteString("// the case worth having them for — and not for reaching the data.\n")
	w.WriteString("func (" + receiverName + " *" + p.declared + ") Lock() { " +
		receiverName + "." + lockField + ".Lock() }\n\n")
	w.WriteString("// Unlock releases the write lock.\n")
	w.WriteString("func (" + receiverName + " *" + p.declared + ") Unlock() { " +
		receiverName + "." + lockField + ".Unlock() }\n\n")
}

// assembled reads the written declarations back.
func assembled(source string, held plan) (layer.Unit, error) {
	fset := token.NewFileSet()

	file, err := parser.ParseFile(fset, "guarded.go", "package forge\n\n"+source, parser.ParseComments)
	if err != nil {
		return layer.Unit{}, fmt.Errorf("%s: what was written for %s is not valid Go: %w",
			container, held.declared, err)
	}

	return layer.Unit{
		Decls:    file.Decls,
		Comments: file.Comments,
		Fset:     fset,
		Imports:  emit.Reaching(file.Decls, imports()),
	}, nil
}

// imports names what the generated code may reach for.
//
// Widely, and narrowed by what turns out to be written: the lock's own two, the
// codec's where there is a codec, and whatever the signatures the view forwards
// happen to name. Which of those the file ends up with is not decidable before
// the declarations exist, so the list is the union and the pruning is done
// afterwards.
func imports() []emit.Import {
	held := naming()

	out := make([]emit.Import, 0, len(held)+3)
	out = append(out,
		emit.Import{Path: stdSync.Path, Name: stdSync.Name},
		emit.Import{Path: stdSlices.Path, Name: stdSlices.Name},
		emit.Import{Path: stdJSONText.Path, Name: stdJSONText.Name},
	)

	for _, one := range held {
		out = append(out, emit.Import{Path: one.Path, Name: one.Name, Aliased: one.Aliased})
	}

	return out
}

// naming returns what the signatures a view forwards reach for.
//
// The sequence package, and nothing else. A surface spells its element by its
// bare name — that is what a surface is for, being read beside the declaration
// it belongs to — so the only package a forwarded signature can name is the one
// every walk names. Which is also the reason this layer refuses a subject the
// file cannot spell that way: see [Layer.Generate].
func naming() []model.Import { return []model.Import{stdIter} }
