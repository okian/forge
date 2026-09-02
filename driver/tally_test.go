package driver_test

import (
	"errors"
	"fmt"
	"go/parser"
	"go/token"

	"github.com/okian/forge/plugin"
)

// The code this layer reports under, in the range a layer forge does not ship
// takes.
//
// Registered at package scope, which is what the documentation says to do and
// what used to panic: forge's own codes stop at 5999 and everything above was
// refused, so the first line a layer author wrote by the book ended the
// process.
var codeNoField = plugin.Register(6001, "tally was given no field to count by")

// tallyPkg is where the marker this layer claims is declared, which is the
// third party's own package and not forge's.
const tallyPkg = "example.com/mine/tally"

// tally is a layer written the way a third party writes one: against the
// published surface, with a marker of its own, and with nothing forge does not
// publish.
//
// It counts the elements of a container by one of the subject's fields, which
// is chosen because it is the smallest thing that needs every part of the
// contract — an option naming a field, a capability it requires of what is
// beneath it, a type spelled for the package it lands in, and a method on the
// declared type.
type tally struct{}

// Origin names the marker this layer claims.
func (tally) Origin() plugin.TypeRef {
	return plugin.TypeRef{Pkg: tallyPkg, Name: "Tally"}
}

// Kind says where in a stack it may appear: it adds to a representation rather
// than being one.
func (tally) Kind() plugin.Kind { return plugin.KindRefining }

// Stage says how far along it is, which for a layer outside forge is the one
// answer there is.
func (tally) Stage() plugin.Stage { return plugin.StageReady }

// Doc is the one line a report puts beside the step.
func (tally) Doc() string { return "a count of the elements sharing each value of one field" }

// OptionSchema declares the field to count by.
func (tally) OptionSchema() []plugin.OptionDef {
	return []plugin.OptionDef{{
		Key:   "by",
		Value: plugin.ValueField,
		Scope: plugin.ScopeDeclaration,
		Doc:   "the field to count by",
	}}
}

// Accepts refuses a stack whose elements it cannot walk.
func (tally) Accepts(below plugin.Shape) error {
	if !below.Caps.Has(plugin.Streamable) {
		return errors.New("tally has to be able to walk the elements, and cannot here")
	}
	return nil
}

// Shape adds nothing and withdraws nothing: a count is another way to read what
// is already there.
func (tally) Shape(_ *plugin.Context, below plugin.Shape) plugin.Shape { return below }

// Binds names the packages its output imports, which is none of its own — the
// spelling of the key's type says what that needs.
func (tally) Binds() []plugin.Import { return nil }

// Writes names what it puts on the subject, which is nothing: a count is about
// the container.
func (tally) Writes() []string { return nil }

// Generate writes the count.
func (tally) Generate(ctx *plugin.Context, _ plugin.Shape) (plugin.Unit, error) {
	by, named := ctx.Options.Get("by")
	if !named {
		return plugin.Unit{}, plugin.New(codeNoField, ctx.Model.Pos,
			"%s names no field to count by", ctx.Model.Name).
			WithHint("write by=<field> on the directive")
	}

	var held plugin.Field
	for _, one := range ctx.Model.Subject.Fields {
		if one.Name == by {
			held = one
		}
	}

	key := plugin.Spell(held.Type.Type, ctx.Model.Pkg.PkgPath, ctx.Bound())

	src := fmt.Sprintf(`package p

// TalliedBy%[1]s counts the elements sharing each %[1]s.
func (c %[2]s) TalliedBy%[1]s() map[%[3]s]int {
	out := make(map[%[3]s]int)
	for _, one := range c {
		out[one.%[1]s]++
	}
	return out
}
`, by, ctx.Declared(), key.Text)

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "tally.go", src,
		parser.ParseComments|parser.SkipObjectResolution)
	if err != nil {
		return plugin.Unit{}, err
	}

	return plugin.Unit{
		Decls:    file.Decls,
		Comments: file.Comments,
		Fset:     fset,
		Imports:  plugin.Reaching(file.Decls, key.Imports),
	}, nil
}
