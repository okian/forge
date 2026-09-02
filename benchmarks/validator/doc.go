// Package validator measures a generated check against the reflective one
// everybody uses.
//
// It is a module of its own, and that is the whole reason it exists as a
// directory rather than as a file beside the example. The comparison is worth
// making — a generated check has to be worth generating, and the thing it has
// to be worth more than is go-playground/validator, which is what a Go
// programmer reaches for. Making it inside the main module would put that
// library and everything under it into the dependency graph of everybody who
// imports forge, for the sake of one benchmark nobody runs in their build.
//
// So it is nested, which the go command reads as not part of the module above
// it: `go build ./...` and `go test ./...` at the root do not see this, and the
// module above keeps its one direct dependency.
//
// What is compared is one subject with its rules written twice — once as
// forge's tags and once as go-playground's — over a value that satisfies them,
// which is the path a check spends its life on. The rules match field for field
// but one, and that one turns out to be most of the result.
//
// Run the benchmarks for the figures on your own machine; a ratio measured on
// somebody else's is not one worth writing down. The half of the result that
// does not depend on the machine is the allocation: the generated check does
// none, and the reflective one allocates six times on every call.
//
// Where those six come from is worth knowing, because it is not reflection.
// One is the subject itself, copied into the `any` that go-playground's entry
// point takes. The other five are inside its `email` rule, which parses the
// address with net/mail. Walking the fields reflectively allocates nothing at
// all. So the figure is mostly about what the two rules ask for rather than
// about how either check was built — see the note on the Email field beside the
// benchmarks, which is the one place the two do not ask the same thing.
package validator
