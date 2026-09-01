package scalars

import (
	"go/token"
	"go/types"
	"testing"

	"github.com/okian/forge/internal/model"
)

// A String a previous run wrote is not evidence that the type has one.
//
// The whole reason this is not types.Implements. A generated file is loaded
// with the package it belongs to, so the type checker's answer includes
// whatever forge left on disk last time — and a check built on that says yes on
// a committed tree and no on a clean checkout, which is a package that cannot
// be rebuilt from its own source.
func TestAStringAPreviousRunWroteIsNotEvidence(t *testing.T) {
	const wrote = token.Pos(42)

	held := saying(wrote)
	local := held.Obj().Pkg().Path()

	if !says(held, Asked{Local: local}) {
		t.Error("a type with a String does not say how it reads")
	}
	if !says(held, Asked{Local: local, Generated: never}) {
		t.Error("a String the author wrote does not count")
	}
	if says(held, Asked{Local: local, Generated: only(wrote)}) {
		t.Error("a String this run is about to rewrite counts as one the type has")
	}
}

// But one generated in another package is a file this run does not touch, so it
// counts like any other.
//
// The distinction is what keeps the check from refusing a configuration that
// works: a subject in a package forge already generated has a String, it is
// there on disk, and nothing this run does replaces it. Suppressing it would
// refuse the field and tell the author to give the type a String they can see
// it already has.
func TestAStringGeneratedInAnotherPackageCounts(t *testing.T) {
	const wrote = token.Pos(42)

	held := saying(wrote)

	if !says(held, Asked{Local: "example.com/other", Generated: only(wrote)}) {
		t.Error("a String generated in another package does not count")
	}
}

// never and only are the two answers a generated-position predicate gives.
func never(token.Pos) bool { return false }

func only(held token.Pos) func(token.Pos) bool {
	return func(pos token.Pos) bool { return pos == held }
}

// A subject of this run counts before its String exists, and a type carrying
// the same tag that nobody declared over does not.
//
// The difference is the whole of what makes this safe. Whether a type will be
// given a String is a fact about the run, not about the type: a struct with a
// display tag that no declaration is over is one forge writes nothing about,
// and rendering a field through its String would name a method that never
// arrives.
func TestOnlyASubjectOfThisRunCounts(t *testing.T) {
	held := plain("Earning")

	if says(held, Asked{}) {
		t.Error("a type nothing in this run is over says how it reads")
	}

	of := Asked{Earning: map[string]bool{model.TypeIdentity(held): true}}
	if !says(held, of) {
		t.Error("a subject of this run does not count before its String is written")
	}

	// And a pointer to one counts too, since that is how a field holding one
	// optionally is written.
	if !says(types.NewPointer(held), of) {
		t.Error("a pointer to a subject of this run does not count")
	}
}

// saying returns a named type with a String declared at a given position.
func saying(at token.Pos) *types.Named {
	held := plain("Stamp")
	pkg := held.Obj().Pkg()

	held.AddMethod(types.NewFunc(at, pkg, "String", types.NewSignatureType(
		types.NewVar(token.NoPos, pkg, "s", held), nil, nil, nil,
		types.NewTuple(types.NewVar(token.NoPos, pkg, "", types.Typ[types.String])),
		false,
	)))

	return held
}

// plain returns a named struct with no methods.
func plain(name string) *types.Named {
	pkg := types.NewPackage("example.com/model", "model")

	return types.NewNamed(
		types.NewTypeName(token.NoPos, pkg, name, nil),
		types.NewStruct(nil, nil),
		nil,
	)
}
