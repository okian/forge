package tags

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// ignoreValue is the whole tag that hides a field. Only the whole tag does it:
// an unquoted dash anywhere else is an ordinary name, so json:"-,omitzero"
// names the field "-" rather than hiding it.
const ignoreValue = "-"

// nameReserved holds the characters that end an unquoted json name: the comma
// that separates options, the backslash an escape begins with, and the three
// quote characters.
//
// A name may otherwise hold anything, spaces and letters outside ASCII
// included, because the name is a JSON object member and not a Go identifier.
const nameReserved = ",\\'\"`"

// valueOption is a json option written as a name and a value.
type valueOption struct {
	// name is the option's own name, the "format" of format:RFC3339.
	name string

	// quoted records whether the value may be single-quoted, which is the one
	// place a quote is legal in a json tag at all. A format value may be, which
	// is what lets a date layout hold the commas it needs; a name, an option
	// and a case value may not.
	quoted bool
}

// valueOptions are the json options that take a value.
//
// The grammar is not uniform: every other option is a bare word, and a colon
// after one of them is a malformed tag rather than a value nobody reads. The
// list lives here rather than with the layer that interprets the values,
// because it is what decides where one option ends and the next begins.
var valueOptions = []valueOption{
	{name: "case"},
	{name: "format", quoted: true},
}

// takesValue returns the rules for an option's value, and whether the option
// takes one.
func takesValue(name string) (valueOption, bool) {
	for _, option := range valueOptions {
		if option.name == name {
			return option, true
		}
	}
	return valueOption{}, false
}

// parseJSON decomposes a json tag value the way encoding/json/v2 decomposes it.
//
// This grammar is not forge's to choose. A field whose name forge computes
// differently from the standard library is a field that arrives on the wire
// under one name and is looked for under another, and no amount of care
// elsewhere recovers from that. Every rule here is one the standard library
// enforces, and the tests hold the two together by marshaling.
//
// What it does not do is judge. Whether an option exists, whether it may be
// written twice, whether it may be written where it was written — those are
// questions for the layer that reads the options, which is the only thing that
// knows the answers. This returns the decomposition and the ways it failed to
// reach one.
func parseJSON(value string) (Tag, []string) {
	if value == ignoreValue {
		return Tag{Ignored: true}, nil
	}

	var (
		tag    Tag
		broken []string
		rest   = value
	)

	// A value opening with a comma keeps the field's own name and carries
	// options only.
	if rest != "" && !strings.HasPrefix(rest, ",") {
		name, n, err := consumeName(rest)
		switch {
		case err != nil:
			broken = append(broken, err.Error())
		case !utf8.ValidString(name):
			broken = append(broken, fmt.Sprintf("name %q is not valid UTF-8", name))
			// Replaced rather than kept, because the standard library replaces
			// it. A name that differs from the standard library's is the one
			// thing this grammar exists to prevent, and a caller may read the
			// name before it reads the problems.
			tag.Name = string([]rune(name))
		default:
			tag.Name = name
		}
		rest = rest[n:]
	}

	for rest != "" {
		// The comma comes first, and it is consumed whether or not it is there,
		// so that a value missing one still makes progress.
		if !strings.HasPrefix(rest, ",") {
			broken = append(broken, fmt.Sprintf("invalid character %q before the next option, expecting a comma", rest[0]))
		} else {
			rest = rest[1:]
			if rest == "" {
				broken = append(broken, "trailing comma")
				break
			}
		}

		var option Option
		option, rest, broken = consumeJSONOption(rest, broken)
		tag.Options = append(tag.Options, option)
	}

	return tag, broken
}

// consumeJSONOption reads one option, and the value that follows it when the
// option is one of the few that take a value, off the front of a tag value.
func consumeJSONOption(rest string, broken []string) (Option, string, []string) {
	name, n, err := consumeOption(rest, false)
	if err != nil {
		broken = append(broken, err.Error())
	}

	option := Option{Name: name, Raw: rest[:n]}
	rest = rest[n:]

	takes, ok := takesValue(name)
	if !ok {
		return option, rest, broken
	}
	if !strings.HasPrefix(rest, ":") {
		broken = append(broken, fmt.Sprintf("option %s is missing its value", name))
		return option, rest, broken
	}
	rest = rest[len(":"):]

	// A value that could not be read is left where it is, to be read again as
	// the option it is not. Consuming it instead would decompose a broken tag
	// into options the standard library never produced, and the whole point of
	// following its grammar is that a caller reading a tag reads the same tag.
	value, vn, err := consumeOption(rest, takes.quoted)
	switch {
	case err != nil:
		broken = append(broken, fmt.Sprintf("option %s has a malformed value: %v", name, err))
		return option, rest, broken

	case value == "":
		// Only a quoted value can be empty without being malformed, and an
		// empty one says nothing that leaving the option off does not.
		broken = append(broken, fmt.Sprintf("option %s cannot have an empty value", name))
		return option, rest, broken
	}

	option.Value, option.HasValue = value, true
	option.Raw += ":" + rest[:vn]

	return option, rest[vn:], broken
}

// consumeName reads the name off the front of a json tag value, returning it
// and how many bytes it took.
//
// A name is an unquoted run of anything but the reserved characters, and
// telling one from a mistake takes one look ahead: a run that neither ends at a
// comma nor reaches the end of the value stopped at a character a name may not
// hold, so whatever is there has to be read the way an option is read — which
// is where it is rejected.
func consumeName(in string) (string, int, error) {
	n := len(in) - len(strings.TrimLeftFunc(in, func(r rune) bool {
		return !strings.ContainsRune(nameReserved, r)
	}))

	if !strings.HasPrefix(in[n:], ",") && n != len(in) {
		return consumeOption(in, false)
	}
	return in[:n], n, nil
}

// consumeOption reads one option off the front of a json tag value: a Go
// identifier, or — where quoting is allowed at all — a single-quoted string.
//
// A malformed option is consumed as far as the next comma, so that one bad
// option costs one option rather than everything after it.
func consumeOption(in string, allowQuoted bool) (string, int, error) {
	end := strings.IndexByte(in, ',')
	if end < 0 {
		end = len(in)
	}

	switch r, _ := utf8.DecodeRuneInString(in); {
	case in == "":
		return "", 0, errors.New("an option is missing")

	case r == '_' || unicode.IsLetter(r):
		n := len(in) - len(strings.TrimLeftFunc(in, identifier))
		return in[:n], n, nil

	case allowQuoted && r == '\'':
		value, n, err := consumeQuoted(in)
		if err != nil {
			return in[:end], end, err
		}
		return value, n, nil

	default:
		return in[:end], end, fmt.Errorf("invalid character %q at the start of an option, expecting a Unicode letter", r)
	}
}

// consumeQuoted reads a single-quoted string off the front of in.
//
// The grammar is Go's own double-quoted string literal with the delimiters
// swapped, because neither a double quote nor a backtick can be written
// verbatim in the backquoted literal a struct tag is usually written as.
// Swapping them back is what lets strconv do the escapes, rather than a second
// implementation of them that could disagree with the first.
func consumeQuoted(in string) (string, int, error) {
	var (
		swapped = []byte{'"'}
		n       = len("'")
		escaped bool
	)

	for n < len(in) {
		r, size := utf8.DecodeRuneInString(in[n:])
		switch {
		case escaped:
			if r == '\'' {
				// \' is not an escape once the delimiters are swapped.
				swapped = swapped[:len(swapped)-1]
			}
			escaped = false

		case r == '\\':
			escaped = true

		case r == '"':
			// A double quote is not special here but will be once it is the
			// delimiter.
			swapped = append(swapped, '\\')

		case r == '\'':
			swapped = append(swapped, '"')
			n += len("'")

			value, err := strconv.Unquote(string(swapped))
			if err != nil {
				return "", n, fmt.Errorf("invalid single-quoted string %q", in[:n])
			}
			return value, n, nil
		}

		swapped = append(swapped, in[n:n+size]...)
		n += size
	}

	return "", n, fmt.Errorf("single-quoted string %q is not terminated", in)
}

// identifier reports whether a rune may appear in an unquoted option.
func identifier(r rune) bool {
	return r == '_' || unicode.IsLetter(r) || unicode.IsNumber(r)
}
