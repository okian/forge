package words

import (
	"bytes"
	"compress/flate"
	_ "embed"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"slices"
	"sync"
)

// Dictionary is everything the inflection rules ask of a body of words.
//
// An interface rather than the embedded table directly, and it is the seam a
// second implementation goes through later: a domain vocabulary, or another
// language, registered the way a layer is and consulting the embedded one on a
// miss rather than replacing it. None of that ships now. What ships now is that
// no call site reaches the asset, so that when it does arrive it is a small
// change rather than a search of the tree.
//
// Every method is asked in whatever case the caller has and answers in lower
// case. Matching the case a caller asked in belongs to the caller, because only
// the caller knows whether it is spelling a name or reading a word.
type Dictionary interface {
	// Plural returns a word's plural where the dictionary carries one, and
	// reports false where the regular rules already have the answer.
	Plural(word string) (string, bool)

	// Singular returns the singular a plural came from, and reports false where
	// it is not a plural the dictionary carries.
	Singular(word string) (string, bool)

	// Agent returns the -er form of a verb where the dictionary carries one,
	// which is where the final consonant doubles and nothing in the spelling
	// says so.
	Agent(verb string) (string, bool)

	// Known reports whether the dictionary has heard of a word at all, which is
	// what separates a word whose ending can be read from a domain word that
	// must fall through to the rules.
	Known(word string) bool
}

// asset is the dictionary, built by the converter under gen and committed.
//
// Embedded rather than read at run time, so that go install is the whole
// installation and two people generating from one declaration cannot get two
// answers. It costs the binary tens of kilobytes and the generated code
// nothing: this never runs in the output.
//
//go:embed english.bin
var asset []byte

// decoded is the asset read into tables, built once and only if something asks.
//
// A forge run that resolves no name needing a dictionary pays nothing for it,
// which is most of what a run does — so the decode is behind a once rather than
// in an init, where it would be a cost every invocation of the command paid.
var decoded = sync.OnceValues(func() (Dictionary, error) { return Load(asset) })

// English returns the dictionary compiled into this binary.
//
// A dictionary that will not decode is a broken build rather than a bad input,
// and it is answered with an empty one instead of a panic: every rule in this
// package works without a dictionary, so what a broken asset costs is Persons
// rather than People, and taking the whole command down for it would be worse.
// [Provenance] is where a caller checks which dictionary answered.
func English() Dictionary {
	held, err := decoded()
	if err != nil {
		return empty{}
	}
	return held
}

// dictionary returns what the inflection rules consult.
func dictionary() Dictionary { return English() }

// Provenance returns the line the converter recorded with the dictionary: which
// upstream release it was built from, the checksum of that release, how many
// entries survived, and which converter wrote it.
//
// What it is for is forge explain, which says of a derived name whether the
// dictionary answered or the rules did — an answer that is worth nothing
// without saying which dictionary.
func Provenance() string {
	line, _, _ := bytes.Cut(asset, []byte{'\n'})
	return string(line)
}

// empty is the dictionary a binary with no usable asset has.
type empty struct{}

func (empty) Plural(string) (string, bool)   { return "", false }
func (empty) Singular(string) (string, bool) { return "", false }
func (empty) Agent(string) (string, bool)    { return "", false }
func (empty) Known(string) bool              { return false }

// The sections an asset holds, in the order the converter writes them.
const (
	sectionPlural = iota
	sectionSingular
	sectionAgent
	sectionVocabulary
	sections
)

// Magic marks the body of the asset, after the provenance line.
const magic = "FWD1"

// ErrAsset reports an asset this package cannot read.
var ErrAsset = errors.New("dictionary asset")

// Load reads a dictionary from the bytes the converter under gen writes.
//
// The format is a provenance line in plain text, so that a change to the
// dictionary is a reviewable line in the diff that makes it, and then a
// deflated body holding four sections of sorted key and value pairs. Sorted,
// because the lookup is a binary search over a string that is never taken
// apart: an entry is found by slicing the blob, which costs nothing, rather
// than by walking a map that had to be built first.
//
// An empty asset loads as an empty dictionary rather than as a failure. That is
// what a checkout has before the converter has ever run, and every rule in this
// package works without one.
func Load(held []byte) (Dictionary, error) {
	_, body, found := bytes.Cut(held, []byte{'\n'})
	if !found || len(body) == 0 {
		return empty{}, nil
	}

	plain, err := io.ReadAll(flate.NewReader(bytes.NewReader(body)))
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrAsset, err)
	}
	if !bytes.HasPrefix(plain, []byte(magic)) {
		return nil, fmt.Errorf("%w: body does not open with %s", ErrAsset, magic)
	}

	return decode(plain[len(magic):])
}

// decode reads the section tables out of an uncompressed body.
func decode(plain []byte) (Dictionary, error) {
	var out embedded

	for at := range sections {
		held, rest, err := section(plain)
		if err != nil {
			return nil, fmt.Errorf("%w: section %d: %w", ErrAsset, at, err)
		}
		out.tables[at], plain = held, rest
	}
	return &out, nil
}

// embedded is the dictionary compiled into the binary: four tables and nothing
// else.
//
// Named for how it got here rather than for the language it holds, since the
// language is the asset's business and this is only what reads it.
type embedded struct {
	tables [sections]table
}

func (e *embedded) Plural(word string) (string, bool) {
	return e.tables[sectionPlural].lookup(word)
}

func (e *embedded) Singular(word string) (string, bool) {
	return e.tables[sectionSingular].lookup(word)
}

func (e *embedded) Agent(verb string) (string, bool) {
	return e.tables[sectionAgent].lookup(verb)
}

func (e *embedded) Known(word string) bool {
	for at := range e.tables {
		if _, held := e.tables[at].lookup(word); held {
			return true
		}
	}
	return false
}

// entry is where one key and its value sit in a table's blob.
type entry struct{ at, key, value int32 }

// table is one sorted section of the asset.
//
// The blob is a string rather than a slice of bytes so that a lookup can return
// a piece of it without copying: slicing a string is a header and no
// allocation, which is what makes a dictionary hit as cheap as the bare rule it
// replaces.
type table struct {
	blob    string
	entries []entry
}

// lookup returns the value stored against a key, and whether there is one.
//
// The key is folded as it is compared rather than before, so that asking about
// Person costs nothing that asking about person does not. Every stored key is
// lower-case ASCII, so folding the question preserves the order the table is
// sorted in and the search stays a binary one.
func (t *table) lookup(key string) (string, bool) {
	at, found := slices.BinarySearchFunc(t.entries, key, func(one entry, want string) int {
		return compareFolded(t.blob[one.at:one.at+one.key], want)
	})
	if !found {
		return "", false
	}

	one := t.entries[at]
	return t.blob[one.at+one.key : one.at+one.key+one.value], true
}

// compareFolded orders a stored key against a question asked in any case.
func compareFolded(key, want string) int {
	for at := 0; at < len(key) && at < len(want); at++ {
		held, asked := key[at], want[at]
		if asked >= 'A' && asked <= 'Z' {
			asked = asked - 'A' + 'a'
		}
		if held != asked {
			return int(held) - int(asked)
		}
	}
	return len(key) - len(want)
}

// section reads one table and returns what is left of the body.
func section(plain []byte) (table, []byte, error) {
	count, read := binary.Uvarint(plain)
	if read <= 0 {
		return table{}, nil, errors.New("truncated entry count")
	}
	plain = plain[read:]

	out := table{entries: make([]entry, 0, count)}

	var at int32
	for range count {
		key, value, rest, err := lengths(plain)
		if err != nil {
			return table{}, nil, err
		}

		out.entries = append(out.entries, entry{at: at, key: key, value: value})
		at, plain = at+key+value, rest
	}

	if int(at) > len(plain) {
		return table{}, nil, errors.New("truncated blob")
	}
	out.blob, plain = string(plain[:at]), plain[at:]

	return out, plain, nil
}

// lengths reads one entry's key and value lengths.
func lengths(plain []byte) (int32, int32, []byte, error) {
	key, read := binary.Uvarint(plain)
	if read <= 0 {
		return 0, 0, nil, errors.New("truncated key length")
	}
	plain = plain[read:]

	value, read := binary.Uvarint(plain)
	if read <= 0 {
		return 0, 0, nil, errors.New("truncated value length")
	}

	// A length wider than the blob it indexes is a corrupt asset rather than a
	// long word, and refusing it here is what keeps the arithmetic below honest.
	if key > math.MaxInt32 || value > math.MaxInt32 {
		return 0, 0, nil, errors.New("entry longer than an asset can hold")
	}

	return int32(key), int32(value), plain[read:], nil
}
