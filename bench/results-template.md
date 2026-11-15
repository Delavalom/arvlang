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
| 4a′ | `run.sh`'s last check: every run's report and summary line the same as Arv's first (`versions agree: …`) | | | — | agree | |
| 4b | Largest peak in flight over all runs | | | — | ≤ 20 for both | |
| 4c | Exit code (20 of 200 down is more than 5%) | | | — | 1 for both | |
| 5 | Escape probe: Arv denied with the same `--allow` flags, nothing at the sink; Go reaches the sink | | | — | Arv denied, Go succeeds | |
| 6 | Would you rather write the next ops script in Arv? Why, in one sentence | | | — | yes | |

**Per-run numbers** (paste `results.csv`, or its summary lines):

```
version,run,wall_s,user_s,sys_s,cpu_s,max_rss_kb,peak_in_flight,requests,exit_code,report_ok
```

**Notes for the reader.** `requests` is 230 for both versions on the default farm: 200 first attempts, a second for each of the 10 flaky targets, two more for each of the 10 dropped ones. A dropped target sends a response head and cuts the body short, so Go's keep-alive transport never re-sends a dropped GET behind its retry loop's back (it would if the connection closed before any response). Go reuses connections; Arv opens a new one for every `http_get`. That difference is real and stays in the measurement. The reports are compared without latency.
