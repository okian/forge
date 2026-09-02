package main

import "strings"

// line is one entry of infl.txt, taken apart.
type line struct {
	// word is the lemma, in the case upstream wrote it.
	word string

	// pos is N, V or A: noun, verb, or adjective and adverb together.
	pos byte

	// certain records that the part of speech came from the part-of-speech
	// database rather than from the shape of the inflected forms alone.
	// Upstream writes the doubtful ones with a question mark, and there are
	// twice as many of those as there are of the others.
	certain bool

	// groups are the inflected forms, one group per slot. A noun has one, the
	// plurals; a verb has the past, sometimes the past participle, the -ing
	// form and the -s form, in that order.
	groups [][]string
}

// parse reads infl.txt into entries, skipping anything that is not one.
//
// Hand-parsed rather than matched, because the format is two separators and a
// suffix and saying so in code is shorter than saying it in a pattern — and
// because what the converter drops matters as much as what it keeps, so the
// skipping is worth being able to read.
func parse(text string) []line {
	var out []line

	for _, one := range strings.Split(text, "\n") {
		held, ok := entry(one)
		if !ok {
			continue
		}
		out = append(out, held)
	}
	return out
}

// entry reads one line, and reports whether it was one.
func entry(text string) (line, bool) {
	head, rest, found := strings.Cut(text, ": ")
	if !found {
		return line{}, false
	}

	word, tag, found := strings.Cut(head, " ")
	if !found || word == "" || tag == "" {
		return line{}, false
	}

	out := line{word: word, pos: tag[0], certain: !strings.HasSuffix(tag, "?")}
	if len(strings.TrimSuffix(tag, "?")) != 1 {
		return line{}, false
	}

	for _, group := range strings.Split(rest, " | ") {
		out.groups = append(out.groups, strings.Split(group, ", "))
	}
	return out, true
}

// pick returns the form a group prefers, or the empty string where every form
// in it is one the converter will not take.
//
// Upstream marks a form it is unsure of, and marks a form that belongs to a
// similar word rather than to this one, and gives a variant level to a form
// that is archaic or obscure or that it could not choose between. What is left
// after all of those is the form somebody would actually write, which is the
// only one a generator has any use for.
func pick(group []string) string {
	for _, one := range group {
		if held, ok := form(one); ok {
			return held
		}
	}
	return ""
}

// form reads one inflected form, and reports whether it is one to take.
//
// A form is a word, then whatever upstream tagged it with, then an optional
// variant level and an optional explanation in braces. The word is taken and
// the rest decides whether to take it.
func form(text string) (string, bool) {
	held := strings.TrimSpace(text)

	at := 0
	for at < len(held) && spelling(held[at]) {
		at++
	}
	word := held[:at]

	tags := at
	for tags < len(held) && strings.IndexByte("~<!?", held[tags]) >= 0 {
		tags++
	}

	if strings.ContainsAny(held[at:tags], "~!?") || !plainWord(word) {
		return "", false
	}

	level, _, _ := strings.Cut(strings.TrimSpace(held[tags:]), " ")
	if variant(level) {
		return "", false
	}
	return word, true
}

// spelling reports whether a byte is one an upstream lemma is spelled with.
func spelling(b byte) bool {
	return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b == '\''
}

// variant reports whether a form carries a variant level that says it is not
// the one to use. Level 0 and its fractions are the preferred form; 1 and 2 are
// the less preferred and the obscure, and anything that is not a number is an
// explanation rather than a level.
func variant(level string) bool {
	whole, _, _ := strings.Cut(level, ".")
	if whole == "" || whole[0] < '0' || whole[0] > '9' {
		return false
	}
	return whole != "0"
}

// plainWord reports whether a form is something a Go identifier could hold:
// ASCII letters and nothing else, so no apostrophes, no accents and no spaces.
func plainWord(word string) bool {
	if word == "" {
		return false
	}
	for i := range len(word) {
		if b := word[i]; (b < 'a' || b > 'z') && (b < 'A' || b > 'Z') {
			return false
		}
	}
	return true
}
