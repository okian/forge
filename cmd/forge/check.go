package main

import "fmt"

// check validates declarations and verifies that what was generated is fresh.
func check(env *environment, cmd command, args []string) error {
	flags := flagsFor(cmd)

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

	return fmt.Errorf("verifying that generated files are fresh %w", errNotBuilt)
}
