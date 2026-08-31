package main

import (
	catalog "github.com/okian/forge/internal/explain"
	"github.com/okian/forge/internal/layers"
)

// list reports the layers this build knows about.
//
// No patterns and no load. Which layers exist is a fact about the binary rather
// than about anybody's code, so this answers without reading a package — which
// is what makes it the thing to run when a declaration will not compose and the
// question is what else there was to write.
func list(env *environment, cmd command, args []string) error {
	flags := flagsFor(cmd)

	asJSON := flags.Bool("json", false, "write the catalog as JSON")

	rest, on, err := parse(env, cmd, flags, args)
	if !on || err != nil {
		return err
	}

	if len(rest) > 0 {
		return answered(cmd, flags, "list takes no arguments, got %q", rest[0])
	}

	// Against the same registry generation composes with, so that a layer this
	// lists is one a declaration can name and a layer it omits is one that
	// would be refused.
	held := catalog.Layers(layers.Builtins())

	if *asJSON {
		return held.JSON(env.stdout)
	}
	return held.Text(env.stdout)
}
