package main

import "fmt"

// here is the package a question is asked about when the command line names
// none.
const here = "."

// explain shows what one declaration resolves to.
func explain(env *environment, cmd command, args []string) error {
	flags := flagsFor(cmd)
	name := flags.String("t", "", "the declared type to explain (required)")
	flags.Bool("json", false, "write the resolution as JSON")

	on, err := parse(env, cmd, flags, args)
	if !on || err != nil {
		return err
	}

	// Required, because explaining every declaration in a package is what the
	// other verbs are for: this one exists to answer a question about one
	// declaration, and without a name there is no question.
	if *name == "" {
		return misusedf("explain needs a declaration to explain: forge explain -t Type")
	}

	// One package, defaulted to the one forge was started in. Explaining a
	// declaration is a question about one of them, and the whole module is both
	// slower to load and ambiguous: two packages may each declare a Persons,
	// and nothing about -t says which was meant.
	where := here
	switch flags.NArg() {
	case 0:
	case 1:
		where = flags.Arg(0)
	default:
		return misusedf("explain takes one package, got %d", flags.NArg())
	}

	found, err := env.pipeline.follow(env, env.loadConfig(where))
	if err != nil {
		return err
	}
	env.report(found.Diagnostics)

	return fmt.Errorf("explaining %s %w", *name, errNotBuilt)
}
