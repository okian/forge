package cli

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"golang.org/x/tools/go/packages"

	"github.com/okian/forge/internal/diag"
	"github.com/okian/forge/internal/emit"
	generated "github.com/okian/forge/internal/generate"
	"github.com/okian/forge/internal/model"
)

// outcome says what a run did with one file, which is what the report at the
// end counts.
type outcome int

const (
	// unchanged is a file already holding exactly what would be written.
	unchanged outcome = iota

	// created is a file that was not there, and updated one that was and held
	// something else.
	created
	updated
)

// codeForeign reports a file forge would write to that it did not write.
//
// A 5xxx like the orphan report beside it: what is wrong is the state of a
// directory rather than anything anybody wrote, and what it takes to fix is a
// deletion or a rename rather than an edit to a declaration.
var codeForeign = diag.Register(5006, "a file forge did not write is in the way")

// place puts a file where it belongs, and reports whether it had to.
//
// A byte-identical file is left alone rather than rewritten with the same
// bytes. Rewriting it would change its modification time, which every build
// cache and every watcher reads as a change — so a run that generated nothing
// new would rebuild the world, and a second run would do it again.
//
// A file that does not say forge wrote it is not written over. The names forge
// chooses are unusual and prefixed to be, but a declaration can still land on
// one somebody already used, and the cost of guessing wrong is somebody's work
// gone with nothing said about it — the worst thing a generator can do, and the
// one thing no amount of care downstream recovers from.
func place(dir string, file generated.File) (outcome, error) {
	path := filepath.Join(dir, file.Name)

	switch held, err := os.ReadFile(path); { //nolint:gosec // the path is a package directory and a name forge chose.
	case err == nil && bytes.Equal(held, file.Content):
		return unchanged, nil
	case err == nil && !writable(held):
		return unchanged, foreign(file)
	case err == nil:
		return updated, os.WriteFile(path, file.Content, 0o644) //nolint:gosec // generated source is read by the compiler and by people.
	case errors.Is(err, os.ErrNotExist):
		return created, os.WriteFile(path, file.Content, 0o644) //nolint:gosec // as above.
	default:
		return unchanged, err
	}
}

// writable reports whether a file already in the way is one forge may write over.
//
// A file that says it is generated is, whatever else has happened to it. So is
// an empty one: a file holding nothing has nothing to lose, and the ordinary
// way to reach that is a tool or an editor that truncated a generated file,
// where refusing would leave somebody with a file forge cannot repair and no
// way to ask it to.
func writable(held []byte) bool {
	if len(bytes.TrimSpace(held)) == 0 {
		return true
	}

	_, generated := emit.ReadHeader(held)
	return generated
}

// foreign reports a file in the way that forge did not write.
//
// Both ways out are offered because forge cannot tell which is right. A file
// somebody wrote wants the declaration renamed; a generated file whose header
// was lost — truncated by a merge, stripped by a tool that rewrites the top of
// every file — wants deleting, and regenerating puts it back.
func foreign(file generated.File) error {
	return diag.New(codeForeign, file.Pos,
		"%s is already there and does not say forge wrote it", file.Name).
		WithHint("%s", "delete it and run again if it is forge's and lost its header, "+
			"or move it out of the way if the file is yours")
}

// identical reports whether a file already holds exactly what would be written.
//
// The question --dry-run asks, and the whole of it. Working out a difference
// costs a table of one cell per pair of lines, and a run that was asked only
// whether anything would change should not pay for an answer nobody asked for.
func identical(dir string, file generated.File) (bool, error) {
	held, err := os.ReadFile(filepath.Join(dir, file.Name)) //nolint:gosec // as in place.
	switch {
	case errors.Is(err, os.ErrNotExist):
		return false, nil
	case err != nil:
		return false, err
	}

	return bytes.Equal(held, file.Content), nil
}

// difference renders what writing a file would change, in the shape a reader of
// diffs expects.
//
// Enough of the unified form to read rather than to apply. What this answers is
// "what is about to happen to my repository", and a patch nobody will feed to
// patch is better as the lines that differ than as a format with an exact
// grammar — so there are no hunk headers, and everything unchanged outside the
// part that moved is simply left out.
func difference(dir string, file generated.File) (string, error) {
	path := filepath.Join(dir, file.Name)

	held, err := os.ReadFile(path) //nolint:gosec // as in write.
	switch {
	case errors.Is(err, os.ErrNotExist):
		held = nil
	case err != nil:
		return "", err
	}

	if bytes.Equal(held, file.Content) {
		return "", nil
	}

	var b strings.Builder
	fmt.Fprintf(&b, "--- %s\n+++ %s\n", path, path)
	b.WriteString(changes(lines(held), lines(file.Content)))

	return b.String(), nil
}

// lines splits a file into its lines, without the empty one a trailing newline
// leaves behind.
func lines(content []byte) []string {
	if len(content) == 0 {
		return nil
	}
	return strings.Split(strings.TrimSuffix(string(content), "\n"), "\n")
}

// grouped gathers the declarations of a run by the package they live in.
//
// By package because that is the unit generation works in: two declarations
// there share the helpers they require and may not share a name, and neither is
// a question a declaration answers about itself. In the order they were found,
// so that what a run reports does not depend on a map.
func grouped(requests []request) []packaged {
	var out []packaged

	for _, one := range requests {
		pkg := one.Declaration.Candidate.Pkg
		if pkg == nil {
			continue
		}

		at := slices.IndexFunc(out, func(held packaged) bool { return held.path == pkg.PkgPath })
		if at < 0 {
			out = append(out, packaged{path: pkg.PkgPath, name: pkg.Name, dir: directory(pkg)})
			at = len(out) - 1
		}

		out[at].requests = append(out[at].requests, generated.Request{
			Model:      built(one),
			Directives: one.Declaration.Candidate.Directives,
		})
	}

	return out
}

// packaged is one package's worth of a run.
type packaged struct {
	// path is the import path, name the package clause a generated file
	// carries, and dir where its files are written.
	path string
	name string
	dir  string

	// requests are the declarations in it, in the order they were found.
	requests []generated.Request
}

// built returns the model of a declaration, and nothing for one whose subject
// was refused — which is reported already, and is a declaration nothing can be
// generated for.
func built(one request) *model.Model {
	if one.Model == nil {
		return nil
	}

	decl := one.Declaration
	return &model.Model{
		Name:    decl.Candidate.Name,
		Form:    decl.Candidate.Form,
		Subject: one.Model,
		Source:  decl.Source,
		Hints:   one.Hints,
		Stack:   decl.Stack,
		Pkg:     decl.Candidate.Pkg,
		Pos:     decl.Candidate.Pos,
	}
}

// directory is where a package's files are, taken from a file it holds.
//
// From a file rather than from a field, because a package's directory is not
// one of the things go/packages reports: what it reports is the files, and they
// are all in it.
func directory(pkg *packages.Package) string {
	for _, files := range [][]string{pkg.GoFiles, pkg.CompiledGoFiles, pkg.OtherFiles} {
		if len(files) > 0 {
			return filepath.Dir(files[0])
		}
	}
	return ""
}
