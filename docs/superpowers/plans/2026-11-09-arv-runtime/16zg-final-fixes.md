# Final fixer rounds (after the Phases 3-8 final review)

Branch `phase/final` (worktree `arvlang-wt/final`) from `feat/phases-3-8` at `8f0eb98`, merged back with `--no-ff`. Each fix below was written test-first. The new or unskipped test failed on the old code, and was checked again under a revert of the fix.

Round 1 wrote every change below but stopped before committing. Round 2 got the same findings, reviewed the uncommitted tree, and checked the unit fixes again. With `evalIfExpression`'s and `compileIf`'s `isReturn` reverted, `TestReturnInConditionIsKept` failed on both engines. With the `::/96` case and the `max(p.Backoff, 0)` reverted, `TestAddrClass` and `TestDelayNegativeBackoff` failed. Round 2 then committed and merged.

## The `TestP6PropRecordReplay` flake: a harness race, now drained (Important)

**Cause.** Three reviewers settled it independently: a dial probe, the replay run under `sandbox-exec` with outbound network denied, and accept and arrival timestamps. A record that fails fast, or whose `match scope` catches a fail-fast and exits 0 (seeds 9, 10 and 39), cancels tasks that have an `http_get` on the wire. The request bytes are already in the kernel, so the loaded test server runs the `/ok` handler up to about 50 ms after the record process has exited. That can be after the test has read `gets`, and the later comparison then reports `the replay made 1 requests`. The replay was never the source: it made no dial in any run, and the hit still came with its network denied. `runtime.Task.Effect` never calls `do` in Replay mode. **I7 holds.** This settles the question that 16p85b left open.

**Fix (tests only).** `e2e/conntrack_test.go` adds a `connTracker` to an unstarted `httptest.Server`. `ConnContext` numbers connections in accept order. A handler wrapper remembers each connection's `?id`. `ConnState` drops a connection once it is closed. `settle` dials a barrier request (`/__drain`). The backlog is FIFO, so the barrier reaches the server after every older connection. `settle` then waits until each older connection is closed, or is known to carry another test's id. It never waits on another test's held `/wait` gate. Every client the suite runs disables keep-alives (arv's `transportFor`, the Anthropic adapter, the Go host in `e2e/p82host`), so each connection carries one request and closes once its handler returns. The shared server (`drainServer(t)`) and each Phase 7 provider stand-in (`llmAPI.drain`, which drains every connection because the stand-in is per test) are tracked. `drainLimit` is one minute.

Drains were added before every counter snapshot taken after a child exits:
- `TestP6PropRecordReplay`, after the record;
- `TestP6PropKillAtGateResume`, after the kill and after the resume and replay;
- `TestP6PropKillAnywhereResume`, after the reference run (`refGets`), after the kill (the request in flight at the kill) and after the resume;
- `TestP6ReplayIsOffline`, after the record and after the replay;
- `TestP67PropAgentRecordReplay`, before `sent`, `w` and `n`;
- `replayLog` (every Phase 7 replay check), before and after the replay;
- after the kill in `TestP6ResumeAfterThreeOfFive` and in `killAgent` (the Phase 7 resume tests).

**Tests of the helper.** `TestDrainCountsRequestsAlreadyWritten` uses a listener whose `Accept` is held. It writes two whole requests for this test and one for another test, closes them all before the server accepts any, and then requires both of this test's requests to be counted when `settle` returns. The other test's handler stays blocked, standing in for a held gate, so `settle` must not wait on it. `TestDrainAllWaitsForEveryOlderConnection` does the same with id `""`. Mutations: `settle` returning once the barrier answers is caught by both tests (counted 0 of 2, and 0 of 3). Waiting on every older connection, whatever its id, is caught by the first test (a 60 s timeout on the other test's held request). Removing the `id == ""` clause is equivalent, because the stand-in's requests carry no `?id`.

**Evidence.** Both runs used `go test -race -count=25`, on this machine with load averages of 115 to 170. With `drainServer` turned into a no-op in a scratch copy, `-run 'TestP6PropRecordReplay$'` failed 4 of 1000 subtests (cases 9, 9, 10, 39, all `the replay made 1 requests`). With the drain, `TestP6PropRecordReplay`, both `TestP6PropKill*` tests and `TestP67PropAgentRecordReplay` passed 25 rounds each (66 s).

## `ARV_ANTHROPIC_URL` only for https or loopback (Minor, fixed)

The release binary used to pass any `ARV_ANTHROPIC_URL` to `llm.NewAnthropic`, so a stray or inherited `http://host/` sent `x-api-key` in clear text to that host. `cmd/arv` `anthropicURL` now accepts the value only when it is "" (the real API), an `https` URL, or an `http` URL whose host is `localhost` or a loopback address (127.0.0.0/8, `::1`, and their IPv4-mapped forms). A URL with userinfo, an opaque URL, a URL with no host, and any other scheme are refused. If `ANTHROPIC_API_KEY` is set and the value is refused, the run stops with exit 1 and `error: ARV_ANTHROPIC_URL must be ...`, before any call. The error does not repeat the value. If the key is unset, the variable is ignored as before, so replays with no key are unaffected. The usage text now names the variable as a test hook. Tests: `TestAnthropicURLOnlyHTTPSOrLoopback` (18 values) and `TestCLIRefusesPlainHTTPAnthropicURL`. Without the check, the latter fails with a network error from the real dial.

## IPv4-compatible addresses (`::a.b.c.d`) (Minor, fixed)

18-handoff item 4. `AddrClass` now classes `::/96` by the IPv4 address it embeds, as it already did for NAT64 and 6to4. `::` and `::1` are classed earlier, as unspecified and loopback. So `::127.0.0.1` is loopback, `::10.0.0.1` is private, `::169.254.169.254` is link-local, and `::8.8.8.8` is public. `TestAddrClass` has the four cases, and three of them failed before the fix. No exploit was found on macOS (a connect to `[::127.0.0.1]` fails). The change closes the coverage gap.

## The two open `t.Skip("BUG: ...")` tests (Minor, both fixed)

- **`TestReturnInConditionIsKept`** (`arv/evaluator/logical_test.go`). A `return` that ends an `if`'s condition now leaves the function in both engines: `evalIfExpression` and `compileIf` pass a `*value.ReturnValue` condition on, through a new helper `isReturn`. `&&` and `||` are that `if`, so they are fixed too. The Phase 8.4 e2e case `return inside &&` (`TestP84EnginesAgreeOnValues`) had pinned the old answer, `false`, on both engines. It now expects `5`. Reverting either engine's line fails the test on that engine (default and `-tags arv_treewalk`). **Known limit, not fixed:** a `return` elsewhere inside an expression still does not leave the function. This is base behaviour, and both engines agree on it. It covers a `let`'s value (`let x = if (true) { return 5 } else { 0 }; 7` gives 7), a call argument (`puts(if (true) { return 5 } else { 0 }); 7` prints 5, then gives 7) and `${...}`. A general fix would treat a return like an error at every place that propagates errors. That would touch every `isError` site in both engines, which is too wide for a final round.
- **`TestDelayNegativeBackoff`** (`arv/runtime/supervisor_delay_test.go`). `Policy.Delay` now treats a negative `Backoff` as 0 (`min(max(p.Backoff, 0), MaxBackoff)`). Before, `Policy{Backoff: -100}.Delay("root/0", 1)` returned -1862845608111266454. Arv cannot reach this case (`backoff_ms` must be positive), but a Go host can. `TestDelayClampsBeforeJitter` still passes and now guards the same path. A side effect on the Phase 4 mutation tables (16l): a backoff below -2^62 now counts as 0 before any doubling. So the mutant that drops `d = min(d, MaxBackoff)` after the doubling loop is equivalent again, as rounds 1 to 3 first said. The final `min` still caps the result, and `TestDelayBounds` covers it.

## Phase 3: the mutation pass over review rounds 7-11 (record)

18-handoff item 1 asked for a mutation pass over `git diff 620538b..phase/p3`. The pass was done, but it was recorded only in the messages of commits `6bea178` and `fbccf75`. The total number of mutants applied and the commands run were not written down. This table lists, from those messages and the tests they added, each mutant that survived and the test added to catch it. No second round was run to re-apply these mutants after the tests landed. Each test's comment says which change it fails under.

| Area | Mutant that survived | Catching test (added) |
|---|---|---|
| canonicalising charge | the walk's initial buffer charge dropped (denials were compared only with each other) | `TestDeniedRequestCostsPrepaid` |
| canonicalising charge | "known" judged against grants of either fs kind | `TestDeniedRequestIntoOtherKindCostsItsText` |
| canonicalising charge | re-prepaying at a second deviation (the cost between two symlinks dropped from the excess) | `TestAdmitChargesTheWalkPastPrepaid` |
| canonicalising charge | the excess not charged when a request fails inside a grant | `TestAdmitChargesTheWalkPastPrepaid` |
| canonicalising charge | buffer-growth charge dropped (under the 16 KiB slack of one call) | `TestCanonicalChargesAddUp` |
| canonicalising charge | result-string charge dropped | `TestCanonicalChargesAddUp` |
| canonicalising charge | Readlink-loop charge dropped | `TestCanonicalChargesAddUp` |
| canonicalising charge | link-target charge dropped | `TestCanonicalChargesAddUp` |
| spawn | not checking cancellation (the old test used 2000 copies of one request) | `TestCancelledSpawnStopsAtDistinctRequests` |
| await | result array charged before waiting | `TestFailedAwaitArrayCostsNothing` |
| fs effects | `confiningRoot` charging a stat without the grant's length | `TestFSEffectChargesEachGrantByLength` |
| constructors | `fs_read`/`fs_write` skipping the length check before `absPath` copies | `TestErrorTextStaysBounded` (two new cases) |
| http_get | a URL one byte over `maxURLLen` accepted | `TestHTTPGetRefusesURLJustOverCap` |
| error text | clipped text cut inside a rune | `TestClipCutsAtARuneBoundary` |
| concatenation | `concatSize` charging an operator other than `+` | `TestStringOperatorOtherThanPlusCostsNothing` |
| capability values | `capabilitySize` below the size of a capability value | `TestCapabilitySizeCoversTheValue` |

The tests are in `arv/runtime/canon_prepaid_test.go`, `arv/runtime/canon_links_test.go`, `arv/evaluator/p3gaps_test.go` and `arv/evaluator/allocerrtext_test.go`.

## Open after this round

- `TestResumedQuietChildSeesItsDeadline` (package `arv`, not changed here) failed once in round 2's `go test -race ./arv/...` run, with `recording: <nil>, "q\nok\n"`. The load average fell from about 150 to about 100 during the run. `spin()` is calibrated once per test binary, so a calibration made while the machine was busier gives a shorter spin later. Here `w`'s four spins then finished inside the 200 ms scope. It passed 5 of 5 on a rerun (`-count=5 -run TestResumedQuiet`). This is a timing flake in the test's calibration, not a runtime bug. It is not fixed here.

- A `return` inside a `let` value, a call argument or `${...}` does not leave the function (above).
- The Minor finding about the same count-after-exit pattern in other tests is covered by the drains listed above. The other replay and resume tests compare server counts with fixed numbers, and their recordings end with nothing in flight. Their replay checks go through `replayLog`, or are covered by a drain added after a kill.

## Stress run after the drain (2026-11-15, orchestrator, at `c67f35d`)

`go test -race -count=200 -run 'TestP6PropRecordReplay$|TestDrain' ./e2e/` reached about 190 rounds of all 40 cases before the 30-minute limit. **No** `the replay made N requests` failure: the drain holds. The nine failures and the timeout were all the race detector on darwin, none an Arv result: exit 66 with `ThreadSanitizer: CHECK failed: tsan_rtl.cpp:94`, `signal: segmentation fault` with empty stderr, and once a fork that never reached exec (the race-built test binary blocked in `os.StartProcess` reading the exec pipe for 28 minutes). The same fork hang left three orphaned `e2e.test` processes from the fixer agents running for days. Known limit: a long `-race -count=N` e2e run on darwin can hang in fork; run it with `-timeout`.
