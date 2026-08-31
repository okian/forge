package explain

import (
	"encoding/json"
	"io"
)

// document is the shape a resolution takes when a program is reading it.
//
// A type of its own rather than tags on [Resolution], because the two answer to
// different people. The table may be rearranged whenever it reads better; this
// is an interface, and a field renamed here breaks somebody's script. Keeping
// them apart makes that difference visible at the point where it matters, which
// is the moment somebody edits one of them.
type document struct {
	Name        string     `json:"name"`
	Declaration string     `json:"declaration"`
	Package     string     `json:"package,omitempty"`
	Position    string     `json:"position,omitempty"`
	Form        string     `json:"form,omitempty"`
	Steps       []stepJSON `json:"steps"`
}

// stepJSON is one step of a resolution, as a program reads it.
type stepJSON struct {
	Step      int      `json:"step"`
	Name      string   `json:"name"`
	Kind      string   `json:"kind"`
	Effect    string   `json:"effect"`
	Adds      []string `json:"adds"`
	Masks     []string `json:"masks"`
	Exposes   []string `json:"exposes"`
	Methods   []string `json:"methods"`
	Withdraws []string `json:"withdraws"`
	Pending   bool     `json:"pending"`
	Staged    bool     `json:"staged"`
}

// JSON writes the resolution as a document a program can read.
//
// Indented, and every list present even when empty. A tool reading this should
// not have to tell "no capabilities" from "the field was omitted", and a human
// diffing two runs of it should get a diff of what changed rather than of where
// the lines were rewrapped. For the same reason a step carries whether its
// layer generates at all: an empty method list otherwise means both "this layer
// emits nothing" and "nobody has written it yet".
func (r Resolution) JSON(w io.Writer) error {
	out := document{
		Name:        r.Name,
		Declaration: r.Declaration,
		Package:     r.Package,
		Position:    r.Position,
		Steps:       make([]stepJSON, 0, len(r.Steps)),
	}
	if r.Form.Valid() {
		out.Form = r.Form.String()
	}

	for _, step := range r.Steps {
		out.Steps = append(out.Steps, stepJSON{
			Step:      step.Number,
			Name:      step.Name,
			Kind:      step.Kind.String(),
			Effect:    step.Effect,
			Adds:      each(step.Adds),
			Masks:     each(step.Masks),
			Exposes:   each(step.Shape),
			Methods:   each(step.Methods),
			Withdraws: each(step.Withdraws),
			Pending:   step.Pending,
			Staged:    step.Staged,
		})
	}

	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	// Left as written: a capability name and a type name are both identifiers,
	// and escaping the handful of characters a browser cares about would make
	// a generic instantiation unreadable for a reader this output never has.
	encoder.SetEscapeHTML(false)

	return encoder.Encode(out)
}

// each renders a list that is empty rather than absent, so that a reader never
// has to tell one from the other.
func each(of []string) []string {
	if of == nil {
		return []string{}
	}
	return of
}
