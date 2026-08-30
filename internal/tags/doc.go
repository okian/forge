// Package tags holds the single interpretation of Go struct tags that every
// layer shares.
//
// A codec, a validator and a query builder that each parse struct tags for
// themselves will eventually disagree about what a tag means: whether
// json:"-" hides a field or names it "-", whether an empty name inherits the
// field's own name, what omitempty tests. Parsing tags once, here, is what
// keeps everything generated for a single struct self-consistent.
//
// The types in this package describe a parsed tag. They carry no policy: what
// a particular option means is the business of the layer that reads it.
package tags
