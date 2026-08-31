package main

import "fmt"

// list reports the layers this build knows about.
func list(env *environment, cmd command, args []string) error {
	flags := flagsFor(cmd)
	flags.Bool("json", false, "write the catalog as JSON")

	rest, on, err := parse(env, cmd, flags, args)
	if !on || err != nil {
		return err
	}

	if len(rest) > 0 {
		return answered(cmd, flags, "list takes no arguments, got %q", rest[0])
	}

	return fmt.Errorf("listing the layer catalog %w", errNotBuilt)
}
