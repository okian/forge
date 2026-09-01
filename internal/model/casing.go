package model

import "unicode"

// Camel writes a Go name with its first word in lower case.
//
// The first word rather than the first letter, because an exported Go name
// often opens with an initialism and lowering one letter of it produces
// jSONValue — a name nobody would write and no reader would recognise. The run
// of capitals a name opens with is one word, except that a capital immediately
// before a lower-case letter has already started the next one.
//
// Here rather than in the layer that first needed it, because two layers need
// it and they have to agree: a codec naming a member jsonValue and an
// enumeration naming the same shape jSONValue would be one library with two
// opinions about what a Go name looks like. It is also the answer to a question
// about Go identifiers rather than about anything either layer does, which is
// what makes this the place for it.
func Camel(name string) string {
	runes := []rune(name)

	end := 0
	for end < len(runes) && unicode.IsUpper(runes[end]) {
		end++
	}

	switch {
	case end == 0:
		return name
	case end < len(runes) && end > 1:
		// The last capital of the run opens the next word: JSONValue is json
		// and Value, not jsonv and alue.
		end--
	}

	for i := range end {
		runes[i] = unicode.ToLower(runes[i])
	}
	return string(runes)
}
