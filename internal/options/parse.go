package options

import (
	"go/token"
	"strings"
	"unicode"

	"github.com/okian/forge/internal/discover"
	"github.com/okian/forge/internal/model"
)

// parse splits one directive's arguments into the pairs it was written as.
//
// Positions are counted through the original text rather than rebuilt from the
// pieces, so that a diagnostic about one option underlines that option however
// much space was written around it. An argument list is short and a caret is
// only useful if it lands.
func parse(d discover.Directive) []model.Option {
	var found []model.Option

	at := d.ArgsPos()
	for offset := 0; offset < len(d.Args); {
		// Space between arguments belongs to neither, and is counted rather
		// than trimmed so that what follows keeps its own column.
		if size := leading(d.Args[offset:]); size > 0 {
			offset += size
			continue
		}

		width := until(d.Args[offset:])

		// A directive is a comment, so a comment written after its options is
		// still on the line. Reading the words of one as options gives an
		// author three complaints about prose they wrote for a reader.
		if strings.HasPrefix(d.Args[offset:], comment) {
			break
		}

		found = append(found, pair(d.Args[offset:offset+width], moved(at, offset)))
		offset += width
	}

	return found
}

// comment opens a remark written after a directive's options, which is where an
// author explains to a reader what the options are for.
//
// Everything after it is left alone, options included: a line reading
// "sort=Age // index=ID later" has commented the second one out, and reading it
// anyway would be forge disagreeing with the author about their own comment.
// The marker has to open an argument to count, so "sort=Age// note" is one
// argument whose value ends in slashes — which is a field name nothing has, and
// is reported as one.
const comment = "//"

// leading returns the length of the space at the start of text.
func leading(text string) int {
	for i, r := range text {
		if !unicode.IsSpace(r) {
			return i
		}
	}
	return len(text)
}

// until returns the length of the argument at the start of text.
func until(text string) int {
	for i, r := range text {
		if unicode.IsSpace(r) {
			return i
		}
	}
	return len(text)
}

// pair splits one argument into its key and value.
//
// At the first equals sign, so that a value may hold one: a regular expression
// or a format string is a plausible option value and neither should have to be
// escaped to be written.
func pair(arg string, at token.Position) model.Option {
	key, value, found := strings.Cut(arg, "=")
	if !found {
		return model.Option{Key: arg, Pos: at}
	}
	return model.Option{Key: key, Value: value, Pos: at}
}

// moved returns a position advanced by a byte offset along one line.
//
// Directives are one line by construction — a comment ends at the newline — so
// a byte offset moves along the column and never down.
func moved(at token.Position, offset int) token.Position {
	at.Column += offset
	at.Offset += offset
	return at
}
