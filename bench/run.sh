#!/usr/bin/env bash
# Phase 5 gate benchmark: runs examples/uptime.arv and bench/uptime-go
# against one local farm, RUNS times each, alternating which goes first,
# and records wall time, CPU time, peak RSS, the farm's peak in-flight
# requests and whether each report.json matches the farm's expectation;
# at the end it checks that every run's report and summary line match.
#
#   bench/run.sh                     # 10 runs each, the gate's farm
#   RUNS=3 FARM_FLAGS="-seed 7" bench/run.sh
#
# Environment: RUNS (default 10), FARM_FLAGS (extra farm flags, after the
# gate's defaults), BENCH_OUT (default bench/out). Results go to
# $BENCH_OUT/results.csv; a summary is printed at the end. The farm listens
# on 127.0.0.1 only; nothing touches the public internet.
set -euo pipefail

REPO=$(cd "$(dirname "$0")/.." && pwd)
RUNS=${RUNS:-10}
OUT=${BENCH_OUT:-$REPO/bench/out}
BIN=$OUT/bin
WORK=$OUT/work
rm -rf "$WORK/runs"
mkdir -p "$BIN" "$WORK/out" "$WORK/runs"
OUT=$(cd "$OUT" && pwd)
BIN=$OUT/bin
WORK=$OUT/work

case "$(uname -s)" in
Darwin) TIMEFLAG=-l ;;
*) TIMEFLAG=-v ;;
esac
[ -x /usr/bin/time ] || { echo "run.sh: /usr/bin/time is required" >&2; exit 2; }

echo "building into $BIN" >&2
(cd "$REPO" && go build -o "$BIN/arv" . && go build -o "$BIN/farm" ./bench/farm/cmd/farm)
(cd "$REPO/bench/uptime-go" && go build -o "$BIN/uptime-go" .)

# shellcheck disable=SC2086
"$BIN/farm" -targets "$WORK/targets.txt" -expect "$WORK/expect.json" ${FARM_FLAGS:-} >"$WORK/farm.url" 2>"$WORK/farm.err" &
FARM_PID=$!
trap 'kill $FARM_PID 2>/dev/null || true' EXIT
for _ in $(seq 300); do
	[ -s "$WORK/farm.url" ] && break
	sleep 0.1
done
BASE=$(head -1 "$WORK/farm.url")
[ -n "$BASE" ] || { echo "run.sh: the farm did not start:" >&2; cat "$WORK/farm.err" >&2; exit 1; }
HOSTPORT=${BASE#http://}
echo "farm at $BASE" >&2

# measure NAME RUN CMD...: one run in $WORK, appended to results.csv.
measure() {
	local name=$1 run=$2
	shift 2
	curl -fsS -X POST "$BASE/reset" >/dev/null
	rm -f "$WORK/out/report.json"
	local code=0
	(cd "$WORK" && /usr/bin/time $TIMEFLAG -o "$WORK/time.txt" "$@" >"$WORK/$name.stdout" 2>"$WORK/$name.stderr") || code=$?
	local stats peak total ok
	stats=$(curl -fsS "$BASE/stats")
	peak=$(echo "$stats" | sed -E 's/.*"peak":([0-9]+).*/\1/')
	total=$(echo "$stats" | sed -E 's/.*"total":([0-9]+).*/\1/')
	ok=no
	if [ -f "$WORK/out/report.json" ] && "$BIN/farm" compare "$WORK/expect.json" "$WORK/out/report.json" >/dev/null; then
		ok=yes
	fi
	# Kept for the comparison between versions at the end.
	cp "$WORK/$name.stdout" "$WORK/runs/$name.$run.stdout"
	[ -f "$WORK/out/report.json" ] && cp "$WORK/out/report.json" "$WORK/runs/$name.$run.report.json"
	awk -v name="$name" -v run="$run" -v peak="$peak" -v total="$total" -v code="$code" -v ok="$ok" '
		/ real / { wall = $1; user = $3; sys = $5 }                      # macOS
		/maximum resident set size/ { rss = $1 / 1024 }                  # macOS: bytes
		/Elapsed \(wall clock\)/ { n = split($NF, p, ":"); wall = (n == 3 ? p[1] * 3600 + p[2] * 60 + p[3] : p[1] * 60 + p[2]) }
		/User time \(seconds\)/ { user = $NF }
		/System time \(seconds\)/ { sys = $NF }
		/Maximum resident set size \(kbytes\)/ { rss = $NF }             # Linux: KiB
		END { printf "%s,%d,%.2f,%.2f,%.2f,%.2f,%d,%d,%d,%d,%s\n", name, run, wall, user, sys, user + sys, rss, peak, total, code, ok }
	' "$WORK/time.txt" >>"$OUT/results.csv"
}

echo "version,run,wall_s,user_s,sys_s,cpu_s,max_rss_kb,peak_in_flight,requests,exit_code,report_ok" >"$OUT/results.csv"
ARV=("$BIN/arv" run --allow fs_read=targets.txt --allow fs_write=out --allow "net=$HOSTPORT" "$REPO/examples/uptime.arv")
for i in $(seq "$RUNS"); do
	if [ $((i % 2)) -eq 1 ]; then
		measure arv "$i" "${ARV[@]}"
		measure go "$i" "$BIN/uptime-go"
	else
		measure go "$i" "$BIN/uptime-go"
		measure arv "$i" "${ARV[@]}"
	fi
	echo "run $i of $RUNS done" >&2
done

# Code lines: not blank, not a // comment.
loc() { grep -v '^[[:space:]]*$' "$1" | grep -cv '^[[:space:]]*//'; }
echo "code lines: arv $(loc "$REPO/examples/uptime.arv"), go $(loc "$REPO/bench/uptime-go/main.go")"
# Medians per version, and the worst peak, any wrong report, the exit codes.
for v in arv go; do
	grep "^$v," "$OUT/results.csv" | awk -F, -v v="$v" '
		{ wall[NR] = $3; cpu[NR] = $6; rss[NR] = $7; if ($8 > peak) peak = $8; if ($11 != "yes") bad++; codes[$10] = 1 }
		function median(a, n,   i, j, t) {
			for (i = 1; i <= n; i++) for (j = i + 1; j <= n; j++) if (a[j] < a[i]) { t = a[i]; a[i] = a[j]; a[j] = t }
			return n % 2 ? a[(n + 1) / 2] : (a[n / 2] + a[n / 2 + 1]) / 2
		}
		END {
			c = ""; for (k in codes) c = c (c == "" ? "" : " ") k
			printf "%s: median wall %.2fs, median cpu %.2fs, median rss %d KiB, max peak in flight %d, wrong reports %d, exit codes %s\n", v, median(wall, NR), median(cpu, NR), median(rss, NR), peak, bad + 0, c
		}'
done
# The versions against each other, not only against expect.json: every
# run's report (latency aside) and printed summary must match Arv's first.
agree() {
	local first=$WORK/runs/arv.1 n=0 diff=""
	for f in "$WORK"/runs/*.stdout; do
		local r=${f%.stdout}
		n=$((n + 1))
		if ! cmp -s "$first.stdout" "$f" || [ ! -f "$r.report.json" ] ||
			! "$BIN/farm" compare "$first.report.json" "$r.report.json" >/dev/null 2>&1; then
			diff="$diff $(basename "$r")"
		fi
	done
	if [ -z "$diff" ]; then
		echo "versions agree: $n reports and summaries, all the same as arv run 1"
	else
		echo "versions differ: of $n runs, these differ from arv run 1 (report or summary):$diff"
	fi
}
agree
echo "results: $OUT/results.csv"
