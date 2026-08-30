package tags

import "strings"

// Tag is one entry of a struct tag, parsed into the shape every consumer of
// tags shares: a leading name followed by comma-separated options.
//
// The grammar is the one encoding/json established and the rest of the
// ecosystem copied, so `json:"name,omitzero"`, `db:"user_id"` and
// `validate:"required,min=3"` all decompose the same way. Whether a given
// option is meaningful is left to whichever layer reads it.
//
// Splitting on commas is nearly right and not quite. Under json/v2 an option
// value may be single-quoted, and a quoted value may contain commas, so
// `format:'2006-01-02, 15:04'` is one option and not two. Anything that
// reconstructs options from [Tag.Raw] has to respect that.
type Tag struct {
	// Key is the struct tag key this entry was written under: "json", "db",
	// "validate", "forge".
	Key string

	// Raw is the tag's value exactly as written, without the surrounding
	// quotes. It is the text a diagnostic should quote back to the author.
	Raw string

	// Name is the leading name, before the first comma. It is empty when the
	// entry opens with a comma, which conventionally means "keep the field's
	// own name but apply these options".
	Name string

	// Options holds every option written after Name, in the order they were
	// written. Order is preserved because some tag grammars are sensitive to it
	// and because generated output must not depend on map iteration.
	Options []Option

	// Ignored records the conventional `-` value, which excludes the field.
	//
	// Only a bare `-` does this. Under json/v2 a trailing comma with nothing
	// after it is a malformed tag rather than an escape hatch, and naming a
	// field `-` takes a real option after the comma, as in `-,omitzero`.
	Ignored bool
}

// Option is one option following a tag's name.
//
// Both separators in circulation are accepted: json/v2 uses a colon
// (`format:RFC3339`, `case:ignore`), while validator libraries use an equals
// sign (`min=3`). Name and Value hold the two halves whichever was written.
type Option struct {
	// Name is the text before the separator, or the whole entry when there is
	// no separator.
	Name string

	// Value is the text after the separator, empty when there is none. A
	// single-quoted value is stored unquoted; Raw keeps the written form.
	Value string

	// HasValue distinguishes an option written with a separator and an empty
	// value, such as `format:`, from a bare option such as `omitzero`.
	HasValue bool

	// Raw is the option exactly as written, quotes included.
	Raw string
}

// String returns the option as it was written.
func (o Option) String() string { return o.Raw }

// Lookup returns the option written under name, and whether the tag carries
// one.
//
// A repeated option resolves to its first occurrence. That is a lookup policy,
// not a judgement about the tag: json/v2 rejects a repeated option outright,
// and reporting it is validation's job rather than lookup's.
func (t Tag) Lookup(name string) (Option, bool) {
	for _, opt := range t.Options {
		if opt.Name == name {
			return opt, true
		}
	}
	return Option{}, false
}

// Has reports whether the tag carries an option written under name, with or
// without a value.
func (t Tag) Has(name string) bool {
	_, ok := t.Lookup(name)
	return ok
}

// Value returns the value written for the named option. It returns an empty
// string both when the option is absent and when it carries no value; use
// [Tag.Lookup] where the difference matters.
func (t Tag) Value(name string) string {
	opt, _ := t.Lookup(name)
	return opt.Value
}

// Count returns how many times the named option appears, which is what a
// validator needs to reject a repeat.
func (t Tag) Count(name string) int {
	n := 0
	for _, opt := range t.Options {
		if opt.Name == name {
			n++
		}
	}
	return n
}

// IsZero reports whether the tag holds nothing, which is the state of a field
// that carries no tag under this key at all.
func (t Tag) IsZero() bool {
	return t.Key == "" && t.Raw == "" && t.Name == "" && len(t.Options) == 0 && !t.Ignored
}

// String returns the tag as it would be written in source, `key:"value"`, so a
// diagnostic or a debug dump reads like the code it came from.
func (t Tag) String() string {
	var b strings.Builder
	b.WriteString(t.Key)
	b.WriteString(`:"`)
	b.WriteString(t.Raw)
	b.WriteString(`"`)
	return b.String()
}
