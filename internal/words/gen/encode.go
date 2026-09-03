package main

import (
	"bytes"
	"compress/flate"
	"encoding/binary"
	"maps"
	"slices"
)

// magic marks the body of the asset, and is checked by whatever reads it.
const magic = "FWD1"

// pair is one key and its value, as the asset stores them.
type pair struct{ key, value string }

// encode writes the asset: a provenance line in plain text, then a deflated
// body holding the four tables.
//
// The line is plain so that a change to the dictionary is a reviewable line in
// the diff that makes it rather than a wall of moved bytes. The body is
// deflated because it is a list of English words, which compresses to about a
// third of itself, and because nobody reads it either way.
func encode(from built) ([]byte, error) {
	var body bytes.Buffer
	body.WriteString(magic)

	for _, table := range [][]pair{
		sorted(from.plurals),
		sorted(from.singulars()),
		sorted(from.agents),
		known(from.vocabulary),
	} {
		write(&body, table)
	}

	var out bytes.Buffer
	out.WriteString(from.provenance())
	out.WriteByte('\n')

	zipped, err := flate.NewWriter(&out, flate.BestCompression)
	if err != nil {
		return nil, err
	}
	if _, err := zipped.Write(body.Bytes()); err != nil {
		return nil, err
	}
	if err := zipped.Close(); err != nil {
		return nil, err
	}

	return out.Bytes(), nil
}

// write puts one table into the body: how many entries, then each entry's key
// and value lengths, then every key and value laid end to end.
//
// Lengths ahead of the bytes so that a reader can walk the table once and take
// the whole blob as a single string, which is what lets a lookup answer with a
// slice of it and no allocation at all.
func write(into *bytes.Buffer, table []pair) {
	into.Write(binary.AppendUvarint(nil, uint64(len(table))))

	for _, one := range table {
		into.Write(binary.AppendUvarint(nil, uint64(len(one.key))))
		into.Write(binary.AppendUvarint(nil, uint64(len(one.value))))
	}
	for _, one := range table {
		into.WriteString(one.key)
		into.WriteString(one.value)
	}
}

// sorted turns a table into the ordered pairs the asset stores, which is what
// keeps two runs over one release from producing two assets.
func sorted(held map[string]string) []pair {
	out := make([]pair, 0, len(held))

	for _, key := range slices.Sorted(maps.Keys(held)) {
		out = append(out, pair{key: key, value: held[key]})
	}
	return out
}

// known turns a vocabulary into pairs with nothing on the other side, since
// what is being recorded is that the word exists.
func known(held []string) []pair {
	out := make([]pair, 0, len(held))

	for _, key := range slices.Sorted(slices.Values(held)) {
		out = append(out, pair{key: key})
	}
	return out
}
