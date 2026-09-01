// Package model holds a package that already declares things forge would
// otherwise write into it.
//
// Every one of them is something an author might reasonably have: a helper of
// their own under a name a layer also wants, a method they wrote in place of a
// generated one, and a method they wrote that no longer does what the layers
// above it were written against.
package model

import (
	"iter"

	"collidefixture/domain"
	"collidefixture/slices"
)

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

// Walked is a collection whose author wrote the walk itself.
//
// The walk is the one method whose signature names the element type, so it is
// where a claim about it has to be spelled the way the package spells it —
// which is with no package name at all.
type Walked []Person

// All is the author's own, doing what the contract says it does.
func (w Walked) All() iter.Seq[Person] {
	return func(yield func(Person) bool) {
		for _, held := range w {
			if !yield(held) {
				return
			}
		}
	}
}

// Wandering is a collection whose author wrote the walk over something other
// than what the collection holds.
//
// It is the one contract break the surface check cannot see: a surface spells
// the walk's result as iter.Seq[Person], which is a spelling for a person to
// read rather than one that can be lined up against the type checker, so the
// comparison there is arity only and this passes it.
type Wandering []Person

// All answers with the wrong thing entirely, which the methods generated around
// it are written against.
func (w Wandering) All() iter.Seq[string] { return nil }

// Elsewhere is a collection over a subject from another package, whose author
// wrote the walk.
//
// It is where two ways of writing one type could disagree: a method the author
// declared is read back out of the type checker, and the element a claim is
// written with comes from the subject — so a claim about this one is right only
// if the two arrive at the same words.
type Elsewhere []domain.Person

// All is the author's own, over the element the collection holds.
func (e Elsewhere) All() iter.Seq[domain.Person] {
	return func(yield func(domain.Person) bool) {
		for _, held := range e {
			if !yield(held) {
				return
			}
		}
	}
}

// Renamed is a collection whose subject lives in a package named after one the
// generated file already imports, and whose author wrote the walk.
//
// The spelling has to rename it, and where it records that rename is the whole
// of what this fixture is about: a claim written with one name and a method read
// under another are two spellings of one type, and nothing downstream can tell
// that from two types.
type Renamed []slices.Person

// All is the author's own, over the element the collection holds.
func (r Renamed) All() iter.Seq[slices.Person] {
	return func(yield func(slices.Person) bool) {
		for _, held := range r {
			if !yield(held) {
				return
			}
		}
	}
}

// DomainPerson is a local type whose name is what a generated name for
// [domain.Person] folds to.
//
// A generated name is built from the type and the package that declares it, and
// folding two things into one identifier has collisions somewhere however it is
// written. This is one of them, written down so that what happens next is a
// report rather than a file that does not build.
type DomainPerson struct {
	Name string
}

// ValidationErrors is a collection named after a type the validation layer
// writes into the file a package shares.
//
// Two generated files of one build, one name: nothing about either file can see
// the other, so the collision has to be found where the package is.
type ValidationErrors []Person

// MaskedPatch is the type a patch over Masked would be called, declared here so
// that the file a package shares is checked against what the author wrote as
// well as against itself.
//
// A companion type lands in that file rather than in any declaration's own, so
// nothing else could find the collision.
type MaskedPatch struct{ Held string }

// Masked is the subject a patch over it would be written for.
type Masked struct {
	Name string
}
