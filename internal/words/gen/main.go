// Command gen builds the dictionary internal/words embeds, from a release of
// the Automatically Generated Inflection Database.
//
// It is run by a maintainer and never by the build or by CI. The asset it
// writes is committed, so a contributor with no wordlist on disk builds forge
// fine, and a change to the dictionary is a reviewable line in the pull request
// that makes it: the provenance the converter records says which upstream
// release it read, what that release hashed to, and how many entries survived.
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
	out := flag.String("out", "internal/words/english.bin", "path to write the asset to")
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

	asset, err := encode(built)
	if err != nil {
		return err
	}

	//nolint:gosec // The asset is committed source, read by every build.
	if err := os.WriteFile(out, asset, 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", out, err)
	}

	fmt.Println(built.provenance())
	return nil
}
