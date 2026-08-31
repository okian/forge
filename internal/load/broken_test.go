package load_test

import (
	"testing"

	"golang.org/x/tools/go/packages"

	"github.com/okian/forge/internal/load"
)

// Whether a package built is not the same question as whether it carries
// errors, and the difference is forge's own doing: a package is loaded with its
// function bodies stripped, so every import used only inside one reports as
// unused. Every generated file has one, so a rule that read the errors raw
// would find every package holding generated code broken — which is the
// opposite of the answer, and which is silent, because the packages it is wrong
// about are exactly the ones forge writes into.
func TestWhichPackagesActuallyFailedToBuild(t *testing.T) {
	unused := func(path string) packages.Error {
		return packages.Error{Msg: `"` + path + `" imported and not used`, Kind: packages.TypeError}
	}

	cases := map[string]struct {
		errors []packages.Error
		broken bool
	}{
		"nothing wrong": {},

		// The one forge causes, in the two phrasings the type-checker has for
		// it, and in the numbers a generated file produces.
		"an import stripping made unused": {
			errors: []packages.Error{unused("slices")},
		},
		"one per generated file": {
			errors: []packages.Error{unused("slices"), unused("iter"), unused("cmp")},
		},
		"an import stripping made unused, named": {
			errors: []packages.Error{{
				Msg:  `"encoding/json/v2" imported as json and not used`,
				Kind: packages.TypeError,
			}},
		},

		// And the ones a build would have raised.
		"a type that is not there": {
			errors: []packages.Error{{Msg: "undefined: Widgets", Kind: packages.TypeError}},
			broken: true,
		},
		"a file that does not parse": {
			errors: []packages.Error{{Msg: "expected ';', found 'type'", Kind: packages.ParseError}},
			broken: true,
		},
		"a package that is not there": {
			errors: []packages.Error{{Msg: "no required module provides package", Kind: packages.ListError}},
			broken: true,
		},

		// A real error beside the ones forge causes is still a real error, and
		// it is the arrangement every broken package holding generated code is
		// actually in.
		"a real one among forge's own": {
			errors: []packages.Error{unused("slices"), {Msg: "undefined: Widgets", Kind: packages.TypeError}},
			broken: true,
		},

		// The same words from a stage that does not strip bodies is not
		// something forge caused. The kind is what tells them apart.
		"the same words from elsewhere": {
			errors: []packages.Error{{Msg: `"slices" imported and not used`, Kind: packages.ListError}},
			broken: true,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := load.Broken(&packages.Package{Errors: tc.errors}); got != tc.broken {
				t.Errorf("Broken() = %v, want %v", got, tc.broken)
			}
		})
	}

	// A package that is not there did not fail to build; it is not there.
	if load.Broken(nil) {
		t.Error("a package that is nothing was reported as one that does not build")
	}
}
