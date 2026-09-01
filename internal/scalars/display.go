package scalars

import (
	"go/token"
	"strconv"
	"strings"

	"github.com/okian/forge/internal/diag"
	"github.com/okian/forge/internal/layer"
	"github.com/okian/forge/internal/model"
)

// displaying writes the subject's String, when a display tag asked for one.
//
// A tag rather than a shape, because how a value should read to a person is not
// something its fields determine. Two strings and an int is a name, an address
// and an age in one type and a host, a path and a port in another, and only the
// author knows which fields are the ones anybody wants to see.
func displaying(of Asked, held model.Spelling, diags *diag.Set) (layer.Unit, bool, error) {
	fields, refused := renderable(of, diags)
	if refused || len(fields) == 0 {
		return layer.Unit{}, false, nil
	}

	w := &strings.Builder{}
	w.WriteString("// String returns the subject as the display tags on its fields ask for it.\n")
	w.WriteString("//\n")
	w.WriteString("// The fields in the order they were written, separated by a space, each\n")
	w.WriteString("// labelled where its tag gave it a name. It is a rendering for a person to\n")
	w.WriteString("// read rather than a format anything should parse: nothing here escapes a\n")
	w.WriteString("// separator that turns up inside a value, because a reader can see what\n")
	w.WriteString("// happened and a parser was never the point.\n")
	w.WriteString("func (v " + held.Text + ") String() string {\n")
	w.WriteString("\tvar b strings.Builder\n\n")

	for i, field := range fields {
		if i > 0 {
			w.WriteString("\tb.WriteString(\" \")\n")
		}
		if name, labelled := label(field); labelled {
			w.WriteString("\tb.WriteString(" + quoted(name+"=") + ")\n")
		}
		w.WriteString(appending("b", "v."+field.Name, field))
		w.WriteString("\n")
	}

	w.WriteString("\treturn b.String()\n}\n\n")

	made, err := unit(w, held, displayNeeds(fields)...)
	return made, err == nil, err
}

// renderable returns the tagged fields a rendering can be written from, and
// reports the ones it cannot.
//
// Refusing the whole method rather than leaving a field out. A String missing a
// field the author asked for is a rendering that is quietly wrong everywhere it
// is printed, where a refused declaration is a message with the field's name in
// it — and the second is the one somebody acts on.
func renderable(of Asked, diags *diag.Set) (fields []model.Field, refused bool) {
	if len(shown(of.Subject)) > 0 && clashes(of, diags, displayMethod) {
		return nil, true
	}

	for _, field := range shown(of.Subject) {
		option(field, diags)

		if _, known := scalar(field.Type); known || says(field.Type.Type, of) {
			fields = append(fields, field)
			continue
		}

		diags.Add(diag.New(codeDisplayUnrenderable, at(of, field),
			"%s is %s, which forge cannot render", field.Name, field.Type.String()).
			WithHint("%s", "a displayed field is a predeclared type, or a type with a String of its own; "+
				"drop the tag, or give the type a String"))
		refused = true
	}

	return fields, refused
}

// option reports a display tag carrying something nothing here reads.
//
// Reported for the same reason every inert directive is: an author who wrote it
// believes it does something, and what they are wrong about is not the thing
// they would look at first. The tag's name is read and nothing else is, so an
// option in one is a word typed for no effect.
//
// Reporting it refuses the declaration, because a diagnostic here has no other
// setting: nothing in this build distinguishes a complaint that stops a run
// from one that does not. That is the right end of the trade for this one — an
// option nobody reads means the author expected behaviour they are not getting,
// and a build that carried on would hand them the output they thought they were
// configuring. But it is a decision rather than an accident, and a diagnostic
// added here later should be read as making the same one.
func option(field model.Field, diags *diag.Set) {
	held, tagged := field.Tag(displayTag)
	if !tagged || len(held.Options) == 0 {
		return
	}

	written := make([]string, 0, len(held.Options))
	for _, one := range held.Options {
		written = append(written, one.Name)
	}

	diags.Add(diag.New(codeDisplayOption, field.Pos,
		"%s: display takes a label and nothing else, so %s does nothing",
		field.Name, strings.Join(written, ", ")).
		WithHint("%s", `write the label alone, as in `+"`display:\"age\"`"+`, or drop the tag's options`))
}

// at returns where a complaint about a field points.
//
// The field itself where its position is known, and the declaration otherwise.
// A field of a subject in another package is modelled without one, and a
// diagnostic at the zero position is one no editor can open.
func at(of Asked, field model.Field) token.Position {
	if field.Pos.Filename != "" {
		return field.Pos
	}
	return of.At
}

// displayNeeds returns what rendering those fields reaches for.
//
// Narrowed to what is actually written, because an import nothing names is not
// a warning in Go: it is a file that does not build.
func displayNeeds(fields []model.Field) []model.Import {
	out := []model.Import{stdStrings}

	for _, field := range fields {
		if held, known := scalar(field.Type); known && held.converts {
			return append(out, stdStrconv)
		}
	}
	return out
}

// appending writes one value into a builder.
//
// Through strconv rather than fmt wherever there is a strconv for it, which is
// most of them. A String that reached for fmt would pull the whole of
// reflection into a binary to render an int, and the one thing every emitter
// here promises is that generated code costs what the code it replaces costs.
func appending(into, from string, field model.Field) string {
	if held, known := scalar(field.Type); known {
		return "\t" + into + ".WriteString(" + held.string(from) + ")\n"
	}

	// Not a scalar, so it says how it reads itself — which [renderable] has
	// already established, since a field that does neither is refused rather
	// than written. Calling it is what a person reading the output would have
	// written, and is what fmt would arrive at by way of reflection.
	call := "\t" + into + ".WriteString(" + from + ".String())\n"
	if !empty(field.Type) {
		return call
	}

	// Anything that can be nil is asked first. A String reached through nothing
	// panics, and a String that panics is worse than any rendering of it, since
	// the point of the method is to be safe to reach for. fmt writes <nil> for
	// exactly this, and a reader who sees it will know what happened.
	return "\tif " + from + " == nil {\n" +
		"\t\t" + into + ".WriteString(" + quoted(nothing) + ")\n" +
		"\t} else {\n" +
		"\t" + call +
		"\t}\n"
}

// nothing is what a nil value reads as, which is what fmt writes for one.
const nothing = "<nil>"

// empty reports whether a field can hold nothing at all, so that reaching for
// its String has to ask first.
//
// A pointer and an interface, which are the two that panic. A nil map, slice or
// channel does not: a method on one of those takes the value, and a value that
// is nil is still a value. And an interface is asked through [model.Classified]
// rather than by its class, since a named interface — error, fmt.Stringer — is
// ClassNamed like every other named type, and branching on the class alone is
// the mistake the model's own documentation names.
func empty(held model.Classified) bool {
	return held.Class == model.ClassPointer || held.IsInterface()
}

// quoted returns a Go string literal for text this package assembled.
//
// Through strconv rather than by hand. The text comes from a struct tag, so it
// holds whatever the author wrote — a quote, a backslash, a control byte, or a
// sequence that is not valid UTF-8 at all — and every one of those is a way for
// a hand-rolled escaper to write a file that does not parse, or a label that is
// not what was asked for.
func quoted(held string) string { return strconv.Quote(held) }
