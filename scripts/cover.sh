#!/usr/bin/env bash
#
# Run the test suite with coverage and fail if total statement coverage falls
# below a threshold.
#
# Coverage is measured with -coverpkg=./... so that every package in the module
# contributes its statements to the total. Without it, a package carrying no
# test file is simply absent from the profile, and the gate would happily pass
# a module whose newest package is untested.
#
# Environment:
#   COVER_MIN      minimum total coverage percentage (default 90)
#   COVER_PROFILE  profile output path (default cover.out)

set -euo pipefail

threshold="${COVER_MIN:-90}"
profile="${COVER_PROFILE:-cover.out}"

case "${threshold}" in
'' | *[!0-9.]* | *.*.*)
	echo "coverage: COVER_MIN must be a number, got ${threshold}" >&2
	exit 1
	;;
esac

go test ./... -covermode=atomic -coverpkg=./... -coverprofile="${profile}"

if [ ! -s "${profile}" ]; then
	echo "coverage: ${profile} was not written" >&2
	exit 1
fi

# A profile holding nothing but its mode line means the module has no coverable
# statements yet. That is a pass, not a 0% failure.
if [ "$(grep -c . "${profile}")" -le 1 ]; then
	echo "coverage: module contains no coverable statements"
	exit 0
fi

total="$(go tool cover -func="${profile}" | awk '/^total:/ { sub(/%/, "", $NF); print $NF }')"

if [ -z "${total}" ]; then
	echo "coverage: could not read a total from ${profile}" >&2
	exit 1
fi

echo "coverage: ${total}% of statements (minimum ${threshold}%)"

awk -v total="${total}" -v threshold="${threshold}" 'BEGIN { exit !(total + 0 >= threshold + 0) }' || {
	echo "coverage: ${total}% is below the ${threshold}% minimum" >&2
	exit 1
}
