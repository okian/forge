package emit

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"slices"
	"strconv"
	"strings"
)

// digestWidth is how many hex characters of the hash a header records.
//
// Eight bytes is far short of the hash's strength and far past what an accident
// could collide with: the question a header answers is whether these inputs are
// the ones this file was made from, and the inputs are not chosen by an
// adversary. The rest is length a reader has to scan past on every file.
const digestWidth = 16

// Digest accumulates the inputs a generated file is made from and reduces them
// to the fingerprint its header records.
//
// It exists so that asking whether a file is stale costs a read and a string
// comparison rather than a regeneration. That only works if the same inputs
// always reduce to the same fingerprint, so inputs are named, sorted before
// hashing, and their lengths are folded in — a caller collecting inputs by
// walking a map cannot produce two answers for one set, and no pair of inputs
// can be run together into a third that hashes alike.
//
// What counts as an input is the caller's to decide, and the answer is
// everything the output depends on. That is more than the source it was
// generated from: it includes the version of forge, the version of the markers,
// the options written on the declaration, and the toolchain, since the same
// declarations formatted by a later gofmt are different bytes. An input left
// out is a file that is stale and reports itself fresh, which is worse than not
// checking at all, because it is trusted.
//
// The zero value is ready to use.
type Digest struct {
	inputs []input
}

// input is one named thing a generated file was made from.
type input struct {
	name    string
	content []byte
}

// Add records one input under a name.
//
// The name is part of the fingerprint, so it has to identify the input rather
// than merely label it: a file's path, a layer's marker, an option's key. Two
// inputs added under one name are both kept, and both count.
func (d *Digest) Add(name string, content []byte) {
	d.inputs = append(d.inputs, input{name: name, content: slices.Clone(content)})
}

// AddString records one input whose content is text.
func (d *Digest) AddString(name, content string) { d.Add(name, []byte(content)) }

// Len returns how many inputs have been recorded.
func (d *Digest) Len() int { return len(d.inputs) }

// String returns the fingerprint of everything recorded so far.
//
// It is safe to call more than once and does not consume what it read, because
// a caller writing several files from overlapping inputs should not have to
// rebuild the digest for each of them.
func (d *Digest) String() string {
	// By content as well as by name. Sorting by name alone would order two
	// inputs recorded under one name by which was recorded first, and the
	// fingerprint would then be a function of the order a caller found them in
	// rather than of the inputs — which is the one thing it must not be.
	sorted := slices.Clone(d.inputs)
	slices.SortFunc(sorted, func(a, b input) int {
		if c := strings.Compare(a.name, b.name); c != 0 {
			return c
		}
		return bytes.Compare(a.content, b.content)
	})

	sum := sha256.New()
	for _, in := range sorted {
		// Lengths, not separators. A separator can appear inside a name or a
		// content, and two inputs that ran together would then hash as one
		// third input that nothing produced.
		sum.Write([]byte(strconv.Itoa(len(in.name))))
		sum.Write([]byte{':'})
		sum.Write([]byte(in.name))
		sum.Write([]byte(strconv.Itoa(len(in.content))))
		sum.Write([]byte{':'})
		sum.Write(in.content)
	}

	return hex.EncodeToString(sum.Sum(nil))[:digestWidth]
}
