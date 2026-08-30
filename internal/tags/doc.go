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
//
// The parser carries as little as it can get away with, which is not none. Two
// grammars live here, because there are two kinds of key. The json key has a
// specification — the standard library's — and forge is not free to disagree
// with it, so that grammar is followed exactly, down to which options take a
// value and where a quote may appear. Every other key is a convention, and a
// convention has no authority to be wrong against: commas separate, nothing
// quotes, and a value is whatever follows the first colon or equals sign.
//
// The line between the two is where the parsing stops and the judging starts.
// A tag that cannot be decomposed is reported here, because nothing downstream
// could recover it. A tag that decomposes into options no layer wants, or into
// the same option twice, is not: it parses, and rejecting it belongs to
// whichever layer knows what those options were supposed to mean.
package tags
