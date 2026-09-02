package main

import (
	"maps"
	"slices"
	"strings"
)

// pair is one key and its value, as the file records them.
type pair struct{ key, value string }

// encode writes the dictionary: a provenance line, a comment explaining the
// format to whoever opens the file next, and the three sections.
//
// Text, because the file is committed and a committed file is source. Somebody
// adding a domain word, correcting a plural, or reviewing what an upstream
// release changed is reading and writing words — and a diff of a compressed
// blob shows none of that, so nobody reviews it. What the reader wants instead
// is a compact table with an index, and that is built from this when the
// dictionary is first asked a question. The optimisation belongs there, once
// per process, rather than here, once and for ever.
//
// The singular table is not written. It is the plural table read backwards, and
// the loader derives it: writing it down as well would be one fact in two
// places, and two places can disagree.
func encode(from built) []byte {
	var out strings.Builder

	out.WriteString("# ")
	out.WriteString(from.provenance())
	out.WriteString("\n")
	out.WriteString(preamble)

	write(&out, "plural", sorted(from.plurals))
	write(&out, "agent", sorted(from.agents))
	write(&out, "vocabulary", known(from.vocabulary))

	return []byte(out.String())
}

// write puts one section into the file, sorted.
//
// Sorted for two reasons that are the same reason. The loader indexes the
// entries in one pass and searches them without sorting again; and a release
// that adds forty words reads as forty added lines rather than as the whole
// file having moved.
func write(into *strings.Builder, name string, table []pair) {
	into.WriteString("\n[")
	into.WriteString(name)
	into.WriteString("]\n")

	for _, one := range table {
		into.WriteString(one.key)
		if one.value != "" {
			into.WriteString("\t")
			into.WriteString(one.value)
		}
		into.WriteString("\n")
	}
}

// sorted turns a table into the ordered pairs the file records, which is what
// keeps two runs over one release from producing two files.
func sorted(held map[string]string) []pair {
	out := make([]pair, 0, len(held))

	for _, key := range slices.Sorted(maps.Keys(held)) {
		out = append(out, pair{key: key, value: held[key]})
	}
	return out
}

// known turns a vocabulary into pairs with nothing on the other side, since
// what is being recorded is that the word exists.
func known(held []string) []pair {
	out := make([]pair, 0, len(held))

	for _, key := range slices.Sorted(slices.Values(held)) {
		out = append(out, pair{key: key})
	}
	return out
}

// preamble is what the file says about itself, so that the first person to open
// it does not have to find this package to know what they are looking at.
const preamble = `#
# The dictionary forge inflects names against, in the form it is reviewed in.
#
# Sections are [name]; blank lines and lines opening with # are ignored. An
# entry is a word on its own, or a word and what it maps to separated by a tab.
# Entries are sorted within a section, which is what lets the loader index the
# file in one pass and what makes an update to the dictionary read as words
# added and removed rather than as a wall of moved bytes. A word inserted out of
# order is still found; the sort is checked when the file is loaded.
#
# There is no singular section. The singular of a word is the plural table read
# backwards, and the loader derives it — where two singulars share a plural,
# the first in alphabetical order wins, so the answer does not depend on the
# order anything was built in. Writing it down as well would be one fact in two
# places, and the two could disagree.
#
# Everything here is the exceptions. A pair the regular rules already get right
# is not carried, because it would be bytes spent reaching the same answer, and
# a word the dictionary has never heard of falls through to those rules — which
# is where a domain word belongs, since a domain word is usually regular.
#
# This file is written by internal/words/gen, from a release of the
# Automatically Generated Inflection Database. Edit it by hand to add a word the
# release does not carry; the converter's overrides.txt is where a correction
# belongs, so that the next release does not undo it.
`
