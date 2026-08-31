package main

import (
	"go/token"
	"os"
	"path/filepath"
	"slices"

	"golang.org/x/tools/go/packages"

	"github.com/okian/forge/internal/diag"
	"github.com/okian/forge/internal/emit"
	generated "github.com/okian/forge/internal/generate"
	"github.com/okian/forge/internal/load"
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
// Only for a package that did not build, and that restriction is the whole of
// what makes this safe to say. A leftover file that breaks nothing is not worth
// stopping a run over, and there are two ordinary reasons a file can look
// unaccounted for while being current: the declaration it belongs to may be
// behind a build constraint this run did not see, and it may be one whose
// subject was refused, which is reported already. Neither of those stops a
// package building on its own, so neither reaches here — and where a package
// *is* broken, an unaccounted-for generated file is worth naming whichever of
// the three it turns out to be.
//
// Named rather than deleted. Removing a file is not something to do on the
// strength of a run that may have been given the wrong patterns, or run on the
// wrong operating system.
//
// The reason a constrained declaration's file breaks the other platform is that
// the generated file carries no constraint of its own. That is the business of
// the stage that decides what build tags output gets, not of this one.
func orphans(found resolved) diag.Set {
	var diags diag.Set

	if found.Session == nil {
		return diags
	}

	held := accountedFor(found)

	for _, pkg := range found.Session.Packages {
		if !load.Broken(pkg) {
			// A package that builds is a package whose files, whatever else is
			// true of them, are not stopping anybody.
			//
			// Asked of the load rather than of the package, because the errors
			// a package carries include the ones forge's own body-stripping
			// causes — every generated file has an import used only inside a
			// body — and reading those raw finds every package holding
			// generated code broken.
			continue
		}

		dir := directory(pkg)
		if dir == "" {
			continue
		}

		for _, name := range unaccounted(dir, held[pkg.PkgPath]) {
			diags.Add(diag.New(codeOrphan, at(found, pkg),
				"%s does not belong to any declaration in this package, and the package does not build", name).
				WithHint("%s", "delete it if the declaration it was written for is gone; "+
					"if that declaration is behind a build constraint, generate for that configuration too"))
		}
	}

	return diags
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
		out[one.Pkg.PkgPath] = append(held, generated.Named(one.Name))
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
