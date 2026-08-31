package options_test

import (
	"strings"
	"testing"

	"github.com/okian/forge/internal/discover"
	"github.com/okian/forge/internal/layer"
	"github.com/okian/forge/internal/model"
	"github.com/okian/forge/internal/options"
	"github.com/okian/forge/internal/shape"
)

// demanding is a layer with the option shapes the catalog has no example of
// yet: one it cannot generate without, one written on its own, and one taking
// free text.
//
// Written here rather than waited for. What a schema means is this package's
// whole subject, and leaving a shape of it untried until some layer happens to
// declare one would carry whatever is wrong with it into that layer's work.
type demanding struct {
	name    string
	options []layer.OptionDef
}

func (d demanding) Origin() model.TypeRef {
	return model.TypeRef{Pkg: model.MarkerPkg, Name: d.name}
}

func (d demanding) Kind() model.Kind                { return model.KindStorage }
func (d demanding) OptionSchema() []layer.OptionDef { return d.options }
func (d demanding) Accepts(shape.Shape) error       { return nil }
func (d demanding) Shape(below shape.Shape) shape.Shape {
	return below
}

func (d demanding) Generate(*layer.Context, shape.Shape) (layer.Unit, error) {
	return layer.Unit{}, nil
}

// catalog returns a registry holding exactly this layer.
func catalog(t *testing.T, l layer.Layer) *layer.Registry {
	t.Helper()

	registry := layer.New()
	if err := registry.Register(l); err != nil {
		t.Fatalf("registering %s: %v", l.Origin(), err)
	}
	return registry
}

// against validates directives over a stack naming one layer, using a catalog
// holding only it.
func against(t *testing.T, l layer.Layer, texts ...string) string {
	t.Helper()

	directives := make([]discover.Directive, len(texts))
	for i, text := range texts {
		directives[i] = written(text)
	}

	_, diags := options.Read(options.Declaration{
		Directives: directives,
		Stack:      naming(l.Origin().Name),
		Subject:    person,
	}, catalog(t, l))

	return diags.Render()
}

// required is a layer that cannot generate without being told something.
var required = demanding{
	name: "Ring",
	options: []layer.OptionDef{
		{Key: "cap", Value: layer.ValueInt, Required: true, Doc: "how many elements the buffer holds"},
		{Key: "strict", Value: layer.ValueNone, Doc: "refuse rather than overwrite"},
		{Key: "name", Value: layer.ValueString, Doc: "what to call the generated type"},
	},
}

// An option a layer cannot generate without is not one an author can leave out,
// and the run that finds out at generation time is a run that has already
// spent a load.
func TestAnOptionTheLayerCannotGenerateWithout(t *testing.T) {
	reported := against(t, required, "//forge:ring strict")

	if !strings.Contains(reported, "FRG3011") {
		t.Errorf("a missing required option was accepted:\n%s", reported)
	}
	if !strings.Contains(reported, "cap") {
		t.Errorf("the failure does not name the option:\n%s", reported)
	}
	if !strings.Contains(reported, "//forge:ring cap=") {
		t.Errorf("the failure does not say what to write:\n%s", reported)
	}
}

// And one written is not missing.
func TestARequiredOptionThatWasWritten(t *testing.T) {
	if reported := against(t, required, "//forge:ring cap=1024"); reported != "" {
		t.Errorf("a required option that was written was reported:\n%s", reported)
	}
}

// A declaration with no directive at all is still missing what the layer cannot
// generate without, and the report points at the declaration rather than at a
// directive nobody wrote.
func TestARequiredOptionWithNoDirectiveAtAll(t *testing.T) {
	_, diags := options.Read(options.Declaration{
		Stack:   naming("Ring"),
		Subject: person,
	}, catalog(t, required))

	if !strings.Contains(diags.Render(), "FRG3011") {
		t.Errorf("a layer that cannot generate was passed over:\n%s", diags.Render())
	}
}

// An option written on its own takes no value, and one given a value was
// written by somebody expecting it to mean something.
func TestAnOptionWrittenOnItsOwn(t *testing.T) {
	if reported := against(t, required, "//forge:ring cap=8 strict"); reported != "" {
		t.Errorf("an option written on its own was reported:\n%s", reported)
	}

	reported := against(t, required, "//forge:ring cap=8 strict=yes")
	if !strings.Contains(reported, "takes no value") {
		t.Errorf("a value given to an option that takes none was accepted:\n%s", reported)
	}
}

// An option taking free text takes some, and one written bare configures
// nothing while looking as though it configures something.
func TestAnOptionTakingFreeText(t *testing.T) {
	if reported := against(t, required, "//forge:ring cap=8 name=Buffer"); reported != "" {
		t.Errorf("free text was reported:\n%s", reported)
	}

	reported := against(t, required, "//forge:ring cap=8 name")
	if !strings.Contains(reported, "written on its own") {
		t.Errorf("an option needing a value was accepted without one:\n%s", reported)
	}
}

// A layer this release does not ship cannot be missing an option either: the
// schema that would say so is provisional.
func TestARequiredOptionOfALayerThisReleaseDoesNotShip(t *testing.T) {
	_, diags := options.Read(options.Declaration{
		Stack:   naming("Sorted"),
		Subject: person,
	}, layer.Builtins())

	if !diags.Empty() {
		t.Errorf("a staged layer was held to a provisional schema:\n%s", diags.Render())
	}
}

// An option naming a list of fields with a gap in it is a list somebody edited
// and left behind, and silently reading four names as three is how it stays
// that way.
func TestAListOfFieldsWithAGapInIt(t *testing.T) {
	_, reported := on(naming("Collection"), "//forge:collection sort=Age,,LastName")

	if !strings.Contains(reported, "empty field") {
		t.Errorf("a gap in a list of fields was passed over:\n%s", reported)
	}
}

// A directive whose arguments are nothing but space configures nothing, which
// is not a mistake — an author part way through writing one has a file that
// still builds.
func TestADirectiveWithNothingButSpace(t *testing.T) {
	if reported := against(t, required, "//forge:ring cap=8    "); reported != "" {
		t.Errorf("space after the options was reported:\n%s", reported)
	}
}

// A declaration naming no layers says so, rather than offering an empty list of
// the layers it has.
func TestADirectiveOnADeclarationWithNoLayers(t *testing.T) {
	_, diags := options.Read(options.Declaration{
		Directives: []discover.Directive{written("//forge:collection sort=Age")},
		Subject:    person,
	}, layer.Builtins())

	if !strings.Contains(diags.Render(), "no layers at all") {
		t.Errorf("the failure does not say the declaration names none:\n%s", diags.Render())
	}
}

// A layer with one option offers it without the punctuation a list would need.
func TestALayerWithOneOption(t *testing.T) {
	one := demanding{name: "Slice", options: []layer.OptionDef{
		{Key: "cap", Value: layer.ValueInt, Doc: "how many elements to allocate for"},
	}}

	reported := against(t, one, "//forge:slice nonesuch=1")

	if strings.Contains(reported, " and ") || strings.Contains(reported, ", ") {
		t.Errorf("one option was offered as a list:\n%s", reported)
	}
	if !strings.Contains(reported, "cap") {
		t.Errorf("the failure does not offer the option:\n%s", reported)
	}
}

// A subject with no fields at all has none to offer, and an empty list would
// read as a subject whose fields nobody looked up.
func TestAFieldOnASubjectWithNoFields(t *testing.T) {
	_, diags := options.Read(options.Declaration{
		Directives: []discover.Directive{written("//forge:collection sort=Age")},
		Stack:      naming("Collection"),
		Subject:    &model.Struct{},
	}, layer.Builtins())

	if !strings.Contains(diags.Render(), "has nothing") {
		t.Errorf("the failure does not say the subject has no fields:\n%s", diags.Render())
	}
}
