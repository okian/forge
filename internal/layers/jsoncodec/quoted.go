package jsoncodec

import (
	"strconv"
	"strings"
)

// plain reports whether a member's JSON name can be written as bytes prepared
// here rather than quoted again on every call.
//
// The encoder quotes a name each time it is handed one: AppendQuote was a sixth
// of the encode profile, scanning literals the generator already knows. A name
// that needs no escaping is the same bytes every time and can be written as a
// raw value instead.
//
// The set is narrow on purpose. What must hold is that the prepared bytes are
// what the encoder would have produced under any options it may carry — and an
// encoder can be asked to escape HTML, which would turn a name holding < or &
// into something the prepared form does not match. So this admits only the
// characters no option can change: ASCII letters and digits, and the three
// separators a field name is written with. Everything else keeps the quoting
// call, which is correct whatever the encoder was told to do.
func plain(name string) bool {
	if name == "" {
		return false
	}

	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '_', r == '-', r == '.':
		default:
			return false
		}
	}

	return true
}

// quoted returns the name as the bytes a JSON document holds for it, which for
// a name [plain] admits is the name between two quotation marks.
func quoted(name string) string { return strconv.Quote(name) }

// nameVar is the identifier the prepared bytes for one name are declared under.
//
// Prefixed with the type the codec is for, so that two subjects in one package
// sharing a member name each get their own and neither has to know about the
// other. The name itself is folded to letters, since a JSON name may hold the
// separators that a Go identifier may not.
func nameVar(prefix, name string) string {
	var b strings.Builder

	b.WriteString(prefix)
	b.WriteString("JSONName")

	upper := true
	for _, r := range name {
		if r == '_' || r == '-' || r == '.' {
			upper = true
			continue
		}
		if upper && r >= 'a' && r <= 'z' {
			r -= 'a' - 'A'
		}
		upper = false
		b.WriteRune(r)
	}

	return b.String()
}
