package emit

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/token"
	"slices"
	"strings"
)

// Section is what one layer generated: declarations, the comments that belong
// to them, and the file set their positions are measured against.
//
// The three travel together because they are only meaningful together, and that
// is the whole reason this type exists rather than a bare slice of
// declarations. A comment is not reachable from the declaration it documents:
// the printer finds it by position, in a list held by the file the declarations
// were parsed from. Print a declaration on its own and every comment inside a
// function body disappears; print it against a file set that is not the one it
// was parsed from and the comments that do survive migrate to wherever those
// positions happen to land — after the body they document, or into the middle
// of a struct.
//
// Neither failure is loud. Both produce Go that compiles, so nothing downstream
// notices, and the output is committed. Layers are written as real Go and
// rewritten into place, so every one of them will arrive with a file set of its
// own; keeping each layer's declarations with the file set that explains them is
// what makes that safe.
type Section struct {
	// Decls holds the declarations, in the order they will appear.
	//
	// Any order will do, and they may come from more than one file and be a
	// mixture of parsed and built. The printer places a declaration where it is
	// told and a comment where its position says, so any of those would leave
	// comments behind — but the declarations are written in stretches the two
	// agree about, and the comments follow.
	//
	// An import declaration is not written at all. Imports are named through
	// the file rather than emitted through a section, so that two layers
	// needing one need it once.
	Decls []ast.Decl

	// Comments holds the comment groups from the files the declarations were
	// parsed from. It is nil for declarations that were built rather than
	// parsed, whose doc comments hang off the declarations themselves.
	//
	// Handing over whole files' groups is the ordinary thing to do and is safe:
	// the groups belonging to declarations this section does not hold are left
	// out when it is written. A template file has a package comment, very
	// likely a licence, and helpers that are not emitted, and none of their
	// comments belong in the output.
	Comments []*ast.CommentGroup

	// Fset resolves the positions the declarations and comments carry. It may
	// be nil only when neither carries one, which is to say for declarations
	// that were built rather than parsed.
	Fset *token.FileSet
}

// Empty reports whether the section would write nothing.
func (s Section) Empty() bool { return len(emitted(s.Decls)) == 0 }

// Render returns the section's declarations as source text.
//
// Everything that reads a tree happens under one recover. The trees are built
// by layers, and one with a hole in it — a type declared as nothing, a comment
// group with no comments in it — makes the ordinary way of asking where
// something is raise rather than answer. A panic reaching the author is a stack
// trace where a diagnostic should be: it says forge is broken and gives them
// nothing to do about it.
func (s Section) Render() (out string, err error) {
	defer func() {
		if caught := recover(); caught != nil {
			out, err = "", errNotWellFormed(caught)
		}
	}()

	decls := emitted(s.Decls)
	if len(decls) == 0 {
		return "", nil
	}

	if err := placed(decls); err != nil {
		return "", err
	}

	fset := s.Fset
	if fset == nil {
		fset = token.NewFileSet()
	}

	var b strings.Builder
	for i, stretch := range stretches(fset, decls) {
		if i > 0 {
			b.WriteString("\n\n")
		}

		b.WriteString(printed(fset, stretch, s.Comments))
	}

	return b.String(), nil
}

// stretches splits declarations into the longest runs the printer can place
// comments across.
//
// The printer writes declarations in the order it is given them and flushes
// each pending comment when it reaches the position that comment sits at. Those
// two agree only while the declarations ascend through one file, so a stretch
// ends wherever they stop doing that — and every stretch is then written as its
// own file, with its own comments, where the disagreement cannot arise.
//
// Three things end a stretch, and each is ordinary rather than exotic. A layer
// emitting a type, then its constructor, then its methods puts them in an order
// its template did not. A template is a package rather than a file, and an
// offset is counted within a file, so two files' positions do not order against
// each other at all. And a rewrite keeps some declarations and builds others,
// and a built one has no position for anything to be placed against.
func stretches(fset *token.FileSet, decls []ast.Decl) [][]ast.Decl {
	var out [][]ast.Decl

	for _, decl := range decls {
		if last := len(out) - 1; last >= 0 && continues(fset, out[last], decl) {
			out[last] = append(out[last], decl)
			continue
		}
		out = append(out, []ast.Decl{decl})
	}

	return out
}

// continues reports whether a declaration can be written in the same stretch as
// the ones before it.
func continues(fset *token.FileSet, stretch []ast.Decl, next ast.Decl) bool {
	last := stretch[len(stretch)-1]

	// Nothing can be placed relative to a declaration that is nowhere, so built
	// declarations run together and never with a parsed one.
	if !last.Pos().IsValid() || !next.Pos().IsValid() {
		return !last.Pos().IsValid() && !next.Pos().IsValid()
	}

	// By identity rather than by name. Two files added to one file set under
	// one name are two files, their offsets are counted separately, and a name
	// cannot tell them apart — which is the ordinary case for a rewriting
	// engine that parses fragments and calls them all the same thing.
	if fset.File(last.Pos()) != fset.File(next.Pos()) {
		return false
	}

	return last.Pos() < next.Pos()
}

// printed writes one stretch of declarations as source text.
//
// They are printed as a file and the package clause is then cut back off, which
// is the only way the printer will place comments at all: it takes them from a
// file's comment list, so a declaration printed on its own is printed without
// them.
//
// A stretch no comment belongs to is given no list rather than an empty one,
// and the difference matters. A file with a comment list has its declarations'
// own doc comments turned off, so a built declaration written beside a parsed
// one would otherwise lose the comment it carries with it.
func printed(fset *token.FileSet, decls []ast.Decl, comments []*ast.CommentGroup) string {
	file := &ast.File{
		Name:     ast.NewIdent("_"),
		Decls:    decls,
		Comments: belonging(decls, comments),
	}

	var buf bytes.Buffer
	if err := format.Node(&buf, fset, file); err != nil {
		// The formatter fails in two ways and neither is reachable from here.
		// It cannot fail to write, because it writes to a buffer. And it
		// reparses what it wrote only when a file's imports need sorting, which
		// no section's do: a section never holds an import declaration. If
		// either ever stopped being true it would be the same kind of thing as
		// a tree with a hole in it, and goes the same way — caught above and
		// reported as forge's own mistake rather than raised at the author.
		panic(err)
	}

	// The clause is the first line and nothing precedes it: every comment in
	// the list belongs to a declaration written after it. If that ever stopped
	// being true the assembled file would not parse, and what is reported then
	// is the whole numbered source rather than a guess about it.
	_, rest, _ := strings.Cut(buf.String(), "\n")

	return strings.Trim(rest, "\n")
}

// placed reports a declaration documented by a comment that is somewhere the
// declaration is not.
//
// The printer places a comment by its position and a declaration by its own, so
// the two have to agree. A rewrite that keeps a comment it parsed and builds
// the declaration under it leaves a comment with a position above a declaration
// without one, and what comes out is the comment printed *after* the thing it
// documents — silently, in Go that compiles and is committed. No arrangement of
// the file's comment list fixes it, so it is reported.
func placed(decls []ast.Decl) error {
	for _, decl := range decls {
		group := doc(decl)
		if !positioned(group) {
			continue
		}
		if !decl.Pos().IsValid() || group.End() > decl.Pos() {
			return fmt.Errorf("a comment documenting a declaration is positioned after it; "+
				"the declaration was built and the comment was parsed: %q",
				strings.TrimSpace(group.Text()))
		}
	}

	return nil
}

// belonging returns the comment groups that belong to these declarations.
//
// A layer's declarations come out of a file it does not emit whole. The file
// has a package comment, very likely a licence, and helpers the layer keeps to
// itself, and every one of those has comments that would otherwise be printed
// beside declarations they say nothing about — silently, in output that
// compiles and is committed.
//
// Leaving them out is also what makes the package clause cuttable. A comment
// that belongs to no declaration sorts before the first one, and the printer
// puts it between the two words of the clause; there is then no first line to
// cut.
func belonging(decls []ast.Decl, comments []*ast.CommentGroup) []*ast.CommentGroup {
	if len(comments) == 0 {
		return nil
	}

	out := make([]*ast.CommentGroup, 0, len(comments))
	for _, group := range comments {
		if slices.ContainsFunc(decls, func(decl ast.Decl) bool { return holds(decl, group) }) {
			out = append(out, group)
		}
	}

	if len(out) == 0 {
		return nil
	}
	return out
}

// holds reports whether a declaration documents a comment group or contains it.
//
// Both have to be somewhere for either question to mean anything. A group and a
// declaration that were both built are both at position zero, where every span
// contains every other and the answer would come out yes for reasons that have
// nothing to do with the code.
func holds(decl ast.Decl, group *ast.CommentGroup) bool {
	if !positioned(group) || !decl.Pos().IsValid() {
		return false
	}

	// A doc comment sits before the declaration it documents, so it falls
	// outside the declaration's span and has to be recognised by identity.
	if doc(decl) == group {
		return group.End() <= decl.Pos()
	}

	return decl.Pos() <= group.Pos() && group.End() <= decl.End()
}

// positioned reports whether a comment group is somewhere.
//
// A group's end is its start plus its length, so a group that was built rather
// than parsed still ends somewhere; where it starts is what says whether it was
// placed at all. A group holding no comments is nowhere and would raise rather
// than answer.
func positioned(group *ast.CommentGroup) bool {
	return group != nil && len(group.List) > 0 && group.Pos().IsValid()
}

// doc returns the comment group a declaration is documented by, if it has one.
//
// A nil pointer of a declaration type is a value that satisfies its own case
// and holds nothing, which is a distinction the language does not make for you.
func doc(decl ast.Decl) *ast.CommentGroup {
	switch typed := decl.(type) {
	case *ast.GenDecl:
		if typed == nil {
			return nil
		}
		return typed.Doc
	case *ast.FuncDecl:
		if typed == nil {
			return nil
		}
		return typed.Doc
	default:
		return nil
	}
}

// emitted returns the declarations a section writes.
//
// Two kinds are left out. A gap is one: a layer with nothing to say for part of
// its output returns a nil entry rather than a shorter slice often enough that
// dropping them here is cheaper than every layer remembering. Only the gap a
// layer wrote as nil is caught — a nil pointer of a declaration type is a
// value, not a gap, and reaches the printer, which is why everything that reads
// a tree here runs under a recover.
//
// The other is an import declaration. A layer's declarations come out of a file
// it does not emit whole, and a file's imports are furniture like its package
// clause: written once for the whole output, after every layer has said what it
// needs, so that two layers needing one import need it once and a template that
// imported something its emitted half no longer uses does not carry it. Left
// in, they would be written a second time below the block and the file would
// not compile. A layer that forgets to name an import instead gets a compile
// failure the first time its output is built, which is where that is cheapest
// to learn.
func emitted(decls []ast.Decl) []ast.Decl {
	if !slices.ContainsFunc(decls, furniture) {
		return decls
	}

	out := make([]ast.Decl, 0, len(decls))
	for _, decl := range decls {
		if !furniture(decl) {
			out = append(out, decl)
		}
	}
	return out
}

// furniture reports whether a declaration belongs to the file it was parsed
// from rather than to the output.
func furniture(decl ast.Decl) bool {
	if decl == nil {
		return true
	}
	// A nil pointer of a declaration type satisfies the assertion and is not
	// something to read a field out of. Every other reader here runs under a
	// recover and can afford to be careless about that; this one is asked by
	// Empty, which a caller may ask before it has anywhere to report to.
	gen, ok := decl.(*ast.GenDecl)
	return ok && gen != nil && gen.Tok == token.IMPORT
}
