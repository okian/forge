package compose

import (
	"github.com/okian/forge/internal/diag"
	"github.com/okian/forge/internal/layer"
	"github.com/okian/forge/internal/model"
)

// What can be wrong with the shape of a stack, before anything is asked of the
// layers in it.
//
// The numbers follow the rules they enforce, so that a code and the rule it
// came from are the same number and a reader of one finds the other. Three of
// the numbers in that range are unused and stay that way: one belongs to a rule
// that says what the outermost layer means rather than forbidding anything, one
// to a rule about what a decorator may take away rather than where it may sit,
// and one to a rule restricting the subject — which the model builder refuses
// before there is a model to compose, and reports in its own range. Two more
// belong to rules that are checked, and not here: what each layer needs of the
// one beneath it is asked during the walk, and how many type arguments a marker
// takes is answered while the declaration is still being resolved.
//
// The three that are not numbered rules sit above them rather than in the gaps,
// so that the mapping stays true. One comes from how options are addressed: a
// directive names a layer, and a layer named twice in one stack leaves every
// directive for it ambiguous. The other two come from what an inline
// declaration is — the author's own type, whose underlying type has to be one
// the layers in it can live with, and one that a second layer inside the first
// would quietly make a slice of slices.
//
// A band of their own rather than the one already above these, at nine hundred.
// That one is for what is wrong with a stack without any rule having been
// broken: a marker nothing claims is not a layer, and a layer that panics is
// not a declaration's fault. These are rules, and a reader who finds one of
// them next to those would take it for neither.
var (
	codeSubjectInStack     = diag.Register(1001, "a subject appears in the stack")
	codeElementsSplit      = diag.Register(1002, "element layers are not together around the subject")
	codeTwoStorageLayers   = diag.Register(1003, "two storage layers in stack")
	codeDecoratorPlacement = diag.Register(1004, "decorator has no storage beneath it")
	codeTransportPlacement = diag.Register(1008, "transport is not the outermost layer")

	codeLayerTwice     = diag.Register(1020, "a layer appears twice in one stack")
	codeNotTransparent = diag.Register(1021, "an inline declaration names a layer that cannot be its underlying type")
	codeNestedInline   = diag.Register(1022, "an inline declaration names more than one layer")

	codeBridgeAlone = diag.Register(1009, "a bridge stands alone over its two types")
)

// validate reports every way the shape of a stack is wrong.
//
// Shape rather than capability: which kinds may sit where, and how many of
// each. What a layer needs of the one beneath it is asked of the layer itself,
// in the walk that runs after this, and is the half that does not grow with the
// catalog. This half is the arrangement the kinds themselves imply, and it is
// checked first because every question that walk asks of a mis-shaped stack has
// an answer that is a consequence: a stack with two storage layers has one of
// them refusing what it was handed, which is true and is a second complaint
// about one mistake.
//
// Every rule is checked and every failure reported. A run that stopped at the
// first would send an author back for each of the others in turn, and the rules
// here are independent: two storage layers and a decorator beneath them are two
// mistakes, not one mistake reported twice.
//
// The stack is the composed one, so an index is an index into what will be
// generated. An entry nobody wrote underlines nothing, which is what keeps a
// caret off the storage layer forge filled in.
func validate(stack []model.LayerRef, layers []layer.Layer, decl Declaration, layout model.Layout) diag.Set {
	var diags diag.Set

	for _, rule := range []func([]model.LayerRef, []layer.Layer, Declaration, model.Layout, *diag.Set){
		once, subjects, elements, storages, decorators, transports, bridges, transparent, nested,
	} {
		rule(stack, layers, decl, layout, &diags)
	}

	return diags
}

// at adds a diagnostic about one entry of the stack, with the caret under it.
func at(diags *diag.Set, code diag.Code, decl Declaration, layout model.Layout, i int,
	hint string, format string, args ...any,
) {
	diags.Add(diag.New(code, decl.Pos, format, args...).
		WithStack(layout.Text, layout.Underline(i)).
		WithHint("%s", hint))
}

// once holds a stack to naming each layer at most once.
//
// Not a rule about composition so much as about addressing: a directive names
// the layer it configures, so a stack naming one twice has directives that
// could belong to either. Nothing in the syntax says which, and picking one
// would make an option silently apply to the wrong half of the stack — so the
// stack is refused rather than the directive, and a syntax that could say which
// would retire this rule rather than change it.
//
// Reported at the second, since the first is the one the author is most likely
// to have meant and a caret under both would say nothing about which to drop.
func once(stack []model.LayerRef, _ []layer.Layer, decl Declaration, layout model.Layout, diags *diag.Set) {
	seen := make(map[model.TypeRef]bool, len(stack))

	for i, ref := range stack {
		if ref.Implicit {
			continue
		}
		if seen[ref.Origin] {
			at(diags, codeLayerTwice, decl, layout, i,
				"drop one of them; a directive naming "+ref.Directive()+" could configure either",
				"%s appears twice in this stack", ref.Origin.Name)
			continue
		}
		seen[ref.Origin] = true
	}
}

// subjects reports a layer that classifies itself as the subject.
//
// The subject of a stack is the type argument at the bottom of it, and it is
// not a layer: nothing is generated for it, nothing is asked of it, and it is
// carried separately from the moment a declaration resolves. A marker that
// claims to be one is a layer built on a misunderstanding of what a stack is,
// and there is no arrangement in which it works.
func subjects(stack []model.LayerRef, _ []layer.Layer, decl Declaration, layout model.Layout, diags *diag.Set) {
	for i, ref := range stack {
		if ref.Kind != model.KindSubject {
			continue
		}

		at(diags, codeSubjectInStack, decl, layout, i,
			"the subject is the type at the bottom of the stack, not a layer written in it",
			"%s classifies itself as a subject, and a stack holds layers", ref.Origin.Name)
	}
}

// elements holds element layers to one run at the bottom of the stack.
//
// An element layer attaches to the subject rather than to the container, so
// what it means is "the subject, with this". A container written between two of
// them would be a container of one subject holding methods the other element
// layer attached to a different one — which is not a thing, and which the
// layers above could not be told about either, since what an element layer
// contributes reaches them as a capability of the subject and nothing else.
//
// Reported at the element that is out of place rather than at the layer that
// separated it: the elements are what belong together, and the one above the
// break is the one to move.
func elements(stack []model.LayerRef, _ []layer.Layer, decl Declaration, layout model.Layout, diags *diag.Set) {
	// Innermost first, which is where the elements belong. Once something that
	// is not one has been passed, every element above it is out of place.
	broken := false

	for i := len(stack) - 1; i >= 0; i-- {
		if stack[i].Kind != model.KindElement {
			broken = true
			continue
		}
		if !broken {
			continue
		}

		at(diags, codeElementsSplit, decl, layout, i,
			"write every element layer together, innermost, around the subject",
			"%s is an element layer with a container between it and the subject", stack[i].Origin.Name)
	}
}

// storages holds a stack to one storage layer.
//
// A storage layer is what the declared type's underlying type is, and a stack
// cannot have two of those. A second container that means to add to the first
// rather than replace it is a refining layer, which is the kind that says so.
//
// Reported at each one after the first, and naming both, because which of them
// to drop is the author's decision and neither of the two names on its own is
// enough to make it.
func storages(stack []model.LayerRef, _ []layer.Layer, decl Declaration, layout model.Layout, diags *diag.Set) {
	first := -1

	for i, ref := range stack {
		if ref.Kind != model.KindStorage {
			continue
		}
		if first < 0 {
			first = i
			continue
		}

		at(diags, codeTwoStorageLayers, decl, layout, i,
			"at most one storage layer; mark "+ref.Origin.Name+" as refining, or drop "+stack[first].Origin.Name,
			"two storage layers in stack (%s, %s)", stack[first].Origin.Name, ref.Origin.Name)
	}
}

// decorators holds a decorator to somewhere above a storage layer.
//
// A decorator wraps what is beneath it without changing what the elements are:
// a lock over a container, a cache over a container. Beneath the storage there
// is no container to wrap — there is a subject and whatever attached to it —
// and a decorator written there would be wrapping the thing that decides what
// the container is rather than the container.
//
// A stack with no storage at all is the same mistake with nothing to point at.
// A refining layer written over none has one filled in, because a query surface
// over the ordinary representation is what it plainly means. A decorator over a
// bare subject could be read the same way — a decorator marker is a defined
// slice type like any other container — and it is refused rather than filled in
// because that is the reversible direction: allowing it later breaks nobody,
// and forbidding it later breaks whoever wrote it in the meantime.
func decorators(stack []model.LayerRef, _ []layer.Layer, decl Declaration, layout model.Layout, diags *diag.Set) {
	// The innermost, where a stack with one has it. A stack with two is refused
	// by the rule above, so which of them this picks decides only what a second
	// complaint about an already-refused stack says.
	storage := -1
	for i, ref := range stack {
		if ref.Kind == model.KindStorage {
			storage = i
		}
	}

	for i, ref := range stack {
		if ref.Kind != model.KindDecorator {
			continue
		}
		if storage >= 0 && i < storage {
			continue
		}

		hint := "write it above the storage layer"
		if storage < 0 {
			hint = "put a storage layer beneath it, or a refining layer that implies one"
		}

		at(diags, codeDecoratorPlacement, decl, layout, i, hint,
			"%s is a decorator and there is no container beneath it to wrap", ref.Origin.Name)
	}
}

// transports hold the outermost place, and only one of them holds it.
//
// A transport terminates a stack: it is an encoding or an I/O boundary for
// everything beneath it, and what it exposes is that boundary rather than a
// container. Nothing can be written over one, because there is no container
// left to write over — and two of them would each be terminating a stack the
// other had already terminated.
func transports(stack []model.LayerRef, _ []layer.Layer, decl Declaration, layout model.Layout, diags *diag.Set) {
	for i, ref := range stack {
		if ref.Kind != model.KindTransport || i == 0 {
			continue
		}

		at(diags, codeTransportPlacement, decl, layout, i,
			"a transport terminates a stack, so write it outermost and write only one",
			"%s is a transport with %s written over it", ref.Origin.Name, stack[i-1].Origin.Name)
	}
}

// bridges holds a bridge to being the only layer of its stack.
//
// A bridge reads one type and writes about another; there is no stream for a
// storage to hold or a refiner to query, so a stack around one describes
// machinery with nothing to attach to. Reported at the bridge, because the
// bridge is the entry whose meaning forbids the company.
func bridges(stack []model.LayerRef, _ []layer.Layer, decl Declaration, layout model.Layout, diags *diag.Set) {
	if len(stack) < 2 {
		return
	}

	for i, ref := range stack {
		if ref.Kind != model.KindBridge {
			continue
		}

		at(diags, codeBridgeAlone, decl, layout, i,
			"declare the bridge on its own: type X Map[Source, Target]",
			"%s is a bridge and composes with nothing else in a stack", ref.Origin.Name)
	}
}

// transparent holds an inline declaration to layers that can live with the
// underlying type it has.
//
// An inline declaration is the author's own type, and its underlying type is
// whatever they wrote — so every layer in the stack has to uphold its
// invariants over that raw form. A ring buffer cannot: its head index and its
// length are invariants that a slice operation on the declared type would
// corrupt, and the type being the author's means nothing stops one. An element
// marker cannot either, for a reason that is about the language rather than
// about the layer: it is a phantom struct, so a declaration naming one has that
// struct as its underlying type rather than the container the author meant.
//
// The layer is named. Transparency is something a layer declares about itself,
// and one that declares nothing is taken to be opaque — so a diagnostic that
// only said "move this to a spec file" would leave the author of a layer from
// outside forge with nothing to search for, and their users with a rule they
// cannot attribute to anything.
func transparent(stack []model.LayerRef, layers []layer.Layer, decl Declaration, layout model.Layout, diags *diag.Set) {
	if !inline(decl) {
		return
	}

	for i, one := range layers {
		// An entry nobody wrote is not the author's to answer for. The default
		// storage is transparent, which is what lets it be filled in at all —
		// but a build that named an opaque one would otherwise report a layer
		// that is not in the file, under a caret of no width.
		if stack[i].Implicit || layer.TransparentLayer(one) {
			continue
		}

		at(diags, codeNotTransparent, decl, layout, i,
			"declare it in a file tagged forgespec, where forge owns the type and its underlying form",
			"%s %s", stack[i].Origin.Name, opaque(stack[i]))
	}
}

// opaque says why a layer cannot be the underlying type of a declaration the
// author wrote, which is two different reasons wearing one code.
//
// An element marker is not a container. Go rejects a generic alias to its own
// type parameter, so an element marker is a zero-sized phantom struct instead —
// and a declaration naming one has that struct as its underlying type rather
// than anything holding elements. Nothing about the layer is at fault; the
// language left it nowhere else to stand.
//
// Every other kind is a container, and what it cannot survive is the author
// being able to reach past it. A ring's underlying type is an honest slice of
// its elements and its head index is not in it, so an append through the
// declared type leaves the ring holding a length it did not agree to.
func opaque(ref model.LayerRef) string {
	switch ref.Kind {
	case model.KindElement:
		return "attaches to the subject rather than holding one, so a declaration naming it " +
			"has that marker as its underlying type rather than a container"
	case model.KindBridge:
		return "is a form over two types rather than a container, so a declaration naming it " +
			"has a phantom struct as its underlying type"
	default:
		return "keeps invariants that the underlying type of a declaration written this way does not protect"
	}
}

// nested holds an inline declaration to one layer.
//
// The inline form works because a container marker is a defined slice type, so
// a declaration over one has the underlying type it appears to have: Collection
// of a subject is a slice of that subject, and the author may range over their
// own type without forge having lied to them. Two of them is a slice of a
// slice. The generated methods are written for the elements the author named,
// the type holds something else, and what comes out is a file that does not
// compile — which is the worst answer a generator has, because it arrives after
// the run said nothing.
//
// The storage forge fills in does not count. What makes a declaration nested is
// a layer the author wrote inside another, and an entry nobody wrote is neither
// of those — a collection over the ordinary representation is the one-layer
// case however many entries the composed stack ends up with.
func nested(stack []model.LayerRef, layers []layer.Layer, decl Declaration, layout model.Layout, diags *diag.Set) {
	if !inline(decl) {
		return
	}

	// A stack already holding a layer that cannot be an underlying type has
	// been told to move, at this position, under a caret, with this hint. The
	// two halves of that one rule are separate codes because each fires without
	// the other — a nested stack of transparent layers, a single opaque one —
	// and saying both about one declaration is saying one sentence twice.
	for i, one := range layers {
		if !stack[i].Implicit && !layer.TransparentLayer(one) {
			return
		}
	}

	written := 0
	for _, ref := range stack {
		if !ref.Implicit {
			written++
		}
	}
	if written < 2 {
		return
	}

	// At the second one written, reading outward: the first is the container
	// the declaration is of, and everything past it is what has to move.
	for i, ref := range stack {
		if ref.Implicit || i == 0 {
			continue
		}

		at(diags, codeNestedInline, decl, layout, i,
			"declare it in a file tagged forgespec, where forge owns the type and its underlying form",
			"a declaration written this way is of one layer, and %s is written inside another",
			ref.Origin.Name)
		return
	}
}

// inline reports whether the declaration is one the author owns the type of.
func inline(decl Declaration) bool {
	return decl.Model != nil && decl.Model.Form == model.FormInline
}
