// Package words owns every decision forge makes about an identifier: how a
// name is taken apart into words, how those words are spelled back out the way
// Go spells them, how a word is made plural or singular, and whether the set of
// names written into a package is one a compiler will accept.
//
// It is here rather than in the layers because a name is not a layer's
// business. A codec naming a wire member, an enumeration naming a set member
// and a collection naming a projection are three layers answering one question
// about Go identifiers, and three answers to it is one library with three
// opinions about what a Go name looks like. Every layer and the plugin surface
// go through this package, and no layer keeps a rule of its own.
//
// # What is data and what is a rule
//
// English inflection is data. It is finite, somebody else has already compiled
// it, and it fits in the binary — so forge ships it rather than guessing at it.
// [Plural] and [Singular] consult an embedded dictionary built from the
// Automatically Generated Inflection Database, and fall through to the regular
// rules for a word the dictionary has never heard of, which is where a domain
// word belongs: a domain word is usually regular.
//
// This replaces an argument the collection layer used to make for itself —
// that a table of exceptions never ends, because no dictionary knows the
// author's domain words. That is right about a hand-written table of forty
// words and wrong about the conclusion. A real dictionary does end. What it
// costs is tens of kilobytes in a binary somebody installed once; what it buys
// is People rather than Persons, Children rather than Childs, Data rather than
// Datums, and Aliases rather than Aliaseses.
//
// Go spelling is a rule, and the three things it needs are kept as Go source
// rather than taken from upstream, because none of them is an English fact: the
// initialism set the language's own linters agree on, the keywords and
// predeclared identifiers a name may not land on, and the method names the
// standard library has already given a meaning to.
//
// Nothing here is asked of the author. There is no wordlist to install, no file
// to point at, no directive to write and no network at generation time. The
// dictionary is compiled into the forge binary, so go install is the whole
// installation, and two people generating from one declaration cannot get two
// answers.
//
// # What the output depends on
//
// Nothing. The dictionary runs in the generator and never in the generated
// code, and adds nothing to what that code imports. This package itself imports
// only the standard library and depends on nothing else in the tree, so that
// everything else may depend on it.
package words
