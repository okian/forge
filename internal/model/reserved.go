package model

import (
	"slices"
	"strings"
)

// Directives forge answers itself, rather than handing to a layer.
//
// They live in the same flat namespace layer names live in — everything after
// //forge: is one word, and what that word means is decided by looking it up —
// so a name forge takes is a name no layer may have. Reserving them is what
// keeps a layer called Skip from silently taking one.
const (
	// SkipDirective names an interface the declaration is not to claim:
	//
	//	//forge:skip io.WriterTo
	//
	// It is forge's rather than a layer's because what it turns off is not a
	// layer's doing. Synthesis reads what a stack turned out to offer and
	// asserts what that adds up to, and an author who does not want one of
	// those claims is arguing with the synthesis rather than with whichever
	// layer happened to supply the method.
	SkipDirective = "skip"
)

// reserved is every directive name forge answers itself.
var reserved = []string{SkipDirective}

// Reserved reports whether a directive name is one forge answers itself rather
// than passing to a layer.
//
// Asked of the name as it is written after the prefix, which is the same form
// [LayerRef.Directive] produces — so a layer and a reserved word are compared
// as the same kind of thing rather than as a marker and a string.
func Reserved(directive string) bool {
	return slices.Contains(reserved, strings.ToLower(directive))
}

// ReservedDirectives returns every name forge answers itself, in the order they
// are declared, so that a diagnostic can list them.
func ReservedDirectives() []string { return append([]string(nil), reserved...) }
