package resolve

import (
	"github.com/okian/forge/internal/diag"
	"github.com/okian/forge/internal/discover"
)

// DeclarationsAgainst resolves candidates against an arbitrary marker package,
// for the tests that need a marker no forge release ships. It is compiled only
// into the test binary, so the package's API stays the one function it is.
func DeclarationsAgainst(markers string, candidates []discover.Candidate) ([]Declaration, diag.Set) {
	return declarations(candidates, markers)
}
