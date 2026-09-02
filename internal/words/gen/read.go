package main

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"strings"
)

// errNoArchive reports a run with nothing to convert.
var errNoArchive = errors.New("-in is required: the path to an AGID release archive")

// release is one upstream archive, read into memory.
type release struct {
	// version is the release, taken from the directory the archive unpacks
	// into, which is where upstream puts it.
	version string

	// sum is the archive's SHA-256, recorded so that the asset says which bytes
	// produced it rather than only which release name.
	sum string

	// inflections is the contents of infl.txt.
	inflections string
}

// read opens an AGID release archive and returns what the converter needs from
// it.
//
// The whole archive is hashed rather than the one file taken out of it, because
// what a reader of the provenance line wants to check is the download they can
// fetch again.
func read(at string) (release, error) {
	held, err := os.ReadFile(at) //nolint:gosec // A path a maintainer typed.
	if err != nil {
		return release{}, fmt.Errorf("reading %s: %w", at, err)
	}

	sum := sha256.Sum256(held)

	out := release{sum: hex.EncodeToString(sum[:])}
	if out.version, out.inflections, err = unpack(held); err != nil {
		return release{}, fmt.Errorf("reading %s: %w", at, err)
	}
	return out, nil
}

// unpack finds infl.txt in a gzipped tar, and the release name around it.
func unpack(held []byte) (string, string, error) {
	zipped, err := gzip.NewReader(strings.NewReader(string(held)))
	if err != nil {
		return "", "", err
	}
	defer func() { _ = zipped.Close() }()

	archive := tar.NewReader(zipped)
	for {
		entry, err := archive.Next()
		if errors.Is(err, io.EOF) {
			return "", "", errors.New("the archive holds no infl.txt")
		}
		if err != nil {
			return "", "", err
		}
		if path.Base(entry.Name) != "infl.txt" {
			continue
		}

		body, err := io.ReadAll(archive)
		if err != nil {
			return "", "", err
		}
		return strings.TrimPrefix(path.Dir(entry.Name), "agid-"), string(body), nil
	}
}
