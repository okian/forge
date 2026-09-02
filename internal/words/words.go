package words

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// Words takes an identifier apart into the words a reader would say it in.
//
// UserIDToken is User, ID and Token; http_server is http and server;
// oauth2Token is oauth2 and Token. The boundaries are the ones a reader draws
// rather than the ones a naive scan finds: a run of capitals is one word, the
// last capital of such a run opens the next word when a lower-case letter
// follows it, and a digit continues whatever word it is in.
//
// The one place that rule is not enough is an initialism made plural. UserIDs
// under the ordinary rule is User, I and Ds, which is not a reading of
// anything — so a run of capitals that spells an initialism keeps a single
// trailing s. That is what lets [Plural] and [Singular] be inverses over a name
// that has one: [Join] of what this returns for IDs is IDs again.
//
// Everything that is not a letter or a digit separates and is dropped, which is
// how an identifier written with underscores comes back as words. Each word is
// returned in the case it was written in; deciding the case is [Join]'s job and
// [Spell]'s, because it depends on what the name is for.
//
// The empty string has no words, and neither does a name made only of
// separators.
func Words(s string) []string {
	var out []string

	for at := 0; at < len(s); {
		start, end := word(s, at)
		if start < 0 {
			break
		}

		out = append(out, s[start:end])
		at = end
	}
	return out
}

// word returns where the first word at or after from begins and ends, or a
// negative start where there is no word left.
func word(s string, from int) (int, int) {
	for at := from; at < len(s); {
		r, width := utf8.DecodeRuneInString(s[at:])
		if !partOfWord(r) {
			at += width
			continue
		}
		return at, boundary(s, at)
	}
	return -1, -1
}

// last returns where the final word of a name begins and ends, so that only
// that word inflects and everything before it survives exactly as written.
//
// Separately from [Words] because it does not allocate, and because what it
// keeps is the name's own bytes rather than a reading of them: HomeAddress
// becomes HomeAddresses and home_address becomes home_addresses, without this
// having to know that one held an underscore.
func last(s string) (int, int) {
	start, end := -1, -1

	for at := 0; at < len(s); {
		from, to := word(s, at)
		if from < 0 {
			break
		}
		start, end, at = from, to, to
	}
	return start, end
}

// boundary returns where the word starting at from ends.
func boundary(s string, from int) int {
	caps, previous, count := from, from, 0

	for caps < len(s) {
		r, width := utf8.DecodeRuneInString(s[caps:])
		if !unicode.IsUpper(r) {
			break
		}
		previous, caps, count = caps, caps+width, count+1
	}

	switch {
	case count == 0:
		// Opens in lower case or a digit, so it runs to the next capital.
		return run(s, from)

	case count == 1:
		// One capital and then whatever follows it, an ordinary word.
		return run(s, caps)

	case plurality(s, from, caps):
		// A run spelling an initialism, and the s that makes it plural.
		return caps + 1

	case opensLower(s, caps):
		// The last capital of the run has already opened the next word.
		return previous

	default:
		return caps
	}
}

// run returns where a stretch of lower-case letters and digits ends.
func run(s string, from int) int {
	at := from
	for at < len(s) {
		r, width := utf8.DecodeRuneInString(s[at:])
		if !partOfWord(r) || unicode.IsUpper(r) {
			break
		}
		at += width
	}
	return at
}

// opensLower reports whether the name carries on in lower case.
func opensLower(s string, at int) bool {
	if at >= len(s) {
		return false
	}

	r, _ := utf8.DecodeRuneInString(s[at:])
	return unicode.IsLower(r)
}

// plurality reports whether the capitals between from and caps spell an
// initialism with a single s after them, which is one word rather than two.
func plurality(s string, from, caps int) bool {
	if caps >= len(s) || s[caps] != 's' || opensLower(s, caps+1) {
		return false
	}

	_, is := Initialism(s[from:caps])
	return is
}

// partOfWord reports whether a rune belongs to a word rather than separating
// two of them.
func partOfWord(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }

// Join writes words as one exported Go identifier.
//
// The parts are taken apart before they are put back together, so that a caller
// may hand over whole names as readily as single words: Join("user", "id") and
// Join("userId") are both UserID. Each word is spelled the way Go spells it —
// an initialism in full case wherever it falls, anything else in the letters it
// was written in with its first raised.
//
// Exported, because that is what nearly every name built out of another one is.
// [Camel] writes the unexported form of the same name, and [Spell] chooses
// between them from the visibility of the declaration the name hangs off.
func Join(parts ...string) string {
	var out strings.Builder

	for _, part := range parts {
		for _, w := range Words(part) {
			out.WriteString(raised(w))
		}
	}
	return exported(out.String())
}

// raised writes one word of an exported name.
//
// A fixed spelling is written as the table has it, because the table is the
// answer: ID rather than Id, gRPC rather than GRPC. Anything else keeps its own
// letters and gives up only its first, since the case a word was written in is
// the author's and the join owns nothing but the seam.
func raised(w string) string {
	spelled, fixed := canonical(w)
	if fixed {
		return spelled
	}
	return Upper(spelled)
}

// exported raises a name's first letter where the word that opens it is spelled
// with a small one.
//
// gRPC is the only word in the table that is, and gRPCClient is not a name a
// package can export. The word loses its spelling rather than the declaration
// losing its visibility, because the visibility is the part the compiler reads.
func exported(name string) string {
	if !opensLower(name, 0) {
		return name
	}
	return Upper(name)
}
