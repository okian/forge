package tmpl

import (
	"encoding/json/jsontext"
	"strings"
	"testing"
)

// What these measure is whether the reimplementation is worth having. A helper
// that matched the standard library byte for byte and cost more than it would
// be a helper to delete, so the comparison is against the thing it replaces
// rather than against nothing.

var benchStrings = []struct {
	name string
	held string
}{
	{"short-plain", "hello world"},
	{"short-escaped", "he said \"hi\"\n"},
	{"long-plain", strings.Repeat("abcdefgh", 32)},
	{"long-escaped", strings.Repeat("a\"b\nc", 51)},
	{"multibyte", strings.Repeat("日本語テキスト", 12)},
}

func BenchmarkAppendString(bm *testing.B) {
	for _, c := range benchStrings {
		bm.Run(c.name+"/ours", func(bm *testing.B) {
			buf := make([]byte, 0, 1024)
			bm.SetBytes(int64(len(c.held)))
			for bm.Loop() {
				var err error
				if buf, err = jsonAppendString(buf[:0], c.held); err != nil {
					bm.Fatal(err)
				}
			}
		})
		bm.Run(c.name+"/jsontext", func(bm *testing.B) {
			buf := make([]byte, 0, 1024)
			bm.SetBytes(int64(len(c.held)))
			for bm.Loop() {
				var err error
				if buf, err = jsontext.AppendQuote(buf[:0], c.held); err != nil {
					bm.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkCloseText measures what appending text in place and settling it
// saves over escaping it on the way in, which is the whole reason the settle
// exists. The plain case is the one the fast path was built for — a text an
// appender wrote that needs nothing done to it — and the escaped case is the
// detour, which must stay affordable rather than fast.
func BenchmarkCloseText(bm *testing.B) {
	for _, c := range []struct {
		name string
		held string
	}{
		{"plain", "0192aefb-74a0-7000-8000-1234567890ab"},
		{"escaped", "he said \"hi\"\n"},
	} {
		bm.Run(c.name+"/close", func(bm *testing.B) {
			buf := make([]byte, 0, 1024)
			bm.SetBytes(int64(len(c.held)))
			for bm.Loop() {
				var err error
				buf = append(buf[:0], '"')
				buf = append(buf, c.held...)
				if buf, err = jsonCloseText(buf, 0); err != nil {
					bm.Fatal(err)
				}
			}
		})
		bm.Run(c.name+"/escaper", func(bm *testing.B) {
			buf := make([]byte, 0, 1024)
			bm.SetBytes(int64(len(c.held)))
			for bm.Loop() {
				var err error
				if buf, err = jsonAppendString(buf[:0], c.held); err != nil {
					bm.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkAppendFloat(bm *testing.B) {
	for _, c := range []struct {
		name string
		held float64
	}{{"fixed", 2.5}, {"exponent", 1e-9}, {"long", 1.0 / 3}} {
		bm.Run(c.name+"/ours", func(bm *testing.B) {
			buf := make([]byte, 0, 64)
			for bm.Loop() {
				buf = jsonAppendFloat(buf[:0], c.held, 64)
			}
		})
		bm.Run(c.name+"/jsontext", func(bm *testing.B) {
			buf := make([]byte, 0, 64)
			for bm.Loop() {
				buf = jsontext.AppendFloat(buf[:0], c.held, 64)
			}
		})
	}
}

func BenchmarkScanString(bm *testing.B) {
	for _, c := range []struct {
		name string
		doc  string
	}{
		{"plain", `"` + strings.Repeat("abcdefgh", 32) + `"`},
		{"escaped", `"` + strings.Repeat(`a\nb`, 51) + `"`},
	} {
		b := []byte(c.doc)
		bm.Run(c.name, func(bm *testing.B) {
			bm.SetBytes(int64(len(b)))
			for bm.Loop() {
				if _, _, _, _, err := jsonScanString(b, 0); err != nil {
					bm.Fatal(err)
				}
			}
		})
	}
}
