package cli

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"

	"golang.org/x/tools/go/packages"

	"github.com/okian/forge/internal/diag"
	"github.com/okian/forge/internal/emit"
	generated "github.com/okian/forge/internal/generate"
	"github.com/okian/forge/internal/load"
	"github.com/okian/forge/internal/model"
)

// codeOrphan reports a generated file no declaration in the package accounts
// for.
//
// A 5xxx, because it is about the state of somebody's working tree rather than
// about anything they wrote: a file forge left behind is not a mistake in their
// source, and what it takes to fix is a deletion rather than an edit.
var codeOrphan = diag.Register(5003, "generated file belongs to no declaration here")

// orphans explains a package that will not build by naming the file forge left
// in it.
//
// Renaming or removing a declaration is the ordinary way to make one. The file
// generated for the old name stays where it was, still declaring methods on a
// type that is gone, so the package stops compiling — and forge then refuses
// everything, reporting a dozen build errors about its own output with nothing
// to say that it wrote them or that deleting the file is the fix. An author who
// has just renamed a type has no reason to suspect the generator.
//
// Two ways a file earns the name, and one condition that stops both.
//
// A file in a package that did not build is the first. A leftover that breaks
// nothing is not worth stopping a run over — a run refused because of one would
// refuse --dry-run and --diff too, and those exist to answer a question without
// touching anything.
//
// A file the load never read is the second, whether or not the package builds.
// A declaration forge owns the type of generates into a file constrained
// against the tag forge itself loads under, so such a file is outside every
// build this tool performs: it cannot break anything, and waiting for it to
// would be waiting for ever while a dead type sits in the public API of the
// only build the author ships.
//
// The condition is that this run saw the whole package. A file the load left
// out — a declaration behind a constraint this configuration does not satisfy —
// takes its generated file with it, and both are current. Nothing here can tell
// that from a file whose declaration is gone, because in both cases the run
// simply has no declaration for it; what it can tell is that something was left
// out, and that is enough to say nothing. The cost of the wrong answer is not
// symmetric: a run that stays quiet leaves a dead file to the next run in the
// configuration that owns it, and a run that speaks tells an author to delete a
// file their other platform needs, refusing every package in the same breath.
//
// Named rather than deleted, either way. Removing a file is not something to do
// on the strength of a run that may have been given the wrong patterns.
func orphans(found resolved) diag.Set {
	var diags diag.Set

	if found.Session == nil {
		return diags
	}

	held := accountedFor(found)

	for _, pkg := range found.Session.Packages {
		dir := directory(pkg)
		if dir == "" {
			continue
		}

		loose := unaccounted(dir, held[pkg.PkgPath])
		if len(loose) == 0 {
			continue
		}

		// A package that builds is a package whose files, whatever else is true
		// of them, are not stopping anybody — except the ones it does not hold,
		// which stop nobody by construction and so are never vouched for.
		//
		// Asked of the load rather than of the package, because the errors a
		// package carries include the ones forge's own body-stripping causes —
		// every generated file has an import used only inside a body — and
		// reading those raw finds every package holding generated code broken.
		builds := !load.Broken(pkg)

		// Asked once, and only once there is something to say, since asking
		// reads files: a tree in the state forge left it in has nothing loose in
		// any package, and pays nothing for the question.
		half := partial(pkg)

		for _, name := range loose {
			if builds && compiled(pkg, name) {
				continue
			}

			// A run that read only part of the package has nothing to say about
			// the half it did not read, and cannot tell which half an unread
			// file belongs to.
			//
			// Only about unread files, though. A file the run compiled is one it
			// saw in full, and what the other configuration holds cannot change
			// that this one has no declaration for it — so the ambiguity this
			// answers does not arise, and withholding the report would throw
			// away what the run plainly knows. It is the ordinary case that
			// suffers: a package with a platform-scoped declaration alongside an
			// ordinary one, where renaming the ordinary one is exactly what this
			// was written to explain.
			if half && !compiled(pkg, name) {
				continue
			}

			diags.Add(diag.New(codeOrphan, at(found, pkg),
				"%s does not belong to any declaration in this package", name).
				WithHint("%s", "delete it if the declaration it was written for is gone; "+
					"if that declaration is behind a build constraint, generate for that configuration too"))
		}
	}

	return diags
}

// partial reports whether a build constraint kept a declaration this run cannot
// see out of the load.
//
// A declaration may sit behind more than one constraint: written under the spec
// tag and an operating system, or inline in a file for one platform, it is a
// declaration only that platform's run can see. Run anywhere else, the
// declaration is absent and so is the file generated for it — and the two
// absences are the same absence, which is why a run that meets one must not
// report the other.
//
// Only spec files count, which is to say only files whose constraint asks for
// the tag. That is exactly the class that can leave a file behind here: a
// declaration forge owns the type of is the only kind whose output is written
// against the tag, and so the only kind whose output survives in a build its
// own declaration is absent from. What a file declares does not enter into it,
// and must not — a directive is optional, so a file can hold a declaration
// without saying anything that distinguishes it from prose.
//
// Platform-split source is therefore ignored, which is the point. Files like
// paths_linux.go and //go:build ignore generator scripts are ordinary Go; a
// rule that counted them would be silent on every package that has one, which
// is most of them, and silence is the failure that leaves dead types in a
// published API.
//
// Forge's own output is not counted. Its stub file is written under the tag and
// so is a spec file by this test, and a package holding one would otherwise
// look partial for ever. The name is enough to recognise it here: a hand-written
// file borrowing the prefix would only cost this package its report, whereas
// reading the header to be sure would cost a second read of every file.
//
// A file that cannot be read or parsed counts. Something is there, and this is
// not the run to decide what.
func partial(pkg *packages.Package) bool {
	fset := token.NewFileSet()

	return slices.ContainsFunc(pkg.IgnoredFiles, func(path string) bool {
		if filepath.Ext(path) != ".go" || generated.Ours(filepath.Base(path)) {
			return false
		}

		file, err := parser.ParseFile(fset, path, nil, parser.ParseComments|parser.PackageClauseOnly)
		return err != nil || load.SpecFile(fset, file)
	})
}

// compiled reports whether the load read this file as part of the package.
//
// A file outside the build forge loads under is one nothing it knows can vouch
// for: the type checker never saw the names in it, so a package holding it
// compiles cleanly whether the file is current or long dead.
//
// The files a constraint kept out are listed too, under a name of their own,
// and are deliberately not read here. That list is exactly the files this is
// looking for — a package that builds vouches for what went into it, and
// nothing at all for what did not.
func compiled(pkg *packages.Package, name string) bool {
	for _, files := range [][]string{pkg.GoFiles, pkg.CompiledGoFiles} {
		if slices.ContainsFunc(files, func(path string) bool { return filepath.Base(path) == name }) {
			return true
		}
	}
	return false
}

// accountedFor returns, per package, the files its declarations ask for.
//
// By what discovery found rather than by what survived the stages after it. A
// declaration whose marker is misspelled does not resolve, and one whose
// subject is a pointer does not model — and both are still declarations in the
// package, whose generated files are still theirs. Naming either as belonging
// to nobody would tell an author to delete a current file, on top of the
// refusal they are already being told about, and the refusal is what they
// should be fixing.
//
// The two files no single declaration owns are claimed by the package. The
// shared one wherever there is a declaration at all, and the file standing in
// for what the tag excludes wherever a declaration forge owns the type of asks
// for one — the second only then, because a package whose spec declarations
// have all been deleted or moved inline no longer has anything to stand in for,
// and the file left behind is exactly the leftover this is looking for.
func accountedFor(found resolved) map[string][]string {
	out := make(map[string][]string)

	for _, one := range found.Candidates {
		if one.Pkg == nil {
			continue
		}

		held := out[one.Pkg.PkgPath]
		if held == nil {
			held = []string{generated.Shared()}
		}
		held = append(held, generated.Named(one.Name))

		if one.Form == model.FormSpec && !slices.Contains(held, generated.Stubs()) {
			held = append(held, generated.Stubs())
		}
		out[one.Pkg.PkgPath] = held
	}

	return out
}

// unaccounted returns the generated files of a directory that nothing in the
// package asks for.
//
// A file is forge's only if it says so in the way forge says it: the name alone
// is a convention anybody could follow, and naming somebody's own
// zz_forge_notes.go would be worse than saying nothing.
func unaccounted(dir string, claimed []string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		// A directory that cannot be read is one the load already had trouble
		// with, and a second complaint about it helps nobody.
		return nil
	}

	var out []string
	for _, entry := range entries {
		name := entry.Name()

		if entry.IsDir() || !generated.Ours(name) || slices.Contains(claimed, name) {
			continue
		}

		held, err := os.ReadFile(filepath.Join(dir, name)) //nolint:gosec // a name read out of the package's own directory.
		if err != nil {
			continue
		}
		if _, ours := emit.ReadHeader(held); ours {
			out = append(out, name)
		}
	}

	slices.Sort(out)
	return out
}

// at is where a complaint about a package points.
//
// A package has no position of its own, so the first declaration in it is what
// a report can name; a package whose declarations are all gone has not even
// that, and the report then points at the file it is about.
func at(found resolved, pkg *packages.Package) token.Position {
	for _, one := range found.Candidates {
		if one.Pkg == pkg {
			return one.Pos
		}
	}

	if files := len(pkg.GoFiles); files > 0 {
		return token.Position{Filename: pkg.GoFiles[0]}
	}
	return token.Position{}
}
