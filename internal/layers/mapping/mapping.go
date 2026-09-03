package mapping

import (
	"errors"

	"github.com/okian/forge/plugin"
)

// container is the marker this layer claims.
const container = "Map"

// Layer generates the bridge's constructor.
//
// It carries no state: what a declaration decides reaches it through the
// context it is asked to generate against, so one value serves every
// declaration in a run.
type Layer struct{}

// New returns the map layer.
func New() Layer { return Layer{} }

// Origin identifies the marker this layer claims.
func (Layer) Origin() plugin.TypeRef { return plugin.TypeRef{Pkg: plugin.MarkerPkg, Name: container} }

// Binds names what this layer's output imports: nothing. A constructor is
// assignments, and every type it spells arrives through the subject's own
// spelling.
func (Layer) Binds() []plugin.Import { return nil }

// Writes names the methods this layer puts on the subject: none. The
// constructor is a package function, so the target's method set is untouched
// and the target need not even be local.
func (Layer) Writes() []string { return nil }

// Kind says where in a stack the layer may appear: nowhere but alone. A
// bridge reads one type and writes about another, and composes with nothing.
func (Layer) Kind() plugin.Kind { return plugin.KindBridge }

// Stage says how far along the layer is.
func (Layer) Stage() plugin.Stage { return plugin.StageReady }

// Doc returns the one-line summary the list command prints.
func (Layer) Doc() string {
	return "a constructor from a source type's values, matched by name and settled by hints"
}

// OptionSchema declares every option the layer accepts.
func (Layer) OptionSchema() []plugin.OptionDef {
	return []plugin.OptionDef{{
		Key: "ignore", Value: plugin.ValueFields,
		Doc: "target fields left unset on purpose",
	}}
}

// Accepts reports whether the layer can sit on the shape beneath it. There is
// no shape beneath a bridge — the composition rule keeps it alone — so there
// is nothing to refuse here. What decides is the pair of types, and they are
// refused where the answer is: when the match ladder runs.
func (Layer) Accepts(plugin.Shape) error { return nil }

// Shape returns what the layer exposes upward, which is what it was given:
// nothing composes above a bridge, so nobody asks.
func (Layer) Shape(_ *plugin.Context, below plugin.Shape) plugin.Shape { return below }

// Generate returns the declared type and the constructor for the declaration.
func (Layer) Generate(ctx *plugin.Context, _ plugin.Shape) (plugin.Unit, error) {
	if ctx == nil || ctx.Model == nil || ctx.Model.Subject == nil || ctx.Model.Source == nil {
		// Not a diagnostic: a diagnostic points at a declaration, and the
		// declaration is what is missing. Reaching here is forge calling
		// itself wrongly rather than anybody writing anything.
		return plugin.Unit{}, errors.New("mapping: asked to generate without a bridged declaration")
	}

	built, err := planned(ctx)
	if err != nil {
		return plugin.Unit{}, err
	}

	return written(ctx, built)
}
