package main

import "fmt"

// generate resolves declarations and writes the files they ask for.
func generate(env *environment, cmd command, args []string) error {
	flags := flagsFor(cmd)

	// Defined and not yet read. A flag that is documented before it works is a
	// command line that keeps meaning what it meant; one that is added later
	// makes every script written against this build wrong for a reason nobody
	// can see in the script.
	flags.Bool("dry-run", false, "resolve and report without writing anything")
	flags.Bool("diff", false, "write what would change to stdout as a diff")

	packages, on, err := parse(env, cmd, flags, args)
	if !on || err != nil {
		return err
	}

	found, err := env.pipeline.follow(env, env.loadConfig(packages...))
	if err != nil {
		return err
	}
	if env.report(found.All()) {
		return errReported
	}

	return fmt.Errorf("writing generated files %w", errNotBuilt)
}
