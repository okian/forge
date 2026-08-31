// Package inner is a module of its own, nested inside the directory of the one
// being generated for and sharing its import path prefix.
package inner

// Thing belongs to somebody else, whatever its path begins with.
type Thing struct{ Name string }
