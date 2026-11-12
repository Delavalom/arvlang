# Phase 7: agents, review round 1 (fixes and dispositions)

Every Critical and Important finding was fixed test first: the regression test failed for the reason the finding gave, then passed after the fix. Minor findings were fixed the same way or recorded as known limits in [`16o`](16o-phase7-notes.md). Commits are on `phase/p7`.

## Critical and Important

| Finding | Fix | Regression tests |
|---|---|---|
| **Critical:** tool descriptions reached the model unredacted, so a caught denial put in a description sent the task ID and held grants | `agentTools` passes each description through `runtime.RedactDenials`, like the prompt, goal and tool results | `TestAgentRedactsToolDescriptions` |
| **Critical / Important (same defect):** a cancelled or failed call was settled to 0, so a loop of calls cancelled after they reached the provider spent without limit under `tokens` and `usd_cents` | `complete` refunds only when `llm.CostNothing(err)`: a redirect, a 4xx or 529 answer, or `ErrInvalid` with no status (refused before sending). Anything else (cancellation, network error, unreadable 200, other 5xx, a non-`*llm.Error`) keeps the whole reservation. A gateway's 502/504 can follow work the API already started, so 5xx other than 529 counts as maybe billed | `TestCancelledCallKeepsItsReservation` (20 cancelled calls under a budget for 2: the provider now gets at most 2, it got 20), `TestUnprocessedCallCostsNothing`, `TestFailedCallMayBeBilled`, `TestCostNothing` |
| **Important:** on Linux `/proc/self/environ` still holds `ANTHROPIC_API_KEY` after `os.Unsetenv`; `fs_read=/` or `fs_read=/proc` could read it | `checkRegular` refuses any file whose descriptor `fstatfs` puts on procfs (`procfs_linux.go`), for `read_file` and `write_file`. Asking the kernel about the open descriptor covers every spelling: `/proc/self`, `/proc/<pid>`, `/proc/thread-self`, a symlink into `/proc`, and other processes' `environ` (a parent shell started with the key) | `TestProcFSIsRefused` (Linux only; compile-checked with `GOOS=linux go vet` and `go test -c`, not run here: no Linux host or container on this machine), `TestOnProcFSRegularFile` (every platform) |
| **Important:** a 200 reply that echoed the key became an Arv value (only error texts were scrubbed) | `Anthropic.Complete` refuses a reply that holds the key as bytes or in any decoded JSON string or object key (a JSON escape hides it from a byte search), with a `decode` error naming no key | `TestAnthropicReplyEchoingTheKeyIsRefused`, `TestCLIReplyEchoingTheKeyFails` |

Re-exec without the variable, or reading the key from a file descriptor, were the other options for the procfs finding. They close `/proc/self/environ` only; the procfs check also closes other processes' `environ` and needs no exec. The remaining exposure, an fs grant covering a file that holds the key, is a known limit in `16o`.

## Minor

| Finding | Disposition |
|---|---|
| fs denials sent to a model named the canonical absolute path (working directory, user name) | Fixed: `DeniedError.Redacted` cuts `fs_read` and `fs_write` to the kind, as `net` was. Test: `TestAgentFSDenialNamesNoPath`, updated `TestRedactDenials`. Effect errors inside a grant keep their path: known limit in `16o`. |
| Task IDs reached the model before a wrapped denial and in tool errors (two findings) | Fixed: new `runtime.RedactTaskIDs` ("task root/0#2" becomes "a task"); `RedactDenials` applies it to the text it keeps, and the agent applies it to every error result. Tests: `TestAgentToolErrorsNameNoTask` (spawn denial and a cross-task `await`), `TestRedactTaskIDs`; `FuzzRedactDenials` now allows the kept text to change by its task IDs only. Prompts, goals and ok results are program data and are not rewritten. |
| The client never follows redirects, but the notes did not say so | Recorded: `NewAnthropic`'s `CheckRedirect` returns `http.ErrUseLastResponse` and a 3xx is an `invalid_request` error, so `x-api-key` is never re-sent to another host. Tested by `TestAnthropicDoesNotFollowRedirects` and `TestAnthropicClientSettings`; noted in `p7-plan-1.md` deviation 10 and the `16o` guarantee table. The `budget.go` comment change was a rewording only. |
| Untested plan behaviours | Added: `TestAgentStopsWhenATimeoutHitsATool` and `...ATurn` (no further provider call, no leaked goroutine), `TestAgentRedactsTheGoal`, `TestAnthropicClientSettings` (the transport's `Proxy` is `http.ProxyFromEnvironment`, keep-alives off, redirects refused), `TestModelLimitsOnlyShrinkProperty` (I6 on random chains for `tokens`, `usd_cents`, `max_turns`). |
| README said proxy settings are not used; embedders' `env` duty undocumented | Fixed: the README says `http_get` uses no proxy and model calls do; `arv.Options` and `NewAnthropic` say an embedder that grants `env` must unset the key's variable. |
| `max_tokens` 4096 may cut off thinking-heavy replies | Recorded as a known limit in `16o` with the trade-off (raising it to 16000 makes one Opus 5.5 call reserve 32 cents). |
| Markdown at or just over 18,000 bytes | Fixed: `16-implementation-notes.md` 17977 bytes, `p7-plan-1.md` 17986 bytes (trimmed wording, no content lost). |
| Replay (design task 8) deferred | Unchanged, still recorded in `16o` and `p7-plan-1.md` deviation 1 as follow-up for p67, so the merge or p67 step keeps it. |
| Reported usage not validated (overflow or negative refunds the reservation) | Fixed: negative usage is an `effect` error charged the whole reservation; the evaluator's tokens sum and the decoder's input-plus-cache sum saturate. Tests: `TestBadUsageIsChargedInFull`, `TestDecodeResponseUsage`. |
| Embedders: `getenv` can read `ANTHROPIC_API_KEY` under an env grant | Documented on `arv.Options` and `NewAnthropic` (see above). Refusing provider-key names in `getenv` was not done: the evaluator does not know which variables hold keys, and a name list would be a guess. |

## Out of scope, noticed

- On Linux, `/proc/self/fd/N` reopens whatever descriptor N refers to, and the reopened file is on its own filesystem, so the procfs check does not see it. It should already be refused: the capability check runs on the path after symlinks are followed, so the target's own path must be inside a grant, and `os.Root` refuses an absolute link that leaves its root. Not verified here (no Linux host).
