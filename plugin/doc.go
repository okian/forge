// Package plugin is what a layer is written against.
//
// A layer claims one marker type and answers three questions about it: whether
// it can sit on what is beneath it, what it exposes to whatever sits above, and
// what it generates. Implement [Layer], register it with Registry.Register,
// and a declaration naming your marker composes with the built-in ones as
// though it had always been there.
//
// # What a layer is
//
// Five kinds, and the kind decides where in a stack a marker may appear and
// what its output is about.
//
// A storage layer is the representation: a slice, a ring buffer, a tree. It
// owns the declared type's underlying form and puts its methods there. At most
// one of them sits in a stack. A declaration naming a refining layer with no
// storage beneath it has one filled in — which is what lets a layer that reads
// a container be written without naming one — and a declaration naming only
// element layers gets the same. A stack of decorators over nothing does not:
// there is nothing there to decorate, and that is refused rather than
// invented.
//
// A refining layer adds to a representation without replacing it — a query
// surface, a sorted view — and requires the capabilities it reads. It puts its
// methods on the declared type too.
//
// An element layer is about one value rather than about a container of them: a
// codec, a check, a copy, a log value. Its methods go on the *subject*, which
// is why two declarations over one subject get one of each rather than two, and
// why what it writes reaches the layers above as a capability rather than as a
// method they could wrap.
//
// A decorator wraps a representation. It may withdraw what it cannot uphold —
// a lock takes iteration away rather than handing out a sequence somebody could
// hold it across — and what it wraps has to be enumerable, which is what the
// method surface in a [Shape] is for.
//
// A transport terminates a stack. What is beneath it is what it carries — over
// a wire, into a file, across a boundary that is not Go — so it is written
// outermost, only one of them may appear, and nothing may be written over one:
// there is no container left above it to write over.
//
// It is the kind furthest from an element layer rather than a variety of one.
// An element layer sits innermost and writes about a single value; a transport
// sits outermost and writes about everything under it.
//
// # The order things are asked in
//
// This is the order a run that generates asks them in. A verb that only
// describes asks a narrower set in its own order — the list command asks every
// layer to accept a shape with no declaration behind it at all, and an
// explanation asks a step to accept and expose more than once. So nothing here
// may be answered by counting how many times it was asked, and nothing may be
// left until a later question: Accepts and Shape have to answer from what they
// are handed, as cheaply as Origin and Kind do.
//
// Layer.Binds and Layer.Writes first, before anything is generated and
// before any of the questions below. Neither is asked about a particular
// declaration — a layer three of them name is asked three times and has to
// answer the same way each time — because they settle two things no single
// declaration can: which package names every file of the package will bind,
// and which methods the run will put on each subject. Answer both wide, naming
// what you may do rather than what you will turn out to do.
//
// Layer.Origin and Layer.Kind whenever anything needs to know which marker
// you claim and where it may sit. Both are asked freely and often, from every
// verb, so both have to be cheap and neither may depend on anything.
//
// Layer.OptionSchema next, so that what was written on the declaration can be
// checked against it and an option naming a field can be resolved against the
// subject. What reaches you afterwards has been checked: a value that names a
// field names one the subject has, an enumerated value is one you listed, and
// anything wrong was reported before you were asked to generate.
//
// Layer.Accepts next, against the shape beneath. Refuse here and the
// declaration is refused, with what you said in the message — the code and the
// position are forge's, because what is wrong is the stack rather than anything
// inside your layer, and a report about a stack should read the same whichever
// layer noticed. Say what is wrong and leave the rest to forge.
//
// Layer.Shape next, and it is asked while the stack is still being composed.
// So it is asked before there is anything above, and a layer whose methods
// depend on what ends up above it cannot answer for those yet — say what you
// know and leave the rest out rather than promising a method a decorator may
// take away.
//
// Layer.Generate last, with a [Context] holding the declaration and the shape
// beneath. Return a [Unit]: declarations, the imports they need, and the
// compile-time claims they earn. This is the one place a diagnostic of yours
// reaches an author as yours — see below.
//
// # Where a method goes
//
// A method on the declared type goes in the unit's own declarations. There is
// one declared type per declaration, so one file, and nothing to reconcile.
//
// A method on the *subject* does not. Two declarations over one subject each
// ask you to generate, and a subject method put in the unit's declarations is
// then written into two files — each of them consistent, neither able to see
// the other, and the package does not build. Put it in Unit.Provides instead,
// under a key naming what it is about: forge writes each key once, into the
// file the package's declarations share, and the second declaration to ask for
// it gets nothing rather than a copy.
//
// Forge reports the mistake rather than writing it, so a layer that gets this
// wrong learns from a diagnostic and not from the compiler. It is still worth
// knowing which is which before writing either.
//
// A storage layer owes one thing more: the declared type itself. Forge writes
// the methods a stack asks for and does not invent the type they are on, so a
// storage layer's declarations include the type declaration — a defined slice,
// a struct holding a buffer — or the package names a type nothing declares.
//
// # Diagnostics are the product
//
// A layer that fails with a stack trace is worse than one that refuses. Return
// a [Diagnostic] built with [New], against a code you took from [Register] at
// package scope, and it reaches the author with the position of their
// declaration and your hint beside it. An ordinary error is accepted and given
// a code by whatever received it, which is a worse report than the one you
// could have written.
//
// Take your codes from 6000 to 9999. Everything below 6000 is forge's:
// 1xxx composition, 2xxx the subject and type model, 3xxx options,
// 4xxx emission and collisions, 5xxx I/O and the toolchain. Ask a code
// Code.Ours to tell one of forge's from one a layer raised, which is what
// says whose documentation to look in.
//
// # What this package promises
//
// Everything named here is what a layer outside forge's own module is written
// against, and is the surface forge keeps. Within a major version a name here
// keeps its meaning: a field is not removed or repurposed, a function's
// signature does not change, and an interface does not gain a method. Names may
// be added.
//
// Most of what is here is an alias for a type forge uses internally. That is
// deliberate and is not a leak: there is one type rather than two of the same
// shape, so nothing is converted at the boundary and nothing drifts, and what
// a layer writes is what forge reads. What the alias points at is forge's to
// move; the name here is not.
//
// It costs one thing, and it is worth knowing before looking for it. An aliased
// type's fields and methods are documented where the type is declared, so this
// page shows the name and the summary and not the members. Read them with the
// go command, which does not mind where a package sits:
//
//	go doc github.com/okian/forge/internal/layer Unit
//	go doc github.com/okian/forge/internal/layer Layer
//
// That works from any module. What it does not do is appear on the web, since a
// documentation host has no reason to publish a package nobody may import — so
// the prose here says what a reader of a web page would otherwise have gone
// looking for.
//
// # What this package does not promise
//
// Forge's own layers reach for machinery this package does not publish, and a
// layer written against this package cannot do everything they do.
//
// The template rewriter is the main one. A storage layer's body is written as
// compiling generic Go and rewritten into the subject's terms, which is how
// forge keeps forty method bodies type-checked rather than held in strings. It
// is an implementation strategy rather than a contract, and publishing it would
// freeze the shape of every template in the tree. A layer here writes its
// declarations directly, which is what the element layers do.
//
// The shared views are the other. The lazy sequence a collection hands back,
// and the locked view a decorator hands into a closure, are types forge emits
// into the package being generated and reads back by name. A layer needing one
// would be depending on forge's output rather than on its API.
//
// Forge's own layers also reuse each other and reuse forge — a failure type, a
// walk over embedded fields, a check one layer runs on another's behalf, the
// helpers that write what a display tag earns. Those are not gaps and are not
// going to be published: they are forge reusing itself, and a layer outside it
// writes its own or does without.
//
// The template rewriter and the shared views are gaps rather than decisions
// against. What closes them is the same change: composition settling a shape in
// two passes rather than building one in a single pass, which is what would
// also let a layer describe methods whose existence depends on what ends up
// above it.
package plugin
