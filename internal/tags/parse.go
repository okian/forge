package tags

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
)

// jsonKey is the one struct tag key whose grammar forge does not get to choose.
// Everything else is a convention; this one is a specification, and a name read
// differently from the way the standard library reads it is a field that
// arrives on the wire under the wrong name.
const jsonKey = "json"

// Problem is one thing wrong with a struct tag: a tag whose grammar broke, so
// that what it was trying to say was lost rather than misread.
//
// Problems are grammatical, not semantic. An option no layer understands, an
// option written twice, an option in a position its own rules forbid — all of
// those decompose, and rejecting them belongs to the layer that knows what the
// option means. What is reported here is a tag that cannot be decomposed.
type Problem struct {
	// Key is the struct tag key the problem is in. It is empty for a problem
	// with the tag as a whole, which is what a tag too malformed to yield a key
	// produces.
	Key string

	// Message says what is wrong, in lower case and without terminating
	// punctuation, so that it composes into a longer diagnostic.
	Message string
}

// String returns the problem with the key it belongs to, "json: trailing
// comma".
func (p Problem) String() string {
	if p.Key == "" {
		return p.Message
	}
	return p.Key + ": " + p.Message
}

// Parse returns every tag written in raw, in the order the keys appear, along
// with the problems in the ones whose grammar broke.
//
// raw is a struct tag's contents without the surrounding backticks, which is
// what go/types hands over. A key written twice is read once, as the reflect
// package reads it, and the repeat is reported rather than silently preferred
// or silently dropped.
//
// A tag whose value could not be fully decomposed is still returned, carrying
// whatever was read before the grammar broke. A layer looking its key up finds
// it present and partial, which is the truth, and the problem beside it is what
// stops anything being generated from it.
func Parse(raw string) ([]Tag, []Problem) {
	var (
		found    []Tag
		seen     []string
		problems []Problem
	)

	rest := raw
	for {
		key, quoted, remainder, ok := next(rest)
		if !ok {
			if remainder != "" {
				problems = append(problems, Problem{
					Message: fmt.Sprintf("struct tag is malformed from %q on", remainder),
				})
			}
			break
		}
		rest = remainder

		// A key is recorded as read whether or not its value could be, so that
		// a repeat after a broken one is not quietly promoted into its place.
		// The reflect package would not find that repeat either.
		if slices.Contains(seen, key) {
			problems = append(problems, Problem{Key: key, Message: "key is written twice, and only the first is read"})
			continue
		}
		seen = append(seen, key)

		// The value is unquoted one key at a time, where the reflect package
		// unquotes it, and not for the tag as a whole. A value that is not a Go
		// string literal is a key nothing can read; it is not a reason to stop
		// reading the keys written after it, which the compiler and the
		// standard library both go on reading.
		value, err := strconv.Unquote(quoted)
		if err != nil {
			problems = append(problems, Problem{
				Key:     key,
				Message: fmt.Sprintf("value %q is not a Go string literal", quoted),
			})
			continue
		}

		tag, broken := parseValue(key, value)
		tag.Key, tag.Raw = key, value
		found = append(found, tag)

		for _, message := range broken {
			problems = append(problems, Problem{Key: key, Message: message})
		}
	}

	return found, problems
}

// parseValue decomposes one tag value by the rules of its key.
func parseValue(key, value string) (Tag, []string) {
	if key == jsonKey {
		return parseJSON(value)
	}
	return parseConventional(value), nil
}

// next reads one key:"value" pair off the front of a struct tag, returning the
// value still quoted.
//
// The grammar is the reflect package's, which is the only definition of it
// there is: space-separated pairs of a bare key and a Go-quoted value. It
// reports false both at the end of the tag and for a tag it cannot read, which
// the caller tells apart by whether anything is left over.
//
// The value is left quoted because finding where it ends and reading what it
// says are two different questions. Its end is found by scanning for the first
// quote no backslash precedes, which never fails; what it says is a Go string
// literal, which can, and only for the key that asked.
func next(tag string) (key, quoted, rest string, ok bool) {
	// Only spaces separate pairs. A tab between them is malformed, not
	// whitespace, which is why this is not a TrimSpace.
	tag = strings.TrimLeft(tag, " ")
	if tag == "" {
		return "", "", "", false
	}

	// The key runs to the colon and may hold no space, quote or control
	// character.
	i := 0
	for i < len(tag) && tag[i] > ' ' && tag[i] != ':' && tag[i] != '"' && tag[i] != 0x7f {
		i++
	}
	if i == 0 || i+1 >= len(tag) || tag[i] != ':' || tag[i+1] != '"' {
		return "", "", tag, false
	}

	// The value is a Go string literal, so its end is the first quote that is
	// not escaped. It is scanned out of a copy rather than out of tag, so that
	// a pair which turns out to be unreadable is reported from where it starts
	// and not from the middle of itself.
	value := tag[i+1:]
	j := 1
	for j < len(value) && value[j] != '"' {
		if value[j] == '\\' {
			j++
		}
		j++
	}
	if j >= len(value) {
		return "", "", tag, false
	}

	return tag[:i], value[:j+1], value[j+1:], true
}

// parseConventional decomposes a tag value into the leading name and the
// options after it.
//
// This is the grammar the ecosystem copied from encoding/json and then stopped
// agreeing about: commas separate, nothing quotes, and an option's value is
// whatever follows its first colon or equals sign. Both separators are accepted
// because both are in circulation — json-descended tags write a colon, and
// validator tags write an equals sign — and a tag that uses neither is a bare
// option.
//
// Nothing here can fail. Every byte belongs to some part of the result, which
// is what it means for a grammar to be a convention rather than a
// specification: there is no authority to be wrong against.
func parseConventional(value string) Tag {
	if value == ignoreValue {
		return Tag{Ignored: true}
	}

	parts := strings.Split(value, ",")

	tag := Tag{Name: parts[0]}
	for _, part := range parts[1:] {
		tag.Options = append(tag.Options, conventionalOption(part))
	}
	return tag
}

// conventionalOption splits one option into its name and value at the first
// separator it carries, if it carries one.
func conventionalOption(raw string) Option {
	if i := strings.IndexAny(raw, ":="); i >= 0 {
		return Option{Name: raw[:i], Value: raw[i+1:], HasValue: true, Raw: raw}
	}
	return Option{Name: raw, Raw: raw}
}
