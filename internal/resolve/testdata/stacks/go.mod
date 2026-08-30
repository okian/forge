module stacksfixture

go 1.27.0

require github.com/okian/forge v0.0.0

// The fixture is written against the markers forge really ships, so that
// resolution is exercised on the declarations an author would write rather than
// on a stand-in that could quietly disagree with them.
replace github.com/okian/forge => ../../../..
