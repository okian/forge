module github.com/okian/forge/x/csv

go 1.27.0

require github.com/okian/forge v0.0.0

require (
	golang.org/x/mod v0.40.0 // indirect
	golang.org/x/sync v0.22.0 // indirect
	golang.org/x/tools v0.49.0 // indirect
)

// Replaced by the checkout above rather than required at a version, because
// there is no released version to require: nothing in this repository is tagged
// yet. What that costs is that this module cannot be fetched — a consumer's own
// module ignores a dependency's replace directives, so what they would see is a
// requirement on a version that does not exist.
//
// Publishing it is four steps in one order: tag the root module; require that
// version here, delete this line and run `make tidy`; commit that, since a tag
// names a commit and this is the commit whose requirements a consumer reads;
// then tag x/csv, under its own version and prefixed with this directory. See
// docs/releasing.md.
replace github.com/okian/forge => ../..
