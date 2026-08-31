package options

import (
	"go/token"
	"slices"
	"strconv"
	"strings"

	"github.com/okian/forge/internal/diag"
	"github.com/okian/forge/internal/discover"
	"github.com/okian/forge/internal/layer"
	"github.com/okian/forge/internal/model"
)

// The failures an author can write into a directive. Every one of them points
// at the text that has to change — the option, not the declaration — because a
// directive is short and a caret is only worth drawing if it lands.
var (
	codeDirectiveUnnamed = diag.Register(3003, "directive names no layer")
	codeLayerNotInStack  = diag.Register(3004, "directive names a layer the declaration does not use")
	codeDirectiveTwice   = diag.Register(3005, "layer is configured by two directives")
	codeOptionUnknown    = diag.Register(3006, "layer has no such option")
	codeOptionTwice      = diag.Register(3007, "option is written twice")
	codeOptionElsewhere  = diag.Register(3008, "option belongs on a field")
	codeOptionValue      = diag.Register(3009, "option value is not what the option takes")
	codeOptionField      = diag.Register(3010, "option does not name a field of the subject")
	codeOptionMissing    = diag.Register(3011, "layer cannot generate without this option")
	codeOptionUnnamed    = diag.Register(3012, "option has no name")
	codeFieldTwice       = diag.Register(3016, "option names one field twice")
)

// Declaration is what validating a declaration's options needs to know about
// it.
type Declaration struct {
	// Pos is where the declaration was written, which is where a diagnostic
	// about the declaration as a whole points — an option a layer cannot
	// generate without, when no directive was written to leave it out of.
	Pos token.Position

	// Directives are the //forge: comments written on the declaration, in the
	// order they were written.
	Directives []discover.Directive

	// Stack is the layers the declaration names, outermost first. A directive
	// naming anything else is configuring a layer that is not there.
	Stack []model.LayerRef

	// Subject is the model of the type the stack is specialised to, which is
	// what an option naming a field is resolved against. A declaration whose
	// subject was refused carries none, and field names are then left
	// unresolved rather than reported against a subject nobody has.
	Subject *model.Struct
}

// Read validates a declaration's directives against the layers it names.
//
// It returns one set per directive that named a layer in the stack, in the
// order the directives were written, holding only the options that survived —
// and everything wrong with the ones that did not.
//
// Only the survivors, because a layer is handed these to act on. An option
// whose value is not a number would reach a layer as a zero it never wrote, and
// one written about a field would be applied to every field at once, which is
// the opposite of what saying so was for. A layer that reads an option it was
// given should not have to ask whether it was worth giving.
func Read(decl Declaration, registry *layer.Registry) ([]model.Options, diag.Set) {
	var (
		out   []model.Options
		diags diag.Set
	)
	if registry == nil {
		registry = layer.New()
	}

	seen := make(map[string]discover.Directive, len(decl.Directives))

	// A layer somebody plainly meant, however they spelled it. Kept apart from
	// seen, which decides what counts as a repeat: a misspelled directive that
	// went into both would win the duplicate check against the correctly spelled
	// one written after it, so the working directive would be reported as the
	// repeat and its options thrown away.
	meant := make(map[string]bool, len(decl.Directives))

	for _, directive := range decl.Directives {
		if directive.Layer == "" {
			diags.Add(diag.New(codeDirectiveUnnamed, directive.Pos,
				"directive %s names no layer", directive.Text).
				WithHint("%s", "write the layer's name against the prefix, as in //forge:collection sort=Age"))
			continue
		}

		ref, named := within(decl.Stack, directive.Layer)
		if !named {
			diags.Add(diag.New(codeLayerNotInStack, directive.Pos,
				"the declaration does not use a %s layer", directive.Layer).
				WithHint("%s", unnamed(decl.Stack, directive.Layer)))

			// Spoken for, so that a layer the author plainly meant is not also
			// reported as unconfigured. One mistyped name is one mistake.
			if name, ok := resembling(decl.Stack, directive.Layer); ok {
				meant[name] = true
			}
			continue
		}

		// A layer appears at most once per stack, so a second directive for one
		// is two answers to a question with one asker, and nothing says which
		// wins. Reporting the repeat leaves the first in place, which is what
		// the author most likely meant and what a partial run acts on.
		if first, twice := seen[directive.Layer]; twice {
			diags.Add(diag.New(codeDirectiveTwice, directive.Pos,
				"%s is configured twice", directive.Layer).
				WithHint("the first is at %s; write one directive per layer", first.Pos))
			continue
		}
		seen[directive.Layer] = directive
		meant[directive.Layer] = true

		set, problems := options(decl, directive, ref, registry)
		diags.Merge(&problems)
		out = append(out, set)
	}

	diags.Merge(missing(decl, seen, meant, registry))

	return out, diags
}

// options validates the options of one directive against its layer's schema.
func options(decl Declaration, directive discover.Directive, ref model.LayerRef, registry *layer.Registry) (model.Options, diag.Set) {
	var diags diag.Set

	set := model.Options{Layer: directive.Layer, Pos: directive.Pos, Entries: parse(directive)}
	schema, lenient := schemaFor(ref, registry)

	written := make(map[string]model.Option, len(set.Entries))
	kept := set.Entries[:0:0]

	for _, entry := range set.Entries {
		if entry.Key == "" {
			// Quoted as written rather than as parsed: an option whose key and
			// value are both empty renders as nothing at all, and a complaint
			// about "" says less than the two characters the author typed.
			diags.Add(diag.New(codeOptionUnnamed, entry.Pos,
				"an option written as %q names nothing", "="+entry.Value).
				WithHint("%s", "write key=value, or leave the text out"))
			continue
		}

		if first, twice := written[entry.Key]; twice {
			diags.Add(diag.New(codeOptionTwice, entry.Pos,
				"%s is written twice", entry.Key).
				WithHint("the first is at %s; the two would have to be read in some order and nothing says which", first.Pos))
			continue
		}
		written[entry.Key] = entry

		def, known := declared(schema, entry.Key)
		if !known {
			// A layer this release does not ship has a schema nobody has
			// finished writing, so a key it does not list is not yet wrong. The
			// answer its author needs is that the layer is not here at all,
			// which generation gives them.
			if lenient {
				kept = append(kept, entry)
				continue
			}
			diags.Add(diag.New(codeOptionUnknown, entry.Pos,
				"%s has no option %s", directive.Layer, entry.Key).
				WithHint("%s", takes(directive.Layer, schema)))
			continue
		}

		problems := value(decl, directive, entry, def)
		if problems.Empty() {
			kept = append(kept, entry)
			continue
		}
		diags.Merge(&problems)
	}

	set.Entries = kept
	return set, diags
}

// value holds one written option to what its declaration says it takes.
func value(decl Declaration, directive discover.Directive, entry model.Option, def layer.OptionDef) diag.Set {
	var diags diag.Set

	if def.Scope == layer.ScopeField {
		// Where it belongs, not how to write it. A field-scoped directive is
		// written above the field it applies to, and nothing in this build
		// reads one from there yet — so telling an author to move it would
		// trade this complaint for the one about a directive attached to
		// nothing.
		diags.Add(diag.New(codeOptionElsewhere, entry.Pos,
			"%s is about one field of %s rather than about the declaration", entry.Key, directive.Layer).
			WithHint("%s", "an option about a field belongs with the field; nothing reads one from there yet, so remove it for now"))
		return diags
	}

	switch def.Value {
	case layer.ValueNone:
		if entry.Value != "" {
			diags.Add(diag.New(codeOptionValue, entry.Pos,
				"%s takes no value, and was given %q", entry.Key, entry.Value).
				WithHint("write %s on its own", entry.Key))
		}

	case layer.ValueBool:
		if _, err := strconv.ParseBool(entry.Value); err != nil {
			diags.Add(diag.New(codeOptionValue, entry.Pos,
				"%s takes true or false, and was given %q", entry.Key, entry.Value).
				WithHint("%s", "write "+entry.Key+"=true or "+entry.Key+"=false"))
		}

	case layer.ValueInt:
		if _, err := strconv.Atoi(entry.Value); err != nil {
			diags.Add(diag.New(codeOptionValue, entry.Pos,
				"%s takes a whole number, and was given %q", entry.Key, entry.Value).
				WithHint("%s", "write a number, as in "+entry.Key+"=1024"))
		}

	case layer.ValueEnum:
		if !slices.Contains(def.Values, entry.Value) {
			diags.Add(diag.New(codeOptionValue, entry.Pos,
				"%s does not take %q", entry.Key, entry.Value).
				WithHint("%s takes %s", entry.Key, strings.Join(def.Values, ", ")))
		}

	case layer.ValueString:
		if entry.Value == "" {
			diags.Add(diag.New(codeOptionValue, entry.Pos,
				"%s takes a value and was written on its own", entry.Key).
				WithHint("%s", "write "+entry.Key+"=something, or leave the option out"))
		}

	case layer.ValueField, layer.ValueFields:
		problems := fields(decl, entry, def)
		diags.Merge(&problems)
	}

	return diags
}

// fields resolves an option that names one or more of the subject's fields.
//
// A renamed field is the failure this exists for. Left unresolved, an option
// naming one goes on being written, goes on being parsed, and quietly stops
// doing anything — which is the way a struct tag rots, and the reason this
// package refuses to work that way.
func fields(decl Declaration, entry model.Option, def layer.OptionDef) diag.Set {
	var diags diag.Set

	// Nothing to resolve against. The subject was refused, which is already
	// reported, and inventing a second complaint about the fields it does not
	// have would bury it.
	if decl.Subject == nil {
		return diags
	}

	named := []string{entry.Value}
	if def.Value == layer.ValueFields {
		named = strings.Split(entry.Value, ",")
	}

	said := make(map[string]bool, len(named))

	for _, name := range named {
		if said[name] {
			// Naming a field twice asks for the same thing twice, and what a
			// layer would make of it is one declaration written twice into
			// somebody's package. Reported here rather than left to each layer
			// that takes fields, because it is meaningless for all of them and
			// this is the one place that holds the option's own position.
			diags.Add(diag.New(codeFieldTwice, entry.Pos,
				"%s names %s more than once", entry.Key, name).
				WithHint("%s", "write it once; naming it again asks for nothing further"))
			continue
		}
		said[name] = true

		if name == "" {
			// A gap in the list, which is nearly always a space written after a
			// comma: the arguments of a directive are separated by space, so
			// "sort=Age, LastName" is two arguments and the second is read as
			// an option nobody declared.
			diags.Add(diag.New(codeOptionField, entry.Pos,
				"%s names an empty field", entry.Key).
				WithHint("%s", "write the fields with no space between them, as in "+entry.Key+"=Age,LastName"))
			continue
		}
		if _, has := decl.Subject.Field(name); !has {
			spelled := spelling(decl.Subject)
			diags.Add(diag.New(codeOptionField, entry.Pos,
				"%s has no field %s", spelled, name).
				WithHint("%s has %s", spelled, listed(decl.Subject.FieldNames())))
		}
	}

	return diags
}

// missing reports the options a layer cannot generate without.
//
// Once per layer, however many times the stack names it. A stack holding one
// layer twice is a composition failure reported where composition is decided;
// saying the same thing twice about it here would bury that with noise.
func missing(decl Declaration, written map[string]discover.Directive, meant map[string]bool, registry *layer.Registry) *diag.Set {
	var diags diag.Set

	said := make(map[string]bool, len(decl.Stack))

	for _, ref := range decl.Stack {
		name := ref.Directive()
		if said[name] {
			continue
		}
		said[name] = true

		directive, configured := written[name]

		// A layer whose only directive was refused for the way it was spelled
		// is one the author addressed. Demanding its options as well would be a
		// second complaint about one typo.
		if !configured && meant[name] {
			continue
		}

		schema, lenient := schemaFor(ref, registry)
		if lenient {
			continue
		}

		problems := demanded(decl, name, schema, directive, configured)
		diags.Merge(&problems)
	}

	return &diags
}

// demanded reports the options one layer was not given and cannot do without.
func demanded(decl Declaration, name string, schema []layer.OptionDef, directive discover.Directive, configured bool) diag.Set {
	var diags diag.Set

	// Parsed once for the whole layer rather than once per option: a directive
	// is short, but re-reading it per required option is work that grows with a
	// schema nobody has finished writing.
	//
	// Reported against the directive when there is one, since the author is
	// being told to add a key to it; against the declaration when there is
	// none, since there is nothing else to point at.
	var already model.Options
	at := decl.Pos
	if configured {
		already = model.Options{Layer: name, Pos: directive.Pos, Entries: parse(directive)}
		at = directive.Pos
	}

	for _, def := range schema {
		if !def.Required || def.Scope == layer.ScopeField {
			continue
		}
		if _, has := already.Lookup(def.Key); has {
			continue
		}

		diags.Add(diag.New(codeOptionMissing, at,
			"%s cannot generate without %s", name, def.Key).
			WithHint("write //forge:%s %s above the declaration", name, def))
	}

	return diags
}

// within finds the stack entry a directive names.
func within(stack []model.LayerRef, name string) (model.LayerRef, bool) {
	for _, ref := range stack {
		if ref.Directive() == name {
			return ref, true
		}
	}
	return model.LayerRef{}, false
}

// schemaFor returns a layer's options, and whether an unknown key is worth
// reporting against it.
func schemaFor(ref model.LayerRef, registry *layer.Registry) (schema []layer.OptionDef, lenient bool) {
	found, ok := registry.Lookup(ref.Origin)
	if !ok {
		// A marker no layer claims is reported by the stage that resolves the
		// stack. Judging its options against a schema nobody wrote would be a
		// second complaint about one mistake.
		return nil, true
	}

	described, says := found.(layer.Described)
	return found.OptionSchema(), says && described.Stage() == layer.StageStaged
}

// declared finds an option in a schema.
func declared(schema []layer.OptionDef, key string) (layer.OptionDef, bool) {
	for _, def := range schema {
		if def.Key == key {
			return def, true
		}
	}
	return layer.OptionDef{}, false
}

// takes says what a layer accepts here, for the author who wrote something it
// does not.
//
// Here, meaning on a declaration. Offering a field-scoped option to somebody
// writing on a declaration would have them take the advice and get the
// complaint that says the option belongs somewhere else.
func takes(name string, schema []layer.OptionDef) string {
	var spelled []string
	for _, def := range schema {
		if def.Scope == layer.ScopeDeclaration {
			spelled = append(spelled, def.String())
		}
	}

	if len(spelled) == 0 {
		return name + " takes no options on a declaration"
	}
	return name + " takes " + listed(spelled)
}

// unnamed says what to write instead of a layer the declaration does not use.
//
// A directive names its layer in lower case, which is the one way it can be
// written and the one way that is easy to get wrong — a marker is spelled
// Collection and the directive is spelled collection. So a name that differs
// only in case is answered with the spelling that works, rather than with a
// list the right answer is already in.
func unnamed(stack []model.LayerRef, written string) string {
	if meant, ok := resembling(stack, written); ok {
		return "a directive names its layer in lower case: write //forge:" + meant
	}
	if len(stack) == 0 {
		return "the declaration names no layers at all"
	}

	named := make([]string, len(stack))
	for i, ref := range stack {
		named[i] = ref.Directive()
	}
	return "the declaration uses " + listed(named)
}

// resembling finds the layer a name differing only in case was meant for.
func resembling(stack []model.LayerRef, written string) (string, bool) {
	for _, ref := range stack {
		if name := ref.Directive(); strings.EqualFold(name, written) {
			return name, true
		}
	}
	return "", false
}

// spelling names the subject an option is resolved against.
//
// A struct assembled rather than loaded carries no name, and a message opening
// with an empty one reads as a sentence with its first word missing.
func spelling(subject *model.Struct) string {
	if name := subject.Ref().Name; name != "" {
		return name
	}
	return "the subject"
}

// listed renders a set of names as prose, so that a hint reads as a sentence
// rather than as a slice printed into one.
func listed(names []string) string {
	switch len(names) {
	case 0:
		return "nothing"
	case 1:
		return names[0]
	default:
		return strings.Join(names[:len(names)-1], ", ") + " and " + names[len(names)-1]
	}
}
