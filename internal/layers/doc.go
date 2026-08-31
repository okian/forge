// Package layers is forge's own catalog: every layer it ships, gathered into
// the registry a run is given.
//
// It sits between the plugin surface and the layers themselves. A layer is
// written against [github.com/okian/forge/internal/layer] and knows nothing
// about the rest of the catalog; the catalog knows every layer and nothing
// about the interface's internals. Assembling them anywhere else would make the
// package that defines what a layer is depend on the layers, which is the one
// import that cannot exist — the interface goes public and forge's own catalog
// does not go with it.
//
// A layer that has not been written yet is a stub: something that knows its
// marker, its kind, its options and its shape, and reports that it generates
// nothing. That is what lets resolution, composition, explain and list be built
// and tested against the whole catalog rather than against whichever layer
// happens to exist, and it is why a declaration naming a marker forge plainly
// ships is answered with what is missing rather than with silence.
package layers
