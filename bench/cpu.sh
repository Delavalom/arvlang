#!/usr/bin/env bash
# CPU-bound speed benchmarks (BenchmarkSpeed in arv/speed_bench_test.go),
# run by the rules in roadmap 03-benchmarks.md §B0.
#
#   bench/cpu.sh                      # both engines, COUNT runs each, interleaved
#   BASE=origin/main bench/cpu.sh     # this tree against BASE, interleaved ABAB
#
# Environment:
#   COUNT          runs per configuration (default 10; benchstat cannot
#                  reach p < 0.05 with fewer than about 6 a side)
#   WARMUP         calls of each binary thrown away first (default 1)
#   BASE           a git revision to compare this tree against
#   BENCH_FILTER   a regexp under BenchmarkSpeed (default: all programs)
#   MAX_LOAD       refuse above this 1-minute load (default 1.0 Linux, 3.0 macOS)
#   BENCH_RESULTS  where result directories go (default bench/results)
#   BENCH_HOST     a name for the machine in the directory name
#
# Output: bench/results/<date>-<host>-<commit>-cpu/ with env.txt, the raw
# benchmark output of each configuration, and benchstat's comparison.
# Test binaries are built once and then run one iteration at a time,
# alternating, so slow drift in the machine hits both sides alike.
set -euo pipefail

REPO=$(cd "$(dirname "$0")/.." && pwd)
. "$REPO/bench/lib.sh"

COUNT=${COUNT:-10}
MAX_LOAD=${MAX_LOAD:-$(default_max_load)}
FILTER=${BENCH_FILTER:-.}
BASE=${BASE:-}

check_load
OUT=$(results_dir "$REPO" cpu)
write_env "$OUT"
echo "count=$COUNT" >>"$OUT/env.txt"
echo "filter=$FILTER" >>"$OUT/env.txt"
echo "warmup=${WARMUP:-1}" >>"$OUT/env.txt"

BUILD=$(mktemp -d)
cleanup() {
	if [ -n "${BASEWT:-}" ]; then git -C "$REPO" worktree remove --force "$BASEWT" >/dev/null 2>&1 || true; fi
	rm -rf "$BUILD"
}
trap cleanup EXIT

# Build the test binaries once, outside the timed loop.
(cd "$REPO" && go test -c -o "$BUILD/a.test" ./arv/)
if [ -n "$BASE" ]; then
	echo "base=$(git -C "$REPO" rev-parse "$BASE")" >>"$OUT/env.txt"
	BASEWT=$BUILD/base-src
	git -C "$REPO" worktree add --detach "$BASEWT" "$BASE" >/dev/null 2>&1
	(cd "$BASEWT" && go test -c -o "$BUILD/b.test" ./arv/)
	ANAME=head BNAME=base
else
	(cd "$REPO" && go test -c -tags arv_treewalk -o "$BUILD/b.test" ./arv/)
	ANAME=compiled BNAME=treewalk
fi

bench() { # BINARY -> one iteration of every selected program
	(cd "$REPO/arv" && "$1" -test.run '^$' -test.bench "BenchmarkSpeed/$FILTER" -test.benchmem -test.count 1 -test.timeout 30m)
}

# Warmup: WARMUP calls of each binary, thrown away (page cache, CPU state).
for _ in $(seq 1 "${WARMUP:-1}"); do
	bench "$BUILD/a.test" >/dev/null
	bench "$BUILD/b.test" >/dev/null
done

: >"$OUT/$ANAME.txt"
: >"$OUT/$BNAME.txt"
for i in $(seq 1 "$COUNT"); do
	if [ $((i % 2)) -eq 1 ]; then
		bench "$BUILD/a.test" >>"$OUT/$ANAME.txt"
		bench "$BUILD/b.test" >>"$OUT/$BNAME.txt"
	else
		bench "$BUILD/b.test" >>"$OUT/$BNAME.txt"
		bench "$BUILD/a.test" >>"$OUT/$ANAME.txt"
	fi
done

BS=$(benchstat_bin)
(cd "$OUT" && "$BS" "$BNAME=$BNAME.txt" "$ANAME=$ANAME.txt") >"$OUT/benchstat.txt"
finish_env "$OUT"
cat "$OUT/benchstat.txt"
echo "results: $OUT"
