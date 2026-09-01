package layers

import (
	"strings"
	"testing"

	"github.com/okian/forge/internal/diag"
	"github.com/okian/forge/internal/layer"
	"github.com/okian/forge/internal/model"
	"github.com/okian/forge/internal/shape"
)

// The two reasons a marker generates nothing are told apart, and told apart in
// words an author can act on.
//
// A layer whose generator is not written yet is coming; a marker forge has not
// committed to may never come. What somebody does about the first is wait, and
// about the second is write the code themselves — so a hint that gave one
// answer for both would send half its readers the wrong way.
//
// Built here rather than looked up, because there is no stub in the catalog to
// look up: every marker this release promised is written. The vocabulary still
// has the stage in it, for the next one, and this is what keeps the half of it
// that nothing ships from rotting.
func TestTheTwoReasonsAMarkerGeneratesNothing(t *testing.T) {
	cases := map[string]struct {
		stage layer.Stage
		says  string
	}{
		"a generator not written yet": {layer.StageStub, "not written yet"},
		"a layer not in this release": {layer.StageStaged, "not in this release"},
	}

	var seen []string
	for name, want := range cases {
		t.Run(name, func(t *testing.T) {
			held := &stub{origin: marker("Example"), kind: model.KindElement, stage: want.stage}

			_, err := held.Generate(nil, shape.Shape{})
			if err == nil {
				t.Fatal("a marker with no generator generated without complaint")
			}

			reported, is := diag.From(err)
			if !is {
				t.Fatalf("the refusal is not a diagnostic: %v", err)
			}
			if !strings.Contains(reported.Hint, want.says) {
				t.Errorf("the hint %q does not say %q", reported.Hint, want.says)
			}
			seen = append(seen, reported.Hint)
		})
	}

	// And they are two hints rather than one written twice, which is the whole
	// of what the distinction is for.
	if len(seen) == 2 && seen[0] == seen[1] {
		t.Errorf("both stages give the same hint: %q", seen[0])
	}
}
