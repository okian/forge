package explain

import (
	"encoding/json"
	"io"
)

// catalogDocument is the shape a catalog takes when a program is reading it.
//
// A type of its own, for the reason the resolution's is: the table may be
// rearranged whenever it reads better, and this is an interface where a field
// renamed breaks somebody's script. Keeping them apart puts that difference in
// front of whoever edits one of them.
type catalogDocument struct {
	Layers []layerJSON `json:"layers"`
}

// layerJSON is one layer, as a program reads it.
type layerJSON struct {
	Name        string       `json:"name"`
	Kind        string       `json:"kind"`
	Stage       string       `json:"stage"`
	Effect      string       `json:"effect"`
	Transparent bool         `json:"transparent"`
	Probed      bool         `json:"probed"`
	Requires    []string     `json:"requires"`
	Adds        []string     `json:"adds"`
	Masks       []string     `json:"masks"`
	Options     []optionJSON `json:"options"`
}

// optionJSON is one option a layer accepts.
//
// The key and the accepted values separately as well as the string the table
// prints. A reader of the table wants "overflow=overwrite|error" in a cell; a
// program wants the key and the two values without taking that string apart
// again, and both are carried so that neither has to derive the other.
type optionJSON struct {
	Key      string   `json:"key"`
	Written  string   `json:"written"`
	Values   []string `json:"values"`
	Default  string   `json:"default,omitempty"`
	Required bool     `json:"required"`
	Field    bool     `json:"field"`
	Effect   string   `json:"effect"`
}

// JSON writes the catalog as a document a program can read.
//
// Indented, and every list present even when empty, for the reason the
// resolution's is: a tool should not have to tell "requires nothing" from "the
// field was omitted", and somebody diffing two runs should get a diff of what
// changed rather than of where the lines were rewrapped.
func (c Catalog) JSON(w io.Writer) error {
	out := catalogDocument{Layers: make([]layerJSON, 0, len(c.Layers))}

	for _, one := range c.Layers {
		out.Layers = append(out.Layers, layerJSON{
			Name:        one.Name,
			Kind:        one.Kind,
			Stage:       one.Stage,
			Effect:      one.Doc,
			Transparent: one.Transparent,
			Probed:      one.Probed,
			Requires:    each(one.Requires),
			Adds:        each(one.Adds),
			Masks:       each(one.Masks),
			Options:     optioned(one.Options),
		})
	}

	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")

	// And not escaped for HTML, which is the encoder's default and is wrong
	// here for the reason it is wrong for a resolution: this is read by a
	// program and by whoever is looking at it, and an option written
	// sort=\u003cfields\u003e is neither.
	encoder.SetEscapeHTML(false)

	return encoder.Encode(out)
}

// optioned turns a layer's options into what a program reads.
func optioned(held []Option) []optionJSON {
	out := make([]optionJSON, 0, len(held))

	for _, one := range held {
		out = append(out, optionJSON{
			Key:      one.Key,
			Written:  one.Written,
			Values:   each(one.Values),
			Default:  one.Default,
			Required: one.Required,
			Field:    one.Field,
			Effect:   one.Doc,
		})
	}
	return out
}
