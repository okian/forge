// Package tmpl holds the JSON wire helpers, as they are emitted.
//
// A generated codec writes bytes and reads them: it appends a string with the
// escapes JSON asks for, finds where a string ends without being fooled by an
// escape inside it, and holds a number to the grammar the standard library
// holds it to. None of that depends on which declaration asked, so it is
// written once here and emitted once per package rather than once per subject.
//
// What stays per subject is everything that knows the subject: the member
// names, their order, which rule each member is held to, and the switch that
// finds a member by name. Those are written against the type and call into
// this, which is the line the two halves are drawn along.
//
// Emitted rather than imported, so a package that holds a generated codec
// holds no dependency it did not already have. That is also why nothing here
// reaches for encoding/json/jsontext: the bytes this writes are the bytes that
// package would write, and the tests beside this file are what hold the two
// together, but a codec that imported it to borrow one function would be a
// codec paying for a coder it never uses.
//
// The comments below are written for the file they end up in rather than for
// this one, since they are what a reader of somebody else's package sees.
package tmpl

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"math"
	"math/bits"
	"slices"
	"strconv"
	"sync"
	"unicode/utf8"
	"unsafe"
)

// What a document can be wrong about.
//
// One sentence each and no position, because the position is the caller's: a
// generated codec knows which member it was reading and adds that, and a
// sentence naming the byte would be a sentence this file cannot write.
var (
	errJSONSyntax    = errors.New("json: invalid syntax")
	errJSONUTF8      = errors.New("json: invalid UTF-8")
	errJSONEscape    = errors.New("json: invalid escape")
	errJSONTruncated = errors.New("json: unexpected end of input")
	errJSONDuplicate = errors.New("json: duplicate object member name")
	errJSONSurrogate = errors.New("json: invalid surrogate pair")
	errJSONRange     = errors.New("json: number out of range for its Go type")
	errJSONDeep      = errors.New("json: value nested deeper than a reader reads")
	errJSONNonfinite = errors.New("json: a non-finite number has no JSON form")
)

// jsonMaxDepth is how deep a document may nest before it is refused.
//
// The bound the standard library holds a document to, and it is held here for
// the same reason: nesting is the one thing a document decides that costs the
// reader stack, and a reader whose stack a document decides is a reader the
// document can stop.
const jsonMaxDepth = 10000

// jsonScratchCap is the size a pooled buffer stops being worth keeping at.
//
// A document that arrived once and was enormous would otherwise park its
// buffer for the life of the process, which is the way a pool turns into a
// leak: the pool holds what the largest caller ever needed rather than what
// the ordinary one needs.
const jsonScratchCap = 1 << 16

// jsonScratch lends the buffers a codec assembles into and unescapes through.
//
// Nothing taken from it escapes the call that took it. A value built in one of
// these and handed back to a caller is copied on the way out, because handing
// back the buffer itself would let the next caller to take it write into bytes
// the first is still reading — which the race detector finds and nothing else
// does.
var jsonScratch = sync.Pool{New: func() any { b := make([]byte, 0, 256); return &b }}

// jsonTakeScratch borrows a buffer.
//
// The assertion is checked rather than assumed, because the pool holds what it
// is given and a panic inside a codec would be reported against the document
// rather than against whoever put the wrong thing in.
func jsonTakeScratch() *[]byte {
	if held, ok := jsonScratch.Get().(*[]byte); ok {
		return held
	}
	fresh := make([]byte, 0, 256)
	return &fresh
}

// jsonDropScratch returns a buffer, unless it has grown past what is worth
// keeping.
func jsonDropScratch(b *[]byte) {
	if cap(*b) <= jsonScratchCap {
		jsonScratch.Put(b)
	}
}

// jsonAppendString appends s as a JSON string.
//
// What is escaped is what encoding/json/v2 escapes when nobody has asked for
// anything else: the quote, the backslash, and the C0 controls — five of those
// by their short forms and the rest as \u00XX. Not the three characters HTML
// would want escaped, not U+007F, and not the two line separators JavaScript
// would want: those are things a caller asks an encoder for, and generated
// code has no encoder to be asked. A caller who needs them applies them to the
// bytes that come back.
//
// Invalid UTF-8 is an error rather than a replacement, which is also what the
// standard library does by default. The alternative writes a document that
// says something different from the value it came from, and says it silently.
func jsonAppendString(dst []byte, s string) ([]byte, error) {
	dst = append(dst, '"')

	// last is where the run of bytes that need nothing done to them began, and
	// wordly is where it is worth asking about eight at a time again.
	//
	// Copying a run at a time rather than a byte at a time is the whole of why
	// this is faster than asking about each one: an ordinary string is one run.
	//
	// The second of those is what keeps the word scan from costing more than
	// it saves. A string of ordinary text is one long run and the scan skips
	// it eight bytes at a time; a string that escapes something every second
	// byte has no run to skip, and asking about a word before each escape is
	// setup paid for nothing. So a failed ask moves the next one eight bytes
	// along: dense escaping falls back to a byte at a time, and text that
	// settles down is picked up again.
	last, wordly := 0, 0
	for i := 0; i < len(s); {
		c := s[i]
		switch {
		case c >= utf8.RuneSelf:
			// Multi-byte, and so nothing to escape — but it does have to be a
			// rune. DecodeRuneInString reports a byte that is not the start of
			// one as U+FFFD of width one, which is the only way it reports it.
			_, size := utf8.DecodeRuneInString(s[i:])
			if size == 1 {
				return dst, errJSONUTF8
			}
			i += size

		case c < ' ' || c == '"' || c == '\\':
			dst = append(dst, s[last:i]...)
			dst = jsonAppendEscape(dst, c)
			i++
			last = i

		case i >= wordly:
			// Worth asking about eight at once. One ask walks every word of
			// ordinary bytes and answers with the byte that stopped it, so
			// text that is one long run costs one ask and one leap. The next
			// ask is moved eight bytes along whatever the answer was: an
			// answer nearby means the text is escaping densely, and the bytes
			// after the escape are cheaper to walk than to ask about again.
			wordly = i + 8
			if next := jsonPlainWords(s, i); next > i {
				i = next
				continue
			}
			i++

		default:
			i++
		}
	}

	dst = append(dst, s[last:]...)
	return append(dst, '"'), nil
}

// jsonCloseText settles a JSON string whose content was appended in place.
//
// The caller wrote an opening quote at mark and let a value's own AppendText
// put its bytes straight after it, which is the append-shaped half of a text
// codec doing what it is for: the ordinary value — a UUID, an address, a
// member's name — lands in the document without ever existing anywhere else.
// What is left is the question the escaper usually answers on the way in, so
// it is answered on what was written: text that is all ordinary bytes gets its
// closing quote and nothing more, and text that is not is rewritten where it
// sits. Either way the verdicts are exactly [jsonAppendString]'s — invalid
// UTF-8 included — which the tests beside this hold byte for byte.
func jsonCloseText(dst []byte, mark int) ([]byte, error) {
	text := dst[mark+1:]
	if len(text) == 0 {
		return append(dst, '"'), nil
	}
	s := unsafe.String(&text[0], len(text)) //nolint:gosec // The view lasts for the scan below, and nothing writes dst while it does.

	// The scan [jsonAppendString] runs, minus the copying: eight bytes at a
	// time while they are ordinary, a byte at a time where they are not, and
	// the same two questions of each — a rune where it is multi-byte, the
	// escaper's detour where it is one of the escaper's bytes.
	wordly := 0
	for i := 0; i < len(s); {
		c := s[i]
		switch {
		case c >= utf8.RuneSelf:
			// Multi-byte and so nothing to escape, but it does have to be a
			// rune, and the continuation bytes are stepped over rather than
			// asked about — a malformed sequence is found at its leading byte.
			_, size := utf8.DecodeRuneInString(s[i:])
			if size == 1 {
				return dst, errJSONUTF8
			}
			i += size

		case c < ' ' || c == '"' || c == '\\':
			return jsonEscapeBehind(dst, mark, i)

		case i >= wordly:
			wordly = i + 8
			if next := jsonPlainWords(s, i); next > i {
				i = next
				continue
			}
			i++

		default:
			i++
		}
	}

	return append(dst, '"'), nil
}

// jsonEscapeBehind rewrites a string's content with its escapes in, having
// found at from the first byte that needs one.
//
// In place rather than through a borrowed buffer, so what the settle costs
// does not depend on a pool's luck: the rest of the text is validated and
// measured first, the buffer is grown once, and then every byte is written at
// its final place, last byte first. Escaping only ever pushes a byte to the
// right, so a cell is read before anything can land on it — which is the whole
// of why the walk runs backwards.
func jsonEscapeBehind(dst []byte, mark, from int) ([]byte, error) {
	length := len(dst) - mark - 1

	// Validated and measured before anything moves, because a refusal must
	// leave the buffer growable rather than half-rewritten, and because the
	// final place of every byte is a function of how much the escapes add.
	var esc [8]byte
	extra := 0
	for i := from; i < length; {
		c := dst[mark+1+i]
		switch {
		case c >= utf8.RuneSelf:
			_, size := utf8.DecodeRune(dst[mark+1+i:])
			if size == 1 {
				return dst, errJSONUTF8
			}
			i += size

		case c < ' ' || c == '"' || c == '\\':
			extra += len(jsonAppendEscape(esc[:0], c)) - 1
			i++

		default:
			i++
		}
	}

	// One growth for the escapes and the closing quote both, then the walk.
	out := slices.Grow(dst, extra+1)[:len(dst)+extra]
	w := len(out)
	for r := length - 1; r >= 0; r-- {
		c := out[mark+1+r]
		if c < ' ' || c == '"' || c == '\\' {
			held := jsonAppendEscape(esc[:0], c)
			w -= len(held)
			copy(out[w:], held)
			continue
		}
		w--
		out[w] = c
	}

	return append(out, '"'), nil
}

// The bytes of a word, for asking about eight at once.
const (
	jsonOnes    = 0x0101010101010101
	jsonHighs   = 0x8080808080808080
	jsonQuotes  = 0x2222222222222222 // '"' in every byte
	jsonSlashes = 0x5c5c5c5c5c5c5c5c // '\\' in every byte
)

// jsonPlainWords advances past bytes a string carries as they are written,
// and returns the index of the first byte that needs attention — as far as
// whole words can say.
//
// It returns i unchanged when the byte at i itself needs attention, or when
// fewer than eight bytes remain; either way the caller learns to go a byte at
// a time — the whole point being that a word is asked about once and either
// clears eight bytes or costs nothing more than the ask.
//
// Eight at a time because the ordinary string is one long run of bytes that
// need nothing done to them, and asking about each one separately is what
// makes an escaper slower than the one it replaces. The word is assembled from
// eight indexed reads off a slice whose bounds were checked once, which the
// compiler turns into a single load — so this needs no unsafe and no
// dependency on the machine's byte order.
//
// A byte needs attention when it is a control byte, a quote, a backslash, or
// part of something multi-byte. The tests below find each of those across a
// whole word: subtracting from every byte and looking at the borrow finds a
// byte below a bound, and exclusive-or against a repeated byte turns a match
// into a zero, which the same trick then finds. Where a word holds one, the
// lowest set bit names the earliest — a borrow can smear a test's answer, but
// only past the first genuine hit, so the position the trailing zeros give is
// exact. Answering with that position instead of the word it sits in is what
// spares the caller a walk back over the bytes the word already cleared.
func jsonPlainWords(s string, i int) int {
	for i+8 <= len(s) {
		chunk := s[i : i+8]
		w := uint64(chunk[0]) | uint64(chunk[1])<<8 | uint64(chunk[2])<<16 |
			uint64(chunk[3])<<24 | uint64(chunk[4])<<32 | uint64(chunk[5])<<40 |
			uint64(chunk[6])<<48 | uint64(chunk[7])<<56

		if mask := jsonBelow(w, ' ') | w&jsonHighs | jsonAnyZero(w^jsonQuotes) | jsonAnyZero(w^jsonSlashes); mask != 0 {
			return i + bits.TrailingZeros64(mask)/8
		}
		i += 8
	}

	return i
}

// jsonAnyZero reports, in the high bit of each byte, which bytes of w are
// zero.
func jsonAnyZero(w uint64) uint64 { return (w - jsonOnes) & ^w & jsonHighs }

// jsonBelow reports, in the high bit of each byte, which bytes of w are less
// than n. Only valid for n below 0x80, which is every bound this file asks
// about.
func jsonBelow(w uint64, n byte) uint64 {
	return (w - jsonOnes*uint64(n)) & ^w & jsonHighs
}

// jsonAppendEscape appends the escape for one byte that needs one.
func jsonAppendEscape(dst []byte, c byte) []byte {
	switch c {
	case '"', '\\':
		return append(dst, '\\', c)
	case '\b':
		return append(dst, `\b`...)
	case '\f':
		return append(dst, `\f`...)
	case '\n':
		return append(dst, `\n`...)
	case '\r':
		return append(dst, `\r`...)
	case '\t':
		return append(dst, `\t`...)
	default:
		const hex = "0123456789abcdef"
		return append(dst, '\\', 'u', '0', '0', hex[c>>4], hex[c&0xf])
	}
}

// jsonAppendFloat appends a float as JSON writes one.
//
// Not what strconv writes with any single verb. JSON has no notion of a
// non-finite number and no preferred exponent form, so the standard library
// picks the verb from the magnitude: fixed notation inside the range where it
// is shorter, exponent notation outside it, and the shortest representation
// that reads back as the same value either way. The one adjustment after that
// is dropping the zero out of a two-digit negative exponent, so that 1e-9
// comes out as it is written here rather than as 1e-09.
//
// width is 32 for a float32 widened to be passed, and 64 for a float64. It
// decides which value the shortest representation has to read back as.
func jsonAppendFloat(dst []byte, f float64, width int) []byte {
	if width == 32 {
		f = float64(float32(f))
	}

	// The comparison is made at the width being written, not at the widest
	// one. A float32 that is the nearest thing to 1e-6 is not less than 1e-6
	// when both are narrowed, and is less than it when both are widened — so
	// asking in float64 about a 32-bit value picks the exponent form for a
	// value the standard library writes in fixed notation.
	abs := math.Abs(f)
	verb := byte('f')
	if abs != 0 {
		if width == 32 && (float32(abs) < 1e-6 || float32(abs) >= 1e21) ||
			width == 64 && (abs < 1e-6 || abs >= 1e21) {
			verb = 'e'
		}
	}

	dst = strconv.AppendFloat(dst, f, verb, -1, width)
	if verb != 'e' {
		return dst
	}

	// e-09 to e-9, and e+09 to e+9 is not a case: strconv writes a positive
	// exponent without the sign and without the padding.
	if n := len(dst); n >= 4 && dst[n-4] == 'e' && dst[n-3] == '-' && dst[n-2] == '0' {
		dst[n-2] = dst[n-1]
		dst = dst[:n-1]
	}

	return dst
}

// jsonAppendFinite appends a float that has a JSON form, and refuses the three
// that do not.
//
// JSON has no NaN and no infinities, and the standard library errors on all
// three unless a caller asks for something else — which generated code has no
// caller to be asked by.
func jsonAppendFinite(dst []byte, f float64, width int) ([]byte, error) {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return dst, errJSONNonfinite
	}
	return jsonAppendFloat(dst, f, width), nil
}

// jsonSkipSpace steps over the whitespace JSON allows between values.
func jsonSkipSpace(b []byte, i int) int {
	// Answered in one comparison for the document every encoder writes: a
	// compact one has no whitespace anywhere this is asked, and every byte of
	// JSON's own alphabet sits above the four space bytes. Only a byte at or
	// below the space is worth asking which one it is.
	if i < len(b) && b[i] > ' ' {
		return i
	}

	for i < len(b) {
		switch b[i] {
		case ' ', '\t', '\n', '\r':
			i++
		default:
			return i
		}
	}
	return i
}

// jsonAtEnd holds a whole document to being one value with nothing after it.
//
// Only a document asks. A value nested inside another is followed by the rest
// of its parent, so a reader that asked this question everywhere would refuse
// every object with two members in it.
func jsonAtEnd(b []byte, i int) error {
	if jsonSkipSpace(b, i) != len(b) {
		return errJSONSyntax
	}
	return nil
}

// jsonScanString finds the bounds of the string beginning at b[i].
//
// It returns where the contents begin and end, where the value after the
// string begins, and whether the contents carry an escape. The last of those
// is what lets the common case cost nothing: a string with no escape in it is
// already the bytes it means, so it can be taken whole rather than rebuilt.
//
// Everything JSON refuses in a string is refused here: a raw control byte, an
// escape that is not one of the eight, a \u that runs off the end, and a byte
// sequence that is not UTF-8. That is the same set encoding/json/jsontext
// refuses, and the tests beside this file are what keep it the same set.
func jsonScanString(b []byte, i int) (lo, hi, next int, esc bool, err error) {
	if i >= len(b) || b[i] != '"' {
		return 0, 0, 0, false, errJSONSyntax
	}
	i++
	lo = i

	// wordly is where it is worth asking about eight bytes at a time again,
	// under the same bargain the escaper strikes: a word of ordinary bytes
	// skips eight, and a word that is not moves the next ask eight along, so
	// escape-dense content stops paying for the ask.
	wordly := 0
	for i < len(b) {
		c := b[i]
		switch {
		case c == '"':
			return lo, i, i + 1, esc, nil

		case c == '\\':
			next, err := jsonScanEscape(b, i)
			if err != nil {
				return 0, 0, 0, false, err
			}
			esc, i = true, next

		case c < ' ':
			// A control byte written into a string rather than escaped. JSON
			// refuses it, and accepting it would let a document carry a byte
			// no document that came from an encoder could carry.
			return 0, 0, 0, false, errJSONSyntax

		case c < utf8.RuneSelf:
			if i >= wordly {
				wordly = i + 8
				if next := jsonPlainBytes(b, i); next > i {
					i = next
					continue
				}
			}
			i++

		default:
			_, size := utf8.DecodeRune(b[i:])
			if size == 1 {
				return 0, 0, 0, false, errJSONUTF8
			}
			i += size
		}
	}

	return 0, 0, 0, false, errJSONTruncated
}

// jsonPlainBytes advances past bytes a string carries as they are written,
// and returns the index of the first byte that needs attention — as far as
// whole words can say. The byte-slice twin of [jsonPlainWords], for the
// reading side, where the byte it names is most often the closing quote: a
// short name or address is cleared in one ask and one leap.
func jsonPlainBytes(b []byte, i int) int {
	for i+8 <= len(b) {
		chunk := b[i : i+8]
		w := uint64(chunk[0]) | uint64(chunk[1])<<8 | uint64(chunk[2])<<16 |
			uint64(chunk[3])<<24 | uint64(chunk[4])<<32 | uint64(chunk[5])<<40 |
			uint64(chunk[6])<<48 | uint64(chunk[7])<<56

		if mask := jsonBelow(w, ' ') | w&jsonHighs | jsonAnyZero(w^jsonQuotes) | jsonAnyZero(w^jsonSlashes); mask != 0 {
			return i + bits.TrailingZeros64(mask)/8
		}
		i += 8
	}

	return i
}

// jsonScanEscape steps over the escape beginning at b[i], which must be the
// backslash, and returns where the byte after it is.
//
// A \u escape is decoded rather than merely shaped, because half of what can
// be wrong with one is in its value: a surrogate half is four perfectly good
// hexadecimal digits naming something that is not a code point, and it is
// legal only as the first of a pair. The standard library refuses a lone half
// even in a value nothing reads, so a scan that waved it through would accept
// documents everything else refuses.
func jsonScanEscape(b []byte, i int) (int, error) {
	if i+1 >= len(b) {
		return 0, errJSONTruncated
	}

	switch b[i+1] {
	case '"', '\\', '/', 'b', 'f', 'n', 'r', 't':
		return i + 2, nil

	case 'u':
		return jsonScanUnicode(b, i)

	default:
		return 0, errJSONEscape
	}
}

// jsonScanUnicode steps over a \u escape and the pair it may be half of. i is
// the backslash.
func jsonScanUnicode(b []byte, i int) (int, error) {
	if i+6 > len(b) {
		return 0, errJSONTruncated
	}

	r, err := jsonHex4(b[i+2 : i+6])
	if err != nil {
		return 0, err
	}
	switch {
	case r < 0xd800 || r > 0xdfff:
		return i + 6, nil
	case r > 0xdbff:
		// A trailing surrogate with no leading one before it.
		return 0, errJSONSurrogate
	case i+12 > len(b):
		return 0, errJSONTruncated
	case b[i+6] != '\\' || b[i+7] != 'u':
		// A leading surrogate with nothing after it to pair with.
		return 0, errJSONSurrogate
	}

	low, err := jsonHex4(b[i+8 : i+12])
	if err != nil {
		return 0, err
	}
	if low < 0xdc00 || low > 0xdfff {
		return 0, errJSONSurrogate
	}
	return i + 12, nil
}

// jsonUnescape appends the contents of a JSON string with its escapes decoded.
//
// Only called for a string jsonScanString said carried one, so the ordinary
// string never reaches it. A lone surrogate is refused rather than replaced,
// which is what the standard library does when nobody has asked it to allow
// invalid UTF-8: a surrogate half is a well-formed escape naming something
// that is not a code point, and writing the replacement rune instead would
// hand back a string the document does not contain.
func jsonUnescape(dst, b []byte) ([]byte, error) {
	for i := 0; i < len(b); {
		c := b[i]
		if c != '\\' {
			dst = append(dst, c)
			i++
			continue
		}

		i++
		if i >= len(b) {
			return dst, errJSONTruncated
		}
		switch b[i] {
		case '"', '\\', '/':
			dst = append(dst, b[i])
			i++
		case 'b':
			dst = append(dst, '\b')
			i++
		case 'f':
			dst = append(dst, '\f')
			i++
		case 'n':
			dst = append(dst, '\n')
			i++
		case 'r':
			dst = append(dst, '\r')
			i++
		case 't':
			dst = append(dst, '\t')
			i++
		case 'u':
			r, size, err := jsonScanEscapedRune(b[i-1:])
			if err != nil {
				return dst, err
			}
			dst = utf8.AppendRune(dst, r)
			i += size - 1
		default:
			return dst, errJSONEscape
		}
	}

	return dst, nil
}

// jsonScanEscapedRune decodes one \u escape and the pair it may be half of.
//
// It returns the rune and how many bytes of b it read, counting the backslash.
func jsonScanEscapedRune(b []byte) (rune, int, error) {
	if len(b) < 6 {
		return 0, 0, errJSONTruncated
	}

	r, err := jsonHex4(b[2:6])
	if err != nil {
		return 0, 0, err
	}

	switch {
	case r < 0xd800 || r > 0xdfff:
		return r, 6, nil
	case r > 0xdbff:
		// A trailing surrogate with no leading one before it.
		return 0, 0, errJSONSurrogate
	case len(b) < 12 || b[6] != '\\' || b[7] != 'u':
		// A leading surrogate with nothing after it to pair with.
		return 0, 0, errJSONSurrogate
	}

	low, err := jsonHex4(b[8:12])
	if err != nil {
		return 0, 0, err
	}
	if low < 0xdc00 || low > 0xdfff {
		return 0, 0, errJSONSurrogate
	}

	return 0x10000 + (r-0xd800)<<10 + (low - 0xdc00), 12, nil
}

// jsonHex4 reads four hexadecimal digits.
func jsonHex4(b []byte) (rune, error) {
	var r rune
	for _, c := range b {
		r <<= 4
		switch {
		case c >= '0' && c <= '9':
			r |= rune(c - '0')
		case c >= 'a' && c <= 'f':
			r |= rune(c-'a') + 10
		case c >= 'A' && c <= 'F':
			r |= rune(c-'A') + 10
		default:
			return 0, errJSONEscape
		}
	}
	return r, nil
}

// jsonScanNumber finds the bounds of the number beginning at b[i].
//
// The grammar is JSON's and not Go's, which is stricter in three places that
// matter: no leading plus, no leading zero on a number with more digits after
// it, and a decimal point or an exponent that has to be followed by a digit.
// A reader that took Go's grammar would accept documents no encoder writes and
// no other reader reads.
func jsonScanNumber(b []byte, i int) (lo, hi int, err error) {
	lo = i

	if i < len(b) && b[i] == '-' {
		i++
	}

	i, err = jsonScanWhole(b, i)
	if err != nil {
		return 0, 0, err
	}
	if i, err = jsonScanFraction(b, i); err != nil {
		return 0, 0, err
	}
	if i, err = jsonScanExponent(b, i); err != nil {
		return 0, 0, err
	}

	return lo, i, nil
}

// jsonScanWhole steps over the digits before the point.
//
// One zero, or a digit that is not zero followed by as many as there are. That
// is where JSON differs from Go and from every language whose literals allow a
// leading zero: 007 is three characters JSON has no number for.
func jsonScanWhole(b []byte, i int) (int, error) {
	switch {
	case i >= len(b):
		return 0, errJSONTruncated
	case b[i] == '0':
		return i + 1, nil
	case b[i] >= '1' && b[i] <= '9':
		for i < len(b) && jsonIsDigit(b[i]) {
			i++
		}
		return i, nil
	default:
		return 0, errJSONSyntax
	}
}

// jsonScanFraction steps over a decimal point and the digits after it, if
// there is one. A point with no digit after it is not a number.
func jsonScanFraction(b []byte, i int) (int, error) {
	if i >= len(b) || b[i] != '.' {
		return i, nil
	}

	i++
	if i >= len(b) || !jsonIsDigit(b[i]) {
		return 0, errJSONSyntax
	}
	for i < len(b) && jsonIsDigit(b[i]) {
		i++
	}
	return i, nil
}

// jsonScanExponent steps over an exponent, if there is one. A sign is allowed
// there and nowhere else in a JSON number, and the digits are not optional.
func jsonScanExponent(b []byte, i int) (int, error) {
	if i >= len(b) || (b[i] != 'e' && b[i] != 'E') {
		return i, nil
	}

	i++
	if i < len(b) && (b[i] == '+' || b[i] == '-') {
		i++
	}
	if i >= len(b) || !jsonIsDigit(b[i]) {
		return 0, errJSONSyntax
	}
	for i < len(b) && jsonIsDigit(b[i]) {
		i++
	}
	return i, nil
}

// jsonIsDigit reports whether a byte is a decimal digit.
func jsonIsDigit(c byte) bool { return c >= '0' && c <= '9' }

// jsonScanInt reads a JSON number into a signed integer of the given width.
//
// A number with a fraction or an exponent is refused rather than truncated,
// which is what the standard library does with one: 1.0 into an int is a
// document describing a value the field cannot hold, and rounding it would be
// this codec deciding what the document meant.
//
// The digits are accumulated here rather than handed to strconv, because this
// is the hottest scan a numeric document has and the grammar was already being
// walked to find where the number ends — parsing it again would read every
// digit twice.
func jsonScanInt(b []byte, i, width int) (int64, int, error) {
	neg := i < len(b) && b[i] == '-'
	if neg {
		i++
	}

	held, next, err := jsonDigits(b, i)
	if err != nil {
		return 0, 0, err
	}

	limit := uint64(1) << (width - 1)
	if !neg {
		limit--
	}
	if held > limit {
		return 0, 0, errJSONRange
	}
	if neg {
		return -int64(held), next, nil //nolint:gosec // held is at most 1<<(width-1), checked above, and negating carries the last unit.
	}
	return int64(held), next, nil //nolint:gosec // held is at most 1<<(width-1)-1, checked above.
}

// jsonScanUint reads a JSON number into an unsigned integer of the given
// width.
func jsonScanUint(b []byte, i, width int) (uint64, int, error) {
	if i < len(b) && b[i] == '-' {
		return 0, 0, errJSONRange
	}

	held, next, err := jsonDigits(b, i)
	if err != nil {
		return 0, 0, err
	}
	if width < 64 && held >= uint64(1)<<width {
		return 0, 0, errJSONRange
	}
	return held, next, nil
}

// jsonDigits accumulates the digits of an integer, holding them to JSON's
// grammar: one zero or a run that does not start with one, and no point or
// exponent after it, since an integer is what is being read.
func jsonDigits(b []byte, i int) (uint64, int, error) {
	if i >= len(b) {
		return 0, 0, errJSONTruncated
	}

	var held uint64
	switch {
	case b[i] == '0':
		i++
	case b[i] >= '1' && b[i] <= '9':
		for i < len(b) && jsonIsDigit(b[i]) {
			digit := uint64(b[i] - '0')
			if held > (math.MaxUint64-digit)/10 {
				return 0, 0, errJSONRange
			}
			held = held*10 + digit
			i++
		}
	default:
		return 0, 0, errJSONSyntax
	}

	if i < len(b) {
		switch b[i] {
		case '.', 'e', 'E', '0', '1', '2', '3', '4', '5', '6', '7', '8', '9':
			// A fraction or an exponent is a number the integer cannot hold,
			// and a digit here is only reachable after a leading zero, which
			// JSON gives no second digit.
			return 0, 0, errJSONSyntax
		}
	}
	return held, i, nil
}

// jsonScanFloat reads a JSON number into a float of the given width.
func jsonScanFloat(b []byte, i, width int) (float64, int, error) {
	lo, hi, err := jsonScanNumber(b, i)
	if err != nil {
		return 0, 0, err
	}

	held, err := strconv.ParseFloat(string(b[lo:hi]), width)
	if err != nil {
		// Out of range is the only way this fails on a number the grammar
		// above accepted, and it is refused rather than kept: ParseFloat
		// returns an infinity there, and an infinity is not a number JSON can
		// carry, so a codec that kept it would read a document into a value
		// that cannot be written back out.
		if errors.Is(err, strconv.ErrRange) {
			return 0, 0, errJSONRange
		}
		return 0, 0, errJSONSyntax
	}
	return held, hi, nil
}

// jsonScanBool reads true or false.
func jsonScanBool(b []byte, i int) (bool, int, error) {
	switch {
	case i+4 <= len(b) && string(b[i:i+4]) == "true":
		return true, i + 4, nil
	case i+5 <= len(b) && string(b[i:i+5]) == "false":
		return false, i + 5, nil
	default:
		return false, 0, errJSONSyntax
	}
}

// jsonScanNull reads null, and reports whether that is what was there.
//
// A reader asks before it reads anything else, because null is what every
// shape may be instead of itself.
func jsonScanNull(b []byte, i int) (int, bool) {
	if i+4 <= len(b) && string(b[i:i+4]) == "null" {
		return i + 4, true
	}
	return i, false
}

// jsonSkipValue steps over a value nobody asked for, which is what an unknown
// member gets.
//
// Stepping over is not looking away. The standard library holds a member
// nothing reads to the same grammar as one everything reads — the syntax, the
// escapes, the number grammar, UTF-8, duplicate names in an object nobody
// wanted — so a reader that relaxed any of it for the members it skips would
// accept documents the rest of the world refuses. depth is how many values
// this one is already inside, which is what bounds a document that nests
// forever.
func jsonSkipValue(b []byte, i, depth int) (int, error) {
	i = jsonSkipSpace(b, i)
	if i >= len(b) {
		return 0, errJSONTruncated
	}

	switch b[i] {
	case '"':
		_, _, next, _, err := jsonScanString(b, i)
		return next, err

	case 't', 'f':
		_, next, err := jsonScanBool(b, i)
		return next, err

	case 'n':
		next, ok := jsonScanNull(b, i)
		if !ok {
			return 0, errJSONSyntax
		}
		return next, nil

	case '{':
		return jsonSkipObject(b, i+1, depth+1)

	case '[':
		return jsonSkipArray(b, i+1, depth+1)

	default:
		_, hi, err := jsonScanNumber(b, i)
		return hi, err
	}
}

// jsonSkipObject steps over an object's members, holding each to the grammar
// and the whole set to the no-duplicates rule. i is just past the brace.
func jsonSkipObject(b []byte, i, depth int) (int, error) {
	if depth > jsonMaxDepth {
		return 0, errJSONDeep
	}

	var names jsonNames
	for first := true; ; first = false {
		next, done, err := jsonMemberNext(b, i, first)
		if err != nil {
			return 0, err
		}
		if done {
			return next, nil
		}

		lo, hi, at, esc, err := jsonMemberName(b, next)
		if err != nil {
			return 0, err
		}
		if names.unknown(b, lo, hi, esc) {
			return 0, errJSONDuplicate
		}

		if i, err = jsonSkipValue(b, at, depth); err != nil {
			return 0, err
		}
	}
}

// jsonSkipArray steps over an array's elements. i is just past the bracket.
func jsonSkipArray(b []byte, i, depth int) (int, error) {
	if depth > jsonMaxDepth {
		return 0, errJSONDeep
	}

	for first := true; ; first = false {
		next, done, err := jsonElementNext(b, i, first)
		if err != nil {
			return 0, err
		}
		if done {
			return next, nil
		}

		if i, err = jsonSkipValue(b, next, depth); err != nil {
			return 0, err
		}
	}
}

// jsonNames records the member names an object has already used, so that a
// name written twice is refused the way the standard library refuses it.
//
// Two halves, because the two cases are not alike. A member the subject
// declares is one of a set known when the code was written, so it is a bit in
// a word and the test is an and. A member nothing declares is an arbitrary
// string, and the only thing to compare it against is the ones before it —
// held as the spans they occupy in the document rather than as strings, so
// that an object with no unknown members in it allocates nothing and an object
// with a few costs a few comparisons.
//
// Quadratic in the number of unknown members, which is the right shape: a
// document carrying many members nothing reads is rare, and the map that would
// make it linear costs an allocation and a hash on every document that carries
// none.
// declared holds one bit per member the subject declares, and spilled the rest
// for a subject with more than sixty-four. few holds the first several member
// names nothing declared, in place, so that the ordinary object costs no
// allocation to check; more holds the rest of a document that turns out to
// carry many, and n counts what few holds.
type jsonNames struct {
	declared uint64
	spilled  []uint64
	few      [8]jsonSpan
	n        int
	more     []jsonSpan
}

// jsonSpan is where one member name sits in the document, and whether reading
// it means decoding an escape.
type jsonSpan struct {
	lo, hi int
	esc    bool
}

// declare records a declared member by its index, and reports whether it had
// already been recorded.
func (n *jsonNames) declare(index int) bool {
	word, bit := index/64, uint(index%64)
	if word == 0 {
		if n.declared&(1<<bit) != 0 {
			return true
		}
		n.declared |= 1 << bit
		return false
	}

	for len(n.spilled) < word {
		n.spilled = append(n.spilled, 0)
	}
	if n.spilled[word-1]&(1<<bit) != 0 {
		return true
	}
	n.spilled[word-1] |= 1 << bit
	return false
}

// saw reports whether a declared member arrived.
//
// What it is for is the rule a member cannot answer by arriving: a member the
// document never mentioned is one whose rules have to be held against whatever
// the destination already had, and this is how a reader knows which those are.
func (n *jsonNames) saw(index int) bool {
	word, bit := index/64, uint(index%64)
	if word == 0 {
		return n.declared&(1<<bit) != 0
	}
	if word > len(n.spilled) {
		return false
	}
	return n.spilled[word-1]&(1<<bit) != 0
}

// unknown records a member name nothing declared, and reports whether the same
// name has already been seen.
//
// Two names are the same name when they decode to the same string, which for
// the ordinary pair — neither carrying an escape — is their bytes being equal.
// A pair where either side carries one is decoded first, because "a" and
// "a" are one name written two ways and the standard library refuses the
// pair.
func (n *jsonNames) unknown(b []byte, lo, hi int, esc bool) bool {
	held := jsonSpan{lo: lo, hi: hi, esc: esc}
	for at := range n.n {
		if jsonSameName(b, n.few[at], held) {
			return true
		}
	}
	for _, seen := range n.more {
		if jsonSameName(b, seen, held) {
			return true
		}
	}

	if n.n < len(n.few) {
		n.few[n.n] = held
		n.n++
		return false
	}
	n.more = append(n.more, held)
	return false
}

// jsonSameName reports whether two spans hold one member name.
func jsonSameName(b []byte, a, c jsonSpan) bool {
	if !a.esc && !c.esc {
		return bytes.Equal(b[a.lo:a.hi], b[c.lo:c.hi])
	}

	// At least one side carries an escape, so both are decoded. The contents
	// were validated when they were scanned, so decoding cannot fail — and a
	// scratch pair per comparison is fine on a path only an escaped name
	// reaches.
	one, two := jsonTakeScratch(), jsonTakeScratch()
	left, _ := jsonUnescape((*one)[:0], b[a.lo:a.hi])
	right, _ := jsonUnescape((*two)[:0], b[c.lo:c.hi])
	same := bytes.Equal(left, right)
	*one, *two = left, right
	jsonDropScratch(one)
	jsonDropScratch(two)
	return same
}

// jsonMemberNext finds the next member of an object, stepping over the comma
// that separates it from the one before. It returns where the member's name
// begins, or that the closing brace ended the object.
//
// The grammar lives here so that every reader of an object — a generated
// decoder, the skipper above, a map — refuses the same documents: a comma
// before the first member, a missing one between two, and a trailing one
// before the brace.
func jsonMemberNext(b []byte, i int, first bool) (int, bool, error) {
	i = jsonSkipSpace(b, i)
	if i >= len(b) {
		return 0, false, errJSONTruncated
	}

	switch b[i] {
	case '}':
		return i + 1, true, nil
	case ',':
		if first {
			return 0, false, errJSONSyntax
		}
		return jsonSkipSpace(b, i+1), false, nil
	default:
		if !first {
			return 0, false, errJSONSyntax
		}
		return i, false, nil
	}
}

// jsonMemberName reads a member's name and the colon after it, and returns
// where the member's value begins.
func jsonMemberName(b []byte, i int) (lo, hi, next int, esc bool, err error) {
	lo, hi, next, esc, err = jsonScanString(b, i)
	if err != nil {
		return 0, 0, 0, false, err
	}

	next = jsonSkipSpace(b, next)
	if next >= len(b) {
		return 0, 0, 0, false, errJSONTruncated
	}
	if b[next] != ':' {
		return 0, 0, 0, false, errJSONSyntax
	}
	return lo, hi, jsonSkipSpace(b, next+1), esc, nil
}

// jsonElementNext finds the next element of an array, stepping over the comma
// that separates it from the one before. It returns where the element begins,
// or that the closing bracket ended the array.
func jsonElementNext(b []byte, i int, first bool) (int, bool, error) {
	i = jsonSkipSpace(b, i)
	if i >= len(b) {
		return 0, false, errJSONTruncated
	}

	switch b[i] {
	case ']':
		return i + 1, true, nil
	case ',':
		if first {
			return 0, false, errJSONSyntax
		}
		return jsonSkipSpace(b, i+1), false, nil
	default:
		if !first {
			return 0, false, errJSONSyntax
		}
		return i, false, nil
	}
}

// jsonName returns a member name's bytes, decoding them through scratch when
// the name carries an escape.
//
// The ordinary name is handed back as the span it already is, so dispatching
// on it costs nothing; switch string(name) compares without allocating by
// compiler rule. The contents were validated by the scan, so decoding cannot
// fail.
func jsonName(b []byte, lo, hi int, esc bool, scratch *[]byte) []byte {
	if !esc {
		return b[lo:hi]
	}
	*scratch, _ = jsonUnescape((*scratch)[:0], b[lo:hi])
	return *scratch
}

// jsonString returns the string a scanned span holds.
//
// borrow is the sharp edge, asked for by name: the string returned points into
// b rather than copying out of it, so it is exact as long as b outlives it and
// is not written over, and wrong the moment either fails. A span with an
// escape in it is decoded into fresh bytes either way, so borrowing buys
// nothing there and the copy is not a choice.
func jsonString(b []byte, lo, hi int, esc, borrow bool) string {
	if esc {
		scratch := jsonTakeScratch()
		held, _ := jsonUnescape((*scratch)[:0], b[lo:hi])
		out := string(held)
		*scratch = held
		jsonDropScratch(scratch)
		return out
	}

	if lo == hi {
		return ""
	}
	if borrow {
		return unsafe.String(&b[lo], hi-lo) //nolint:gosec // The borrow is the function's contract, stated above; the span was bounds-checked by the scan.
	}
	return string(b[lo:hi])
}

// jsonAppendBytes appends a byte slice as JSON carries one: base64 in a
// string. A nil slice is the empty string rather than null, which is the
// standard library's encoding and not a choice available here.
func jsonAppendBytes(dst, held []byte) []byte {
	dst = append(dst, '"')
	dst = base64.StdEncoding.AppendEncode(dst, held)
	return append(dst, '"')
}

// jsonScanBytes decodes the base64 a scanned string span holds, into dst's
// capacity where it fits.
//
// Line breaks are refused by hand, because the two libraries disagree about
// them: the base64 package skips a newline inside an encoding and the standard
// library's JSON reader refuses it, and the reader is the behaviour being
// promised. Padding bits under the final character are not checked, because
// the reader does not check them either.
func jsonScanBytes(b []byte, lo, hi int, esc bool, dst []byte) ([]byte, error) {
	src := b[lo:hi]
	if esc {
		scratch := jsonTakeScratch()
		defer jsonDropScratch(scratch)
		held, _ := jsonUnescape((*scratch)[:0], src)
		*scratch = held
		src = held
	}

	for _, c := range src {
		if c == '\r' || c == '\n' {
			return nil, errJSONSyntax
		}
	}

	need := base64.StdEncoding.DecodedLen(len(src))
	if cap(dst) < need {
		dst = make([]byte, need)
	} else {
		dst = dst[:need]
	}
	n, err := base64.StdEncoding.Decode(dst, src)
	if err != nil {
		return nil, errJSONSyntax
	}
	return dst[:n], nil
}

// jsonWroteEmpty reports whether the value written at b[at:] is one of the
// four JSON empties, which is what omitempty leaves out: null, the empty
// string, the empty object and the empty array.
func jsonWroteEmpty(b []byte, at int) bool {
	switch len(b) - at {
	case 2:
		return (b[at] == '"' && b[at+1] == '"') ||
			(b[at] == '{' && b[at+1] == '}') ||
			(b[at] == '[' && b[at+1] == ']')
	case 4:
		return b[at] == 'n' && b[at+1] == 'u' && b[at+2] == 'l' && b[at+3] == 'l'
	default:
		return false
	}
}

// jsonKeysScratch lends the slices a map's keys are gathered and sorted in.
var jsonKeysScratch = sync.Pool{New: func() any { held := make([]string, 0, 16); return &held }}

// jsonSortedKeys gathers a map's keys and sorts them, into a borrowed slice.
//
// Members of an object come out in sorted key order, always: range order
// varies between two runs over one map, and generated output that differed
// between runs would be a diff nobody could account for. The slice is borrowed
// rather than made because gathering keys is the entire allocation profile of
// writing a map, and give it back with [jsonDropKeys].
func jsonSortedKeys[K ~string, V any](m map[K]V) *[]string {
	keys := jsonTakeKeys()
	held := (*keys)[:0]
	for k := range m {
		held = append(held, string(k))
	}
	slices.Sort(held)
	*keys = held
	return keys
}

// jsonTakeKeys borrows a key slice.
func jsonTakeKeys() *[]string {
	if held, ok := jsonKeysScratch.Get().(*[]string); ok {
		return held
	}
	fresh := make([]string, 0, 16)
	return &fresh
}

// jsonDropKeys returns a key slice, unless it has grown past what is worth
// keeping. What it held is cleared first, so the pool does not keep somebody's
// strings alive for the life of the process.
func jsonDropKeys(keys *[]string) {
	if cap(*keys) > 1<<12 {
		return
	}
	clear(*keys)
	jsonKeysScratch.Put(keys)
}

// jsonNumsScratch lends the slices a numeric-keyed map's keys are gathered and
// sorted in. One pool for every numeric kind, because every kind's bits fit a
// uint64 and a pool cannot lend a slice of a type parameter.
var jsonNumsScratch = sync.Pool{New: func() any { held := make([]uint64, 0, 16); return &held }}

// jsonTakeNums borrows a numeric key slice.
func jsonTakeNums() *[]uint64 {
	if held, ok := jsonNumsScratch.Get().(*[]uint64); ok {
		return held
	}
	fresh := make([]uint64, 0, 16)
	return &fresh
}

// jsonDropNums returns a numeric key slice, unless it has grown past what is
// worth keeping. Numbers keep nothing alive, so nothing is cleared.
func jsonDropNums(keys *[]uint64) {
	if cap(*keys) > 1<<12 {
		return
	}
	jsonNumsScratch.Put(keys)
}

// jsonSortedIntKeys gathers a signed-keyed map's keys, in the bits of a
// uint64, sorted the way the members must come out: by the bytes of the name
// each key becomes. That order is the standard library's under
// Deterministic(true), and it is not the numeric one — "10" sorts before "3" —
// so the keys are compared as the decimals they will be written as.
func jsonSortedIntKeys[K ~int | ~int8 | ~int16 | ~int32 | ~int64, V any](m map[K]V) *[]uint64 {
	keys := jsonTakeNums()
	held := (*keys)[:0]
	for k := range m {
		held = append(held, uint64(int64(k))) //nolint:gosec // The bits carry the value; the emission converts back through int64.
	}
	slices.SortFunc(held, func(a, b uint64) int {
		var ab, bb [24]byte
		return bytes.Compare(
			strconv.AppendInt(ab[:0], int64(a), 10), //nolint:gosec // The bits went in from an int64 above.
			strconv.AppendInt(bb[:0], int64(b), 10), //nolint:gosec // Likewise.
		)
	})
	*keys = held
	return keys
}

// jsonSortedUintKeys is [jsonSortedIntKeys] for the unsigned kinds.
func jsonSortedUintKeys[K ~uint | ~uint8 | ~uint16 | ~uint32 | ~uint64 | ~uintptr, V any](m map[K]V) *[]uint64 {
	keys := jsonTakeNums()
	held := (*keys)[:0]
	for k := range m {
		held = append(held, uint64(k))
	}
	slices.SortFunc(held, func(a, b uint64) int {
		var ab, bb [24]byte
		return bytes.Compare(strconv.AppendUint(ab[:0], a, 10), strconv.AppendUint(bb[:0], b, 10))
	})
	*keys = held
	return keys
}

// jsonSortedFloatKeys is [jsonSortedIntKeys] for the float kinds, carrying
// each key as its bit pattern so one pool serves every kind.
//
// A key that is not finite has no name to sort by — writing it is refused
// where the member is written — so any pair involving one is ordered by bits,
// which is an order the refusal keeps anybody from observing.
func jsonSortedFloatKeys[K ~float32 | ~float64, V any](m map[K]V, width int) *[]uint64 {
	keys := jsonTakeNums()
	held := (*keys)[:0]
	for k := range m {
		held = append(held, math.Float64bits(float64(k)))
	}
	slices.SortFunc(held, func(a, b uint64) int {
		fa, fb := math.Float64frombits(a), math.Float64frombits(b)
		if math.IsNaN(fa) || math.IsInf(fa, 0) || math.IsNaN(fb) || math.IsInf(fb, 0) {
			return int(a>>32) - int(b>>32)
		}
		var ab, bb [32]byte
		return bytes.Compare(jsonAppendFloat(ab[:0], fa, width), jsonAppendFloat(bb[:0], fb, width))
	})
	*keys = held
	return keys
}

// jsonNameInt reads a member name as a signed integer, holding it to the
// grammar and range a number of that width is held to as a value.
//
// The whole name and nothing but: "03", "+3" and "3.0" are refused here for
// the reason they are refused as values, since a name that parses differently
// from the value it stands for is two numbers under one spelling.
func jsonNameInt(name []byte, width int) (int64, error) {
	held, next, err := jsonScanInt(name, 0, width)
	if err != nil {
		return 0, err
	}
	if next != len(name) {
		return 0, errJSONSyntax
	}
	return held, nil
}

// jsonNameUint is [jsonNameInt] for the unsigned kinds.
func jsonNameUint(name []byte, width int) (uint64, error) {
	held, next, err := jsonScanUint(name, 0, width)
	if err != nil {
		return 0, err
	}
	if next != len(name) {
		return 0, errJSONSyntax
	}
	return held, nil
}

// jsonNameFloat is [jsonNameInt] for the float kinds.
func jsonNameFloat(name []byte, width int) (float64, error) {
	held, next, err := jsonScanFloat(name, 0, width)
	if err != nil {
		return 0, err
	}
	if next != len(name) {
		return 0, errJSONSyntax
	}
	return held, nil
}

// jsonFinish turns an appended document into the slice MarshalJSON answers
// with: an exact copy, with the borrowed buffer handed back.
//
// The copy is not hesitancy. The buffer is pooled, and a caller holding a
// slice of it while the next caller appends over it is a race only the
// detector would ever see.
func jsonFinish(scratch *[]byte, b []byte, err error) ([]byte, error) {
	*scratch = b
	var out []byte
	if err == nil {
		out = make([]byte, len(b))
		copy(out, b)
	}
	jsonDropScratch(scratch)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// jsonCannotRead says that a value of the wrong kind arrived for a target,
// naming both.
func jsonCannotRead(what string, b []byte, i int) error {
	return fmt.Errorf("cannot read %s from a JSON %s", what, jsonKindName(b, i))
}

// jsonKindName names the kind of the value beginning at b[i], for an error a
// person reads.
func jsonKindName(b []byte, i int) string {
	if i >= len(b) {
		return "end of input"
	}
	switch b[i] {
	case '"':
		return "string"
	case '{':
		return "object"
	case '[':
		return "array"
	case 't', 'f':
		return "boolean"
	case 'n':
		return "null"
	case '-', '0', '1', '2', '3', '4', '5', '6', '7', '8', '9':
		return "number"
	default:
		return "value no grammar names"
	}
}

// jsonFlushWindow is how many bytes a streaming writer gathers before handing
// them to the writer beneath it.
const jsonFlushWindow = 1 << 12

// jsonScanState carries the search for the end of one value across refills,
// which is what lets a reader hand a decoder whole elements while holding only
// the largest one.
// depth is how many objects and arrays the scan is inside. str records being
// inside a string, and esc that the next byte of it is escaped — both needed
// so that a brace inside a string does not close the object it appears in, and
// a refill can resume mid-string.
type jsonScanState struct {
	depth int
	str   bool
	esc   bool
}

// jsonValueEnd scans b from i for the end of the value the state is in the
// middle of. It returns where the value ends, or that the window ran out and
// the scan should resume, with the same state, once more bytes arrive.
//
// It finds the end without judging what it finds: the bytes are handed whole
// to a decoder that validates them, so validating here would read everything
// twice. What it cannot get wrong is strings and nesting, since those decide
// where the end is.
func jsonValueEnd(b []byte, i int, st *jsonScanState) (int, bool) {
	for ; i < len(b); i++ {
		c := b[i]

		if st.str {
			switch {
			case st.esc:
				st.esc = false
			case c == '\\':
				st.esc = true
			case c == '"':
				st.str = false
				if st.depth == 0 {
					return i + 1, false
				}
			}
			continue
		}

		switch c {
		case '"':
			st.str = true
		case '{', '[':
			st.depth++
		case '}', ']':
			if st.depth == 0 {
				// The bracket closing whatever holds a bare scalar, which is
				// not part of it.
				return i, false
			}
			st.depth--
			if st.depth == 0 {
				return i + 1, false
			}
		case ',', ' ', '\t', '\n', '\r':
			if st.depth == 0 {
				// The delimiter after a bare scalar, which is not part of it.
				return i, false
			}
		}
	}
	return len(b), true
}

// jsonFeed reads a document out of a reader a window at a time, handing over
// one complete value's bytes at a time.
//
// The window grows to the largest single value and no further, which is the
// bound a streaming reader promises: the document as a whole never has to fit.
// The bytes handed out are valid until the next call that reads, because the
// window is compacted and refilled underneath them.
// i is where the scan stands in the window, and done counts the bytes already
// dropped off the front of it — their sum is the position in the stream, which
// is what a caller reporting how much it consumed needs.
type jsonFeed struct {
	r    io.Reader
	buf  *[]byte
	i    int
	done int64
}

// jsonNewFeed starts a feed over a reader, on a borrowed window.
//
// The window is emptied before it is read into, because a borrowed buffer
// arrives holding whatever its last user composed in it — and a feed that
// started past index zero would read a stale document nobody sent.
func jsonNewFeed(r io.Reader) jsonFeed {
	buf := jsonTakeScratch()
	*buf = (*buf)[:0]
	return jsonFeed{r: r, buf: buf}
}

// close hands the window back. The feed is not usable after it.
func (f *jsonFeed) close() {
	if f.buf != nil {
		jsonDropScratch(f.buf)
		f.buf = nil
	}
}

// offset is how many bytes of the stream have been consumed.
func (f *jsonFeed) offset() int64 { return f.done + int64(f.i) }

// more reads another chunk into the window, growing it when it is full.
//
// A read that returns bytes and an error keeps the bytes and holds the error
// for the call that finds the window empty, which is the contract io.Reader
// asks its callers to follow.
func (f *jsonFeed) more() error {
	held := *f.buf
	if cap(held)-len(held) < 128 {
		held = slices.Grow(held, max(512, cap(held)))
	}

	n, err := f.r.Read(held[len(held):cap(held)])
	held = held[:len(held)+n]
	*f.buf = held
	if n == 0 {
		if err == nil {
			return io.ErrNoProgress
		}
		return err
	}
	return nil
}

// peek returns the first byte of the next value, past whatever whitespace
// stands before it, without consuming it. An exhausted reader answers io.EOF.
func (f *jsonFeed) peek() (byte, error) {
	for {
		f.i = jsonSkipSpace(*f.buf, f.i)
		if f.i < len(*f.buf) {
			return (*f.buf)[f.i], nil
		}
		if err := f.more(); err != nil {
			return 0, err
		}
	}
}

// take consumes the byte peek answered with.
func (f *jsonFeed) take() { f.i++ }

// null consumes the null literal beginning at the scan position, and reports
// whether that is what was there.
func (f *jsonFeed) null() (bool, error) {
	for len(*f.buf)-f.i < 4 {
		if err := f.more(); err != nil {
			return false, err
		}
	}
	if next, ok := jsonScanNull(*f.buf, f.i); ok {
		f.i = next
		return true, nil
	}
	return false, nil
}

// cannotRead names the kind of the value the scan stands before, for an error
// a person reads.
func (f *jsonFeed) cannotRead(what string) error {
	return jsonCannotRead(what, *f.buf, f.i)
}

// element returns the complete bytes of the next value, refilling from the
// reader until its end is in the window.
//
// The bytes are the window's and stay right only until the next call that
// reads, so a caller decodes them before asking again — and never lends them
// out, which is why a streaming reader cannot offer borrowing.
func (f *jsonFeed) element() ([]byte, error) {
	// What was consumed is dropped first, so the window holds this value and
	// not the document so far.
	if f.i > 0 {
		held := *f.buf
		f.done += int64(f.i)
		*f.buf = held[:copy(held, held[f.i:])]
		f.i = 0
	}

	var st jsonScanState
	at := 0
	for {
		end, needs := jsonValueEnd(*f.buf, at, &st)
		if !needs {
			f.i = end
			return (*f.buf)[:end], nil
		}
		at = end
		if err := f.more(); err != nil {
			return nil, err
		}
	}
}
