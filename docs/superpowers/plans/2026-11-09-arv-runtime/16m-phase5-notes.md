# Phase 5: the gate (as built)

Built from `p5-plan-1.md` to `p5-plan-11.md`. Their deviations from `12-phase5-gate.md` are listed in `p5-plan-1.md`. This file records what the implementation tasks built. The measurements and the decision are not here: they go in `docs/research/2026-phase5-gate.md`, which a separate step writes from `bench/results-template.md`. Review rounds, e2e cases and the mutation table for this phase go below, or in a continuation file once this one nears 18 KB.

**What Phase 5 adds:**
- **Builtins** (`arv/evaluator`): `lines(s)`, `split(s, sep)`, `json(v)` and `clock_ms()`. The scenario cannot be written without them: Arv had no way to split text, read a clock or format a number. They are library functions, not the syntax that criterion 6's ergonomics week would add, so the gate still measures today's syntax.
- **`bench/farm`**: the farm as a library (`farm.Config`, `Plan`, `New`, `Expected`, `ParseReport`, `Compare`), plus `bench/farm/cmd/farm`, which serves it on a loopback address and has a `compare` subcommand.
- **`examples/uptime.arv`**: the Arv version (28 code lines). `uptime_example_test.go` runs it in process against a farm.
- **`bench/uptime-go`**: the Go baseline, in its own module (`golang.org/x/sync` v0.19.0). Its tests also run the Arv example against the same farm and require the same report.
- **`bench/run.sh`**, `bench/results-template.md`, **`bench/escape/probe.sh`**, and the Makefile targets `bench`, `bench-test` and `escape`.

## The scenario, as both versions implement it

| Rule | Arv | Go |
|---|---|---|
| Targets | `lines(read_file("targets.txt"))` | `strings.Fields(os.ReadFile(...))` |
| At most 20 in flight | `scope (tasks: 20)` around the fan-out | `errgroup` with `SetLimit(20)` |
| 2 s per check, retries included | `timeout_ms: 2000` on the check's restart scope | `context.WithTimeout` around the retry loop |
| Up to 2 retries, on transport errors only | `supervise: "restart", retries: 2` (retries kind `effect`) | the loop retries `err != nil`, not error statuses |
| Backoff | 100 ms, doubling, plus up to 10% jitter from an FNV hash | 100 ms, doubling, plus up to 10% random jitter |
| Up | status 200 to 299 | the same |
| Report | `json(results)`: an array in target order of `{latency_ms, status, up, url}` | `json.Marshal([]result)` of the same fields |
| Summary and exit | prints `{"down":N,"total":M}`; `error(...)` if `down * 20 > total` | prints the same line; exits 1 with the same message |

Status is 0 when no response came. The latency is the last attempt's, and is not compared: Arv reports 0 for a check that never got an answer, Go the time its last attempt took. The default farm has 20 of 200 targets down, so both versions exit 1 by design; the exit code is part of correctness.

## Known limits

- **`json` charges its text in chunks.** It is charged before it is written, `value.InspectChunk` (64 KiB) at a time, so at most one chunk is uncharged at once. The final copy into the result allocates the text a second time, transiently. Sorting a hash's keys allocates 32 bytes per pair, transiently, against the 48 per pair the hash was charged. Measured: a 1 MiB limit refused a 14 MiB encoding after 1.07 MiB of heap.
- **`json` writes integers, strings, booleans, `null`, arrays and hashes with string keys.** Unlike `encoding/json`, it does not escape `<`, `>`, `&`, U+2028 or U+2029. Invalid UTF-8 becomes U+FFFD byte by byte, as in `encoding/json`. Nesting deeper than 10000 levels is an error.
- **`lines` charges the most lines the text can have,** 32 bytes for each newline plus one, including the blank lines it drops. `split` charges 32 bytes per piece. Pieces share the input string, so a held piece keeps the whole text alive; the text was charged when it was read.
- **`split` rejects an empty separator.** Go would split into UTF-8 sequences.
- **`clock_ms` needs no capability,** like `sleep`. It reads the process's monotonic clock, which starts at 0 when the process starts; only differences mean anything. It is nondeterministic input (I7).
- **The farm counts requests in flight on the server.** A request leaves the count before any of its response is written, so a client that has its answer and sends the next request is never counted twice. A client that gives up (a timeout) leaves only when the server sees its context end.
- **A dropped target cuts its body short.** It sends a 200 head promising 64 bytes, 7 of them, and closes. Closing before any response let Go's keep-alive `http.Transport` re-send the GET by itself, with no backoff, inside one attempt of the retry loop (review round 1, [`16n`](16n-phase5-round1.md)). Now both versions make the same requests: 230 on the default farm (200 first attempts, 10 flaky retries, 2 × 10 dropped retries), and `bench/uptime-go`'s tests require 230 from each.
- **No check times out on a farm the command serves.** `farm` refuses a max latency above `farm.MaxCheckLatency` (500 ms). The slowest check is a dropped target: three attempts plus 300-330 ms of backoff, 1.83 s at 500 ms and 1.53 s on the default farm. A flaky one takes at most 1.11 s. `farm.Expected` assumes every check ends inside 2 s; the library still plans slower farms (a test uses one hour) and `Expected` is then wrong.
- **Where the tests run.** `go test ./...` at the root does not run `bench/uptime-go`'s tests; `make bench-test` does. `TestRunScript` and `TestProbeScript` (root module) build `bench/uptime-go` with `go build` and skip under `-short`. That build needs `golang.org/x/sync` v0.19.0 in the module cache, or network access, so on a fresh offline machine the root `go test ./...` fails there while `go test -short ./...` passes. The main module itself stays dependency-free.
- **A target outside the grants stops the Arv run.** `uptime.arv` attenuates each check to `net(host)` when it spawns it, in the root scope, outside the check's `match`. A line in `targets.txt` without a scheme, or for a host not in `--allow`, makes that spawn fail with `capability denied`, and the run stops before `report.json` is written. Go reports such a line as status 0, down. The farm's target list never has such a line. It is the capability model working as designed (a script cannot reach a host it was not granted), not a leak; a script that wanted Go's behaviour could attenuate only inside the check's `match` (the `probe` spawn), at the cost of each check task holding the root's whole authority. The gate keeps the tighter version.
- **The Arv escape probe sends with GET.** Arv has no POST, so the key would travel in a query string. The probes never read the real `~/.ssh`: `HOME` is a temp dir holding a canary key.

## Follow-through for later phases

- **Phase 6 (replay):** `clock_ms` is nondeterministic input and must be recorded. `json` already writes hash keys in order, which replay needs; `Hash.Inspect` is still random.
- **Phase 8.4 (speed):** do it only if the gate's CPU numbers say so.
- **If criterion 6 fails (ergonomics):** `examples/uptime.arv` shows where the syntax hurts:
  - `if (status > 199) { status < 300 } else { false }` stands in for `&&` and `>=`.
  - Counting the down targets takes a recursive function, because there is no fold or filter.
  - The error message carries no numbers, because there is no string interpolation or number formatting.
  - `split(url, "/")[2]` stands in for a URL host function.
