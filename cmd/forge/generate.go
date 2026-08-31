package main

import (
	"fmt"

	"github.com/okian/forge/internal/compose"
	"github.com/okian/forge/internal/diag"
	generated "github.com/okian/forge/internal/generate"
	"github.com/okian/forge/internal/layers"
)

// generate resolves declarations and writes the files they ask for.
func generate(env *environment, cmd command, args []string) error {
	flags := flagsFor(cmd)

	dry := flags.Bool("dry-run", false, "resolve and report without writing anything")
	diff := flags.Bool("diff", false, "write what would change to stdout as a diff")

	packages, on, err := parse(env, cmd, flags, args)
	if !on || err != nil {
		return err
	}

	found, err := env.pipeline.follow(env, env.loadConfig(packages...))
	if err != nil {
		return err
	}

	// Everything that was wrong before generation, reported before any of it is
	// attempted. A package that does not build is a package whose declarations
	// resolve to whatever survived the failure, and writing files from that is
	// how a broken build becomes a broken repository.
	//
	// A file forge wrote for a declaration that is gone is reported alongside,
	// because it is very often the reason the package does not build — and the
	// errors it causes are all about generated code, which is the last place an
	// author who has just renamed a type will think to look.
	problems := found.All()
	stale := orphans(found)
	problems.Merge(&stale)

	if env.report(problems) {
		return errReported
	}

	return emitting(env, found, *dry || *diff, *diff)
}

// emitting generates each package's files and does what was asked with them.
//
// The two flags that stop a write are one behaviour with two reports: neither
// touches anything, and one of them says what the difference would have been.
// Keeping them one path is what stops --dry-run and --diff from disagreeing
// about what would happen.
func emitting(env *environment, found resolved, hold, show bool) error {
	cfg := configured()

	var (
		problems diag.Set
		changed  int
		failed   error
	)

	for _, pkg := range grouped(found.Requests) {
		files, reported := generated.Package(pkg.path, pkg.name, pkg.requests, cfg)
		problems.Merge(&reported)

		// A package is written whole or not at all. Its files reach into each
		// other — a declaration's own file calls helpers that live in the one
		// the package shares — so writing the half that succeeded leaves a
		// package holding an answer to a question the other half was going to
		// ask.
		if !reported.Empty() {
			continue
		}

		touched, err := placing(env, pkg, files, hold, show)
		changed += touched

		if err != nil {
			// Kept rather than returned. What went wrong writing one package
			// does not make what was wrong with another one less worth saying,
			// and an author told only about a permission would fix it and meet
			// the rest one run later.
			failed = err
			break
		}
	}

	reported := env.report(problems)

	switch {
	case failed != nil:
		return failed
	case reported:
		return errReported
	}

	env.announce(hold, "%s", counted(changed, hold))
	return nil
}

// configured returns what generation is given: the catalog of layers this
// binary knows, and the three versions a generated file records.
//
// One place, because it is a description of this build rather than of a run.
// Anything assembling it a second time would be generating against a catalog or
// a version that the command does not use, and would agree with it right up
// until one of them changed.
func configured() generated.Config {
	self, markers, toolchain := versions()

	return generated.Config{
		Catalog: compose.Catalog{
			Registry:       layers.Builtins(),
			DefaultStorage: layers.DefaultStorage(),
		},
		Forge: self, Markers: markers, Toolchain: toolchain,
	}
}

// placing does what the flags asked with one package's files.
func placing(env *environment, pkg packaged, files []generated.File, hold, show bool) (int, error) {
	if pkg.dir == "" {
		// A package whose files forge cannot find is one it cannot write
		// beside. Writing into the working directory instead is the one
		// behaviour that would be worse than not writing at all.
		return 0, fmt.Errorf("the package %s has no directory to write into", pkg.path)
	}

	changed := 0
	for _, file := range files {
		touched, err := handle(env, pkg, file, hold, show)
		if err != nil {
			return changed, err
		}
		if touched {
			changed++
		}
	}
	return changed, nil
}

// handle does what the flags asked with one generated file, and reports whether
// it was or would be different from what is there.
func handle(env *environment, pkg packaged, file generated.File, hold, show bool) (bool, error) {
	if !hold {
		did, err := place(pkg.dir, file)
		if err != nil {
			return false, err
		}
		if did != unchanged {
			env.progress("wrote %s", file.Name)
		}
		return did != unchanged, nil
	}

	// Nothing is written, so the only thing a difference costs is working it
	// out — and --dry-run does not want it.
	if !show {
		same, err := identical(pkg.dir, file)
		if err != nil {
			return false, err
		}
		if !same {
			env.announce(true, "would write %s", file.Name)
		}
		return !same, nil
	}

	text, err := difference(pkg.dir, file)
	if err != nil {
		return false, err
	}
	if text == "" {
		return false, nil
	}

	say(env.stdout, "%s", text)
	return true, nil
}

// counted says what a run did, in the words that fit what it was allowed to do.
func counted(changed int, hold bool) string {
	verb := "wrote"
	if hold {
		verb = "would write"
	}

	if changed == 1 {
		return verb + " 1 file"
	}
	return fmt.Sprintf("%s %d files", verb, changed)
}
