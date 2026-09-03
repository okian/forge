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
// module above keeps the two dependencies it has.
//
// What is compared is one subject with the same rules written twice — once as
// forge's tags and once as go-playground's — over a value that satisfies them,
// which is the path a check spends its life on. See the benchmarks for what the
// numbers turned out to be and why the honest claim is narrower than the one
// the plan started with.
package validator
