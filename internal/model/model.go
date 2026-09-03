package model

import (
	"go/token"
	"go/types"
	"strings"
	"unicode/utf8"

	"golang.org/x/tools/go/packages"
)

// Model is one generation request: a single type declaration, resolved.
//
// It is the unit of work the whole pipeline passes around. Discovery produces
// one per candidate declaration, resolution fills in the stack and subject,
// validation accepts or rejects it, and every layer generating for it sees the
// same value.
type Model struct {
	// Name is the declared type's identifier, the Persons of
	// "type Persons Collection[Person]". It names the generated type and, in
	// lower case, the file the generated code is written to.
	Name string

	// Decl is the declared type itself. For a spec-form declaration it is the
	// placeholder the spec file declares, which exists only under the forgespec
	// build tag; generation replaces it.
	Decl *types.Named

	// Form records how the declaration was written, which decides whether
	// generation adds methods to an existing type or owns the type outright.
	Form Form

	// Subject is the concrete type the stack is specialised to: the innermost
	// type argument, held separately because it is not a layer.
	Subject *Struct

	// Source is the type a bridge reads, raw rather than modelled: what a
	// bridge needs from it — exported fields, zero-parameter methods — is a
	// question go/types answers directly, and a *Struct model of it would
	// carry a fields list that is wrong for an interface. Nil for every
	// declaration that is not a bridge.
	Source types.Type

	// Stack holds the layers the declaration names, outermost first. Stack[0]
	// determines the public API and the generated type's name.
	Stack []LayerRef

	// Options holds the option sets written on the declaration, in the order
	// the directives appear above it. It is ordered rather than keyed so that a
	// diagnostic can walk every directive — reporting one written for a layer
	// that is not in the stack, say — without an iteration order reaching the
	// output.
	Options []Options

	// Pkg is the package the declaration lives in, as the loader saw it.
	Pkg *packages.Package

	// Pos is the position of the declaration itself. Every diagnostic about
	// this model points here, never at a generated file, because a generated
	// file is not what the author can fix.
	Pos token.Position
}

// Outermost returns the layer that determines the public API, and whether the
// stack has one at all.
func (m *Model) Outermost() (LayerRef, bool) {
	if m == nil || len(m.Stack) == 0 {
		return LayerRef{}, false
	}
	return m.Stack[0], true
}

// OptionsFor returns the option set written for the layer addressed by
// directive, and whether the declaration carries one. A directive written
// twice resolves to its first occurrence; rejecting the repeat is validation's
// job.
func (m *Model) OptionsFor(directive string) (Options, bool) {
	if m == nil {
		return Options{}, false
	}
	for _, opts := range m.Options {
		if opts.Layer == directive {
			return opts, true
		}
	}
	return Options{}, false
}

// String returns the declaration as it reads in source,
// "Persons Collection[Ring[Json[Person]]]".
func (m *Model) String() string {
	if m == nil {
		return "<nil model>"
	}
	return strings.TrimSpace(m.Name + " " + m.Layout().Text)
}

// Span locates one stack entry inside a rendered declaration.
type Span struct {
	// Offset is the number of bytes before the entry's name.
	Offset int

	// Width is the length of the entry's name in bytes, and is zero for an
	// entry that resolution inferred rather than the author writing.
	Width int
}

// Layout is a rendered declaration together with the position of each stack
// entry inside it. It is what lets a diagnostic underline one layer of a
// nested stack:
//
//	Collection[Ring[Heap[Person]]]
//	                ^^^^
//
// Rendering lives here so that the text a diagnostic prints and the offsets it
// underlines can never disagree.
type Layout struct {
	// Text is the stack as the declaration spells it.
	Text string

	// Spans holds one entry per element of [Model.Stack], in the same order.
	Spans []Span

	// Subject locates the innermost type, which no stack entry covers and
	// which the diagnostics that refuse a subject have to point at.
	Subject Span
}

// Underline returns a line of spaces and carets that marks the i'th stack
// entry when printed directly beneath [Layout.Text]. It returns an empty
// string for an out-of-range index and for anything [Span.Underline] declines
// to mark.
func (l Layout) Underline(i int) string {
	if i < 0 || i >= len(l.Spans) {
		return ""
	}
	return l.Spans[i].Underline(l.Text)
}

// Underline returns a line of spaces and carets that marks the span when
// printed directly beneath text. It returns an empty string for a span that
// marks nothing, as an entry nobody wrote does, and for one that does not lie
// within the text.
//
// Spans are measured in bytes, because that is what slicing the text needs,
// while the caret line is measured in characters, because that is what lines
// up on screen. A marker named with a non-ASCII letter is a legal Go
// identifier, so the two are not the same count.
func (s Span) Underline(text string) string {
	if s.Width <= 0 || s.Offset < 0 || s.Offset+s.Width > len(text) {
		return ""
	}

	lead := utf8.RuneCountInString(text[:s.Offset])
	width := utf8.RuneCountInString(text[s.Offset : s.Offset+s.Width])

	return strings.Repeat(" ", lead) + strings.Repeat("^", width)
}

// OpenStack renders the layers of a stack down to the point where the type they
// wrap is written — "Collection[Ring[" — and reports where each layer's name
// falls in that rendering and how many brackets close it.
//
// Markers are spelled unqualified whether the declaration imported them
// qualified or not, so the rendering is canonical rather than verbatim. An entry
// resolution inferred contributes no text and gets a zero-width span, since
// nothing may point a caret at an entry nobody wrote; that is also why the
// bracket count is returned rather than counted from the entries.
//
// It is exported so that one declaration renders one way wherever it is
// rendered. [Model.Layout] closes the stack over the subject's name, and a
// stage that has no model yet closes it over whatever it does have.
func OpenStack(stack []LayerRef) (text string, spans []Span, open int) {
	spans = make([]Span, len(stack))

	var b strings.Builder
	for i, ref := range stack {
		if ref.Implicit {
			// The entry occupies no source text, but it still has a place: the
			// point where it would have been written had the author spelled it.
			spans[i] = Span{Offset: b.Len()}
			continue
		}
		name := ref.Origin.Name
		spans[i] = Span{Offset: b.Len(), Width: len(name)}
		b.WriteString(name)
		b.WriteByte('[')
		open++
	}

	return b.String(), spans, open
}

// Layout renders the declaration's stack the way the author wrote it, with the
// subject innermost, and reports where each entry falls within that rendering.
func (m *Model) Layout() Layout {
	if m == nil {
		return Layout{}
	}
	return LayoutOf(m.Stack, m.Subject.Type())
}

// LayoutOf renders a stack over a subject that has no model yet.
//
// The stage that refuses a subject is the stage that would otherwise have built
// the model, so the picture a diagnostic draws has to be available before there
// is one. Both entry points render through the same code, because a rendering
// that disagreed with the offsets underneath it would draw a caret under the
// wrong layer.
func LayoutOf(stack []LayerRef, subject types.Type) Layout {
	text, spans, open := OpenStack(stack)

	name := TypeString(subject)
	at := Span{Offset: len(text), Width: len(name)}

	return Layout{
		Text:    text + name + strings.Repeat("]", open),
		Spans:   spans,
		Subject: at,
	}
}

// Options is one layer's option set, as written on a declaration.
type Options struct {
	// Layer is the directive name the options were attached to: the
	// "collection" of //forge:collection.
	Layer string

	// Entries holds the key=value pairs in the order they were written. Order
	// is preserved so that diagnostics report them the way the author sees
	// them, and so that generated output cannot depend on a map's whim.
	Entries []Option

	// Pos is the position of the directive comment, which is where a
	// diagnostic about any of these options points.
	Pos token.Position
}

// Option is one key=value pair from a //forge: directive.
type Option struct {
	// Key is the text before the equals sign.
	Key string

	// Value is the text after it, empty for a key written on its own.
	Value string

	// Pos is the position of the key within the directive comment.
	Pos token.Position
}

// String returns the option as it was written, "key=value", or just the key
// when it carries no value.
func (o Option) String() string {
	if o.Value == "" {
		return o.Key
	}
	return o.Key + "=" + o.Value
}

// Lookup returns the option written under key, and whether the set carries
// one. A key written twice resolves to its first occurrence; rejecting the
// repeat is validation's job, not lookup's.
func (o Options) Lookup(key string) (Option, bool) {
	for _, entry := range o.Entries {
		if entry.Key == key {
			return entry, true
		}
	}
	return Option{}, false
}

// Get returns the value written for key, and whether the set carries it.
func (o Options) Get(key string) (string, bool) {
	entry, ok := o.Lookup(key)
	return entry.Value, ok
}

// List returns the value written for key split on commas, which is how an
// option that names more than one thing is spelled: sort=Age,LastName.
//
// Each part is trimmed of surrounding spaces, and parts that are then empty
// are dropped, so a stray comma yields a shorter list rather than a blank
// entry. Reporting that stray comma is validation's job; List's contract is
// only that everything it returns is a non-empty name. It returns nil when the
// key is absent, and when nothing survives.
func (o Options) List(key string) []string {
	value, ok := o.Get(key)
	if !ok || value == "" {
		return nil
	}

	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// String returns the option set as the directive that produced it,
// "//forge:collection sort=Age index=Name".
func (o Options) String() string {
	var b strings.Builder
	b.WriteString("//forge:")
	b.WriteString(o.Layer)
	for _, entry := range o.Entries {
		b.WriteByte(' ')
		b.WriteString(entry.String())
	}
	return b.String()
}
