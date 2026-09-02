package csv

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/okian/forge/plugin"
)

// The marker this layer claims, and the name a directive writes it under.
//
// Forge's own, rather than one declared here. Forge publishes the type without
// a generator behind it so that a declaration naming it type-checks, and
// registering this layer takes it over from the placeholder — so an author
// writes forge.Csv[…] whether or not the binary they are running knows how to
// generate for it, and the report tells them which.
const (
	marker     = "Csv"
	markerName = "csv"
)

// The options a declaration may write.
const (
	optionHeader = "header"
	optionComma  = "comma"
)

// The defaults.
//
// A header by default because a CSV document without one is a document whose
// columns are decided by whoever wrote the reader, and the whole reason the
// header is checked before an element is read is that a column order is not
// something to guess at. A comma by default because that is the C in CSV.
const (
	headerByDefault = true
	commaByDefault  = ","
)

// The packages generated code imports, and the names they bind.
//
// Written down rather than derived from the paths, because a path does not say
// what it binds. Answered wide rather than exactly: which of these a given
// declaration turns out to name depends on which halves of the codec its stack
// can carry, and this is asked before that is known. A name reserved and not
// imported costs the subject an alias it did not need; one imported and not
// reserved costs a file that does not build.
var imports = []plugin.Import{
	{Path: "encoding/csv", Name: "csv"},
	{Path: "errors", Name: "errors"},
	{Path: "fmt", Name: "fmt"},
	{Path: "io", Name: "io"},
	{Path: "slices", Name: "slices"},
	{Path: "strconv", Name: "strconv"},
}

// Layer generates a CSV codec for a whole stack.
//
// It carries no state: what a declaration decides reaches it through the
// context it is asked to generate against, so one value serves every
// declaration in a run.
type Layer struct{}

// New returns the CSV transport layer.
func New() Layer { return Layer{} }

// Origin identifies the marker this layer claims.
func (Layer) Origin() plugin.TypeRef {
	return plugin.TypeRef{Pkg: plugin.MarkerPkg, Name: marker}
}

// Kind says where in a stack the layer may appear.
//
// A transport: what is beneath it is what it carries, out of the process and
// into a file or across a wire. So it is written outermost, only one of them may
// appear in a stack, and nothing may be written over one — there is no
// container left above it to wrap.
func (Layer) Kind() plugin.Kind { return plugin.KindTransport }

// Doc returns the one-line summary the list command prints.
func (Layer) Doc() string {
	return "the whole stack as CSV, mapping the subject's fields to a header row"
}

// Binds names what this layer's output imports, so that every layer of the
// stack spells its types against the same set.
func (Layer) Binds() []plugin.Import { return slices.Clone(imports) }

// Writes names what this layer puts on the subject, which is nothing.
//
// A CSV document is about the container: one row per element, one column per
// field, and a header naming the columns. The row codec it writes is a
// package-level function rather than a method, so the subject gains nothing a
// neighbouring declaration could be asking after.
func (Layer) Writes() []string { return nil }

// OptionSchema declares every option the layer accepts.
func (Layer) OptionSchema() []plugin.OptionDef {
	return []plugin.OptionDef{
		{
			Key:     optionHeader,
			Value:   plugin.ValueBool,
			Default: strconv.FormatBool(headerByDefault),
			Doc:     "whether the document opens with a row naming the columns",
		},
		{
			Key:     optionComma,
			Value:   plugin.ValueString,
			Default: commaByDefault,
			Doc:     "the character between two fields of a record",
		},
	}
}

// Accepts reports whether the layer can sit on the shape beneath it.
//
// A container of a subject with fields, and nothing less. Structured is what
// says there are fields to make columns out of — a stack of containers over
// nothing has none, however deep it goes — and Streamable is what says the
// elements can be reached one at a time.
//
// A decorator that withdrew the walk is refused here rather than worked around.
// A lock hands out no sequence because holding one across the lock is what
// breaks it, and a document written by going round that would be the one method
// in the file that could corrupt the container it came from.
func (Layer) Accepts(below plugin.Shape) error {
	switch {
	case !below.Caps.Has(plugin.Structured):
		return errors.New("CSV is a table of the subject's fields, and there is no subject with fields here")
	case !below.Caps.Has(plugin.Streamable):
		return errors.New("CSV is one row per element, and the elements cannot be walked here")
	}
	return nil
}

// Shape returns what the stack offers once this layer has been applied.
//
// Encodable, and nothing else changes. A transport adds a wire form for
// everything beneath it and takes nothing away — and there is nothing above it
// to take anything away from, since a transport terminates a stack.
//
// The methods it writes are not put on the surface, and that is not an
// omission. Which of them exist is decided by what the stack beneath turned out
// to expose, and a surface is asked for while the stack is still being composed
// — so a layer that promised both halves here would be promising a reader a
// method it may then not write.
func (Layer) Shape(_ *plugin.Context, below plugin.Shape) plugin.Shape {
	below.Caps = below.Caps.With(plugin.Encodable)
	return below
}

// Generate returns the declarations this layer contributes.
func (Layer) Generate(ctx *plugin.Context, below plugin.Shape) (plugin.Unit, error) {
	if ctx == nil || ctx.Model == nil || ctx.Model.Subject == nil {
		// Not a diagnostic: a diagnostic points at a declaration, and the
		// declaration is what is missing. Reaching here is forge calling this
		// wrongly rather than anybody writing anything.
		return plugin.Unit{}, errors.New("csv: asked to generate without a modelled declaration")
	}

	comma, err := delimiter(ctx)
	if err != nil {
		return plugin.Unit{}, err
	}

	of, err := tabulated(ctx)
	if err != nil {
		return plugin.Unit{}, err
	}

	over, err := streaming(ctx, below, of)
	if err != nil {
		return plugin.Unit{}, err
	}
	over.comma = comma
	over.header = heading(ctx)

	return contributed(of, over)
}

// heading reports whether the document opens with a row naming its columns.
//
// The default the schema declares, applied here as well, because an option
// reaches a layer as it was written: a layer that read the raw value would
// generate an unwritten declaration one way and describe it the other.
//
// Parsed rather than compared against a word. A boolean option is validated
// with [strconv.ParseBool], which accepts 1, t, T, TRUE, 0, f, F and FALSE as
// well as the two spellings anybody writes — so a declaration written
// header=0 passes validation, and a layer that compared against "false" would
// put a header into the one document that asked for none and report nothing.
func heading(ctx *plugin.Context) bool {
	held, written := ctx.Options.Get(optionHeader)
	if !written {
		return headerByDefault
	}

	on, err := strconv.ParseBool(held)
	if err != nil {
		// Unreachable: a value this cannot parse is refused by the option
		// checker before a layer is asked to generate. The default is the
		// answer that leaves a document able to describe itself.
		return headerByDefault
	}

	return on
}

// codeDelimiter reports a delimiter that is not one character.
//
// In the range a layer forge does not ship takes. Everything below 6000 is
// forge's own, and a code is what says whose documentation to look in.
var codeDelimiter = plugin.Register(6100, "the CSV delimiter is not a single character")

// The characters encoding/csv refuses as a delimiter, and the reason each is
// refused, so that the report says which of them was written rather than that
// something was wrong.
//
// A quote is how a field containing the delimiter is escaped and a line ending
// is how one record is told from the next, so a document delimited by any of
// them has no way to say where a field stops. The standard library refuses all
// four at run time; refusing them here turns that into a diagnostic against the
// declaration, which is the difference between a build that fails and a program
// that does.
var reserved = map[rune]string{
	'"':            "the quote a field containing the delimiter is escaped with",
	'\r':           "half of a line ending",
	'\n':           "half of a line ending",
	utf8.RuneError: "the replacement character, which is what invalid UTF-8 decodes to",
}

// delimiter returns the character between two fields, as a Go rune literal.
//
// One character and not a string. encoding/csv takes a rune, so a delimiter of
// two would be a value the standard library cannot be given — and finding that
// out at run time, in generated code the author cannot edit, is the worst place
// for it.
func delimiter(ctx *plugin.Context) (string, error) {
	held, written := ctx.Options.Get(optionComma)
	if !written {
		held = commaByDefault
	}

	runes := []rune(held)
	if len(runes) != 1 {
		return "", plugin.New(codeDelimiter, ctx.Model.Pos,
			"%s is delimited by %q, which is %d characters",
			ctx.Model.Name, held, len(runes)).
			WithHint("write %s=<one character> on the directive, or leave it out for a comma", optionComma)
	}

	if why, is := reserved[runes[0]]; is {
		return "", plugin.New(codeDelimiter, ctx.Model.Pos,
			"%s is delimited by %q, which a CSV document cannot be: it is %s",
			ctx.Model.Name, held, why).
			WithHint("write %s=<one character> on the directive, or leave it out for a comma", optionComma)
	}

	return quoted(runes[0]), nil
}

// quoted writes a rune as the literal generated code holds it.
//
// [strconv.QuoteRune] would do it, and it renders a tab as '\t' where this
// renders it as the character between two apostrophes. Both compile; the first
// is what a person would have written, so it is the one to write into a file
// people read.
func quoted(r rune) string {
	if r == '\'' || r == '\\' {
		return `'\` + string(r) + `'`
	}
	return "'" + string(r) + "'"
}

// contributed assembles what the layer adds: the container's methods in the
// declaration's own file, and the row codec in the file the package shares.
//
// The division is the one an element layer's output has, arrived at from the
// other end. A row codec is decided entirely by the subject's fields, so two
// declarations over one subject want the same functions and a package can hold
// them once — which is what Provides is for. The container's methods belong to
// the declared type, and there is one of those per declaration.
func contributed(of table, over stack) (plugin.Unit, error) {
	// What both halves may name: the packages this layer's own output imports,
	// and the ones the subject and its fields are spelled with. The second set
	// is the reason this is assembled rather than taken from [Layer.Binds] —
	// Binds answers about what the layer imports of its own, and a subject from
	// another package is found by the spelling instead. A file that named one
	// and imported the other would not compile.
	offered := append(slices.Clone(imports), of.imports...)

	if !over.writes && !over.reads {
		// Nothing beneath offers a walk or a sink, so there is no document to
		// write and none to read — and a lone method naming the columns of a
		// table nothing can produce is worse than silence. The stack composed,
		// which is not the same as having earned anything: what this is is a
		// layer with nothing to contribute, which is what a decorator that
		// withdrew the walk asked for.
		return plugin.Unit{}, nil
	}

	unit, err := parsed(container(over), offered)
	if err != nil {
		return plugin.Unit{}, err
	}

	row, err := parsed(rowCodec(of, over), offered)
	if err != nil {
		return plugin.Unit{}, err
	}

	unit.Provides = map[string]plugin.Unit{of.key: row}

	return unit, nil
}

// parsed turns assembled source into the declarations to emit, binding
// whichever of the offered imports it turned out to name.
//
// Assembled as text and parsed rather than built as syntax, because what is
// written here is a function with loops and branches in it and a tree for one
// is many times its own size. What that costs is the possibility of assembling
// something that is not Go, which is why it is parsed here: a failure to parse
// is reported against the declaration rather than discovered in a file on disk.
//
// Offered wide and narrowed by what was written. This layer's set is the same
// for every declaration and a given one names a few of it, so binding the whole
// set would leave every file importing packages nothing in it mentions — which
// does not compile either.
func parsed(src string, offered []plugin.Import) (plugin.Unit, error) {
	file, fset, err := parse(src)
	if err != nil {
		return plugin.Unit{}, fmt.Errorf("csv: assembled source does not parse: %w", err)
	}

	return plugin.Unit{
		Decls:    file.Decls,
		Comments: file.Comments,
		Fset:     fset,
		Imports:  plugin.Reaching(file.Decls, offered),
	}, nil
}

// Stage says how far along the layer is, which for a layer outside forge is the
// one answer there is.
//
// Answered rather than left silent. A layer that says nothing about itself is
// read as ready, so silence would work — and it would also leave the list
// command with no summary to print beside the marker, which is the other half
// of what saying something buys.
func (Layer) Stage() plugin.Stage { return plugin.StageReady }

// hint joins the sentences of a hint into the one line a report has room for.
func hint(parts ...string) string { return strings.Join(parts, "; ") }
