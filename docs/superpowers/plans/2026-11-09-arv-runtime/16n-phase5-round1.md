# Phase 5: review round 1 (fixes and dispositions)

Continues [`16m-phase5-notes.md`](16m-phase5-notes.md). Two reviewers, eleven findings. Each Critical and Important finding got a regression test that failed for the right reason before the fix. Measurements and the verdict are still not here.

## Critical: net/http re-sent dropped GETs (two findings, one cause)

**Finding.** A dropped target (and a flaky one on its first request) closed the connection after its latency, before any response. When the request had gone out on a reused keep-alive connection, Go's `http.Transport` treated the close as a server-side read error on an idle connection and re-sent the GET by itself, inside one attempt of `check`'s retry loop. Each dropped attempt cost Go two latencies or more, and dropped targets held their semaphore slot until the 2 s deadline. Arv opens a new connection per `http_get`, so it never paid that. The reports still matched, because a timeout and spent retries both give status 0, but the gate's wall time, criterion 2, was skewed against Go (reviewers measured about 4.35 s against 3.6 s), and Go's farm totals varied from run to run (247-271 against Arv's 230).

**Fix (in the farm, not in either checker).** `drop` now sends a 200 head with `Content-Length: 64`, writes 7 bytes, flushes and closes. The response has started, so the transport never re-sends; both checkers see a body read error, an effect error (Go: `io.Copy` fails; Arv: `readError`, kind `effect`). The status is 200 so that a checker which ignored a body error would report the target up and fail `farm.Compare`. Go keeps its keep-alive transport: reusing connections is a real advantage of the idiomatic version, and it stays in the measurement. Turning keep-alives off in the farm (the other suggested fix) would have removed it.

**Tests.**
- `bench/farm/keepalive_test.go` `TestDropFailsOnceOnAReusedConnection`: a keep-alive client warms a connection on an OK target, then fetches a Drop and (after a reset) a Flaky target. Before: Drop made 3 requests, Flaky answered 200. After: an error and 2 requests each.
- `bench/uptime-go` `TestReportsTheFarm` now requires 230 requests; `TestSameReportAsTheArvExample` requires 230 from each version. Before: 255 and 271 from Go.
- A sanity run of `bench/run.sh` on the default farm (2 runs each; not the measurement) gave 230 requests in every run of both versions, and "versions agree".

**Not added:** a test that runs both checkers on the 50-400 ms gate farm and requires 230. Its total depends on no drop check reaching 2 s (1.53 s nominal), which a load average above 300 can break. The three tests above pin the mechanism without timing.

## Important: a flaky target near the budget split the reports

**Finding.** Same cause. With the hidden re-send (no backoff), Go brought a flaky target with latency L up in about 2L; Arv took L + 100 ms + L. For L in about 950-1000 ms Go said up and Arv down, and Go's answer depended on whether the connection happened to be reused.

**Fix.** The drop fix removes the cause. Separately, `farm` (the command) refuses a max latency above `farm.MaxCheckLatency` (500 ms), so every check ends well inside 2 s, where the two versions cannot be split by timing. **Test:** `TestFlakyOnAReusedConnectionGoesThroughTheRetryLoop` (`bench/uptime-go`): a flaky target at 980 ms, on a warm connection, must be down after 3 requests. Before the drop fix: up, status 200, in 1969 ms.

## Minor findings

| Finding | Disposition |
|---|---|
| The notes said no check times out on the default farm | True now for both versions; 16m restates it with the numbers and the latency cap. |
| The escape tests accepted any denial for read-and-send | Fixed: `TestArvProbesAreDenied` requires `capability denied: fs_read("…/.ssh/id_ed25519")` for read-and-send and `capability denied: net("…")` for send-only; `TestProbeScript` checks the same in each probe's stderr. Checked by mutation: granting `fs_read` on the key makes both tests fail. |
| `TestProbeScript` did not check that Go delivered the key | Fixed: the summary must read `sink bytes: N`, N being the canary length that `probe.sh` prints. Checked by mutation: an empty POST body fails it. |
| `run.sh` never compared the versions with each other | Fixed: each run's report and stdout are kept, and `run.sh` ends with `versions agree: N reports and summaries, all the same as arv run 1` or names the runs that differ. `TestRunScript` requires the line; the results template has a row for it. Checked by mutation: a trailing space in Go's summary prints `versions differ: … go.1`. |
| Root `go test ./...` needs `golang.org/x/sync` in the module cache | Known limit, kept. `TestRunScript` and `TestProbeScript` must build the real `bench/uptime-go`; vendoring x/sync into the repo would be the alternative. Recorded in 16m with the `-short` workaround. |
| Deviations from `12-phase5-gate.md` | All listed in `p5-plan-1.md`. Two are judgement calls the measuring step should state in the write-up: the semaphore is `errgroup.SetLimit(20)`, not a separate `x/sync/semaphore`; and the Arv probe sends the key in a GET query string (Arv has no POST) and reads it by an absolute path, while the Go probe uses `$HOME`. |
| `farm.Expected` ignores the 2 s budget | Fixed by the latency cap above (`farm` exits 2 on `-latency 1ms-1000ms`); `Expected` documents the assumption. |
| NaN, Inf and huge fractions; a huge latency span panicked | Fixed in `Plan`: fail and flaky must be in 0 to 1 before rounding (NaN fails every comparison), and latencies are drawn with `Uint64N(span+1)`, which cannot overflow and gives the same stream as `Int64N`. Tests: `TestPlanRejectsBadConfigs`, `TestPlanTakesTheWidestSpan`, `TestRefusesBadFlags` (now with a cancelled context, so a farm that wrongly starts returns instead of hanging). |
| A target outside the grants aborts the Arv run | Known limit, recorded in 16m: it is the capability model refusing the spawn, not a leak, and the farm never produces such a line. |
