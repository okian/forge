// Command forge generates Go from the types a declaration names.
//
// A declaration like
//
//	type Persons Collection[Ring[Json[Person]]]
//
// names a stack of layers over a subject. forge resolves it, decides what each
// layer contributes, and writes the result into the package the declaration
// lives in. The generated files are committed, so a build needs no tool
// installed and an editor needs no plugin.
//
// Run forge with no arguments for the commands.
package main

import (
	"os"
)

// main runs one command line and ends with the status it produced.
//
// It holds nothing but that call. A tool whose only entry point ends the
// process can be tested only by starting a process, and such a suite needs a
// built binary, cannot tell one stream from the other, and reads back nothing
// it did not print. Everything worth testing is in run, which returns a status
// and writes to whatever it is given.
func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}
