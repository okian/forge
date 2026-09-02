// Package failures carries the types generated code reports a failure through
// into the package it is generated into.
//
// One vocabulary rather than one per layer. A check reports that a rule was not
// met and a builder reports that a field was never given, and both are the same
// thing to whoever catches them: a list of things wrong with a value, each
// naming the field it is about. A caller who handles one handles the other, and
// a package with both layers holds one set of types rather than two that mean
// the same and compare unequal.
//
// Here rather than in either layer, because a layer that embedded it would be
// the layer the other one had to import — and a layer importing a layer is an
// order of precedence between two things that have none.
package failures

import (
	_ "embed"
	"fmt"
	"slices"

	"github.com/okian/forge/internal/layers/embedded"
	"github.com/okian/forge/plugin"
)

// The keys the two contributions are made under.
//
// One key each for the package rather than one per subject, because there is
// one copy of each however many declarations asked: a package holding two
// ValidationError types does not compile, and the key is what says two
// contributions are the same thing.
const (
	// Key is what the failure types are contributed under.
	Key = "failures: what generated code reports"

	// NestedKey is what the folding a check needs is contributed under. It is
	// separate because a package with a builder and no check would otherwise be
	// written a function nothing in it calls.
	NestedKey = "failures: folding a nested failure into its holder"
)

// The two files, embedded from the package beside this one.
//
// Embedded rather than quoted, so that what is emitted is Go this repository's
// own build compiles, its own vet reads and its own tests exercise. Code that
// is only ever a string is code nothing checks until somebody's generated file
// fails to build.
var (
	//go:embed shared/shared.go
	reported []byte

	//go:embed shared/nested.go
	folding []byte
)

// binds names what the two files import, and what each binds.
//
// Written down rather than read off the files, so that an import added to one
// of them is a change somebody makes here as well — and so that what a run
// narrows against is a list rather than a parse of the same bytes twice.
var binds = []plugin.Import{
	{Path: "errors", Name: "errors"},
	{Path: "strconv", Name: "strconv"},
	{Path: "strings", Name: "strings"},
}

// Binds names what these files import, for a layer answering what its own
// output binds.
//
// A copy, because the caller folds it into a union and a shared slice appended
// to is a shared slice changed for everybody who holds it.
func Binds() []plugin.Import { return slices.Clone(binds) }

// Unit returns the failure types as a contribution the package holds once.
func Unit() (plugin.Unit, error) {
	return carried("shared.go", reported)
}

// Nested returns the folding a check needs, which is contributed beside the
// types rather than with them.
func Nested() (plugin.Unit, error) {
	return carried("nested.go", folding)
}

// carried turns one of the files into a contribution.
func carried(name string, source []byte) (plugin.Unit, error) {
	unit, err := embedded.Unit(name, source, binds)
	if err != nil {
		return plugin.Unit{}, fmt.Errorf("failures: %w", err)
	}
	return unit, nil
}
