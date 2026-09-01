// Package model holds a package that already declares things forge would
// otherwise write into it.
//
// Every one of them is something an author might reasonably have: a helper of
// their own under a name a layer also wants, a method they wrote in place of a
// generated one, and a method they wrote that no longer does what the layers
// above it were written against.
package model

// Person is the subject every declaration below is over.
type Person struct {
	Name string
}

// Taken is a collection whose package already declares the view type the
// collection layer names after it.
type Taken []Person

// TakenSeq is the author's own, under the name the layer wants.
//
// It is not a mistake worth guessing about: forge writes a file it owns, and a
// name in it that the author also declared is two declarations in one package.
type TakenSeq struct{ Held string }

// Overridden is a collection whose author wrote one of the methods the storage
// layer would have written.
type Overridden []Person

// Len is the author's own, doing what the contract says it does. Generating a
// second one would not compile, so the generated one is the one that gives way.
func (o Overridden) Len() int { return len(o) * 2 }

// Contradicting is a collection whose author wrote a method under a name the
// stack promises, with a different answer.
type Contradicting []Person

// Len here answers with a string, which is not what a layer written against a
// sized shape can call.
func (c Contradicting) Len() string { return "some" }
