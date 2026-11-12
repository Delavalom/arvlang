# Phase 7 notes: independent mutation check

This file holds the mutation table for the "Phase 7: agents" section of `16-implementation-notes.md`. The phase's own notes are `16o` to `16r`. It is a separate file because `16` is at the 18 KB limit. Like `16`, it overrides older plan text.

## Mutation check, round 1 (independent)

**Method.** Each mutation was applied alone to the product code and never committed. The whole suite was then run with `go test -count=1 -timeout 400s ./...`, which covers every unit package, the root package's in-process CLI tests and the e2e suite through a freshly built binary. After each run the file was restored with `git checkout --`, and `git status` was checked clean before the next mutation. Load averages were 11 to 26. Another agent kept clearing the shared Go build cache, so the runs used a private `GOCACHE`. Three mutants that first failed to build because of the missing cache were run again; those reruns are the rows below.

"Caught" means at least one test failed. "Unit only" means no e2e case failed. Test names are top-level, and e2e names are marked (e2e). `M26` also got a 90 s `FuzzAnthropicReply` run against the mutant, which found nothing.

**Result.** 53 mutations: 47 caught (15 of them by unit tests only) and 6 not caught. Of the 6, three are equivalent for every Arv-visible output and one cannot be observed on darwin. The other two are **test gaps**: the code is correct, but no test catches the mutant. No product defect was found.

| # | Guarantee | Mutation | Caught | Catching test |
|---|---|---|---|---|
| M01 | only held tools run (I1) | `runTool` skips `Require(tool(name))` | yes | `TestAgentDeniedToolIsAToolResult`, `TestSpawnedAgentHoldsOnlyItsGrants`, `TestDeniedToolNeverExecutesProperty`, `FuzzAgentToolCall`, `TestCLIAgentDeniedTool`, `TestAgentExample`; (e2e) `TestPhase7Acceptance`, `TestPhase7DesignExample`, `TestPhase7Concurrency` |
| M02 | llm coverage (I1) | `"p:*"` covers every provider's models | yes, unit only | `TestAgentCoverage`, `TestAgentAttenuation`. The CLI has only one provider, so no e2e case can name a second one. |
| M03 | attenuation (I2) | any grant covers a wildcard request (`r == "*"`) | yes, unit only | `TestAgentCoverage`, `TestAgentAttenuation` |
| M04 | `ask` needs `llm(...)` (I1, I2) | `model` skips `Require(llm(spec))` | yes | `TestAskNeedsTheModelCapability`, `TestAgentArgumentErrors`; (e2e) `TestPhase7Acceptance` (ask without llm), `TestPhase7Concurrency` (spawn without `with`, I2) |
| M05 | a denied tool is a result, not a crash | a tool denial stops the agent with a capability error | yes | the M01 tests |
| M06 | denied and failed tools are `is_error` results | every result has `IsError: false` | yes | `TestAgentDeniedToolIsAToolResult`, `TestAgentToolFailuresAreResults`, `FuzzAgentToolCall`, `TestDeniedToolNeverExecutesProperty`; (e2e) `TestPhase7MalformedToolCalls`, `TestPhase7Redaction`, `TestPhase7Acceptance` |
| M07 | a tool's error is a result | a tool's effect error stops the agent | yes | `TestAgentToolFailuresAreResults`, `TestAgentToolEffectIsChecked` (the test panics on the mutant); (e2e) `TestPhase7Acceptance`, `TestPhase7Redaction` |
| M08 | an unknown tool is a result | an unknown tool stops the agent | yes | `TestAgentToolFailuresAreResults`, `FuzzAgentToolCall`, `TestDeniedToolNeverExecutesProperty`; (e2e) `TestPhase7MalformedToolCalls/unknown_tool` |
| M09 | budgets enforced | `complete` reserves 0 tokens and 0 cents | yes | `TestAskTokensBudget`, `TestAskUSDBudget`, `TestCancelledCallKeepsItsReservation`, `TestModelBudgetsOnlyShrinkThroughCallsProperty`; (e2e) `TestPhase7Budgets` (9 cases), `TestPhase7Acceptance` (usd_cents) |
| M10 | usd_cents enforced | `Reserve` ignores the usd_cents refusal | yes | `TestUSDLimitIsInCents`, `TestRefusedReservationTakesNothing`, `TestAskUSDBudget`, `TestCLIReplyWithoutUsageIsNotFree`; (e2e) `TestPhase7Acceptance`, `TestPhase7Budgets` |
| M11 | tokens enforced | `Reserve` ignores the tokens refusal | yes | `TestReserveAndSettleTokens`, `TestModelBudgetsNest`, `TestConcurrentReservationsProperty`, `TestAskTokensBudget`; (e2e) `TestPhase7Budgets` (9 cases) |
| M12 | max_turns only shrinks (I6) | a larger inner `max_turns` replaces the outer one | yes | `TestModelLimitsOnlyShrinkProperty`, `TestModelBudgetsNest`, `TestModelBudgetsOnlyShrinkThroughCallsProperty`; (e2e) `TestPhase7Budgets` (larger inner max_turns) |
| M13 | tokens only shrinks (I6) | a reservation charges only the nearest tokens level | yes | `TestModelLimitsOnlyShrinkProperty`, `TestModelBudgetsNest`, `TestConcurrentReservationsProperty`; (e2e) `TestPhase7Budgets` (larger inner tokens) |
| M14 | usd_cents only shrinks (I6) | a reservation charges only the nearest usd_cents level | yes | `TestModelLimitsOnlyShrinkProperty`, `TestModelBudgetsOnlyShrinkThroughCallsProperty`; (e2e) `TestPhase7Budgets` (larger inner usd_cents) |
| M15 | a call that may have been billed keeps its reservation | every failed call is refunded | yes | `TestCancelledCallKeepsItsReservation`, `TestFailedCallMayBeBilled`, `TestCLIReplyWithoutUsageIsNotFree`; (e2e) `TestPhase7Budgets` (maybe billed, unreadable 200) |
| M16 | usage past a reservation is charged | `adjust` ignores overage | yes | `TestSettleOverageSpendsTheLevel`, `TestAskOverageIsABudgetError`; (e2e) `TestPhase7Budgets`, `TestPhase7Acceptance` |
| M17 | only unprocessed calls are free | `CostNothing` treats HTTP 500 as free (`< 500` to `<= 500`) | yes | `TestCostNothing`; (e2e) `TestPhase7Budgets` (maybe billed) |
| M18 | real usage is charged | settle a successful call to 0 | yes | `TestAskUSDBudget`, `TestAskOverageIsABudgetError`, `TestBadUsageIsChargedInFull`; (e2e) `TestPhase7Acceptance`, `TestPhase7Budgets` |
| M19 | max_turns enforced | off by one (`turn > turns+1`) | yes | `TestAgentMaxTurns`, `TestModelBudgetsOnlyShrinkThroughCallsProperty`, `TestP7NoGoroutineOutlivesRun`; (e2e) `TestPhase7Acceptance` (max_turns 3, CLI, default 20), `TestPhase7Budgets` |
| M20 | usd_cents needs a price | an unpriced model runs under usd_cents | yes, unit only | `TestAskUSDBudget`. No e2e case asks an unpriced model under `--usd-cents`. |
| M21 | negative usage is charged in full | the evaluator's negative-usage check is removed | yes, unit only | `TestBadUsageIsChargedInFull`. The Anthropic decoder refuses negative usage first, so the binary cannot reach this check. |
| M22 | the key is not in Arv | the CLI does not unset `ANTHROPIC_API_KEY` | yes | `TestCLIKeyIsNotInTheEnvironment`; (e2e) `TestPhase7Secrets/env=*` |
| M23 | the key is in no error | `fail` does not scrub the key | yes | `TestAnthropicErrors`, `FuzzAnthropicReply`, `TestCLIProviderErrorHidesTheKey`; (e2e) `TestPhase7Secrets` (2 cases) |
| M24 | a reply holding the key is refused | `holdsKey` always returns false | yes | `TestAnthropicReplyEchoingTheKeyIsRefused`, `TestAnthropicReplySplittingTheKeyIsRefused`, `FuzzAnthropicReply`, `TestCLIReplyEchoingTheKeyFails`; (e2e) `TestPhase7Secrets` (3 cases) |
| M25 | a key split across blocks is refused | the joined-text and joined-values check is removed | yes, unit only | `TestAnthropicReplySplittingTheKeyIsRefused`, `FuzzAnthropicReply`, `TestCLIReplySplittingTheKeyFails` |
| M26 | the key in a decoded JSON object key | `anyStringHolds` skips object keys | **no (equivalent for Arv)** | none. Only an escaped key in an object key reaches this check; a plain one fails the byte search first. Object keys never become Arv values: tool arguments are matched to parameter names, and an unknown one goes to the model, quoted only when 32 bytes or fewer. The defence-in-depth check is not pinned by any test, and 90 s of fuzzing did not find an input. |
| M27 | printing the provider shows no key | `String()` appends the key | yes, unit only | `TestAnthropicNeverPrintsTheKey` |
| M28 | redirects are not followed (key not re-sent) | `CheckRedirect` returns nil | yes, unit only | `TestAnthropicDoesNotFollowRedirects` |
| M29 | procfs refused (BSD names) | `linprocfs` is not a procfs name | yes, unit only | `TestProcFSNames` |
| M30 | procfs refused | `checkRegular` skips `onProcFS` | **no (not observable on darwin)** | macOS has no procfs, and `TestProcFSIsRefused` is Linux only. This is the same Linux run `16o` already asks the merge step for. |
| M31 | redaction: prompt and goal | `userText` skips `RedactDenials` | yes | `TestAskRedactsDenialsInThePrompt`, `TestAgentRedactsTheGoal`; (e2e) `TestPhase7Redaction` (prompt, goal) |
| M32 | redaction: descriptions | descriptions are not redacted | yes | `TestAgentRedactsToolDescriptions`; (e2e) `TestPhase7Redaction` (goal and description) |
| M33 | redaction: tool results | `runTools` skips `RedactDenials` | yes | `TestAgentToolEffectIsChecked`, `TestAgentToolErrorsNameNoTask`, `TestAgentFSDenialNamesNoPath`, `FuzzAgentToolCall`; (e2e) `TestPhase7Redaction` (3 cases), `TestPhase7Acceptance` |
| M34 | redaction: task IDs in tool errors | error results skip `RedactTaskIDs` | yes | `TestAgentToolErrorsNameNoTask`, `TestAgentFSDenialNamesNoPath`; (e2e) `TestPhase7Redaction` (net denial), `TestPhase7Acceptance` |
| M35 | redaction: net names no host | `Redacted` keeps the net scope | yes | `TestRedactDenials`, `TestAgentToolEffectIsChecked`; (e2e) `TestPhase7Redaction`, `TestPhase7Acceptance` |
| M36 | redaction: fs names no path | `Redacted` keeps the fs scope | yes | `TestRedactDenials`, `TestAgentFSDenialNamesNoPath`; (e2e) `TestPhase7Redaction` (fs denial result) |
| M37 | redaction: task IDs before a denial | the kept prefix is not passed through `RedactTaskIDs` | yes, unit only | `TestRedactDenials`. No e2e prompt holds a task ID before its denial. |
| M38 | redaction: an unparseable denial | the whole message is returned | yes, unit only | `TestRedactDenials` |
| M39 | redaction: task IDs with `#` | the regexp drops `#` | yes, unit only | `TestRedactDenials`, `TestRedactTaskIDs` |
| M40 | redaction: a tool denial | `runTool` returns `err.Error()`, not `Redacted()` | **no (equivalent)** | none needed. `runTools` passes every result through `RedactDenials`, which turns that text into exactly `Redacted()`. Only the alloc charge differs. |
| M41 | provider errors have kinds | every provider error gets kind `capability` | yes | `TestAskErrors`, `TestCLIProviderErrorHidesTheKey`; (e2e) `TestPhase7ProviderErrors` (13 cases), `TestPhase7Budgets`, `TestPhase7Secrets` |
| M42 | provider error kinds | 429 maps to `server` | yes | `TestAnthropicErrors`; (e2e) `TestPhase7ProviderErrors/429`, `TestPhase7Budgets` (429 is free) |
| M43 | a cancelled call is `cancelled` | `complete` skips its `Task.Err` check | **no (equivalent)** | none. The provider returns `context.Cause`, which already has a kind, and the scope's own cause replaces the call's error in every path. A throwaway probe gave identical output for a timeout, `collect` with a timeout, and a failing sibling with the error matched inside the task. |
| M44 | a refusal is an error | `StopRefusal` returns the reply | yes | `TestAskErrors`; (e2e) `TestPhase7ProviderErrors/stop_refusal` |
| M45 | `max_tokens` is an error, never a truncated answer | `StopMaxTokens` returns the reply | yes | `TestAskErrors`; (e2e) `TestPhase7ProviderErrors/stop_max_tokens` |
| M46 | a 200 with zero input tokens is not free | the zero-input check is removed | yes, unit only | `TestAnthropicReplyWithoutUsageIsRefused` |
| M47 | no goroutine leaks | keep-alives on | yes, unit only | `TestAnthropicCancelAndLeaks`, `TestAnthropicClientSettings`. `TestP7NoGoroutineOutlivesRun` passed. |
| M48 | calls are cancelled (I4, I5, leaks) | the request ignores ctx (`context.WithoutCancel`) | yes | `TestAnthropicCancelAndLeaks`, `TestP7NoGoroutineOutlivesRun` (both hung to the timeout); (e2e) `TestPhase7Concurrency` (scope timeout, failing sibling, agent call, I5) |
| M49 | no goroutine leaks | the reply body is never closed | **no (test gap)** | none. `io.ReadAll` reads every reply under 16 MiB to EOF, which releases the connection. A reply over `maxResponse` is left unread and unclosed, so its connection goroutines outlive the run. No leak test sends an oversized reply. |
| M50 | a cancelled tool stops the agent | `runTool` drops the `KindCancelled` and `Task.Err` checks | yes, unit only | `TestAgentStopsWhenATimeoutHitsATool`. In the e2e case the next turn's request fails before it is sent, so the fake sees nothing. |
| M51 | a call names one model (I3) | `ParseModel` accepts wildcards | yes, unit only | `TestParseModel`, `TestAskErrors`, `TestAgentArgumentErrors` |
| M52 | the capability is checked before the host is revealed (I1) | `model` reports "no model providers" before `Require` | **no (test gap)** | none. With no host (the CLI without `ANTHROPIC_API_KEY`), an `ask` the task does not hold returns an `effect` error saying no provider is configured instead of a denial. No test runs an unheld `ask` without a host. |
| M53 | usd_cents enforced | the pool is 10 times the limit | yes | `TestUSDLimitIsInCents`, `TestRefusedReservationTakesNothing`, `TestAskUSDBudget`, `TestCLIReplyWithoutUsageIsNotFree`; (e2e) `TestPhase7Acceptance`, `TestPhase7Budgets` |

**Invariants.** I1: M01, M02, M04, M52. I2: M03, M04. I3: M51. I4 and I5: M48. I6: M12 to M14. I7 has nothing to mutate: model calls do not go through the effect gate on this base (`16o`, deferred to p67). I8: Phase 7 adds no shared state, since tools run in the agent's own task.

**Gaps for a later round.** These are listed here only; no tests were added in this round.
- An unheld `ask` with no host configured should be a capability denial (M52).
- A leak check for a reply over 16 MiB (M49).
- A unit case with an escaped key in a tool input's object key (M26).
- The Linux procfs run (M30).
- Possible e2e cases for M02 (a grant for another provider), M20, M37 and M46.
