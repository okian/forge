#!/usr/bin/env bash
#
# Fuzz every fuzz target in the module, one at a time, for a fixed time each.
#
# One at a time because the go command allows one target per run: -fuzz takes a
# pattern, and a pattern matching two targets is an error rather than two runs.
# So the targets are found rather than listed — a target added to a package is
# fuzzed by this without anybody remembering to add it here, which is the only
# arrangement that survives the file being written once and read never.
#
# Found by package and name together rather than by name alone. Two packages may
# each declare a target under one name, and a list of names would fuzz one of
# them and report as though it had fuzzed both.
#
# What this gates is that no target reports a failure in the time it is given.
# It cannot gate that a target found something, because finding nothing is what
# a healthy run looks like: a fuzz run is a search, and the same seconds spent
# tomorrow search somewhere else.
#
# The corpus a run discovers goes to the build cache and is not committed. A
# failing case is different — the go command writes it into the package's
# testdata, and that one belongs in the repository under its own commit, because
# it is a bug with a name and a reproduction.
#
# Environment:
#   FUZZ_TIME   how long to spend on each target (default 30s)

set -euo pipefail

seconds="${FUZZ_TIME:-30s}"

# A target is a function whose name opens with Fuzz and which takes a
# *testing.F. Anchored at the start of a line, so that a mention inside a
# comment or a string is not one.
#
# Read out of the source because the go command offers no way to ask. `go test
# -list` lists tests and benchmarks and not fuzz targets, and a signature
# gofmt has put on one line is what this needs to see.
found="$(grep -rE '^func Fuzz[A-Za-z0-9_]*\(f \*testing\.F\) \{' --include='*_test.go' . |
	sed -E 's|^\./(.*)/[^/]*_test\.go:func (Fuzz[A-Za-z0-9_]*)\(.*|\1 \2|' | sort -u)"

if [ -z "${found}" ]; then
	echo "fuzz: no fuzz targets found in this module" >&2
	exit 1
fi

# Read a line at a time, so that a directory holding a space is one directory.
while read -r dir target; do
	[ -n "${target}" ] || continue

	echo "fuzz: ${target} in ./${dir} for ${seconds}"
	go test "./${dir}" -run '^$' -fuzz "^${target}\$" -fuzztime "${seconds}"
done <<<"${found}"

echo "fuzz: every target survived ${seconds}"
