package main

import "fmt"

// list reports the layers this build knows about.
func list(env *environment, cmd command, args []string) error {
	flags := flagsFor(cmd)
	flags.Bool("json", false, "write the catalog as JSON")

	on, err := parse(env, cmd, flags, args)
	if !on || err != nil {
		return err
	}

	if flags.NArg() > 0 {
		return misusedf("list takes no arguments, got %q", flags.Arg(0))
	}

	return fmt.Errorf("listing the layer catalog %w", errNotBuilt)
}
