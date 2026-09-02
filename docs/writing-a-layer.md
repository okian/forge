# Writing a layer

A layer is a plugin claiming one marker type. Writing one takes no change to
`forge`: you declare a generic type nobody makes a value of, implement an
interface, and link the result into a binary.

This is the walkthrough. The reference is
[`plugin`'s package documentation](https://pkg.go.dev/github.com/okian/forge/plugin),
which says what a layer is asked and in what order, where a method goes, and
which of forge's own machinery is deliberately not published. Read it before you
start; read this to see the shape of a whole one. Where the two overlap, the
reference is the one that is right — this is a tour, and a tour that has drifted
should be corrected against it.

[`x/csv`](../x/csv) is a finished layer, in a module of its own so it cannot
reach past the published surface. It is worth opening when something here is
vague, with two differences to expect: it claims a marker forge publishes rather
than declaring its own, and it is a transport rather than the refining layer
built below.

## 1. Declare the marker

A marker is a generic type with one parameter, and it needs nothing from forge:

```go
// Package tally counts a container's elements by one of the subject's fields.
package tally

// Tally counts the elements of a container by one of the subject's fields.
type Tally[T any] struct{ _ [0]T }
```

Your users write `tally.Tally[Person]` and the type exists, so their spec file
compiles with nothing installed.

Zero-sized rather than a defined slice, and the difference is not cosmetic.
Forge decides where a declaration may be written from the layer, not from the
marker: a stack is refused outside a spec file unless every layer in it
implements the optional `Transparent` interface and answers yes. So
`type Tally[T any] []T` on its own buys nothing — an author writing
`type People tally.Tally[Person]` in an ordinary file gets FRG1021, about
invariants your layer never mentioned.

Use the phantom struct unless you mean to implement `Transparent` as well, and
implement it only if the raw underlying type really does uphold whatever your
layer promises. A slice is transparent, because any slice is a valid one; a ring
buffer's head index is not, and a lock's exclusion is not. Getting it wrong the
safe way costs a declaration that has to move under a build tag; getting it
wrong the other way costs a corrupted value at run time with nothing to point
at.

You may instead claim a marker **forge** publishes and has not implemented —
anything `forge list` calls *staged*, which is what `x/csv` does. Return it from
`Origin` and registration takes it over from the placeholder, so your users write
`forge.Csv[…]` and their declaration does not change when your layer arrives.

The cost is that forge may one day write that layer too, and two layers that both
generate for one marker are refused at registration — a panic, if the binary used
`MustRegister`. A marker of your own cannot collide that way, which makes it the
safer of the two.

## 2. Answer what you are

Six methods, all cheap. Four belong to `Layer`; `Stage` and `Doc` are the
optional `Described` interface, which is separate because neither question has
an answer a layer written outside forge could give — how far along a layer is is
forge's own roadmap, and a summary of itself is documentation rather than
something the pipeline acts on.

They are not all asked the same way. `Origin` and `Kind` are asked freely and
often, from every verb, so both have to be cheap and neither may depend on
anything. `Binds` and `Writes` are asked once, before anything generates and
before any declaration is in hand.

```go
// Layer counts a container's elements by a field.
type Layer struct{}

// New returns the layer.
func New() Layer { return Layer{} }

// Origin identifies the marker this layer claims.
func (Layer) Origin() plugin.TypeRef {
	return plugin.TypeRef{Pkg: "example.com/mylayers/tally", Name: "Tally"}
}

// Kind says where in a stack it may appear.
func (Layer) Kind() plugin.Kind { return plugin.KindRefining }

// Stage says how far along it is, which for a layer outside forge is the one
// answer there is.
func (Layer) Stage() plugin.Stage { return plugin.StageReady }

// Doc is the line a report prints beside the step.
func (Layer) Doc() string { return "a count of the elements sharing each value of one field" }

// Binds names the packages the output imports. Nil means none of your own.
func (Layer) Binds() []plugin.Import { return nil }

// Writes names what you put on the subject rather than on the declared type.
func (Layer) Writes() []string { return nil }
```

`Kind` is load-bearing, and forge refuses a layer that reports none rather than
registering it — so the mistake costs a line in your layer rather than a
diagnostic about somebody else's declaration. Every rule about the shape of a
stack is written in kinds, which may sit where and how many of each, and a layer
invisible to all of them would have its neighbours reported instead: a container
that forgot to say it was one leaves a decorator above it told there is nothing
beneath to wrap. The zero value is the natural way to arrive there, which is
why it is the one refused.

The five are described in `plugin`'s documentation; the short version is that
storage *is* the representation, refining adds to one, an element layer is about
a single value, a decorator wraps, and a transport terminates.

`Stage` and `Doc` are optional and worth implementing anyway. A layer that says
nothing about itself is read as *ready*, so leaving them off works — and it also
leaves `forge list` with no summary to print beside your marker.

`Binds` is the one that is easy to get wrong. It names every package your output
may import, and the name each is bound to, so that forge can move the subject's
own types out of the way of those names. Answer it **wide**: it is asked before
anything is generated, so name what you might write rather than what you will
turn out to write. A name reserved and not used costs a subject an alias it did
not need; a name used and not reserved costs a file that does not build. What
you do *not* need to name is where the subject's own types come from — the
spelling finds those.

## 3. Declare your options

Declared rather than parsed, so one validator checks every layer's options and
a misspelling is reported with the nearest name beside it:

```go
// OptionSchema declares the field to count by.
func (Layer) OptionSchema() []plugin.OptionDef {
	return []plugin.OptionDef{{
		Key:   "by",
		Value: plugin.ValueField,
		Scope: plugin.ScopeDeclaration,
		Doc:   "the field to count by",
	}}
}
```

`Required: true` is the field to reach for when a layer cannot generate without
an option, and it is deliberately left off here. Forge then reports the missing
option itself, as FRG3011, before `Generate` is called at all — which is the
better report, and which would make the refusal written in step 6 unreachable.
Set it when you want forge's wording; leave it off when you want your own.

`ValueField` means the value names one of the subject's fields, and it is
resolved against the subject before you see it — so a renamed field is an error
on the declaration rather than a generated call to a field that is gone. What
reaches `Context.Options` has been checked: a field value names a real field, an
enumerated value is one you listed, and anything wrong was reported before you
were asked to generate.

Apply your own defaults. An option reaches you as it was written, and `Shape` is
asked before anything has filled defaults in — so a layer that read the raw
value would describe an unwritten declaration one way and generate it the other.

## 4. Say what you can sit on, and what you offer

```go
// Accepts refuses a stack whose elements it cannot walk.
func (Layer) Accepts(below plugin.Shape) error {
	if !below.Caps.Has(plugin.Streamable) {
		return errors.New("a tally has to walk the elements, and cannot here")
	}
	return nil
}

// Shape adds nothing and withdraws nothing: a count is another way to read
// what is already there.
func (Layer) Shape(_ *plugin.Context, below plugin.Shape) plugin.Shape { return below }
```

`Accepts` returns a plain error with no code and no position, because you have
neither: you were handed a shape and know nothing about the declaration that
produced it. Say what is missing and leave the rest to forge, so that a report
about a stack reads the same whichever layer noticed.

`Shape` is asked while the stack is still being composed, so it is asked before
there is anything above. Say what you know. Do not promise a method whose
existence depends on what ends up above you — a decorator may take it away, and
a surface that named it would have that decorator wrapping something that is
not there. Every method you put on the surface carries your layer as its
`Owner`.

Both are asked more than once, by more than one verb, and sometimes with no
declaration at all: `forge list` asks every layer to accept a shape with nothing
behind it. So neither may be answered by counting how many times it was asked,
and neither may leave work until later.

## 5. Generate

```go
// Generate writes the count.
func (Layer) Generate(ctx *plugin.Context, below plugin.Shape) (plugin.Unit, error) {
	by, named := ctx.Options.Get("by")
	if !named {
		// Step 6 is what this returns; the option is not declared required,
		// so refusing it is this layer's to do.
		return plugin.Unit{}, missing(ctx)
	}

	var held plugin.Field
	for _, one := range ctx.Model.Subject.Fields {
		if one.Name == by {
			held = one
		}
	}

	key := plugin.Spell(held.Type.Type, ctx.Model.Pkg.PkgPath, ctx.Bound())

	src := fmt.Sprintf(`package p

// TalliedBy%[1]s counts the elements sharing each %[1]s.
func (c %[2]s) TalliedBy%[1]s() map[%[3]s]int {
	out := make(map[%[3]s]int)
	for v := range c.All() {
		out[v.%[1]s]++
	}
	return out
}
`, by, ctx.Declared(), key.Text)

	fset := token.NewFileSet()

	file, err := parser.ParseFile(fset, "tally.go", src,
		parser.ParseComments|parser.SkipObjectResolution)
	if err != nil {
		return plugin.Unit{}, err
	}

	return plugin.Unit{
		Decls:    file.Decls,
		Comments: file.Comments,
		Fset:     fset,
		Imports:  plugin.Reaching(file.Decls, append(Layer{}.Binds(), key.Imports...)),
	}, nil
}
```

Five things in that are not obvious and all five matter.

**`ctx.Declared()`, not `ctx.Model.Name`.** A layer beneath an enclosing
decorator declares onto a type of the decorator's making, because what the
decorator wraps has to become unreachable without going through it. Writing
onto the declaration instead would put the unlocked methods on the locked type.

**`plugin.Spell`, not `types.TypeString`.** A type has to be written the way the
file that holds it can write it, and that depends on what the package's files
bind — a subject from a package called `slices`, in a file where somebody bound
the standard library's, has to be written under another name. Pass
`ctx.Bound()`, which is what every layer of the run is handed, and use the
`Imports` that come back.

**`plugin.Reaching`, not the whole of `Binds`.** You answered `Binds` wide,
before you knew what this declaration would need. `Reaching` drops what your
output did not name, because a file importing a package nothing in it mentions
does not compile.

**`Comments` and `Fset` travel with `Decls`.** A comment is not reachable from
the declaration it documents — the printer finds it by position, in a list
belonging to the file — so declarations handed over without all three are
printed without every comment inside a function body, or with the comments in
the wrong places. Both produce Go that compiles, and the output is committed.

**Assemble as text, then parse.** A function with loops and branches in it is
many times its own size as a tree. What text costs is the possibility of
assembling something that is not Go, which is why it is parsed inside the layer:
a failure to parse is then reported against the declaration rather than
discovered in a file on disk.

### Where a method goes

A method on the **declared type** goes in `Unit.Decls`, as above. There is one
declared type per declaration, so one file.

A method on the **subject** does not. Two declarations over one subject each ask
you to generate, so a subject method in `Unit.Decls` is written into two files —
each consistent, neither able to see the other, and the package does not build.
Put it in `Unit.Provides` under a key naming what it is about; forge writes each
key once. Forge reports the mistake rather than writing it, but it is worth
knowing which is which before writing either.

A **storage** layer owes one thing more: the declared type itself. Forge writes
the methods a stack asks for and does not invent the type they are on, so your
`Unit.Decls` includes the `type` declaration or the package names a type nothing
declares.

## 6. Refuse well

A layer that fails with a stack trace is worse than one that refuses. Take a
code at package scope and report against the declaration — this is the `missing`
that step 5 returns:

```go
// codeNoField reports a tally with nothing to count by.
var codeNoField = plugin.Register(6001, "tally was given no field to count by")

// missing says the option was not written, and what to write.
func missing(ctx *plugin.Context) error {
	return plugin.New(codeNoField, ctx.Model.Pos,
		"%s names no field to count by", ctx.Model.Name).
		WithHint("write by=<field> on the directive")
}
```

Take your codes from **6000 to 9999**. Everything below 6000 is forge's, and
`Code.Ours` is what tells a reader whose documentation to look in. Register at
package scope so that two diagnostics claiming one number is a panic at
start-up rather than two reports an author cannot tell apart.

Say what is wrong in the message and what to do about it in the hint. A report
without the second is one an author has to guess from. An ordinary `error` is
accepted and given a code by whatever received it, which is a worse report than
the one you could have written.

## 7. Link it

A layer is code, so the binary that knows about it is the binary somebody linked
it into. There is no plugin file to drop in and no directory to scan:

```go
package main

import (
	"github.com/okian/forge/driver"

	"example.com/mylayers/tally"
)

func main() {
	catalog := driver.Builtins()
	catalog.MustRegister(tally.New())

	driver.Main(catalog)
}
```

Start from `driver.Builtins()` rather than an empty catalog. A stack composes
across every layer the run knows, so a catalog holding one layer can generate
only for a declaration naming one layer and nothing else — and the storage a
refining layer needs beneath it is one of forge's.

The result takes the same command line as forge's own binary, walks the same
packages, and writes the same files. A declaration naming your marker composes
with the built-in layers; one naming none of them is left alone exactly as
before.

## 8. Test it against the real thing

`driver.Run` takes a command line and two writers and returns a status, so a
test drives the whole pipeline without starting a process:

```go
var out bytes.Buffer
status := driver.Run(catalog, []string{"-C", dir, "generate", "."}, &out, &out)
```

The tests worth having, in the order they earn their keep:

- **One end-to-end**, walking list → explain → generate → compile in a single
  test. A layer that lists and does not resolve, or resolves and generates
  nothing, passes three separate tests and is useless; the failure worth
  catching is the seam.
- **The refusals**, one per diagnostic, over fixtures that actually provoke
  them. Assert the code, the position and the hint.
- **A matrix** over the arrangements your layer can appear in, generated and
  compiled. Whether a stack that satisfies every rule produces a package that
  builds is a product of the layers rather than a property of any one of them,
  so it is found by trying the combinations.
- **A committed worked example** with its generated output beside it, and an
  acceptance test that regenerates into a temporary directory and compares. A
  committed output that has gone stale still compiles, so nothing fails until
  somebody reads it and believes it.

## What is deliberately not published

Forge's own layers reach for machinery `plugin` does not publish, and a layer
written against `plugin` cannot do everything they do. Two gaps are worth
knowing before you go looking:

**The template rewriter.** A storage layer's bodies are compiling generic Go,
rewritten into the subject's terms — which is how forge keeps forty method
bodies type-checked rather than held in strings. It is an implementation
strategy rather than a contract, and publishing it would freeze the shape of
every template in the tree. Write your declarations directly, as the element
layers do.

**The shared views.** The lazy sequence a collection hands back and the locked
view a decorator hands into a closure are types forge emits into the package
being generated and reads back by name. A layer needing one would be depending
on forge's *output* rather than on its API.

Both are gaps rather than decisions against, and closing them is the same
change: composition settling a shape in two passes rather than building one in
a single pass.

Forge's own layers also reuse each other and reuse forge — a failure type, a
walk over embedded fields, a check one layer runs on another's behalf, the
helpers that write what a display tag earns. Those are not gaps and are not
going to be published: they are forge reusing itself, and a layer outside it
writes its own or does without.

## The compatibility promise

`plugin`'s own documentation states it and is the version that counts. The short
of it: within a major version a name there keeps its meaning, and names may be
added.

The one consequence worth knowing before you go looking for something. Most of
what `plugin` publishes is an alias for a type forge uses internally — so that
there is one type rather than two of the same shape, and nothing converts at the
boundary. An aliased type's members are documented where the type is declared,
which means the web page shows you the name and not the fields. Read them with
the go command instead:

```
go doc github.com/okian/forge/internal/layer Unit
go doc github.com/okian/forge/internal/layer Layer
```

That works from any module whose graph holds forge, which yours does.
