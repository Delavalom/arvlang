# Phase 5, task p5.9: `bench/run.sh`

> **For agentic workers:** this file is one task, p5.9. Do it test first, exactly as written; if something here does not match the code you find, stop and report instead of improvising. Prerequisite: tasks p5.1 to p5.8 are committed in your tree. Start every shell command with `cd <your worktree> &&` and use absolute paths: the shell's working directory does not persist. Before committing: `gofmt -l .` prints nothing, `go vet ./...` is clean and `go test -race ./...` is green, and `make bench-test` is green. End the commit message with a blank line and then `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`. Never use `--no-verify` and never push; keep Go files under 15 KB and markdown under 18 KB. `go.mod` says `go 1.24.0` (toolchain 1.26.2): `t.Chdir`, `min` and `max` are available; do not use `testing/synctest` or `sync.WaitGroup.Go`. Before a long test run, check `df -h /System/Volumes/Data`; below 1 GB free, stop and report (only `go clean -cache` may be deleted). This machine is heavily loaded: keep wall-clock bounds generous.
>
> Every code block below was applied, in task order, to a scratch copy of `feat/phases-3-8` (Phases 3 and 4 merged) and passed `gofmt -l .`, `go vet ./...` and `go test -race ./...` (including `e2e/`), and `bench/uptime-go`'s own `go vet` and `go test -race`. Design: `12-phase5-gate.md`. Deviations from it, and why: `p5-plan-1.md`. **Do not measure or write the gate verdict**: a separate step does that.

### Task p5.9: the benchmark script and the results template

`bench/run.sh` builds `arv`, `farm` and `uptime-go` into `$BENCH_OUT/bin` and starts one farm (the gate's defaults, plus `FARM_FLAGS`). Then, `RUNS` times (default 10), it runs each version, alternating which goes first. Before each run it resets the farm. Each run executes in `$BENCH_OUT/work` under `/usr/bin/time -l` (macOS) or `-v` (Linux), with `-o` so the program's own stderr stays apart. It records:
- wall time, user and system CPU, and peak RSS (in KiB on both systems);
- the farm's peak in flight and its request total;
- the exit code;
- whether `report.json` matches the farm's expectation (`farm compare`).

It writes one row per run to `results.csv`, then prints both versions' code-line counts (not blank, not `//`) and per-version medians, the largest peak, the number of wrong reports and the exit codes seen. The Arv version runs with exactly `--allow fs_read=targets.txt --allow fs_write=out --allow net=127.0.0.1:PORT`.

`bench/results-template.md` is the table the measuring step fills in, one row per decision criterion.

The test runs the script once per version on a 40-target, 1 to 5 ms farm (4 of 40 down, so both exit 1). It skips under `-short`, because it builds three binaries.

**Files:**
- Create: `bench/run.sh` (executable)
- Create: `bench/results-template.md`
- Create (test): `bench/run_test.go`
- Modify: `Makefile`, `.gitignore`

- [ ] **Step 1: Write the failing test**

Create `bench/run_test.go`:

```go
// Package bench holds the Phase 5 gate benchmark: run.sh, the farm, the
// Go baseline (its own module, in uptime-go) and the escape probe.
package bench

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// run.sh, end to end, on a small fast farm: one run of each version,
// each with a correct report, at most 20 requests in flight, and both
// exiting 1 (4 of 40 targets down is more than 5%).
func TestRunScript(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the binaries")
	}
	out := t.TempDir()
	cmd := exec.Command("bash", "run.sh")
	cmd.Env = append(os.Environ(), "RUNS=1", "FARM_FLAGS=-n 40 -latency 1ms-5ms", "BENCH_OUT="+out)
	b, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("run.sh: %v\n%s", err, b)
	}
	csv, err := os.ReadFile(filepath.Join(out, "results.csv"))
	if err != nil {
		t.Fatal(err)
	}
	rows := strings.Split(strings.TrimSpace(string(csv)), "\n")
	if len(rows) != 3 || rows[0] != "version,run,wall_s,user_s,sys_s,cpu_s,max_rss_kb,peak_in_flight,requests,exit_code,report_ok" {
		t.Fatalf("results.csv:\n%s", csv)
	}
	seen := map[string]bool{}
	for _, row := range rows[1:] {
		f := strings.Split(row, ",")
		if len(f) != 11 {
			t.Fatalf("row %q", row)
		}
		seen[f[0]] = true
		wall, _ := strconv.ParseFloat(f[2], 64)
		rss, _ := strconv.Atoi(f[6])
		peak, _ := strconv.Atoi(f[7])
		if wall <= 0 || rss <= 0 || peak < 1 || peak > 20 || f[9] != "1" || f[10] != "yes" {
			t.Errorf("row %q: want wall and rss > 0, peak 1 to 20, exit 1, report yes", row)
		}
	}
	if !seen["arv"] || !seen["go"] {
		t.Errorf("versions %v, want arv and go", seen)
	}
	for _, want := range []string{"code lines: arv ", "arv: median wall ", "go: median wall ", "wrong reports 0"} {
		if !strings.Contains(string(b), want) {
			t.Errorf("summary lacks %q:\n%s", want, b)
		}
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test -run TestRunScript ./bench/`
Expected: FAIL: `run.sh: exit status 127` and `bash: run.sh: No such file or directory`.

- [ ] **Step 3: Implement**

Create `bench/run.sh` and make it executable (`chmod +x bench/run.sh`):

```bash
#!/usr/bin/env bash
# Phase 5 gate benchmark: runs examples/uptime.arv and bench/uptime-go
# against one local farm, RUNS times each, alternating which goes first,
# and records wall time, CPU time, peak RSS, the farm's peak in-flight
# requests and whether each report.json matches the farm's expectation.
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
mkdir -p "$BIN" "$WORK/out"
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
echo "results: $OUT/results.csv"
```

Create `bench/results-template.md`:

````markdown
# Phase 5 gate results (template)

Fill this in from `bench/run.sh` (10 runs of each version) and `bench/escape/probe.sh`, then copy it into `docs/research/2026-phase5-gate.md` with the decision. The criteria are in `docs/superpowers/plans/2026-11-09-arv-runtime/12-phase5-gate.md`; they were fixed before measuring and must not be tuned after.

**Setup.** Machine: … · OS: … · Go: … · Load average during the runs: … · Farm: the defaults (`-n 200 -latency 50ms-400ms -fail 0.1 -flaky 0.05 -seed 1`) unless stated · Commit: …

**Reproduce.** `make bench` (or `RUNS=10 bench/run.sh`), then `make escape`. Raw rows: `bench/out/results.csv`; probe output: `bench/out/escape/summary.txt`.

| # | Criterion | Arv | Go | Arv / Go | Threshold | Holds? |
|---|---|---|---|---|---|---|
| 1 | Code lines (not blank, not a `//` comment) | | | | ≤ 0.5 | |
| 2 | Median wall time (s) | | | | ≤ 1.2 | |
| 3a | Median peak RSS (KiB) | | | | ≤ 3 | |
| 3b | Median CPU time, user + sys (s) | | | | ≤ 5 | |
| 4a | Runs whose report matched the farm (`report_ok`) | /10 | /10 | — | 10/10 for both | |
| 4b | Largest peak in flight over all runs | | | — | ≤ 20 for both | |
| 4c | Exit code (20 of 200 down is more than 5%) | | | — | 1 for both | |
| 5 | Escape probe: Arv denied with the same `--allow` flags, nothing at the sink; Go reaches the sink | | | — | Arv denied, Go succeeds | |
| 6 | Would you rather write the next ops script in Arv? Why, in one sentence | | | — | yes | |

**Per-run numbers** (paste `results.csv`, or its summary lines):

```
version,run,wall_s,user_s,sys_s,cpu_s,max_rss_kb,peak_in_flight,requests,exit_code,report_ok
```

**Notes for the reader.** `requests` differs between the versions: Go's `http.Transport` retries a GET on a reused keep-alive connection that closed without a response, on its own, while Arv opens a new connection for every `http_get`. The reports are compared without latency.
````

In `Makefile`, replace `.PHONY: build test e2e bench-test` with `.PHONY: build test e2e bench-test bench`, and append:

```make

# Phase 5 gate benchmark; output goes to bench/out/.
bench:
	bench/run.sh
```

Append to `.gitignore`:

```text

# Phase 5 benchmark output (bench/run.sh, bench/escape/probe.sh)
bench/out/
```

- [ ] **Step 4: Run to verify it passes**

Run: `bash -n bench/run.sh`, `go test -race -run TestRunScript -v ./bench/`, then `gofmt -l .`, `go vet ./...` and `go test -race ./...`. Optionally smoke it by hand: `RUNS=1 FARM_FLAGS="-latency 5ms-40ms" BENCH_OUT=$(mktemp -d) bench/run.sh`. Do not run the full 10-run benchmark or record its numbers: the measuring step does.
Expected: PASS everywhere; `git status` shows no `bench/out/`.

- [ ] **Step 5: Commit**

```bash
git add bench/run.sh bench/results-template.md bench/run_test.go Makefile .gitignore
git commit -m "feat(bench): run.sh and the results template

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```
