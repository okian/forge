package main

import "fmt"

// doctor reports on the toolchain and the setup around it.
func doctor(env *environment, cmd command, args []string) error {
	flags := flagsFor(cmd)

	on, err := parse(env, cmd, flags, args)
	if !on || err != nil {
		return err
	}

	if flags.NArg() > 0 {
		return misusedf("doctor takes no arguments, got %q", flags.Arg(0))
	}

	return fmt.Errorf("diagnosing the toolchain %w", errNotBuilt)
}
