#!/usr/bin/env bash
#
# Hold the embedded dictionary and the forge binary to the sizes recorded for
# them.
#
# Two things are being protected and they are different. The dictionary is data
# somebody may upgrade, and an upgrade that brings a megabyte with it is one
# every user of the tool pays for without being asked. The binary is everything
# else, and a budget on the asset alone would be satisfied by a change that
# doubled the generator.
#
# A ceiling rather than a band, which is the difference from scripts/bench.sh. A
# benchmark that spends far less than its budget has usually stopped doing the
# work; a binary that is smaller than its budget is just smaller.
#
# Environment:
#   SIZE_BUDGET  path to the budget file (default scripts/size.txt)

set -euo pipefail

budget="${SIZE_BUDGET:-scripts/size.txt}"

if [ ! -f "${budget}" ]; then
	echo "size: no budget file at ${budget}" >&2
	exit 1
fi

built="$(mktemp -d)"
trap 'rm -rf "${built}"' EXIT

go build -o "${built}/forge" ./cmd/forge

bad=0

while read -r what limit; do
	case "${what}" in
	'' | '#'*) continue ;;
	esac

	case "${what}" in
	cmd/forge) at="${built}/forge" ;;
	*) at="${what}" ;;
	esac

	if [ ! -f "${at}" ]; then
		echo "size: ${what} has a budget and does not exist" >&2
		bad=1
		continue
	fi

	held="$(wc -c <"${at}" | tr -d ' ')"
	printf 'size: %-28s %9s bytes (budget %s)\n' "${what}" "${held}" "${limit}"

	if [ "${held}" -gt "${limit}" ]; then
		echo "size: ${what} is ${held} bytes, budget ${limit}" >&2
		bad=1
	fi
done < <(sed 's/#.*//' "${budget}")

if [ "${bad}" -ne 0 ]; then
	echo "size: over budget" >&2
	exit 1
fi
