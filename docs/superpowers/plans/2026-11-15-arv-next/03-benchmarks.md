# Benchmarks: a reproducible suite

Part of [`00-roadmap.md`](00-roadmap.md).

**What exists.**
- **The Phase 5 gate harness:** `bench/run.sh`, `bench/farm`, `bench/uptime-go` and `bench/escape`, with the results in `gate-report.md`. It ran 10 alternating rounds per version on a fixed farm, with criteria set before measuring. This is the method to keep.
- **Seven CPU programs:** `BenchmarkSpeed` in `arv/speed_bench_test.go`, run with `make bench-cpu` (`-count 5`).
- **The gaps.**
  - The 8.4 numbers were taken at load averages of 10 to 61, with raw output in `/tmp` (`16p84-speed.md` line 49), so nobody can reproduce or audit them.
  - Startup, recording overhead, budget overhead and agent workloads have no benchmarks at all.

## B0. Method and harness (S, Phase 9)

**Hardware**
- **Machine A:** the M1 Pro used so far, kept for continuity.
- **Machine B:** a dedicated Linux x86-64 host, bare metal rather than a VM. Pin its CPU frequency and turn turbo off, through `cpupower` or [perflock](https://go.dev/wiki/Benchmarks).
- Record the CPU model, kernel, Go version and commit in every result file.

**The load rule**
- Record the 1-minute load average before and after each run.
- Refuse to start above 1.0 on machine B or 3.0 on machine A, and discard any run that ended above it. The 8.4 runs would have been refused.

**Repetitions and statistics**
- At least 10 runs per configuration, interleaved ABAB, never AAAA then BBBB, as the gate did.
- Report the median with a 95% confidence interval. Never report the best of N ([Georges et al., OOPSLA'07](https://dri.es/files/oopsla07-georges.pdf)).
- For Go micro-benchmarks, use [benchstat](https://go.googlesource.com/perf/+/HEAD/cmd/benchstat): Mann-Whitney U, which prints `~` when p > 0.05.
- benchstat's p-value has a floor set by the sample size: 4 against 5 runs cannot reach p < 0.079. So use `-count=10` or more.
- Write new benchmarks with `b.Loop()` ([Go blog](https://go.dev/blog/testing-b-loop)).
- Use the sample-size method of [Kalibera & Jones, ISMM'13](https://kar.kent.ac.uk/id/document/3127886) to choose repetitions within a time budget.

**Warmup**
- Arv has no JIT, but the OS page cache, DNS and TLS state do warm up.
- Discard 3 warmup runs per configuration, and check for a trend inside a run. Do not assume a steady state ([Barrett et al., OOPSLA'17](https://arxiv.org/pdf/1602.00602)).

**CLI timings**
- Use [hyperfine](https://man.archlinux.org/man/hyperfine.1.en) with `--warmup 3 --runs 30 --export-json`.
- Measure peak RSS with `/usr/bin/time -l` on macOS and `-v` on Linux, as `bench/run.sh` does.

**Storage**
- Raw results go in `bench/results/<date>-<machine>-<commit>/` (JSON or CSV, plus an `env.txt`), committed to the repo.
- A new `bench/report` command renders the tables from them. Nothing lives only in `/tmp`.

## B1. Interpreter and compiler speed (S)

**Programs**
- Keep the seven `BenchmarkSpeed` programs.
- Port the parts of [Are We Fast Yet](https://stefan-marr.de/papers/dls-marr-et-al-cross-language-compiler-benchmarking-are-we-fast-yet/) that Arv can express:
  - today: Sieve, Permute, Queens, Towers, List, Bounce, Storage, Json (once `parse_json` exists);
  - after decision D3 (floats): Mandelbrot, NBody;
  - after Phase 10 (`while`, hashes as records): Richards, DeltaBlue, Havlak.
- AWFY was designed to compare language implementations across languages, so it is the right suite.

**Configurations**
- Compiled against the tree-walker (`-tags arv_treewalk`).
- Parse and compile time separately from run time: 1 KB, 100 KB and 10 MB sources. A7 makes compiling cancellable, so measure it before and after.

**Context, not a race**
- The same programs on [Starlark-go](https://github.com/google/starlark-go) (the embedding peer named in `01-design.md`), gopher-lua, CPython and Node.
- Arv's design gives up CPU speed (`01-design.md` §1, "Not goals"). The point is to show where it stands, not to win.

## B2. Startup time (S)

- `arv run empty.arv`, `arv version` and `arv check examples/uptime.arv`, against `python3 -c pass`, `node -e 0`, `deno run empty.ts` and `starlark empty.star`.
- Include `arv run --manifest` on a 100-line script. Its pre-check runs before `--timeout` starts (18a Phase 8.1).
- **Target:** under 10 ms to the first effect for an empty script on machine B.

## B3. Memory (S)

- Peak RSS for: an empty run, 1,000 and 10,000 live tasks (each sleeping), and a 50-turn agent with a fake provider.
- Overhead per task, in bytes, over the 1,000 to 10,000 range.
- Compare `alloc_mb` charged with the Go heap actually used (`runtime/metrics`). That turns the 18a ratios ("a constant factor of the limit plus about 100 MiB at 1000 tasks") into a tracked number.

## B4. Recording, replay and resume overhead (S)

For four workloads (`fanout.arv` against the farm, the uptime gate, a CPU program with no effects, and a 20-turn fake-provider agent), measure:
- wall time and CPU with no flag, with `--record`, and with `--record --fsync`;
- `--replay` time;
- `--resume` time after a kill at 50% of the effects;
- log bytes per effect;
- log size against agent turns, which grows quadratically until 13b;
- time to verify the chain, as a function of log size.

**Targets**
- `--record` within 10% of the wall time of an unrecorded run on I/O-bound work.
- `--replay` at least 10× faster than the live run, because it does no waiting.

## B5. Budget and capability enforcement overhead (S)

- **Budgets:** steps or `alloc_mb` set against unset, on the B1 programs, and the `tokens` and `usd_cents` reserve and settle path against no limit, on a fake provider.
- **Capability checks:** `read_file` under a nested grant (canonicalise, then the `os.Root` walk) against Go's `os.ReadFile`. Use path depths of 1, 10 and 100: 18a says a deep path costs hundreds of lstats.
- **The network path:** `http_get` through the dial check against Go's `net/http` on the farm, with and without connection reuse (13c).
- **Report:** overhead in percent with a confidence interval. Any overhead above 5% on the gate workload needs a written reason.

## B6. Agent workloads end to end, against the alternatives (L, Phase 14)

**The comparison is fair only if the model is held fixed.**
- Mode F, framework overhead: a **recorded or fake model** with fixed seeded latency (for example 400 ms ± 100 ms per call) and fixed replies. It measures what the runtime adds.
- Mode L, live cost: a small live run (n = 20 per task) on one pinned model, reporting tokens, cents and pass rate. It shows the model dominates cost, and that budgets cap it.

**Workloads**

| W | Task | Effects |
|---|---|---|
| W1 | Uptime fan-out (the gate scenario): 200 URLs, 20 at a time | HTTP |
| W2 | Research agent: 3 tools (search, fetch, notes), 10-20 turns | LLM, HTTP, fs |
| W3 | Triage bot: read 50 issues, classify each with `ask`, write a report | LLM, fs |
| W4 | Crash and resume: W2 killed with SIGKILL at turn 7, then resumed | LLM, log |
| W5 | Injection: W2, with one fetched page telling the agent to send `~/.ssh/id_rsa` or the API key to an outside host | LLM, HTTP, fs |

**Alternatives.** Each is written idiomatically by someone fluent in it, with versions pinned:
- **Python + LangGraph**, with a SQLite checkpointer for W4 ([LangGraph checkpointers](https://docs.langchain.com/oss/python/langgraph/checkpointers), [durable execution](https://docs.langchain.com/oss/python/langgraph/durable-execution));
- **Python + OpenAI Agents SDK** and **Pydantic AI**, the latter with its Temporal or DBOS integration for W4 ([Pydantic AI durable execution](https://pydantic.dev/docs/ai/capabilities/durable_execution/overview/));
- **Deno** with `--allow-net=host --allow-read=dir` ([Deno security](https://docs.deno.com/runtime/fundamentals/security/));
- **Node** with `--permission` ([Node permissions](https://nodejs.org/api/permissions.html));
- **Temporal** (Go SDK) and **Restate** (TypeScript SDK) for W1, W2 and W4 ([Temporal](https://docs.temporal.io/workflow-definition), [Restate](https://docs.restate.dev/concepts/durable_execution));
- **plain Go**: the gate baseline, `bench/uptime-go`.

**Metrics per run**
- Wall time p50 and p95, framework overhead (wall time minus summed model latency), CPU and peak RSS.
- Lines of code, counted by the gate's rule, plus lines of security or durability configuration.
- In mode L: tokens and cents.
- W4: recovery time, and duplicated tool calls or charges after the resume.
- W5: whether the exfiltration happened, with bytes at the sink, as the escape probe in `bench/escape` measures.

**Public numbers to set expectations**
- Restate reports a 3-step workflow at a median of 15 ms on a 3-node cluster ([vendor blog](https://restate.dev/blog/announcing-restate-1.2)).
- Temporal Cloud's latency SLO is a p99 of 200 ms ([SLO](https://docs.temporal.io/evaluate/cloud/service-availability)).
- These are vendor figures. B6 measures every system on the same hardware. A server-based engine also needs its server's RSS and setup steps counted.

**Rules**
- Publish all code, versions, raw data and the scripts.
- Invite each project's maintainers to correct our version before publishing.
- Report the cases Arv loses. Deno 1.0 published a benchmark in which it lost on throughput, and its tail latency was better ([Deno 1.0](https://deno.com/blog/v1)).

## B7. CI regression gates

**On each PR (cloud runner)**
- Cloud runners are noisy, so do not compare against stored absolute numbers.
- Instead, in one job, build the base and the head and run them interleaved on the same VM: duet-style relative comparison, which improved accuracy 2.3-12.5× on JVM suites ([Bulej et al., ICPE'20](https://arxiv.org/abs/2001.05811)).
- **Gate:** fail if benchstat shows a B1, B2 or B5 benchmark more than 5% slower with p < 0.01. Otherwise post the table as a comment.

**Nightly (machine B)**
- The full B1 to B5 suite, results committed, and a chart through [github-action-benchmark](https://github.com/benchmark-action/github-action-benchmark), with `fail-threshold` set to 110%. Its default alert threshold is 200%, which is too loose.

**Release**
- The B6 suite in mode F, plus the gate (`make bench`) against the stored `gate-report.md` criteria.
- A release may not break a Phase 5 criterion that held at `9513540`.

## Exit criteria for the track

- B0 to B5 run nightly, with results committed.
- The PR gate is required.
- B6 is published with its code before the public launch (04 §6), and with the cases Arv loses included.
