package layers

import (
	"github.com/okian/forge/internal/layer"
	"github.com/okian/forge/internal/layers/builder"
	"github.com/okian/forge/internal/layers/clone"
	"github.com/okian/forge/internal/layers/collection"
	"github.com/okian/forge/internal/layers/contenthash"
	"github.com/okian/forge/internal/layers/enum"
	"github.com/okian/forge/internal/layers/guarded"
	"github.com/okian/forge/internal/layers/jsoncodec"
	"github.com/okian/forge/internal/layers/mapping"
	"github.com/okian/forge/internal/layers/patch"
	"github.com/okian/forge/internal/layers/redact"
	"github.com/okian/forge/internal/layers/ring"
	"github.com/okian/forge/internal/layers/slice"
	"github.com/okian/forge/internal/layers/validate"
	"github.com/okian/forge/internal/model"
)

// written is every layer forge ships that generates, in the order the catalog
// reads.
//
// A marker named here has no row in the catalog beside it, and the registry is
// what enforces that: registering two layers for one marker panics, so a layer
// that lands without its stub being removed fails at the first test rather than
// leaving two answers about one marker and a stale row nothing reads. The
// division is that the catalog describes a layer nobody has written and a
// package here is one somebody has.
func written() []layer.Layer {
	return []layer.Layer{
		builder.New(),
		clone.New(),
		collection.New(),
		contenthash.New(),
		enum.New(),
		guarded.New(),
		jsoncodec.New(),
		mapping.New(),
		patch.New(),
		redact.New(),
		ring.New(),
		slice.New(),
		validate.New(),
	}
}

// Builtins returns a registry holding every layer forge ships.
//
// A fresh registry each time rather than one shared value: a caller that adds a
// layer of its own is doing that for one run, and a registry that outlived the
// run would carry it into the next.
func Builtins() *layer.Registry {
	r := layer.New()

	for _, l := range written() {
		r.MustRegister(l)
	}
	for _, l := range declared {
		r.MustRegister(l)
	}

	return r
}

// DefaultStorage is the storage a refining layer gets when a declaration is
// written with none beneath it, so that Collection[Person] resolves as though
// Collection[Slice[Person]] had been written.
//
// It is exported because the stage that inserts it has to name it, and a marker
// name written out again there would be a second copy of a fact this package
// owns. A function rather than a variable, so that nothing importing this
// package can quietly make it something else.
func DefaultStorage() model.TypeRef { return marker("Slice") }
