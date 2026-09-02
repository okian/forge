package words

import (
	"slices"
	"strings"
	"unicode"
)

// keywords are the words Go reserves, which no declaration may be named.
var keywords = []string{
	"break", "case", "chan", "const", "continue", "default", "defer", "else",
	"fallthrough", "for", "func", "go", "goto", "if", "import", "interface",
	"map", "package", "range", "return", "select", "struct", "switch", "type",
	"var",
}

// predeclared are the identifiers the universe block already binds.
//
// Declaring one is legal and is always a mistake in generated code: a file that
// shadows len or error or any breaks every later line that meant the original,
// and the breakage is in a file the author cannot edit. Refusing the name is
// the only report that reaches them in time.
var predeclared = []string{
	"any", "append", "bool", "byte", "cap", "clear", "close", "comparable",
	"complex", "complex64", "complex128", "copy", "delete", "error", "false",
	"float32", "float64", "imag", "int", "int8", "int16", "int32", "int64",
	"iota", "len", "make", "max", "min", "new", "nil", "panic", "print",
	"println", "real", "recover", "rune", "string", "true", "uint", "uint8",
	"uint16", "uint32", "uint64", "uintptr",
}

// standard names the methods the standard library has already given a meaning
// to, and says what that meaning is.
//
// A generated method landing on one of these is not a style problem. String is
// what fmt calls to print a value and Error is what makes a type an error, so a
// method derived from a field and given one of these names changes what every
// printf in the caller's program does — silently, correctly compiled, and
// nowhere near the declaration that caused it.
//
// A layer that means to write one says so, by reserving the name before it asks
// for it. What this catches is the layer that did not mean to.
var standard = map[string]string{
	"String":      "what fmt prints a value through",
	"Error":       "what makes a type an error",
	"MarshalJSON": "what encoding/json encodes a value through",
	"Len":         "what sort.Interface counts through",
	"Less":        "what sort.Interface orders through",
	"Swap":        "what sort.Interface reorders through",
}

// Safe reports whether Go source may declare a name, and says what stops it
// where it may not.
//
// The reason rather than a suffixed guess. A derived name that lands on a
// keyword is a declaration that will not parse and one that lands on a
// predeclared identifier is a package that compiles into something nobody
// meant, and neither is improved by forge quietly writing type2 instead: the
// author asked for a name and is owed the sentence that says why they cannot
// have it.
//
// The reason is written to compose into a diagnostic — lower case, no
// terminating punctuation — so that a caller may put a position in front of it
// and a hint after it.
func Safe(name string) (string, bool) {
	switch {
	case name == "":
		return "is empty", false

	case name == "_":
		return "is the blank identifier, which nothing may be declared as", false

	case slices.Contains(keywords, name):
		return "is a Go keyword", false

	case slices.Contains(predeclared, name):
		return "is a predeclared identifier", false

	case !startsAName(name):
		return "does not begin with a letter", false

	case !identifier(name):
		return "holds a character a Go identifier may not", false

	default:
		return "", true
	}
}

// Standard returns what the standard library means by a method name, and
// whether it means anything by it.
func Standard(name string) (string, bool) {
	meaning, is := standard[name]
	return meaning, is
}

// startsAName reports whether a name opens with something an identifier may
// open with, which is a letter or an underscore and never a digit.
func startsAName(name string) bool {
	for _, r := range name {
		return unicode.IsLetter(r) || r == '_'
	}
	return false
}

// identifier reports whether every rune of a name is one Go allows in an
// identifier.
func identifier(name string) bool {
	return !strings.ContainsFunc(name, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_'
	})
}
