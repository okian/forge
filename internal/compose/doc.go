// Package compose decides whether a stack of layers is one, and works out what
// each of them is handed.
//
// Two questions, in that order. The first is whether the arrangement is one a
// stack can have: how many storage layers, where a decorator may sit, what an
// inline declaration may name. Those follow from the kinds and are answered by
// counting.
//
// The second is asked in the vocabulary of capabilities rather than in terms of
// which layer may sit on which. A layer says what it needs and what it
// contributes, and this asks each of them, from the subject outward, whether
// what is beneath it is enough — so the rules do not grow with the square of
// the catalog, and a layer written outside forge composes with the rest without
// anybody having written down a pair.
//
// The other half is what a declaration means but does not say. A refining layer
// with no storage beneath it is over a slice: the author wrote the query
// surface they wanted and left the representation to be the ordinary one, and
// filling that in here is what makes an inline declaration's underlying type
// honest rather than a special case. An entry filled in this way is marked, so
// that nothing points a diagnostic caret at a layer nobody wrote.
package compose
