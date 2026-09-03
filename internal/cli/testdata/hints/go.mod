module hintsclifixture

go 1.27.0

require github.com/okian/forge v0.0.0

// The fixture is written against the markers forge really ships, so that the
// hint matcher is exercised on the functions an author would write.
replace github.com/okian/forge => ../../../..
