package mapping

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"strings"

	"github.com/okian/forge/plugin"
)

// writer assembles the source of a constructor.
//
// Text rather than syntax, for the reason the codec's writer gives: what is
// assembled is a function of assignments, and a tree for one is many times its
// own size. What it costs is the possibility of writing something that is not
// Go, and that cost is paid where the layer can still be stopped — the source
// is parsed before it leaves the package.
type writer struct{ out strings.Builder }

// line writes one line of the body. Indentation is left to gofmt, which the
// emitter runs over everything anyway.
func (w *writer) line(format string, args ...any) {
	if len(args) == 0 {
		w.out.WriteString(format)
	} else {
		fmt.Fprintf(&w.out, format, args...)
	}
	w.out.WriteByte('\n')
}

// blank separates two declarations.
func (w *writer) blank() { w.out.WriteByte('\n') }

// wrapped writes a sentence over however many comment lines it takes.
func (w *writer) wrapped(text string) {
	for _, line := range plugin.Wrapped(text, plugin.CommentWidth) {
		w.line("// %s", line)
	}
}

// String returns the assembled source.
func (w *writer) String() string { return w.out.String() }

// written assembles the declared type and the constructor.
func written(ctx *plugin.Context, built *plan) (plugin.Unit, error) {
	source, ok := types.Unalias(built.source).(*types.Named)
	if !ok {
		return plugin.Unit{}, plugin.New(codeUnnamedEnd, ctx.Model.Pos,
			"%s is not a named type, and the constructor is named from both of the bridge's ends",
			plugin.TypeString(built.source)).
			WithHint("declare a named type for the source and bridge from that")
	}

	local := ctx.Model.Pkg.PkgPath
	sourceSpelled := plugin.Spell(built.source, local, ctx.Bound())
	targetSpelled := ctx.Model.SubjectSpelling(sourceSpelled.Bound(ctx.Bound()))

	names := naming(seeds(built, sourceSpelled.Text, targetSpelled.Text, ctx.Model.Name)...)
	src, held := names.name("src"), names.name("held")
	name := plugin.Upper(built.target.Named.Obj().Name()) + "From" + plugin.Upper(source.Obj().Name())

	w := &writer{}
	declared(w, ctx)
	header(w, built, name, targetSpelled.Text, sourceSpelled.Text)
	body(w, built, name, src, held, sourceSpelled.Text, targetSpelled.Text)

	decls, comments, fset, err := parsed(w.String(), ctx.Model.Name)
	if err != nil {
		return plugin.Unit{}, err
	}

	return plugin.Unit{
		Decls: decls, Comments: comments, Fset: fset,
		Imports: plugin.Reaching(decls, sourceSpelled.Bound(targetSpelled.Imports)),
	}, nil
}

// seeds gathers every name the constructor's body spells, which is what the
// locals are allocated out of the way of: both type spellings, the declared
// name, and whatever the hint's expressions mention — less the hint's own
// parameters, which are respelled to the locals rather than dodged.
func seeds(built *plan, spellings ...string) []string {
	out := spellings

	for _, member := range built.members {
		if member.via != settledHint {
			continue
		}
		for _, mentioned := range plugin.Mentioned(types.ExprString(member.hint)) {
			if mentioned != built.srcParam && mentioned != built.dstParam {
				out = append(out, mentioned)
			}
		}
	}

	return out
}

// declared writes the declaration the spec file's placeholder becomes: an
// empty struct, because the mapping's product is the constructor beside it.
func declared(w *writer, ctx *plugin.Context) {
	if ctx.Model.Form != plugin.FormSpec {
		return
	}

	w.line("// %s is the mapping's declaration; the constructor beside it is what it", ctx.Model.Name)
	w.line("// produces. It holds nothing.")
	w.line("type %s struct{}", ctx.Model.Name)
	w.blank()
}

// header writes the constructor's doc comment, ledger included: how every
// member was settled, in words a reader checks without rerunning the ladder.
func header(w *writer, built *plan, name, target, source string) {
	w.line("// %s builds one %s from what one %s holds.", name, target, source)
	w.line("//")
	w.wrapped(ledger(built))
}

// body writes the constructor itself: the matched members in the literal, the
// hinted ones assigned beneath it, in each case in the target's own order.
func body(w *writer, built *plan, name, src, held, source, target string) {
	param := source
	if _, ok := built.source.Underlying().(*types.Interface); !ok {
		param = "*" + source
	}

	w.line("func %s(%s %s) %s {", name, src, param, target)
	w.line("%s := %s{", held, target)
	for _, member := range built.members {
		switch member.via {
		case settledField:
			w.line("%s: %s.%s,", member.field.Name, src, member.from)
		case settledMethod:
			w.line("%s: %s.%s(),", member.field.Name, src, member.from)
		case settledHint, settledIgnored, settledInvalid:
			// A hint's expression may read members the literal is still
			// building, so it lands after the literal; an ignored member
			// stays the zero value the literal already gives it.
		}
	}
	w.line("}")

	renamed := map[string]string{built.srcParam: src, built.dstParam: held}
	for _, member := range built.members {
		if member.via == settledHint {
			w.line("%s.%s = %s", held, member.field.Name, respelled(member.hint, renamed))
		}
	}

	w.line("return %s", held)
	w.line("}")
}

// ledger says how every member was settled, one clause per way.
func ledger(built *plan) string {
	var fields, methods, folds, pins, hinted, ignored []string

	for _, member := range built.members {
		switch member.via {
		case settledField:
			switch {
			case member.tagged:
				pins = append(pins, fmt.Sprintf("%s (from %s)", member.field.Name, member.from))
			case member.folded:
				folds = append(folds, fmt.Sprintf("%s (from %s)", member.field.Name, member.from))
			default:
				fields = append(fields, member.field.Name)
			}
		case settledMethod:
			switch {
			case member.tagged:
				pins = append(pins, fmt.Sprintf("%s (from %s())", member.field.Name, member.from))
			case member.folded:
				folds = append(folds, fmt.Sprintf("%s (from %s())", member.field.Name, member.from))
			default:
				methods = append(methods, member.field.Name)
			}
		case settledHint:
			held := member.field.Name
			if member.overrode != "" {
				held += " (overriding the match on " + member.overrode + ")"
			}
			hinted = append(hinted, held)
		case settledIgnored, settledInvalid:
			ignored = append(ignored, member.field.Name)
		}
	}

	var parts []string
	for _, clause := range []struct {
		label string
		names []string
	}{
		{"matched by field: ", fields},
		{"by method: ", methods},
		{"folded: ", folds},
		{"pinned by tag: ", pins},
		{"from the hint: ", hinted},
		{"ignored: ", ignored},
	} {
		if len(clause.names) > 0 {
			parts = append(parts, clause.label+strings.Join(clause.names, ", "))
		}
	}

	out := strings.Join(parts, "; ") + "."
	return strings.ToUpper(out[:1]) + out[1:]
}

// parsed turns the assembled source into the declarations a unit carries.
//
// The failure is an error about the constructor written for a named type,
// raised where the layer can still be stopped, rather than a file on disk that
// does not parse.
func parsed(source, about string) ([]ast.Decl, []*ast.CommentGroup, *token.FileSet, error) {
	fset := token.NewFileSet()

	file, err := parser.ParseFile(fset, "map.go", "package forge\n\n"+source, parser.ParseComments)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("mapping: the constructor assembled for %s is not valid Go: %w", about, err)
	}

	return file.Decls, file.Comments, fset, nil
}
