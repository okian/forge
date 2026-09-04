package jsoncodec

import (
	"errors"
	"fmt"
	"go/types"
	"strings"

	"github.com/okian/forge/plugin"
)

// Fused returns the two package functions that write the subject's document
// from a source, reading each top-level member from the expression a mapping
// settled it to instead of from a held value.
//
// reads is called once with the source parameter's allocated name and returns
// one expression per top-level field of the subject, keyed by Go field name —
// every field, ignored ones included, spelled as the zero they hold. The
// nested codecs and the wire runtime are not emitted here: the Json layer of
// the same declaration provides them into the same package, which the
// composition rule guarantees is beneath the bridge that asks for this.
func Fused(ctx *plugin.Context, reads func(src string) map[string]string) (plugin.Unit, error) {
	if ctx == nil || ctx.Model == nil || ctx.Model.Subject == nil || ctx.Model.Source == nil || reads == nil {
		// Not a diagnostic: a diagnostic points at a declaration, and the
		// declaration is what is missing. Reaching here is forge calling
		// itself wrongly rather than anybody writing anything.
		return plugin.Unit{}, errors.New("json: asked to fuse without a bridged declaration")
	}

	source, ok := types.Unalias(ctx.Model.Source).(*types.Named)
	if !ok {
		return plugin.Unit{}, errors.New("json: asked to fuse from a source that is not a named type")
	}

	held := ctx.Model.Subject
	if !held.Reachable() {
		return plugin.Unit{}, fmt.Errorf("json: %s cannot be named from the package being generated into", ctx.Model.Name)
	}

	// The subject is planned exactly as its own codec is, so the fused writer
	// refuses whatever the codec refuses, in the codec's own words.
	built := &planner{
		into:      ctx.Model.Pkg.PkgPath,
		bound:     ctx.Bound(),
		willWrite: ctx.Writes,
		authored:  ctx.Authored,
		style:     style(ctx.Options),
		omitZero:  flag(ctx.Options, optionOmitZero),
	}
	root := built.plan(held)
	if err := built.diags.Err(); err != nil {
		return plugin.Unit{}, err
	}

	srcSpelled := plugin.Spell(ctx.Model.Source, ctx.Model.Pkg.PkgPath, ctx.Bound())
	w := newWriter(naming(append(spelled(root), srcSpelled.Text)...))
	src := w.n("src")

	table := reads(src)
	if err := covered(root, table); err != nil {
		return plugin.Unit{}, err
	}
	w.reads = func(path string) string {
		top, rest, cut := strings.Cut(path, ".")
		expr := "(" + table[top] + ")"
		if !cut {
			return expr
		}
		return expr + "." + rest
	}

	param := srcSpelled.Text
	if _, isInterface := ctx.Model.Source.Underlying().(*types.Interface); !isInterface {
		param = "*" + param
	}

	target := plugin.Upper(held.Named.Obj().Name())
	from := plugin.Upper(source.Obj().Name())
	w.fusedAppend("Append"+target+"JSONFrom"+from, root, src, param, from)
	w.fusedWrite("Write"+target+"JSONFrom"+from, "Append"+target+"JSONFrom"+from, src, param)

	return fusedUnit(w, ctx.Model.Name, srcSpelled)
}

// fusedUnit reads the assembled functions back as the unit a layer returns.
func fusedUnit(w *writer, about string, srcSpelled plugin.Spelling) (plugin.Unit, error) {
	if len(w.refused) > 0 {
		return plugin.Unit{}, cannotOmit(w.refused[0])
	}

	decls, comments, fset, err := parsed(w.String(), about)
	if err != nil {
		return plugin.Unit{}, err
	}

	imports := append([]plugin.Import{{Path: "io", Name: "io"}}, srcSpelled.Imports...)
	return plugin.Unit{
		Decls: decls, Comments: comments, Fset: fset,
		Imports: plugin.Reaching(decls, imports),
	}, nil
}

// covered refuses a reads table that misses a member, by name: emitting the
// body anyway would put a file on disk that does not compile, blamed on
// nobody.
func covered(root *form, table map[string]string) error {
	for _, one := range root.members {
		top, _, _ := strings.Cut(one.path, ".")
		if _, ok := table[top]; !ok {
			return fmt.Errorf("json: the mapping handed no read for %s", top)
		}
	}
	return nil
}

// fusedAppend writes the append half: the subject's own appender body, under
// the mapping's reads and a source parameter instead of a receiver.
func (w *writer) fusedAppend(name string, root *form, src, param, from string) {
	dst := w.n("dst")

	w.line("// %s appends %s's JSON document read straight from a %s,", name, root.spelled.Text, from)
	w.line("// with no %s built.", root.spelled.Text)
	w.line("//")
	w.line("// Byte-identical to building the value and appending it: members, order,")
	w.line("// names and escaping are the codec's own, driven by the target's tags.")
	w.line("func %s(%s []byte, %s %s) ([]byte, error) {", name, dst, src, param)
	w.appendBody(root)
	w.line("}")
	w.blank()
}

// fusedWrite writes the io.Writer half: the append half into a borrowed
// buffer, handed to the writer in one call.
func (w *writer) fusedWrite(name, appendName, src, param string) {
	sink, scratch := w.n("w"), w.n("scratch")
	held, errName, count := w.n("held"), w.n("err"), w.n("n")

	w.line("// %s writes what %s appends to %s, in one call.", name, appendName, sink)
	w.line("func %s(%s io.Writer, %s %s) (int64, error) {", name, sink, src, param)
	w.line("%s := jsonTakeScratch()", scratch)
	w.line("%s, %s := %s((*%s)[:0], %s)", held, errName, appendName, scratch, src)
	w.line("*%s = %s", scratch, held)
	w.line("if %s != nil {", errName)
	w.line("jsonDropScratch(%s)", scratch)
	w.line("return 0, %s", errName)
	w.line("}")
	w.line("%s, %s := %s.Write(%s)", count, errName, sink, held)
	w.line("jsonDropScratch(%s)", scratch)
	w.line("return int64(%s), %s", count, errName)
	w.line("}")
	w.blank()
}
