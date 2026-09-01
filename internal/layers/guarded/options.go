package guarded

import (
	"github.com/okian/forge/internal/layer"
	"github.com/okian/forge/internal/model"
	"github.com/okian/forge/internal/view"
)

// The method a JSON codec is entered through, which the elements have and this
// layer writes for the container holding them.
const marshalMethod = "MarshalJSONTo"

// viewName returns what a scope over this declaration hands over.
func viewName(ctx *layer.Context) string {
	if ctx == nil {
		return ""
	}
	return view.Named(ctx.Declared())
}

// elem returns how the element is written in the file being generated into.
//
// The subject, spelled for the package the declaration lives in. A snapshot is
// a slice of them and the view's signatures name them, so it is the one type
// this layer has to write down that is not one it invented.
func elem(ctx *layer.Context) string {
	if ctx == nil || ctx.Model == nil || ctx.Model.Subject == nil {
		return ""
	}
	return model.Spell(ctx.Model.Subject.Type(), local(ctx), nil).Text
}

// local returns the import path of the package being generated into.
func local(ctx *layer.Context) string {
	if ctx == nil || ctx.Model == nil || ctx.Model.Pkg == nil {
		return ""
	}
	return ctx.Model.Pkg.PkgPath
}

// The options a declaration writes, and the values that change what is emitted.
//
// Only the values that do. `encode=snapshot` is the default and is named in the
// schema rather than here, because a default is what happens when nothing is
// written and there is nothing here to compare it against.
const (
	optionEncode = "encode"
	optionExpose = "expose"

	encodeLocked = "locked"
	exposeLocker = "locker"
)

// locker reports whether the declaration asked for the lock itself.
func locker(ctx *layer.Context) bool { return named(ctx, optionExpose) == exposeLocker }

// lockedWrite reports whether the declaration asked for encoding to hold the
// lock rather than copy first.
func lockedWrite(ctx *layer.Context) bool { return named(ctx, optionEncode) == encodeLocked }

// holds returns how the container beneath the lock is made, and nothing where
// its zero value is already one.
func holds(ctx *layer.Context) *layer.Constructor {
	made, needs := ctx.Holds()
	if !needs {
		return nil
	}
	return &made
}

// named returns what an option was set to, or nothing.
func named(ctx *layer.Context, key string) string {
	if ctx == nil {
		return ""
	}

	held, written := ctx.Options.Lookup(key)
	if !written {
		return ""
	}
	return held.Value
}
