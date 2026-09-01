package validate

import (
	_ "embed"
	"fmt"

	"github.com/okian/forge/internal/layer"
	"github.com/okian/forge/internal/layers/embedded"
	"github.com/okian/forge/internal/model"
)

// sharedKey is what the error types are contributed under.
//
// One key for the package rather than one per subject, because there is one
// copy of them however many declarations asked: a package holding two
// ValidationError types does not compile, and the key is what says the two
// contributions are the same thing.
const sharedKey = "validate: what a check reports"

// failures is the source of the types every check reports through, embedded
// from the package beside this one.
//
// Embedded rather than quoted, so that what is emitted is Go this repository's
// own build compiles, its own vet reads and its own tests exercise. Code that
// is only ever a string is code nothing checks until somebody's generated file
// fails to build.
//
// It is not a template: nothing in it depends on the subject, so there is
// nothing to rewrite and no reason for it to be anything but a file.
//
//go:embed shared/shared.go
var failures []byte

// sharedImports names what the shared file imports, and what each binds.
//
// Written down rather than read off the file, so that an import added to it
// is a change somebody makes here as well — and so that what a run narrows
// against is a list rather than a parse of the same bytes twice.
var sharedImports = []model.Import{
	{Path: "errors", Name: "errors"},
	{Path: "strconv", Name: "strconv"},
	{Path: "strings", Name: "strings"},
}

// shared returns the error types as a contribution the package holds once.
func shared() (layer.Unit, error) {
	unit, err := embedded.Unit("shared.go", failures, sharedImports)
	if err != nil {
		return layer.Unit{}, fmt.Errorf("validate: %w", err)
	}
	return unit, nil
}
