package main

import "fmt"

// doctor reports on the toolchain and the setup around it.
func doctor(env *environment, cmd command, args []string) error {
	flags := flagsFor(cmd)

	rest, on, err := parse(env, cmd, flags, args)
	if !on || err != nil {
		return err
	}

	if len(rest) > 0 {
		return answered(cmd, flags, "doctor takes no arguments, got %q", rest[0])
	}

	return fmt.Errorf("diagnosing the toolchain %w", errNotBuilt)
}
