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
//
// This binary knows the layers forge ships. A binary that also knows one
// somebody else wrote is a few lines of its own, over the same commands and the
// same command line — see [github.com/okian/forge/driver].
package main

import (
	"github.com/okian/forge/driver"
)

// main runs one command line and ends with the status it produced.
//
// It holds nothing but that call, which is also all a plugin binary's main
// holds: the difference between the two is the catalog, and everything else
// about a run comes off the command line.
func main() {
	driver.Main(driver.Builtins())
}
