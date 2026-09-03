package layer

import (
	"strconv"

	"github.com/okian/forge/internal/model"
)

// Stage says how far along a layer is, which is the difference between a
// declaration that will work in the next release and one that will not work in
// this one.
type Stage uint8

const (
	// StageReady is a layer that generates.
	StageReady Stage = iota

	// StageStub is a layer whose marker is declared, so that a declaration
	// naming it type-checks and composes, and whose generator has not been
	// written. Asking one to generate reports exactly that.
	StageStub

	// StageStaged is a marker forge declares for a layer it has not committed
	// to yet. It is here so that a declaration naming one is answered with what
	// is missing rather than with silence about a marker forge plainly ships.
	StageStaged
)

// stageNames gives each stage the spelling the list command prints.
var stageNames = [...]string{
	StageReady:  "ready",
	StageStub:   "stub",
	StageStaged: "staged",
}

// String returns the stage's lower-case name.
func (s Stage) String() string {
	if int(s) >= len(stageNames) {
		return "stage(" + strconv.Itoa(int(s)) + ")"
	}
	return stageNames[s]
}

// Described is what a layer may say about itself beyond what generation needs:
// how far along it is, and what it is for in one line.
//
// It is separate from [Layer] because neither question has an answer a layer
// written outside forge could give. Which release ships a layer is forge's own
// roadmap, and a plugin's summary of itself is documentation rather than
// something the pipeline acts on. A caller that wants either asks for it.
type Described interface {
	// Stage says how far along the layer is.
	Stage() Stage

	// Doc is the one-line summary of what the layer is for. It begins in lower
	// case and carries no terminating punctuation.
	Doc() string
}

// Transparent is implemented by a layer whose invariants survive direct access
// to the type underneath it.
//
// A declaration written in an ordinary file has a real underlying type that
// anything in the package can read and write. Any slice is a valid collection,
// so writing one inline costs nothing; a ring's head index, a set's dedup and a
// lock's exclusion are all invariants a raw write would quietly break, so a
// stack containing one belongs in a spec file where the representation is
// opaque.
//
// A layer that says nothing is taken to be opaque, which is the safe direction:
// the cost of being wrong that way is a declaration that has to move to a spec
// file, and the cost of being wrong the other way is a corrupted value at run
// time with nothing to point at.
type Transparent interface {
	// Transparent reports whether the raw underlying type upholds this layer's
	// invariants on its own.
	Transparent() bool
}

// TransparentLayer reports whether a layer upholds its invariants over the raw
// underlying type, which is what decides whether a stack containing it may be
// written outside a spec file.
//
// An element layer is never transparent, whatever it says. That is not a
// judgement about its invariants but about how it is written: an element marker
// cannot be a defined slice, because Go rejects a generic alias to its own type
// parameter, so it is a zero-sized phantom struct — and a declaration holding
// one has an underlying type of struct{} rather than []Person. Deciding it here
// makes the rule fall out of the marker representation, instead of resting on
// every element layer in the catalog remembering not to claim otherwise.
func TransparentLayer(l Layer) bool {
	if l.Kind() == model.KindElement {
		return false
	}

	declared, ok := l.(Transparent)
	return ok && declared.Transparent()
}
