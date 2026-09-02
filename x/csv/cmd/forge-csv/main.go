// Command forge-csv is forge with the CSV transport linked in.
//
// It takes the same command line as forge's own binary, walks the same packages
// and writes the same files — and a declaration naming
// [github.com/okian/forge.Csv] generates rather than being reported as work
// forge has not done yet.
//
// Install it beside forge, or run it out of the module:
//
//	go run github.com/okian/forge/x/csv/cmd/forge-csv generate ./...
//
// This is the whole of what linking a layer takes, and it is here to be read as
// much as to be run: a binary that means to know about a layer is one somebody
// linked it into, so there is no plugin file to drop in and no directory to
// scan.
package main

import (
	"github.com/okian/forge/driver"
	"github.com/okian/forge/x/csv"
)

func main() {
	catalog := driver.Builtins()
	catalog.MustRegister(csv.New())

	driver.Main(catalog)
}
