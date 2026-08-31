package explain

import (
	"io"
	"slices"
	"strings"

	"github.com/okian/forge/internal/layer"
	"github.com/okian/forge/internal/shape"
)

// unknown is what a column shows for a layer that would not answer the question
// it asks. It is not the same as the mark for an empty answer, and the two are
// spelled differently because reading one as the other is the mistake.
const unknown = "?"

// Catalog is what a build knows about the layers it can compose.
type Catalog struct {
	// Layers holds every registered layer, ordered by the marker it claims.
	Layers []Registered
}

// Registered is one layer, described for somebody deciding whether to use it.
type Registered struct {
	// Name is the marker the layer claims, and Kind where in a stack it may
	// appear.
	Name string
	Kind string

	// Stage says how far along it is, and Doc what it is for in one line.
	Stage string
	Doc   string

	// Transparent records that the raw underlying type upholds the layer's
	// invariants, which is what decides whether a declaration over it may be
	// written in an ordinary file.
	Transparent bool

	// Requires, Adds and Masks are the shape it works in: what the stack
	// beneath it must already have, what it contributes, and what it takes away
	// from the layers above.
	//
	// Masks is the capabilities alone. A layer may also withdraw particular
	// methods — a lock replaces the ones that hand out a sequence — and what it
	// withdraws depends on the declaration beneath it, which a catalog does not
	// have. That answer belongs to the resolution of a declaration, where there
	// is one to ask about.
	Requires []string
	Adds     []string
	Masks    []string

	// Probed records that the answers above were obtained by asking the layer
	// and it answered. A layer that refuses every shape, or that cannot be
	// asked at all, has three empty lists that mean "unknown" rather than
	// "none" — and those read alike unless something says which.
	Probed bool

	// Options are what may be written about a declaration using it.
	Options []Option
}

// Option is one option a layer accepts.
type Option struct {
	// Key is the option's name, the "sort" of sort=Age.
	Key string

	// Written is the option as it would appear in a directive, with its
	// accepted values where it has a closed set: "overflow=overwrite|error".
	Written string

	// Values holds the accepted values where the option has a closed set, and
	// nothing where it does not.
	//
	// Beside Written rather than inside it, because the two are for different
	// readers: a person wants the one string a table can hold, and a program
	// wants the set without splitting that string back apart — which is work
	// forge already did and would then be doing twice, differently.
	Values []string

	// Default is the value used when it is not written, and Required records
	// that leaving it out is an error rather than a default.
	Default  string
	Required bool

	// Field records that the option is written above one field of the subject
	// rather than above the declaration.
	//
	// It has to be reported, because it changes where somebody types it. An
	// option shown beside a layer's others and written where they are written is
	// refused with a diagnostic telling the author it belongs somewhere else —
	// so a listing that omitted this would be telling them to do the thing
	// forge then complains about.
	Field bool

	// Doc is the one-line summary of what it decides.
	Doc string
}

// Layers describes every layer a registry holds.
//
// What each layer says about itself, asked of the layer rather than read from a
// table kept beside it. A table would be a second answer to every question here
// — and it would agree with the layers right up until somebody changed one,
// which is exactly when a reader is relying on this.
//
// Three of the answers are not things a layer states, and are worked out by
// asking it: what it requires, what it adds and what it masks are all
// consequences of the shape it returns for a shape it is given, so they are
// found by giving it one and reading the difference.
func Layers(registry *layer.Registry) Catalog {
	if registry == nil {
		return Catalog{}
	}

	out := Catalog{Layers: make([]Registered, 0, registry.Len())}
	for _, one := range registry.All() {
		out.Layers = append(out.Layers, described(one))
	}
	return out
}

// described asks one layer everything the catalog reports about it.
func described(l layer.Layer) Registered {
	pending, staged := unwritten(l)

	needs, probed := requires(l)

	out := Registered{
		Name:        l.Origin().Name,
		Kind:        l.Kind().String(),
		Stage:       stage(pending, staged),
		Doc:         summary(l),
		Transparent: layer.TransparentLayer(l),
		Requires:    names(needs),
		Probed:      probed,
		Options:     accepted(l),
	}

	out.Adds, out.Masks = contributes(l)
	return out
}

// stage says how far along a layer is, in the words the table uses.
//
// Read back from what the report already works out rather than from the layer a
// second time, so that a layer described here as pending is exactly one the
// resolution of a declaration would describe the same way.
func stage(pending, staged bool) string {
	switch {
	case staged:
		return "staged"
	case pending:
		return "stub"
	default:
		return "ready"
	}
}

// requires returns the capabilities a layer will not sit without, and whether
// the layer answered at all.
//
// Found by taking them away one at a time: the layer is offered everything and
// then everything but one capability, and the ones it refuses without are the
// ones it needs. Asking rather than reading a declared list means the answer is
// the one composition will give, since it is the same method being asked.
//
// It assumes what every layer forge ships is: that a layer's answer depends on
// the capabilities beneath it and on nothing else, and that it needs all of
// what it needs at once. A layer that also reads the surface beneath it — a
// decorator wrapping a particular method — is offered a shape with none, and
// refuses everything.
//
// So refusing everything is reported as not having answered, rather than as
// needing nothing. Nothing here can say which capability was at fault, and
// three empty lists that mean "unknown" read exactly like three that mean
// "none".
func requires(l layer.Layer) (needs shape.CapSet, probed bool) {
	if asked, refuses := accepts(l, shape.Shape{Caps: shape.Every()}); !asked || refuses != nil {
		return 0, false
	}

	for _, one := range shape.Every().All() {
		below := shape.Shape{Caps: shape.Every().Without(one)}

		if asked, refuses := accepts(l, below); asked && refuses != nil {
			needs = needs.With(one)
		}
	}
	return needs, true
}

// contributes returns what a layer adds to the stack beneath it and what it
// takes away from the layers above.
//
// Two shapes, because the two answers are only visible against different ones.
// What a layer adds shows against a stack that had nothing, and what it masks
// shows only against one that had everything — a layer withdrawing iteration
// from a stack that could not iterate withdraws nothing anybody can see.
func contributes(l layer.Layer) (adds, masks []string) {
	over, ok := shaped(l, nil, shape.Shape{})
	if !ok {
		return nil, nil
	}

	full := shape.Shape{Caps: shape.Every()}
	under, ok := shaped(l, nil, full)
	if !ok {
		return names(over.Caps), nil
	}

	return names(over.Caps), names(full.Caps.Without(under.Caps.All()...))
}

// accepted returns what may be written about a declaration using this layer.
func accepted(l layer.Layer) []Option {
	schema := l.OptionSchema()
	if len(schema) == 0 {
		return nil
	}

	out := make([]Option, 0, len(schema))
	for _, def := range schema {
		out = append(out, Option{
			Key:      def.Key,
			Written:  def.String(),
			Values:   slices.Clone(def.Values),
			Default:  def.Default,
			Required: def.Required,
			Field:    def.Scope == layer.ScopeField,
			Doc:      def.Doc,
		})
	}
	return out
}

// Text writes the catalog as three tables.
//
// Three because one is unreadable. What a layer is, what shape it works in and
// what it may be configured with are different questions, and the answers are
// different lengths — a row carrying all of them runs past two hundred
// characters, which is not a table on any terminal anybody has.
//
// Every layer appears in the first two and only the configurable ones in the
// third, which is the same division a resolution makes for the same reason: a
// row that exists to hold an empty cell is a row somebody reads and learns
// nothing from.
func (c Catalog) Text(w io.Writer) error {
	var b strings.Builder

	table(&b, []string{"Layer", "Kind", "Stage", "Declare", "Effect"}, func(row func(...string)) {
		for _, one := range c.Layers {
			row(one.Name, one.Kind, one.Stage, declared(one.Transparent), one.Doc)
		}
	})

	b.WriteString("\nShape\n")
	table(&b, []string{"Layer", "Requires", "Adds", "Masks"}, func(row func(...string)) {
		for _, one := range c.Layers {
			if !one.Probed {
				row(one.Name, unknown, unknown, unknown)
				continue
			}
			row(one.Name, list(one.Requires), list(one.Adds), list(one.Masks))
		}
	})

	c.options(&b)

	_, err := io.WriteString(w, b.String())
	return err
}

// options writes the table of what every layer accepts, if any layer accepts
// anything.
//
// Where each one is written is a column rather than a footnote. An option a
// layer accepts is not always one written where the layer's others are, and an
// author who types a field-scoped option above their declaration is refused
// with a diagnostic sending them elsewhere — which is a bad way to find out
// something this table could have said.
func (c Catalog) options(b *strings.Builder) {
	held := false
	for _, one := range c.Layers {
		if len(one.Options) > 0 {
			held = true
			break
		}
	}
	if !held {
		return
	}

	b.WriteString("\nOptions\n")
	table(b, []string{"Layer", "Option", "Written on", "Default", "Effect"}, func(row func(...string)) {
		for _, one := range c.Layers {
			for _, opt := range one.Options {
				row(one.Name, opt.Written, written(opt), given(opt), opt.Doc)
			}
		}
	})
}

// written says where an option goes.
func written(o Option) string {
	if o.Field {
		return "a field"
	}
	return "the declaration"
}

// given says what an option is worth when nobody writes it.
func given(o Option) string {
	switch {
	case o.Required:
		return "required"
	case o.Default != "":
		return o.Default
	default:
		return nothing
	}
}

// declared says where a declaration over this layer may be written.
//
// The question a reader is actually asking, rather than the property that
// answers it: "transparent" is a fact about the underlying type, and what
// somebody wants to know is which file to put their declaration in.
func declared(transparent bool) string {
	if transparent {
		return "anywhere"
	}
	return "spec file"
}
