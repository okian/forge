// Package driver runs forge with a catalog of layers you choose.
//
// It is what a layer forge does not ship is reached through. A layer is code,
// so a binary that means to know about one is a binary somebody linked it into
// — there is no plugin file to drop in and no directory to scan. What that
// binary's main looks like is this:
//
//	package main
//
//	import (
//		"github.com/okian/forge/driver"
//		"example.com/mylayers/csv"
//	)
//
//	func main() {
//		catalog := driver.Builtins()
//		catalog.MustRegister(csv.New())
//
//		driver.Main(catalog)
//	}
//
// The result takes the same command line as forge's own binary, walks the same
// packages, and writes the same files. A declaration naming the added marker
// composes with the built-in layers, and one naming none of them is left alone
// exactly as before.
//
// Start from [Builtins] rather than from an empty catalog. A stack is composed
// across every layer a run knows, so a catalog holding one layer can generate
// for a declaration naming one layer and nothing else — and the storage a
// refining layer needs beneath it is one of the built-ins.
package driver

import (
	"io"
	"os"

	"github.com/okian/forge/internal/cli"
	"github.com/okian/forge/internal/layers"
	"github.com/okian/forge/plugin"
)

// Registry is the catalog a run is given: the layers it knows about, by the
// marker each claims.
//
// Named here as well as in the plugin package so that a binary's main needs one
// import rather than two. It is the same type — a layer registers into the
// catalog a driver runs, and there is nothing to convert.
type Registry = plugin.Registry

// Builtins returns a catalog holding the layers forge ships, ready to have
// more registered into it.
//
// A fresh one on every call, so that registering into it cannot reach another
// caller's. The layers themselves hold no state — a layer answers questions
// about a declaration and keeps nothing between them — so sharing them across
// catalogs costs nothing and is what makes the copy cheap.
func Builtins() *Registry { return layers.Builtins() }

// Main runs the command line this process was given and ends the process with
// the status it produced.
//
// The whole of a plugin binary's main. What it does not do is hold anything
// else: a tool whose only entry point ends the process can be tested only by
// starting one, so [Run] is where the work is and this is the two lines around
// it.
func Main(catalog *Registry) {
	// Ending the process is the whole of what this adds, and it is why the rest
	// is in Run: a function that exits can be called from a main and tested
	// from nowhere.
	os.Exit(Run(catalog, os.Args[1:], os.Stdout, os.Stderr))
}

// Run dispatches one command line against a catalog and returns the status to
// exit with, writing to the streams it is given.
//
// Three statuses and no more: the run did what was asked, the run reported
// something wrong with the input, or the command line did not name a run. A
// caller in a shell script can act on all three, and a caller in a test can
// read both streams and assert on either.
func Run(catalog *Registry, args []string, stdout, stderr io.Writer) int {
	return cli.Run(catalog, args, stdout, stderr)
}
