// Command gen builds the dictionary internal/words embeds, from a release of
// the Automatically Generated Inflection Database.
//
// It is run by a maintainer and never by the build or by CI. What it writes is
// committed, so a contributor with no wordlist on disk builds forge fine — and
// it is written as text, so a change to the dictionary is a diff somebody can
// read: the words that were added, the words that went, and a provenance line
// saying which upstream release it came from, what that release hashed to, and
// how many entries survived.
//
// The compact form a lookup wants is not committed. internal/words builds it
// from this file the first time something asks the dictionary a question, which
// costs a fifth of a millisecond once per process and keeps the repository
// holding words rather than bytes.
//
// There is no network here either. The release is fetched once, by hand, from
// wordlist.aspell.net, and its path is given on the command line:
//
//	go run ./internal/words/gen -in ~/Downloads/agid-2016.01.19.tar.gz
//
// What survives the conversion is small on purpose. Nouns only, from entries
// the upstream part-of-speech database was sure about, without the proper nouns
// — forge matches a word without regard to its case, so an entry that can only
// be reached by a field named after a Greek genus is bytes spent on nothing.
// Then every pair the regular rules already get right is dropped, because a
// pair that agrees with the rules is bytes spent to reach the same answer. What
// is left is the exceptions, the vocabulary that answers whether a word ending
// in s is one, and the verbs whose final consonant doubles.
package main

import (
	"flag"
	"fmt"
	"os"
)

func main() {
	in := flag.String("in", "", "path to the AGID release archive")
	out := flag.String("out", "internal/words/english.txt", "path to write the dictionary to")
	version := flag.String("version", "", "upstream version, taken from the archive when empty")
	flag.Parse()

	if err := run(*in, *out, *version); err != nil {
		fmt.Fprintln(os.Stderr, "words/gen:", err)
		os.Exit(1)
	}
}

// run converts one archive into one asset.
func run(in, out, version string) error {
	if in == "" {
		return errNoArchive
	}

	held, err := read(in)
	if err != nil {
		return err
	}
	if version != "" {
		held.version = version
	}

	built, err := convert(held)
	if err != nil {
		return err
	}

	//nolint:gosec // The dictionary is committed source, read by every build.
	if err := os.WriteFile(out, encode(built), 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", out, err)
	}

	fmt.Println(built.provenance())
	return nil
}
