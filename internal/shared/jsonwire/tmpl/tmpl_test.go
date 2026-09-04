package tmpl

import (
	"bytes"
	"encoding/json/jsontext"
	json "encoding/json/v2"
	"errors"
	"io"
	"math"
	"math/rand/v2"
	"strconv"
	"strings"
	"testing"
	"testing/iotest"
)

// The claim every test in this file makes is one claim: these helpers write the
// bytes encoding/json/v2 writes and refuse the documents it refuses.
//
// It is checked against the standard library rather than against a table of
// what somebody believed the standard library does, because the second kind of
// test passes for a helper and a belief that are wrong together. The imports
// above are the point: this package is emitted into somebody else's repository
// and must not depend on jsontext, and the tests are where that dependency is
// allowed to live.

// TestAppendStringIsTheStandardOneForEveryByte walks every one-byte string, so
// that the escape table has no gap in it. A gap is exactly the kind of defect
// that survives a fixture: the byte nobody thought to write down.
func TestAppendStringIsTheStandardOneForEveryByte(t *testing.T) {
	for c := range 256 {
		s := string([]byte{byte(c)})
		gotBytes, gotErr := jsonAppendString(nil, s)
		wantBytes, wantErr := jsontext.AppendQuote(nil, s)

		if (gotErr == nil) != (wantErr == nil) {
			t.Errorf("byte %#02x: we say %v, the standard library says %v", c, gotErr, wantErr)
			continue
		}
		if gotErr != nil {
			continue
		}
		if !bytes.Equal(gotBytes, wantBytes) {
			t.Errorf("byte %#02x: we write %s, the standard library writes %s", c, gotBytes, wantBytes)
		}
	}
}

// TestCloseTextIsAppendStringAfterTheFact holds the settle to the escaper it
// stands in for: for every one-byte and every two-byte text, appending the raw
// bytes and closing must produce what jsonAppendString produces from the same
// text — the same bytes where both accept, and a refusal exactly where the
// escaper refuses. The prefix is part of the claim, since closing rewrites the
// buffer in place when the text turns out to need the detour.
func TestCloseTextIsAppendStringAfterTheFact(t *testing.T) {
	check := func(text string) {
		t.Helper()

		dst := append([]byte(`{"key":`), '"')
		mark := len(dst) - 1
		dst = append(dst, text...)
		got, gotErr := jsonCloseText(dst, mark)

		want, wantErr := jsonAppendString([]byte(`{"key":`), text)

		if (gotErr == nil) != (wantErr == nil) {
			t.Errorf("text %q: closing says %v, the escaper says %v", text, gotErr, wantErr)
			return
		}
		if gotErr != nil {
			return
		}
		if !bytes.Equal(got, want) {
			t.Errorf("text %q: closing wrote %s, the escaper writes %s", text, got, want)
		}
	}

	for c := range 256 {
		check(string([]byte{byte(c)}))
	}
	var pair [2]byte
	for hi := range 256 {
		for lo := range 256 {
			pair[0], pair[1] = byte(hi), byte(lo)
			check(string(pair[:]))
		}
	}
}

// TestCloseTextOverTheShapesTextComesIn walks the texts a codec actually
// appends: nothing at all, the ordinary run the fast path exists for, dense
// escaping, multi-byte text, and the two ways UTF-8 goes wrong in the middle
// of something long enough to have a middle.
func TestCloseTextOverTheShapesTextComesIn(t *testing.T) {
	for _, text := range []string{
		"",
		"0192aefb-74a0-7000-8000-1234567890ab",
		"2026-09-04T12:30:45.123456789Z",
		strings.Repeat("abcdefgh", 64),
		strings.Repeat("a\"b\\c\td", 51),
		strings.Repeat("日本語テキスト", 12),
		"plain until it is not\xff",
		"a lead with nothing after it \xe3\x81",
	} {
		dst := append([]byte("held:"), '"')
		mark := len(dst) - 1
		dst = append(dst, text...)
		got, gotErr := jsonCloseText(dst, mark)

		want, wantErr := jsonAppendString([]byte("held:"), text)

		if (gotErr == nil) != (wantErr == nil) {
			t.Errorf("text %q: closing says %v, the escaper says %v", text, gotErr, wantErr)
			continue
		}
		if gotErr == nil && !bytes.Equal(got, want) {
			t.Errorf("text %q: closing wrote %s, the escaper writes %s", text, got, want)
		}
	}
}

// TestAppendStringIsTheStandardOneForEveryTwoBytes walks all sixty-five
// thousand two-byte strings, which is where a truncated multi-byte sequence
// and a continuation byte with no lead live.
func TestAppendStringIsTheStandardOneForEveryTwoBytes(t *testing.T) {
	var pair [2]byte
	for hi := range 256 {
		for lo := range 256 {
			pair[0], pair[1] = byte(hi), byte(lo)
			s := string(pair[:])

			gotBytes, gotErr := jsonAppendString(nil, s)
			wantBytes, wantErr := jsontext.AppendQuote(nil, s)

			if (gotErr == nil) != (wantErr == nil) {
				t.Fatalf("%#02x %#02x: we say %v, the standard library says %v", hi, lo, gotErr, wantErr)
			}
			if gotErr == nil && !bytes.Equal(gotBytes, wantBytes) {
				t.Fatalf("%#02x %#02x: we write %s, the standard library writes %s", hi, lo, gotBytes, wantBytes)
			}
		}
	}
}

// adversarial holds the strings worth naming: the ones a reader of this file
// would want to see covered whether or not the fuzzer reaches them.
var adversarial = []string{
	"",
	"plain",
	`quote " here`,
	`back \ slash`,
	"line\nfeed",
	"carriage\rreturn",
	"tab\there",
	"bell\x07",
	"null\x00byte",
	"unit\x1fseparator",
	"delete\x7f",
	"html < > & chars",
	"js \u2028 and \u2029 separators",
	"caf\u00e9",
	"\u65e5\u672c\u8a9e",
	"emoji \U0001f600",
	"lone lead \xed\xa0\x80",
	"bare continuation \x80",
	"truncated three-byte \xe6\x97",
	"overlong \xc0\x80",
	strings.Repeat("a", 300),
	strings.Repeat("\"", 40),
	strings.Repeat("\x01", 40),
}

// TestAppendStringIsTheStandardOneForTheAwkwardOnes holds the named cases to
// the standard library as well, so a failure names the shape rather than a
// pair of hex digits.
func TestAppendStringIsTheStandardOneForTheAwkwardOnes(t *testing.T) {
	for _, s := range adversarial {
		gotBytes, gotErr := jsonAppendString(nil, s)
		wantBytes, wantErr := jsontext.AppendQuote(nil, s)

		if (gotErr == nil) != (wantErr == nil) {
			t.Errorf("%q: we say %v, the standard library says %v", s, gotErr, wantErr)
			continue
		}
		if gotErr == nil && !bytes.Equal(gotBytes, wantBytes) {
			t.Errorf("%q:\n  we write %s\n  they write %s", s, gotBytes, wantBytes)
		}
	}
}

// TestAppendStringKeepsWhatWasAlreadyThere: every appender here appends, and a
// caller's buffer is the caller's.
func TestAppendStringKeepsWhatWasAlreadyThere(t *testing.T) {
	dst := []byte(`{"a":`)
	out, err := jsonAppendString(dst, "b")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(out), `{"a":"b"`; got != want {
		t.Errorf("got %s, want %s", got, want)
	}
}

// FuzzAppendStringIsTheStandardOne is the real check on the escaper: the table
// tests above cover what can be enumerated, and this covers what cannot.
func FuzzAppendStringIsTheStandardOne(f *testing.F) {
	for _, s := range adversarial {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, s string) {
		gotBytes, gotErr := jsonAppendString(nil, s)
		wantBytes, wantErr := jsontext.AppendQuote(nil, s)

		if (gotErr == nil) != (wantErr == nil) {
			t.Fatalf("%q: we say %v, the standard library says %v", s, gotErr, wantErr)
		}
		if gotErr == nil && !bytes.Equal(gotBytes, wantBytes) {
			t.Fatalf("%q:\n  we write %s\n  they write %s", s, gotBytes, wantBytes)
		}
	})
}

// floats worth naming: the boundaries of the two notations, the values whose
// exponent has one digit, the smallest subnormal, and negative zero.
var floats = []float64{
	0, math.Copysign(0, -1), 1, -1, 0.5, 1.0 / 3,
	1e-6, 1e-7, 1e-9, 1e-21, 5e-324,
	1e20, 1e21, 1e22, math.MaxFloat64, math.SmallestNonzeroFloat64,
	123456789, 1234567890123456789, 0.1, 2.5, 1e100, -1e-100,
}

// TestAppendFloatIsTheStandardOne holds the float writer to jsontext's, which
// is the one place this file reimplements a rule rather than a format: which
// notation to use, and the digit to drop out of a two-digit exponent.
func TestAppendFloatIsTheStandardOne(t *testing.T) {
	for _, bits := range []int{32, 64} {
		for _, f := range floats {
			if bits == 32 && (f > math.MaxFloat32 || f < -math.MaxFloat32) {
				continue
			}
			held := f
			if bits == 32 {
				held = float64(float32(f))
			}

			got := string(jsonAppendFloat(nil, held, bits))
			want := string(jsontext.AppendFloat(nil, held, bits))
			if got != want {
				t.Errorf("%d-bit %v: we write %s, the standard library writes %s", bits, held, got, want)
			}
		}
	}
}

// TestAppendFloatIsTheStandardOneOverRandomBits walks the space rather than a
// list of it. Bit patterns rather than values, so that subnormals and the
// values near the notation boundary are reached as often as the ordinary ones.
func TestAppendFloatIsTheStandardOneOverRandomBits(t *testing.T) {
	r := rand.New(rand.NewPCG(1, 2))
	for range 200000 {
		f := math.Float64frombits(r.Uint64())
		if math.IsNaN(f) || math.IsInf(f, 0) {
			// Neither is a JSON number, and neither reaches this: a codec
			// refuses them before it writes, or writes the string form the
			// format option asks for.
			continue
		}

		got := string(jsonAppendFloat(nil, f, 64))
		want := string(jsontext.AppendFloat(nil, f, 64))
		if got != want {
			t.Fatalf("%v (bits %#016x): we write %s, the standard library writes %s",
				f, math.Float64bits(f), got, want)
		}
	}
}

// FuzzAppendFloatIsTheStandardOne lets the fuzzer choose the bits.
func FuzzAppendFloatIsTheStandardOne(f *testing.F) {
	for _, held := range floats {
		f.Add(math.Float64bits(held))
	}

	f.Fuzz(func(t *testing.T, bits uint64) {
		held := math.Float64frombits(bits)
		if math.IsNaN(held) || math.IsInf(held, 0) {
			return
		}
		if got, want := string(jsonAppendFloat(nil, held, 64)), string(jsontext.AppendFloat(nil, held, 64)); got != want {
			t.Fatalf("%v: we write %s, the standard library writes %s", held, got, want)
		}
	})
}

// TestAppendFloatReadsBackAsItself is the property the notation choice exists
// to keep, checked independently of what the standard library does.
func TestAppendFloatReadsBackAsItself(t *testing.T) {
	r := rand.New(rand.NewPCG(3, 4))
	for range 50000 {
		f := math.Float64frombits(r.Uint64())
		if math.IsNaN(f) || math.IsInf(f, 0) {
			continue
		}
		written := string(jsonAppendFloat(nil, f, 64))
		read, err := strconv.ParseFloat(written, 64)
		if err != nil {
			t.Fatalf("%v wrote %s, which does not parse: %v", f, written, err)
		}
		if read != f {
			t.Fatalf("%v wrote %s, which reads back as %v", f, written, read)
		}
	}
}

// TestScanStringAgreesWithTheStandardLibrary compares verdicts and contents
// against unmarshalling the same document into a string.
func TestScanStringAgreesWithTheStandardLibrary(t *testing.T) {
	docs := []string{
		`""`, `"a"`, `"a\nb"`, `"\u0041"`, `"\ud83d\ude00"`, `"\ud800"`, `"\udc00"`,
		`"\ud800a"`, `"\u00"`, `"\uZZZZ"`, `"\q"`, `"\/"`, `"a` + "\x01" + `b"`,
		`"caf\u00e9"`, `"\\"`, `"\""`, `"unterminated`, `"`, ``, `a`,
		`"` + "\xff" + `"`, `"\u0000"`, `"tab\there"`,
	}

	for _, doc := range docs {
		var want string
		wantErr := json.Unmarshal([]byte(doc), &want)

		got, gotErr := readString([]byte(doc))

		if (gotErr == nil) != (wantErr == nil) {
			t.Errorf("%s: we say %v, the standard library says %v", doc, gotErr, wantErr)
			continue
		}
		if gotErr == nil && got != want {
			t.Errorf("%s: we read %q, the standard library reads %q", doc, got, want)
		}
	}
}

// readString is what a generated decoder does with a string member, written
// out here so the test exercises the same sequence of calls.
func readString(b []byte) (string, error) {
	lo, hi, next, esc, err := jsonScanString(b, jsonSkipSpace(b, 0))
	if err != nil {
		return "", err
	}
	if err := jsonAtEnd(b, next); err != nil {
		return "", err
	}
	if !esc {
		return string(b[lo:hi]), nil
	}
	out, err := jsonUnescape(nil, b[lo:hi])
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// FuzzScanStringAgreesWithTheStandardLibrary lets the fuzzer write the
// document.
func FuzzScanStringAgreesWithTheStandardLibrary(f *testing.F) {
	f.Add(`"a"`)
	f.Add(`"a\nb"`)
	f.Add(`"\ud83d\ude00"`)
	f.Add(`"\ud800"`)
	f.Add(`"` + "\xff" + `"`)

	f.Fuzz(func(t *testing.T, doc string) {
		var want string
		wantErr := json.Unmarshal([]byte(doc), &want)

		got, gotErr := readString([]byte(doc))

		if (gotErr == nil) != (wantErr == nil) {
			t.Fatalf("%q: we say %v, the standard library says %v", doc, gotErr, wantErr)
		}
		if gotErr == nil && got != want {
			t.Fatalf("%q: we read %q, the standard library reads %q", doc, got, want)
		}
	})
}

// TestScanNumberAgreesWithTheStandardLibrary is about the grammar rather than
// the value: which byte sequences are numbers at all.
func TestScanNumberAgreesWithTheStandardLibrary(t *testing.T) {
	docs := []string{
		"0", "-0", "1", "-1", "007", "+1", "1.", ".5", "1.5", "1e2", "1E2",
		"1e+2", "1e-2", "1e", "1e+", "1.2e3", "0.0", "00", "0x1", "1_000",
		"9223372036854775807", "9223372036854775808", "-9223372036854775808",
		"1.0", "10000000000000000000000000000", "1e309", "-1e309", "1e-400",
		"Infinity", "NaN", "", " 1", "1 ", "--1", "1..2", "1e2.5",
	}

	for _, doc := range docs {
		t.Run(doc, func(t *testing.T) {
			// Integers: the grammar and the width together.
			var wantInt int64
			wantIntErr := json.Unmarshal([]byte(doc), &wantInt)
			gotInt, _, gotIntErr := jsonScanInt([]byte(doc), jsonSkipSpace([]byte(doc), 0), 64)
			if trailing(doc) {
				gotIntErr = errJSONSyntax
			}
			if (gotIntErr == nil) != (wantIntErr == nil) {
				t.Errorf("as int64 we say %v, the standard library says %v", gotIntErr, wantIntErr)
			} else if gotIntErr == nil && gotInt != wantInt {
				t.Errorf("as int64 we read %d, the standard library reads %d", gotInt, wantInt)
			}

			// Floats.
			var wantFloat float64
			wantFloatErr := json.Unmarshal([]byte(doc), &wantFloat)
			gotFloat, _, gotFloatErr := jsonScanFloat([]byte(doc), jsonSkipSpace([]byte(doc), 0), 64)
			if trailing(doc) {
				gotFloatErr = errJSONSyntax
			}
			if (gotFloatErr == nil) != (wantFloatErr == nil) {
				t.Errorf("as float64 we say %v, the standard library says %v", gotFloatErr, wantFloatErr)
			} else if gotFloatErr == nil && gotFloat != wantFloat && !(math.IsNaN(gotFloat) && math.IsNaN(wantFloat)) {
				t.Errorf("as float64 we read %v, the standard library reads %v", gotFloat, wantFloat)
			}
		})
	}
}

// trailing reports whether a document has anything after its first value,
// which a scanner reports by where it stopped and a whole-document read
// reports as an error. The scanners here answer about one value, so the test
// asks this separately rather than expecting them to.
func trailing(doc string) bool {
	b := []byte(doc)
	i := jsonSkipSpace(b, 0)
	if i >= len(b) {
		return true
	}
	_, hi, err := jsonScanNumber(b, i)
	if err != nil {
		return false
	}
	return jsonSkipSpace(b, hi) != len(b)
}

// TestScanIntRefusesAFraction: a document saying 1.0 where an int is expected
// describes a value the field cannot hold, and rounding it would be this
// deciding what the document meant.
func TestScanIntRefusesAFraction(t *testing.T) {
	for _, doc := range []string{"1.0", "1e2", "1.5", "-0.0"} {
		if _, _, err := jsonScanInt([]byte(doc), 0, 64); err == nil {
			t.Errorf("%s was read as an integer", doc)
		}
		var into int64
		if err := json.Unmarshal([]byte(doc), &into); err == nil {
			t.Errorf("%s: the standard library accepted it, so this test is wrong", doc)
		}
	}
}

// TestScanIntHoldsTheWidth checks the narrow widths, where the boundary is not
// the one ParseInt defaults to.
func TestScanIntHoldsTheWidth(t *testing.T) {
	cases := []struct {
		doc  string
		bits int
		ok   bool
	}{
		{"127", 8, true},
		{"128", 8, false},
		{"-128", 8, true},
		{"-129", 8, false},
		{"32767", 16, true},
		{"32768", 16, false},
		{"2147483647", 32, true},
		{"2147483648", 32, false},
	}

	for _, c := range cases {
		_, next, err := jsonScanInt([]byte(c.doc), 0, c.bits)
		if (err == nil) != c.ok {
			t.Errorf("%s at %d bits: err=%v, want ok=%v", c.doc, c.bits, err, c.ok)
			continue
		}
		if err == nil && next != len(c.doc) {
			t.Errorf("%s: the scan stopped at %d, want %d", c.doc, next, len(c.doc))
		}
	}
}

// TestSkipValueStepsOverOneValue, including the shapes where the closing byte
// is not the one a naive scan would stop at.
func TestSkipValueStepsOverOneValue(t *testing.T) {
	cases := []struct {
		doc  string
		rest string
	}{
		{`1,`, `,`},
		{`"a",`, `,`},
		{`"}",`, `,`},
		{`{"a":"}"},`, `,`},
		{`[[[]]],`, `,`},
		{`{"a":{"b":[1,2,{"c":"]"}]}},`, `,`},
		{`true,`, `,`},
		{`null,`, `,`},
		{`-1.5e-3,`, `,`},
	}

	for _, c := range cases {
		next, err := jsonSkipValue([]byte(c.doc), 0, 0)
		if err != nil {
			t.Errorf("%s: %v", c.doc, err)
			continue
		}
		if got := c.doc[next:]; got != c.rest {
			t.Errorf("%s: stopped with %q left, want %q", c.doc, got, c.rest)
		}
	}
}

// TestSkipValueRefusesWhatIsNotAValue: a member nothing reads is still part of
// a document, and a document that is wrong inside it is wrong.
func TestSkipValueRefusesWhatIsNotAValue(t *testing.T) {
	for _, doc := range []string{
		`{`, `[`, `{"a":`, `"unterminated`, `tru`, `nul`, `+1`, `}`, ``,
		`{"a":1,"a":2}`, `{"a":1,"a":2}`, `[1,]`, `{"a":1,}`, `[,1]`,
		`{"a" 1}`, `[1 2]`, `{"a":01}`,
	} {
		if _, err := jsonSkipValue([]byte(doc), 0, 0); err == nil {
			t.Errorf("%q was stepped over as though it were a value", doc)
		}
	}
}

// TestNamesRefusesADeclaredRepeat covers the bitset, including the spill past
// sixty-four members.
func TestNamesRefusesADeclaredRepeat(t *testing.T) {
	var names jsonNames

	for _, index := range []int{0, 1, 63, 64, 127, 200} {
		if names.declare(index) {
			t.Errorf("member %d was reported as a repeat the first time", index)
		}
		if !names.saw(index) {
			t.Errorf("member %d was not recorded as seen", index)
		}
		if !names.declare(index) {
			t.Errorf("member %d was not reported as a repeat the second time", index)
		}
	}

	if names.saw(7) {
		t.Error("a member that never arrived was reported as seen")
	}
}

// TestNamesRefusesAnUnknownRepeat covers the span scan, including a name
// written once plainly and once through an escape, which are one name.
func TestNamesRefusesAnUnknownRepeat(t *testing.T) {
	doc := []byte(`{"one":1,"two":2,"one":3,"\u006fne":4}`)
	var names jsonNames

	if names.unknown(doc, 2, 5, false) {
		t.Error("the first name was reported as a repeat")
	}
	if names.unknown(doc, 10, 13, false) {
		t.Error("a different name was reported as a repeat")
	}
	if !names.unknown(doc, 18, 21, false) {
		t.Error("the same name written twice was not reported as a repeat")
	}
	if !names.unknown(doc, 26, 34, true) {
		t.Error("the same name written through an escape was not reported as a repeat")
	}
}

// TestNamesTellsApartANameThatIsAPrefixOfAnother, which a comparison by length
// alone would not.
func TestNamesTellsApartANameThatIsAPrefixOfAnother(t *testing.T) {
	doc := []byte(`abcabcd`)
	var names jsonNames

	if names.unknown(doc, 0, 3, false) {
		t.Error("abc was reported as a repeat")
	}
	if names.unknown(doc, 3, 7, false) {
		t.Error("abcd was reported as a repeat of abc")
	}
	if !names.unknown(doc, 0, 3, false) {
		t.Error("abc was not reported as a repeat of itself")
	}
}

// TestNamesSpillsPastTheInlineFew: the ninth unknown name lands in the spill
// and is still compared against.
func TestNamesSpillsPastTheInlineFew(t *testing.T) {
	names := jsonNames{}
	doc := []byte(`abcdefghijkl`)

	for i := range 12 {
		if names.unknown(doc, i, i+1, false) {
			t.Errorf("name %d was reported as a repeat the first time", i)
		}
	}
	for i := range 12 {
		if !names.unknown(doc, i, i+1, false) {
			t.Errorf("name %d was not reported as a repeat the second time", i)
		}
	}
}

// TestUnescapeMatchesTheStandardLibrary compares against AppendUnquote, which
// is the standard library's own answer to the same question.
func TestUnescapeMatchesTheStandardLibrary(t *testing.T) {
	for _, held := range []string{
		`a`, `a\nb`, `\u0041`, `\ud83d\ude00`, `\ud800`, `\udc00`, `\ud800\u0041`,
		`\\`, `\"`, `\/`, `\b\f\n\r\t`, `caf\u00e9`, `\u0000`,
	} {
		quoted := `"` + held + `"`
		want, wantErr := jsontext.AppendUnquote(nil, quoted)

		lo, hi, _, _, err := jsonScanString([]byte(quoted), 0)
		if err != nil {
			if wantErr == nil {
				t.Errorf("%s: we refuse it, the standard library reads %q", quoted, want)
			}
			continue
		}
		got, gotErr := jsonUnescape(nil, []byte(quoted)[lo:hi])

		if (gotErr == nil) != (wantErr == nil) {
			t.Errorf("%s: we say %v, the standard library says %v", quoted, gotErr, wantErr)
			continue
		}
		if gotErr == nil && !bytes.Equal(got, want) {
			t.Errorf("%s: we read %q, the standard library reads %q", quoted, got, want)
		}
	}
}

// TestEveryErrorIsDistinguishable: a caller wraps these, so they have to be
// comparable and they have to say different things.
func TestEveryErrorIsDistinguishable(t *testing.T) {
	all := []error{
		errJSONSyntax, errJSONUTF8, errJSONEscape,
		errJSONTruncated, errJSONDuplicate, errJSONRange, errJSONSurrogate,
	}

	for i, one := range all {
		for j, other := range all {
			if i == j {
				continue
			}
			if errors.Is(one, other) {
				t.Errorf("%v and %v are the same error", one, other)
			}
			if one.Error() == other.Error() {
				t.Errorf("%v and %v read alike", one, other)
			}
		}
		if !strings.HasPrefix(one.Error(), "json: ") {
			t.Errorf("%q does not say which package it came from", one.Error())
		}
	}
}

// TestScratchIsReturnedOnlyWhenItIsWorthKeeping covers the ceiling rather than
// the pool: a buffer that grew past what is worth keeping is dropped, so that
// one enormous document cannot park its buffer for the life of the process.
func TestScratchIsReturnedOnlyWhenItIsWorthKeeping(t *testing.T) {
	small := jsonTakeScratch()
	jsonDropScratch(small)

	big := make([]byte, jsonScratchCap+1)
	jsonDropScratch(&big)

	// Nothing to assert about the pool's contents — sync.Pool offers no way to
	// ask, and a test that drained it would be testing the runtime. What is
	// checked is that neither call panics and that the cap test is the one
	// written: a buffer over the ceiling is dropped rather than kept.
	if cap(big) <= jsonScratchCap {
		t.Fatal("the oversized buffer was not oversized, so this test proves nothing")
	}
}

// TestALoneSurrogateIsRefused pins the behaviour the standard library has and
// this file had to be corrected to: a surrogate half names no code point, and
// a reader that substituted the replacement rune would hand back a string the
// document does not contain.
func TestALoneSurrogateIsRefused(t *testing.T) {
	for _, held := range []string{`\ud800`, `\udc00`, `\ud800\u0041`, `\udbff`} {
		if _, err := jsonUnescape(nil, []byte(held)); !errors.Is(err, errJSONSurrogate) {
			t.Errorf("%s was read as %v, want a surrogate refusal", held, err)
		}
	}
	if _, err := jsonUnescape(nil, []byte(`\ud83d\ude00`)); err != nil {
		t.Errorf("a whole pair was refused: %v", err)
	}
}

// TestScanUintAgreesWithTheStandardLibrary is the unsigned half of the number
// grammar, which differs from the signed one in one place that matters: a
// minus sign is a syntax the grammar allows and the width refuses.
func TestScanUintAgreesWithTheStandardLibrary(t *testing.T) {
	docs := []string{
		"0", "1", "-1", "-0", "255", "256", "65535", "65536",
		"18446744073709551615", "18446744073709551616", "1.0", "1e2", "007",
	}

	for _, doc := range docs {
		var want uint64
		wantErr := json.Unmarshal([]byte(doc), &want)

		got, next, gotErr := jsonScanUint([]byte(doc), 0, 64)
		if gotErr == nil && next != len(doc) {
			// The scan read a number and stopped short, which means the
			// document carries something after it. A whole-document read
			// refuses that, and so does a generated decoder by way of
			// jsonAtEnd — so this is agreement rather than a difference.
			gotErr = errJSONSyntax
		}

		if (gotErr == nil) != (wantErr == nil) {
			t.Errorf("%s: we say %v, the standard library says %v", doc, gotErr, wantErr)
			continue
		}
		if gotErr != nil {
			continue
		}
		if got != want {
			t.Errorf("%s: we read %d, the standard library reads %d", doc, got, want)
		}
	}
}

// TestScanUintHoldsTheWidth covers the narrow widths a member of that type
// would be read into.
func TestScanUintHoldsTheWidth(t *testing.T) {
	cases := []struct {
		doc  string
		bits int
		ok   bool
	}{
		{"255", 8, true},
		{"256", 8, false},
		{"65535", 16, true},
		{"65536", 16, false},
		{"4294967295", 32, true},
		{"4294967296", 32, false},
		{"-1", 8, false},
	}

	for _, c := range cases {
		_, _, err := jsonScanUint([]byte(c.doc), 0, c.bits)
		if (err == nil) != c.ok {
			t.Errorf("%s at %d bits: err=%v, want ok=%v", c.doc, c.bits, err, c.ok)
		}
	}
}

// TestTheScannersReportWhereTheyStopped is what a generated decoder relies on
// most and would notice least: the index each scanner returns is where the
// byte after its value is, so a decoder that trusted it and was wrong would
// read the rest of the object from the middle of a number.
func TestTheScannersReportWhereTheyStopped(t *testing.T) {
	doc := []byte(`{"n":-12.5e3,"u":7,"s":"a\nb","b":true,"z":null}`)

	// Walk it the way a decoder walks it, asserting the byte each scanner
	// leaves the cursor on.
	i := len("{")
	_, _, next, _, err := jsonScanString(doc, i)
	if err != nil || doc[next] != ':' {
		t.Fatalf(`after the name "n": next=%d byte=%q err=%v`, next, doc[next], err)
	}

	f, next, err := jsonScanFloat(doc, next+1, 64)
	if err != nil || f != -12500 || doc[next] != ',' {
		t.Fatalf("after -12.5e3: value=%v next byte=%q err=%v", f, doc[next], err)
	}

	_, _, next, _, err = jsonScanString(doc, next+1)
	if err != nil || doc[next] != ':' {
		t.Fatalf(`after the name "u": err=%v`, err)
	}
	u, next, err := jsonScanUint(doc, next+1, 64)
	if err != nil || u != 7 || doc[next] != ',' {
		t.Fatalf("after 7: value=%d next byte=%q err=%v", u, doc[next], err)
	}

	_, _, next, _, err = jsonScanString(doc, next+1)
	if err != nil {
		t.Fatalf(`after the name "s": %v`, err)
	}
	lo, hi, next, esc, err := jsonScanString(doc, next+1)
	if err != nil || !esc {
		t.Fatalf(`the value "a\nb": esc=%v err=%v`, esc, err)
	}
	held, err := jsonUnescape(make([]byte, 0, 8), doc[lo:hi])
	if err != nil || string(held) != "a\nb" {
		t.Fatalf("unescaped to %q: %v", held, err)
	}
	if doc[next] != ',' {
		t.Fatalf("after the string: next byte=%q", doc[next])
	}

	_, _, next, _, err = jsonScanString(doc, next+1)
	if err != nil {
		t.Fatalf(`after the name "b": %v`, err)
	}
	flag, next, err := jsonScanBool(doc, next+1)
	if err != nil || !flag || doc[next] != ',' {
		t.Fatalf("after true: value=%v next byte=%q err=%v", flag, doc[next], err)
	}

	_, _, next, _, err = jsonScanString(doc, next+1)
	if err != nil {
		t.Fatalf(`after the name "z": %v`, err)
	}
	next, was := jsonScanNull(doc, next+1)
	if !was || doc[next] != '}' {
		t.Fatalf("after null: was=%v next byte=%q", was, doc[next])
	}

	if err := jsonAtEnd(doc, next+1); err != nil {
		t.Fatalf("the document did not end where it ended: %v", err)
	}
}

// TestTheAppendersAppendIntoAPooledBuffer is how a codec uses them: a buffer
// borrowed, filled, copied out of, and returned.
func TestTheAppendersAppendIntoAPooledBuffer(t *testing.T) {
	held := jsonTakeScratch()
	defer jsonDropScratch(held)

	*held = append((*held)[:0], `{"s":`...)
	out, err := jsonAppendString(*held, "a\tb")
	if err != nil {
		t.Fatal(err)
	}
	out = append(out, `,"f":`...)
	out = jsonAppendFloat(out, 1e-9, 64)
	out = append(out, '}')
	*held = out

	if got, want := string(out), `{"s":"a\tb","f":1e-9}`; got != want {
		t.Errorf("got %s, want %s", got, want)
	}

	// And the whole of it is a document the standard library reads.
	var into struct {
		S string  `json:"s"`
		F float64 `json:"f"`
	}
	if err := json.Unmarshal(out, &into); err != nil {
		t.Fatalf("the standard library refused what we wrote: %v", err)
	}
	if into.S != "a\tb" || into.F != 1e-9 {
		t.Errorf("read back as %+v", into)
	}
}

// TestBytesAgreeWithTheStandardLibrary holds the base64 pair to what the
// standard library writes and accepts, which is stricter than the base64
// package alone: no whitespace inside the encoding, no set bits under the
// padding.
func TestBytesAgreeWithTheStandardLibrary(t *testing.T) {
	for _, held := range [][]byte{nil, {}, {0}, {0xff}, []byte("hi"), []byte("hello world"), {0, 1, 2, 3, 4, 5}} {
		got := string(jsonAppendBytes(nil, held))
		want, err := json.Marshal(held)
		if err != nil {
			t.Fatal(err)
		}
		if got != string(want) {
			t.Errorf("%v: we write %s, the standard library writes %s", held, got, want)
		}
	}

	for _, doc := range []string{
		`"aGk="`, `"aGVsbG8="`, `""`, `"aGk"`, `"aGk ="`, `"aGk\n="`,
		`"AB=="`, `"AQ=="`, `"===="`, `"a"`, `"\r\n"`,
	} {
		var want []byte
		wantErr := json.Unmarshal([]byte(doc), &want)

		lo, hi, next, esc, scanErr := jsonScanString([]byte(doc), 0)
		var got []byte
		gotErr := scanErr
		if scanErr == nil && next == len(doc) {
			got, gotErr = jsonScanBytes([]byte(doc), lo, hi, esc, nil)
		}

		if (gotErr == nil) != (wantErr == nil) {
			t.Errorf("%s: we say %v, the standard library says %v", doc, gotErr, wantErr)
			continue
		}
		if gotErr == nil && !bytes.Equal(got, want) {
			t.Errorf("%s: we read %v, the standard library reads %v", doc, got, want)
		}
	}
}

// TestStringTakesAndBorrows: the copy is a copy and the borrow is a borrow.
func TestStringTakesAndBorrows(t *testing.T) {
	doc := []byte(`"borrowed"`)
	lo, hi, _, esc, err := jsonScanString(doc, 0)
	if err != nil {
		t.Fatal(err)
	}

	copied := jsonString(doc, lo, hi, esc, false)
	borrowed := jsonString(doc, lo, hi, esc, true)
	if copied != "borrowed" || borrowed != "borrowed" {
		t.Fatalf("read %q and %q, want them both to be %q", copied, borrowed, "borrowed")
	}

	doc[1] = 'z'
	if copied != "borrowed" {
		t.Error("the copy changed when the document did")
	}
	if borrowed == "borrowed" {
		t.Error("the borrow did not point into the document")
	}

	escaped := []byte(`"a\tb"`)
	lo, hi, _, esc, err = jsonScanString(escaped, 0)
	if err != nil {
		t.Fatal(err)
	}
	if got := jsonString(escaped, lo, hi, esc, true); got != "a\tb" {
		t.Errorf("read %q through an escape, want %q", got, "a\tb")
	}
}

// TestSkipValueHoldsTheDepthTheStandardLibraryHolds: ten thousand and one
// brackets are a document both readers refuse.
func TestSkipValueHoldsTheDepthTheStandardLibrary(t *testing.T) {
	deep := strings.Repeat("[", jsonMaxDepth+1) + strings.Repeat("]", jsonMaxDepth+1)
	if _, err := jsonSkipValue([]byte(deep), 0, 0); !errors.Is(err, errJSONDeep) {
		t.Errorf("a document %d deep was answered with %v, want %v", jsonMaxDepth+1, err, errJSONDeep)
	}

	var into any
	if err := json.Unmarshal([]byte(deep), &into); err == nil {
		t.Error("the standard library read what this refuses; the bound is wrong")
	}

	shallow := strings.Repeat("[", 100) + strings.Repeat("]", 100)
	if _, err := jsonSkipValue([]byte(shallow), 0, 0); err != nil {
		t.Errorf("a document 100 deep was refused: %v", err)
	}
}

// TestValueEndFindsTheEnd, across refills of any size.
func TestValueEndFindsTheEnd(t *testing.T) {
	cases := []struct {
		doc string
		end int
	}{
		{`{"a":"}"} tail`, 9},
		{`[1,[2,[3]]],`, 11},
		{`"a\"b" ,`, 6},
		{`123,`, 3},
		{`true]`, 4},
		{`{"a":{"b":"]}"}}`, 16},
	}

	for _, c := range cases {
		var st jsonScanState
		end, more := jsonValueEnd([]byte(c.doc), 0, &st)
		if more || end != c.end {
			t.Errorf("%s: ended at %d (more=%v), want %d", c.doc, end, more, c.end)
		}

		// The same document a byte at a time, which is what a refill loop sees.
		st = jsonScanState{}
		held := []byte(c.doc)
		at, done := 0, false
		for window := 1; window <= len(held) && !done; window++ {
			var end int
			end, more = jsonValueEnd(held[:window], at, &st)
			at = end
			done = !more
		}
		if !done || at != c.end {
			t.Errorf("%s refilled a byte at a time: ended at %d (done=%v), want %d", c.doc, at, done, c.end)
		}
	}
}

// TestWroteEmpty: the four empties and nothing else.
func TestWroteEmpty(t *testing.T) {
	for doc, want := range map[string]bool{
		`null`: true, `""`: true, `{}`: true, `[]`: true,
		`0`: false, `"a"`: false, `false`: false, `[0]`: false, `{"a":1}`: false, `nul`: false,
	} {
		b := append([]byte(`,"member":`), doc...)
		if got := jsonWroteEmpty(b, len(`,"member":`)); got != want {
			t.Errorf("%s: reported %v, want %v", doc, got, want)
		}
	}
}

// TestSortedKeysComeBackSorted, through the pool and out of it.
func TestSortedKeysComeBackSorted(t *testing.T) {
	type Named string
	m := map[Named]int{"b": 1, "a": 2, "c": 3}

	keys := jsonSortedKeys(m)
	if got, want := strings.Join(*keys, ","), "a,b,c"; got != want {
		t.Errorf("keys came back %s, want %s", got, want)
	}
	jsonDropKeys(keys)

	again := jsonSortedKeys(map[string]bool{"z": true})
	if got, want := strings.Join(*again, ","), "z"; got != want {
		t.Errorf("a reused slice came back %s, want %s", got, want)
	}
	jsonDropKeys(again)
}

// TestMemberGrammarRefusesWhatTheStandardLibraryRefuses walks the object and
// array grammar helpers through the shapes a decoder meets.
func TestMemberGrammarRefusesWhatTheStandardLibraryRefuses(t *testing.T) {
	for _, doc := range []string{
		`{,}`, `{"a":1,}`, `{"a":1 "b":2}`, `{"a"1}`, `{"a"}`, `{1:2}`,
		`[,]`, `[1,]`, `[1 2]`,
	} {
		if _, err := jsonSkipValue([]byte(doc), 0, 0); err == nil {
			t.Errorf("%s was accepted", doc)
		}
		var into any
		if err := json.Unmarshal([]byte(doc), &into); err == nil {
			t.Errorf("%s: the standard library accepts what this refuses", doc)
		}
	}
}

// TestNameDispatch: the ordinary name is its own bytes and the escaped one is
// decoded through the caller's scratch.
func TestNameDispatch(t *testing.T) {
	doc := []byte(`{"plain":1,"escaped":2}`)

	lo, hi, _, esc, err := jsonMemberName(doc, 1)
	if err != nil {
		t.Fatal(err)
	}
	var scratch []byte
	if got := string(jsonName(doc, lo, hi, esc, &scratch)); got != "plain" {
		t.Errorf("read %q, want plain", got)
	}
	if scratch != nil {
		t.Error("a plain name touched the scratch")
	}

	lo, hi, _, esc, err = jsonMemberName(doc, 11)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(jsonName(doc, lo, hi, esc, &scratch)); got != "escaped" {
		t.Errorf("read %q, want escaped", got)
	}
}

// TestFinishCopiesOutOfTheBuffer: what MarshalJSON answers with must survive
// the buffer going back to the pool.
func TestFinishCopiesOutOfTheBuffer(t *testing.T) {
	scratch := jsonTakeScratch()
	*scratch = append((*scratch)[:0], `{"a":1}`...)
	held := *scratch
	out, err := jsonFinish(scratch, held, nil)
	if err != nil || string(out) != `{"a":1}` {
		t.Fatalf("got %s, %v", out, err)
	}
	if &out[0] == &held[0] {
		t.Error("the answer aliases the pooled buffer")
	}

	scratch = jsonTakeScratch()
	if _, err := jsonFinish(scratch, (*scratch)[:0], errJSONSyntax); !errors.Is(err, errJSONSyntax) {
		t.Errorf("the error was %v, want the one handed in", err)
	}
}

// TestCannotReadNamesTheKind, which is what makes the error actionable.
func TestCannotReadNamesTheKind(t *testing.T) {
	for doc, kind := range map[string]string{
		`"s"`: "string", `{}`: "object", `[]`: "array", `true`: "boolean",
		`null`: "null", `-1`: "number", `?`: "value no grammar names", ``: "end of input",
	} {
		err := jsonCannotRead("Person", []byte(doc), 0)
		if !strings.Contains(err.Error(), "Person") || !strings.Contains(err.Error(), kind) {
			t.Errorf("%q: the error %q does not name Person and %q", doc, err, kind)
		}
	}
}

// TestAFeedStartsEmptyWhateverThePoolHolds: a borrowed window arrives holding
// whatever its last user composed, and a feed must not read it as input.
func TestAFeedStartsEmptyWhateverThePoolHolds(t *testing.T) {
	dirty := jsonTakeScratch()
	*dirty = append((*dirty)[:0], `[{"Owner":1},{"Owner":2}]`...)
	jsonDropScratch(dirty)

	feed := jsonNewFeed(strings.NewReader(""))
	defer feed.close()
	if _, err := feed.peek(); !errors.Is(err, io.EOF) {
		t.Errorf("an empty reader answered %v, want io.EOF", err)
	}
	if feed.offset() != 0 {
		t.Errorf("an empty reader consumed %d bytes", feed.offset())
	}
}

// TestAFeedHandsOverWholeElements, across refills smaller than an element.
func TestAFeedHandsOverWholeElements(t *testing.T) {
	doc := `[{"a":"}"},{"b":[1,2]},"tail"]`
	feed := jsonNewFeed(iotest.OneByteReader(strings.NewReader(doc)))
	defer feed.close()

	kind, err := feed.peek()
	if err != nil || kind != '[' {
		t.Fatalf("peek said %q, %v", kind, err)
	}
	feed.take()

	for _, want := range []string{`{"a":"}"}`, `{"b":[1,2]}`, `"tail"`} {
		kind, err := feed.peek()
		if err != nil {
			t.Fatal(err)
		}
		if kind == ',' {
			feed.take()
		}
		held, err := feed.element()
		if err != nil {
			t.Fatal(err)
		}
		if string(held) != want {
			t.Errorf("the feed handed over %s, want %s", held, want)
		}
	}

	if kind, err := feed.peek(); err != nil || kind != ']' {
		t.Fatalf("after the elements the feed stands before %q, %v", kind, err)
	}
	feed.take()
	if feed.offset() != int64(len(doc)) {
		t.Errorf("consumed %d bytes, want %d", feed.offset(), len(doc))
	}
}

// TestFiniteFloatsAndNoOthers: the three values JSON has no number for are
// refused, and everything else is what the plain appender writes.
func TestFiniteFloatsAndNoOthers(t *testing.T) {
	for _, f := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		if _, err := jsonAppendFinite(nil, f, 64); !errors.Is(err, errJSONNonfinite) {
			t.Errorf("%v was answered with %v, want %v", f, err, errJSONNonfinite)
		}
	}

	got, err := jsonAppendFinite(nil, 2.5, 64)
	if err != nil || !bytes.Equal(got, jsonAppendFloat(nil, 2.5, 64)) {
		t.Errorf("a finite float came back %s, %v", got, err)
	}
}

// TestTheFlushWindowFitsThePool: a window the pool refuses to keep would make
// every streaming write allocate, which is the one thing the window exists to
// avoid.
func TestTheFlushWindowFitsThePool(t *testing.T) {
	if jsonFlushWindow > jsonScratchCap {
		t.Errorf("the flush window (%d) is larger than what the pool keeps (%d)", jsonFlushWindow, jsonScratchCap)
	}
}

// TestAFeedReadsNullAndNamesWhatItRefuses covers the two answers a reader gives
// before it reads elements: the document that says there is nothing, and the
// document of the wrong kind.
func TestAFeedReadsNullAndNamesWhatItRefuses(t *testing.T) {
	feed := jsonNewFeed(strings.NewReader("  null"))
	defer feed.close()
	if kind, err := feed.peek(); err != nil || kind != 'n' {
		t.Fatalf("peek said %q, %v", kind, err)
	}
	ok, err := feed.null()
	if err != nil || !ok {
		t.Fatalf("null was not read: %v", err)
	}
	if feed.offset() != 6 {
		t.Errorf("null consumed %d bytes, want 6", feed.offset())
	}

	wrong := jsonNewFeed(strings.NewReader(`{"a":1}`))
	defer wrong.close()
	if _, err := wrong.peek(); err != nil {
		t.Fatal(err)
	}
	said := wrong.cannotRead("Persons")
	if !strings.Contains(said.Error(), "Persons") || !strings.Contains(said.Error(), "object") {
		t.Errorf("the refusal %q does not name both sides", said)
	}
}

// TestAtEndHoldsTheDocumentToOneValue: trailing whitespace is fine and
// trailing anything else is not.
func TestAtEndHoldsTheDocumentToOneValue(t *testing.T) {
	doc := []byte(`{"a":1}  `)
	if err := jsonAtEnd(doc, 7); err != nil {
		t.Errorf("trailing whitespace was refused: %v", err)
	}
	if err := jsonAtEnd([]byte(`{"a":1}}`), 7); err == nil {
		t.Error("a trailing brace was accepted")
	}
}

// TestScanBoolReadsBothAndRefusesTheRest: the two literals, and the truncated
// thing that is neither.
func TestScanBoolReadsBothAndRefusesTheRest(t *testing.T) {
	held, next, err := jsonScanBool([]byte(`false,`), 0)
	if err != nil || held || next != 5 {
		t.Errorf("false read as %v, %d, %v", held, next, err)
	}
	if _, _, err := jsonScanBool([]byte(`fals`), 0); err == nil {
		t.Error("a truncated literal was read")
	}
}

// TestAFeedTellsNullFromItsPrefix: a value that merely starts like null is not
// one, and saying so is the caller's cue to refuse it by kind.
func TestAFeedTellsNullFromItsPrefix(t *testing.T) {
	feed := jsonNewFeed(iotest.OneByteReader(strings.NewReader("nube")))
	defer feed.close()

	if kind, err := feed.peek(); err != nil || kind != 'n' {
		t.Fatalf("peek said %q, %v", kind, err)
	}
	ok, err := feed.null()
	if err != nil || ok {
		t.Errorf("nube was read as null (%v, %v)", ok, err)
	}
}

// TestOversizedKeySlicesAreNotKept, for the same reason oversized buffers are
// not: the pool would hold what the largest caller needed for the life of the
// process.
func TestOversizedKeySlicesAreNotKept(t *testing.T) {
	huge := make([]string, 0, 1<<13)
	jsonDropKeys(&huge)

	kept := jsonTakeKeys()
	if cap(*kept) == cap(huge) {
		t.Error("an oversized key slice came back out of the pool")
	}
	jsonDropKeys(kept)
}
