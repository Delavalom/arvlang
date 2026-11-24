#!/usr/bin/env bash
# Startup time of the arv binary (roadmap 03-benchmarks.md §B2), with
# hyperfine (https://github.com/sharkdp/hyperfine): 3 warmup runs, then
# RUNS timed runs of each command, by the load rules in bench/lib.sh.
#
#   bench/startup.sh
#
# Environment: RUNS (default 30), MAX_LOAD, BENCH_RESULTS, BENCH_HOST.
# Output: bench/results/<date>-<host>-<commit>-startup/ with env.txt,
# hyperfine.json and hyperfine.md.
set -euo pipefail

REPO=$(cd "$(dirname "$0")/.." && pwd)
. "$REPO/bench/lib.sh"

RUNS=${RUNS:-30}
MAX_LOAD=${MAX_LOAD:-$(default_max_load)}

if ! command -v hyperfine >/dev/null 2>&1; then
	echo "bench: hyperfine is not installed (brew install hyperfine, or apt install hyperfine)" >&2
	exit 2
fi

check_load
OUT=$(results_dir "$REPO" startup)
write_env "$OUT"
echo "runs=$RUNS" >>"$OUT/env.txt"
echo "hyperfine=$(hyperfine --version)" >>"$OUT/env.txt"

BUILD=$(mktemp -d)
trap 'rm -rf "$BUILD"' EXIT
(cd "$REPO" && go build -o "$BUILD/arv" ./cmd/arv)
printf '' >"$BUILD/empty.arv"
printf 'puts(1 + 1);\n' >"$BUILD/one.arv"

(cd "$REPO" && hyperfine --warmup 3 --runs "$RUNS" -N \
	--export-json "$OUT/hyperfine.json" --export-markdown "$OUT/hyperfine.md" \
	-n 'arv version' "$BUILD/arv version" \
	-n 'arv run empty.arv' "$BUILD/arv run $BUILD/empty.arv" \
	-n 'arv run one.arv' "$BUILD/arv run $BUILD/one.arv" \
	-n 'arv check uptime.arv' "$BUILD/arv check examples/uptime.arv")

finish_env "$OUT"
cat "$OUT/hyperfine.md"
echo "results: $OUT"
