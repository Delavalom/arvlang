# Phase 5: The gate (demo, Go baseline, decision)

**Status:** design and task outline. This phase is mostly measurement. Its output is a decision, written down.

**Goal:** Find out with numbers whether Arv beats plain Go for the job it was built for. If it doesn't, stop adding language features and ship the runtime as a Go library. Nothing built so far is wasted either way.

## The scenario

An uptime checker:
1. Read a list of 200 URLs from `targets.txt` (`fs_read`).
2. Check each one concurrently, at most 20 in flight, with a 2 s timeout per check and up to 2 restarts on effect errors.
3. Write `report.json` with status and latency per URL (`fs_write` on `./out` only).
4. Exit non-zero if more than 5% are down.

## Deliverables

| File | What |
|---|---|
| `bench/farm/main.go` | Local HTTP server farm: `-n 200 -latency 50ms-400ms -fail 0.1 -flaky 0.05`. It serves `/t/{i}`, and `/stats` reports peak in-flight requests and the total. The benchmark never touches the public internet. |
| `examples/uptime.arv` | The Arv version. Target ≤ 40 lines. |
| `bench/uptime-go/main.go` | The idiomatic Go version: `errgroup`, a semaphore, `context.WithTimeout`, a retry loop. Write it as you would for production, not as a strawman. |
| `bench/run.sh` | Runs both against the farm 10 times each; collects wall time, peak RSS (`/usr/bin/time -l` on macOS, `-v` on Linux), CPU time, peak in-flight requests (from `/stats`) and correctness (identical `report.json` modulo latency). |
| `bench/escape/` | The security probe. Each version gets a one-line change that tries to read `~/.ssh/id_ed25519` and POST it to a host that isn't in the list. Record what happens. |
| `docs/research/2026-phase5-gate.md` | Numbers, the probe results, and the decision. |

## Decision criteria (agree on them before measuring)

Continue to Phases 6–8 as a language **only if all of these hold:**

1. **Concision:** the Arv script is ≤ 50% of the Go version's non-blank, non-comment lines.
2. **Speed where it matters:** median wall time ≤ 1.2× Go. The work is I/O-bound, so the interpreter shouldn't dominate.
3. **Overhead is sane:** peak RSS ≤ 3× Go, and CPU time ≤ 5× Go.
4. **Correctness:** identical reports; peak in-flight ≤ 20 for both.
5. **Safety is real:** the Arv probe fails with `capability denied` *without* changing the `--allow` flags; the Go probe succeeds (expected, and it's the point).
6. **The honest one:** you'd rather write the next ops script in Arv. Write a sentence on why.

**If 1–5 hold but 6 doesn't,** the syntax is the problem, not the runtime. Spend one week on ergonomics (`.field` access, string interpolation, `&&`/`||`, `<=`/`>=`) and re-run the gate once.

**If 2 or 3 fail,** profile first. Closure compilation (Phase 8) usually gives 2–5× on tree-walkers. Re-run the gate after it.

**If 1 or 5 fail,** pivot: extract `arv/runtime` plus the capability-checked effects as a Go library (`arvrt`: `arvrt.Scope`, `arvrt.Go(ctx, caps, fn)`, `arvrt.HTTPGet(task, url)`). Phases 6–7 still apply, as library features.

## Task outline

1. `bench/farm` with its own tests (latency distribution, failure injection, `/stats`).
2. `examples/uptime.arv` + an evaluator test running it against an in-process farm.
3. `bench/uptime-go` + a test asserting the same report.
4. `bench/run.sh` + a results table template.
5. The `bench/escape` probes.
6. Run, write the gate doc, decide. **The decision goes in the doc and the PR description before any Phase 6 work starts.**
