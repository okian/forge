#!/usr/bin/env bash
#
# Run the module's benchmarks and hold each one to the budget recorded for it.
#
# A benchmark nobody reads is documentation, and documentation nobody reads is
# nothing. This turns the suite into a gate: every benchmark declares what it
# may spend, the run measures what it spent, and a change that costs more than
# it used to fails here rather than being noticed a year later by a user.
#
# What is gated and what is not:
#
#   allocs/op  gated exactly. An allocation count is a property of the code —
#              the same source on the same toolchain allocates the same number
#              of times — so a budget can be a number and a regression is never
#              noise.
#   B/op       gated with a small headroom, and only where the run allocated
#              at all. The accounting jitters by a few tens of bytes where a
#              map grows, which is far below any real regression.
#
#              The exception is what makes the gate usable at zero. B/op is a
#              total divided by the iteration count, so a cost paid once over
#              a run of two thousand reports as one byte per operation while
#              allocs/op rounds to none — and a budget of zero bytes has no
#              proportional headroom to absorb it, so the honest figure fails.
#              A run that allocated no times cannot have allocated bytes per
#              operation, so the byte ceiling is not applied there. The count
#              is what holds that case, and it holds it exactly.
#   ns/op      printed, never gated. A shared runner's timings vary by more
#              than most regressions do, and a gate that cries wolf is a gate
#              somebody switches off.
#
# The run has to be long enough that a fixed cost is not a per-operation one.
# A benchmark that pays for a buffer once and runs two hundred times reports a
# two-hundredth of that buffer per operation, which is a number about the run
# length rather than about the code — and a budget written around it moves every
# time somebody passes a different -benchtime. Long enough that the fixed costs
# round away is the only length a budget can be written against.
#
# A budget is a band and not a ceiling. Something spending far less than its
# budget has usually stopped doing the work rather than got faster — shrink the
# fixture a benchmark builds and every figure collapses, the gate stays green,
# and the file goes on asserting numbers nothing has produced since. So a figure
# that falls below half its budget fails too, and the fix for a real
# improvement is to write the new number down, which is where somebody reads it.
#
# Every benchmark must have a budget and every budget must name a benchmark
# that ran, so that neither a new measurement nor a deleted one slips past
# unread. That second half is why the defaults run the whole module: narrowing
# the run reports every budget outside it as one that did not run, which is
# true and is not what the person narrowing it wanted to hear.
#
# Environment:
#   BENCH_BUDGET  path to the budget file (default scripts/budget.txt)
#   BENCH_PKGS    packages to benchmark (default ./...)
#   BENCH_TIME    -benchtime value (default 2000x)
#   BENCH_FILTER  -bench value (default .)

set -euo pipefail

budget="${BENCH_BUDGET:-scripts/budget.txt}"
pkgs="${BENCH_PKGS:-./...}"
benchtime="${BENCH_TIME:-2000x}"
filter="${BENCH_FILTER:-.}"

# Bytes are compared with this much room above the budget, in parts per
# thousand, to absorb the accounting jitter described above.
headroom=10

if [ ! -f "${budget}" ]; then
	echo "bench: no budget file at ${budget}" >&2
	exit 1
fi

measured="$(mktemp)"
trap 'rm -f "${measured}"' EXIT

# -run '^$' so that no test runs: this is measuring, and a suite that also ran
# its tests would report the time they took as part of the package.
go test "${pkgs}" -run '^$' -bench="${filter}" -benchmem -benchtime="${benchtime}" -count=1 |
	tee "${measured}"

echo

awk -v budgetfile="${budget}" -v headroom="${headroom}" '
	# The budget file: one benchmark per line, as
	#
	#   <package> <benchmark> <allocs/op> <B/op>
	#
	# with # comments and blank lines ignored.
	BEGIN {
		while ((getline line < budgetfile) > 0) {
			sub(/#.*/, "", line)
			if (split(line, f, /[ \t]+/) < 4) continue
			key = f[1] " " f[2]
			allocs[key] = f[3]
			bytes[key] = f[4]
			declared[key] = 1
		}
		close(budgetfile)
	}

	# Each package that runs a benchmark prints its own header first, so the
	# package a result belongs to is the last one named.
	/^pkg:/ { pkg = $2; next }

	# BenchmarkName-12   200   3122 ns/op   224 B/op   9 allocs/op
	#
	# The -12 is the GOMAXPROCS the benchmark ran at, which is the machine
	# rather than the code and is not part of the name a budget is written
	# against.
	/^Benchmark/ && /allocs\/op/ {
		name = $1
		sub(/-[0-9]+$/, "", name)
		sub(/^Benchmark/, "", name)

		# Cleared per line rather than carried, so that a line missing one of
		# the two figures is reported as missing instead of being compared
		# against the benchmark before it.
		gotallocs = gotbytes = ""
		for (i = 1; i <= NF; i++) {
			if ($i == "allocs/op") gotallocs = $(i-1)
			if ($i == "B/op") gotbytes = $(i-1)
		}

		key = pkg " " name
		seen[key] = 1

		if (!declared[key]) {
			printf "bench: %s %s has no budget; add one to %s\n", pkg, name, budgetfile
			bad = 1
			next
		}

		if (gotallocs == "" || gotbytes == "") {
			printf "bench: %s %s reported one figure and not the other, so nothing here can hold it to anything\n",
				pkg, name
			bad = 1
			next
		}

		if (gotallocs + 0 > allocs[key] + 0) {
			printf "bench: %s %s allocated %s times, budget %s\n", pkg, name, gotallocs, allocs[key]
			bad = 1
		}

		# Only where something was allocated. See the note on B/op above: with
		# no allocations the figure is the run length showing through, and the
		# count above has already held the run to its budget exactly.
		ceiling = bytes[key] + (bytes[key] * headroom / 1000)
		if (gotbytes + 0 > ceiling) {
			if (gotallocs + 0 == 0) {
				printf "bench: %s %s reports %s bytes over %s with no allocations, which is the run length rather than the code\n",
					pkg, name, gotbytes, bytes[key]
			} else {
				printf "bench: %s %s allocated %s bytes, budget %s\n", pkg, name, gotbytes, bytes[key]
				bad = 1
			}
		}

		# The other side of the band. Only where there is room to fall by half:
		# a budget of one allocation has no meaningful floor, and holding it to
		# one would refuse the improvement that removes it.
		if (allocs[key] + 0 >= 2 && gotallocs + 0 < allocs[key] / 2) {
			printf "bench: %s %s allocated %s times against a budget of %s; write the new figure down\n",
				pkg, name, gotallocs, allocs[key]
			bad = 1
		}
		if (bytes[key] + 0 >= 2 && gotbytes + 0 < bytes[key] / 2) {
			printf "bench: %s %s allocated %s bytes against a budget of %s; write the new figure down\n",
				pkg, name, gotbytes, bytes[key]
			bad = 1
		}
	}

	END {
		# A budget for a benchmark that no longer runs is a line nobody will
		# ever delete, and reads in a review like a benchmark that is passing.
		for (key in declared) {
			if (!seen[key]) {
				printf "bench: %s has a budget but did not run; remove it from %s\n", key, budgetfile
				bad = 1
			}
		}

		if (bad) {
			print "bench: over budget"
			exit 1
		}
		print "bench: every benchmark is within its budget"
	}
' "${measured}"
