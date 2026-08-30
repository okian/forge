// Package merge combines what each layer of a stack generated into what one
// file will hold.
//
// Every layer produces its own declarations without knowing what the others
// produced, which is what keeps a layer's implementation about its own subject
// rather than about the catalog. The cost of that independence is paid here:
// two layers may need the same import, may need the same helper type emitted
// once, and may want the same method name.
//
// The first two have one right answer and this package gives it. The third does
// not, and this package deliberately does not decide it yet — which of two
// layers wanting WriteTo keeps the unqualified name is a question with an
// author-visible answer and a diagnostic attached to getting it wrong, so it
// belongs with the stage that can report it rather than in a concatenation.
//
// What merging does guarantee is order. Units arrive in stack order and their
// declarations are kept in it, so the file a declaration produces is a function
// of the declaration alone. Nothing here sorts, because sorting would separate
// a type from the constructor that goes with it — and nothing here iterates a
// map, because the same inputs have to produce the same bytes.
package merge
