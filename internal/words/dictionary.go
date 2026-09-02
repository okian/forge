package words

import (
	"bytes"
	_ "embed"
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"
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
// Text, and committed as text. A dictionary is source: somebody adding a domain
// word, correcting a plural, or reviewing what an upstream release changed is
// reading and writing words, and a file they cannot read in the diff is a file
// nobody checks. The compact form a lookup wants is built from this when it is
// first asked for — see [Load] — so the cost of keeping it readable is paid
// once per process that needs a name, and never in the repository.
//
// Embedded rather than read at run time, so that go install is the whole
// installation and two people generating from one declaration cannot get two
// answers. It costs the binary seventy-odd kilobytes and the generated code
// nothing: this never runs in the output.
//
//go:embed english.txt
var asset []byte

// decoded is the asset read into tables, built once and only if something asks.
//
// A forge run that resolves no name needing a dictionary pays nothing for it,
// which is most of what a run does — so the parse is behind a once rather than
// in an init, where it would be a cost every invocation of the command paid.
var decoded = sync.OnceValues(func() (Dictionary, error) { return Load(asset) })

// English returns the dictionary compiled into this binary.
//
// A dictionary that will not parse is a broken build rather than a bad input,
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

// Provenance returns the line the converter recorded at the head of the file:
// which upstream release it was built from, the checksum of that release, how
// many entries survived, and which converter wrote it.
//
// What it is for is forge explain, which says of a derived name whether the
// dictionary answered or the rules did — an answer that is worth nothing
// without saying which dictionary.
func Provenance() string {
	line, _, _ := bytes.Cut(asset, []byte{'\n'})
	return string(bytes.TrimSpace(bytes.TrimPrefix(bytes.TrimSpace(line), []byte("#"))))
}

// empty is the dictionary a binary with no usable asset has.
type empty struct{}

func (empty) Plural(string) (string, bool)   { return "", false }
func (empty) Singular(string) (string, bool) { return "", false }
func (empty) Agent(string) (string, bool)    { return "", false }
func (empty) Known(string) bool              { return false }

// The tables a loaded dictionary holds.
//
// Three of them are sections of the file. The fourth is not written down: see
// [Load].
const (
	sectionPlural = iota
	sectionAgent
	sectionVocabulary
	sectionSingular
	sections
)

// named maps a section header in the file to the table it fills. A header the
// loader does not know is refused rather than skipped, because a section
// spelled wrong is a section silently doing nothing.
var named = map[string]int{
	"plural":     sectionPlural,
	"agent":      sectionAgent,
	"vocabulary": sectionVocabulary,
}

// ErrAsset reports an asset this package cannot read.
var ErrAsset = errors.New("dictionary asset")

// Load reads a dictionary from the text the converter under gen writes.
//
// The format is the one a person edits: comment lines opening with #, a
// [section] header per table, and one entry per line — a word on its own, or a
// word and what it maps to with a tab between. That is the whole of it, and
// there is deliberately no escaping to get wrong, because every value is a word
// with no tab and no newline in it.
//
// What this builds is the shape a lookup wants and the file is a bad one for: a
// single string holding every word laid end to end, and a slice of offsets into
// it, sorted, so that finding an entry is a binary search that slices the blob
// and allocates nothing. That is the optimisation, and this is where it belongs
// — done once, from a file that stayed readable, rather than committed as bytes
// nobody can review.
//
// The singular table is derived here rather than stored. It is the plural table
// read backwards, so writing it down would be one fact in two places that could
// disagree; where two singulars share a plural the first in alphabetical order
// wins, which is what makes the derivation give one answer rather than whichever
// answer an iteration order produced. It costs no bytes at all: the entries
// point into the same blob the plurals do, with the key and the value swapped.
//
// An empty asset loads as an empty dictionary rather than as a failure. That is
// what a checkout has before the converter has ever run, and every rule in this
// package works without one.
func Load(held []byte) (Dictionary, error) {
	// Every offset below is an int32, which is what keeps an entry to sixteen
	// bytes. An asset that could not be addressed by one is not a dictionary
	// that grew — the committed one is seventy kilobytes — so it is refused
	// here, once, rather than checked on every entry.
	if len(held) > math.MaxInt32 {
		return nil, fmt.Errorf("%w: %d bytes is more than an offset can address", ErrAsset, len(held))
	}

	var out embedded

	// One builder for the whole file rather than one per section. The sections
	// are laid into it end to end and every entry carries absolute offsets, so
	// a table is a view of the same string rather than a copy of its share of
	// it — which is also what lets the singulars point at the plurals.
	var blob strings.Builder
	blob.Grow(len(held))

	section := -1
	for line := range lines(held) {
		if name, is := header(line); is {
			// Converted where it is used rather than kept: a string built for
			// a map lookup that does not outlive it is one the compiler does
			// not allocate.
			at, known := named[string(name)]
			if !known {
				return nil, fmt.Errorf("%w: no section is called %q", ErrAsset, name)
			}
			section = at
			continue
		}

		if section < 0 {
			return nil, fmt.Errorf("%w: %q comes before any section", ErrAsset, first(line))
		}

		key, value, err := fields(line, section)
		if err != nil {
			return nil, err
		}

		//nolint:gosec // The asset is shorter than MaxInt32, checked above, and
		// the blob holds a subset of it — so every offset and length here fits.
		out.tables[section].entries = append(out.tables[section].entries, entry{
			keyAt: int32(blob.Len()), keyLen: int32(len(key)),
			valueAt: int32(blob.Len() + len(key)), valueLen: int32(len(value)),
		})
		blob.Write(key)
		blob.Write(value)
	}

	whole := blob.String()
	for at := range sections {
		out.tables[at].blob = whole
	}

	// Sorted so the search below is a binary one. The file is written sorted
	// and normally is, so this is a scan that finds nothing to do — but the
	// file is one a person edits, and a word inserted in the wrong place would
	// otherwise be a word the dictionary silently cannot find.
	for at := range sections {
		out.tables[at].sort()
	}

	out.tables[sectionSingular] = out.tables[sectionPlural].reversed()
	return &out, nil
}

// header returns the name a [section] line carries.
func header(line []byte) ([]byte, bool) {
	if len(line) < 2 || line[0] != '[' || line[len(line)-1] != ']' {
		return nil, false
	}
	return line[1 : len(line)-1], true
}

// fields splits an entry into what it maps from and what it maps to, and holds
// it to the shape its section takes.
//
// The arity is the section's rather than the line's, because the two mistakes a
// hand edit makes are opposites and both are silent otherwise: a plural written
// with no word to map to would answer with the empty string, and a vocabulary
// word written with one would carry a mapping nothing reads.
func fields(line []byte, section int) ([]byte, []byte, error) {
	key, value, tabbed := bytes.Cut(line, tab)
	if bytes.Contains(value, tab) {
		return nil, nil, fmt.Errorf("%w: %q has more than two fields", ErrAsset, first(line))
	}
	if len(key) == 0 {
		return nil, nil, fmt.Errorf("%w: %q begins with a tab", ErrAsset, first(line))
	}

	if section == sectionVocabulary {
		if tabbed {
			return nil, nil, fmt.Errorf("%w: %q is a vocabulary word and maps to something", ErrAsset, first(line))
		}
		return key, nil, nil
	}

	if len(value) == 0 {
		return nil, nil, fmt.Errorf("%w: %q maps to nothing", ErrAsset, first(line))
	}
	return key, value, nil
}

// tab is what separates an entry's two fields, and the only punctuation the
// format has.
var tab = []byte{'\t'}

// lines walks the entries of an asset, leaving out everything that is not one.
//
// It yields the asset's own bytes rather than a string per line. A line becomes
// a string exactly once, when its words are written into the blob, and turning
// each one into a string on the way past would be an allocation per entry to
// build a structure that holds one — five thousand of them, for nothing.
func lines(held []byte) func(func([]byte) bool) {
	return func(yield func([]byte) bool) {
		for len(held) > 0 {
			var line []byte
			if at := bytes.IndexByte(held, '\n'); at >= 0 {
				line, held = held[:at], held[at+1:]
			} else {
				line, held = held, nil
			}

			// Trailing space is invisible in a diff and would otherwise become
			// part of a word. A carriage return is the same thing arriving from
			// a checkout that rewrote the line endings.
			line = bytes.TrimRight(line, " \t\r")
			if len(line) == 0 || line[0] == '#' {
				continue
			}
			if !yield(line) {
				return
			}
		}
	}
}

// first returns enough of a line to place it in the file, without putting a
// whole line of somebody's data into an error message.
func first(line []byte) string {
	const most = 40
	if len(line) <= most {
		return string(line)
	}
	return string(line[:most]) + "…"
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
//
// Both are addressed rather than the value being taken to follow the key,
// because the singular table is the plural table with the two swapped: an
// entry that had to be contiguous would mean a second copy of every word.
type entry struct{ keyAt, keyLen, valueAt, valueLen int32 }

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

// key returns what an entry is found by.
func (t *table) key(one entry) string {
	return t.blob[one.keyAt : one.keyAt+one.keyLen]
}

// sort puts the entries in the order lookup searches, and is a scan over a file
// that is already in it.
//
// Only the offsets move. The blob is addressed absolutely, so ordering the
// entries costs no bytes and touches no words.
func (t *table) sort() {
	if slices.IsSortedFunc(t.entries, func(a, b entry) int {
		return strings.Compare(t.key(a), t.key(b))
	}) {
		return
	}
	slices.SortFunc(t.entries, func(a, b entry) int {
		return strings.Compare(t.key(a), t.key(b))
	})
}

// reversed returns this table read backwards: every value becomes a key and
// every key its value.
//
// Where two keys share a value the first in alphabetical order wins, which the
// entries already being sorted by key is what decides — so the answer is the
// file's, not an iteration order's.
func (t *table) reversed() table {
	out := table{blob: t.blob, entries: make([]entry, 0, len(t.entries))}

	for _, one := range t.entries {
		out.entries = append(out.entries, entry{
			keyAt: one.valueAt, keyLen: one.valueLen,
			valueAt: one.keyAt, valueLen: one.keyLen,
		})
	}
	out.sort()

	// Sorting brought the duplicates together, and the one to keep is the one
	// whose value sorts first — which is the singular that came first in the
	// plural table.
	out.entries = slices.CompactFunc(out.entries, func(a, b entry) bool {
		return out.key(a) == out.key(b)
	})
	return out
}

// lookup returns the value stored against a key, and whether there is one.
//
// The key is folded as it is compared rather than before, so that asking about
// Person costs nothing that asking about person does not. Every stored key is
// lower-case ASCII, so folding the question preserves the order the table is
// sorted in and the search stays a binary one.
func (t *table) lookup(key string) (string, bool) {
	at, found := slices.BinarySearchFunc(t.entries, key, func(one entry, want string) int {
		return compareFolded(t.key(one), want)
	})
	if !found {
		return "", false
	}

	one := t.entries[at]
	return t.blob[one.valueAt : one.valueAt+one.valueLen], true
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
