// Package ring generates fixed-capacity circular storage.
//
// A ring holds a declared number of the most recent elements and never grows.
// That is what it is for: a producer that outruns whatever reads it costs a
// bounded amount of memory rather than an increasing one, and the cost is
// decided where the container is declared rather than discovered in production.
//
// # What the options decide
//
// Two, and each of them decides the shape of two methods rather than the value
// of a flag.
//
// A capacity written as cap=1024 is fixed at build time: it becomes a constant,
// the constructor takes no argument, and no caller can be handed a container
// sized differently from the one it expected. A capacity left unwritten is
// passed to the constructor instead, which is what a caller sizing a buffer
// from configuration needs.
//
// An overflow policy of overwrite — the default — drops the oldest element to
// make room. A policy of error refuses instead, and the two methods that add
// elements return one. The signature is where the difference has to show: a
// container that can refuse and says so only through a comment is a container
// whose callers do not check.
//
// # What emptying keeps
//
// Reset drops the elements and keeps the buffer, so a container emptied and
// filled again allocates nothing the first filling did not. The capacity is
// part of what the type is rather than of what a value holds, and giving it up
// on the way to empty would mean a container that had been read into twice was
// a different size from one read into once.
//
// # Why the representation is not the author's to write
//
// The underlying type is a struct of a buffer, a head and a count, and those
// three are only meaningful together: a head past the end of the buffer, or a
// count larger than it, is a value of the type that every method reads wrongly.
// So this layer is not representation-transparent, and a declaration over it
// belongs in a spec file where forge owns the type. Writing it inline would put
// a type in the author's package that the language lets anybody construct
// wrongly, and nothing downstream could tell that from one built properly.
package ring
