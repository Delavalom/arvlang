# Phase 7 mutation check, round 2

This round re-checks only the six mutants that round 1 ([`16s`](16s-phase7-mutations.md)) left uncaught. A fixer has since added tests for them (commit `18983a9`). Each mutant was applied alone in `arvlang-wt/p7` on `phase/p7`. The run covered `go test -count=1 ./arv/llm ./arv/evaluator ./e2e`. The mutant was then restored with `git checkout -- <file>`, and `git status` was clean before the next one. Nothing was committed. Before any mutant, the baseline on the same packages was green (load average about 5, 6.6 GB free).

| # | Guarantee | Mutation | Caught | Catching test |
|---|---|---|---|---|
| M26 | the key is refused in a decoded JSON object key | in `anyStringHolds` (`arv/llm/keycheck.go`), the `strings.Contains(k, key)` check on object keys is removed | yes, unit only | `TestAnthropicReplyWithTheKeyInAnObjectKeyIsRefused` |
| M30 | procfs is refused | `checkRegular` (`arv/evaluator/effects.go`) never calls `procFS` (`if false && ...`) | yes, unit only (darwin) | `TestCheckRegularRefusesWhatProcFSReports` (through the `procFS` seam). `TestProcFSIsRefused` is still Linux-only and is left for the merge step. |
| M40 | redaction: a tool denial | `runTool` returns `err.Error()` instead of `Redacted()` | yes, unit only | `TestRunToolDeniedResultIsRedacted` |
| M43 | a cancelled call has the `cancelled` kind | `complete` skips the `in.Task.Err()` check after a provider error | yes, unit only | `TestCancelledCallIsCancelledWhateverTheProviderSays` |
| M49 | no goroutine leaks | `defer resp.Body.Close()` is removed from `Anthropic.Complete` | yes, unit only | `TestAnthropicOversizedReplyLeavesNoGoroutine` (3 goroutines before the call, 6 after, at the 20 s settle limit) |
| M52 | the capability is checked before the host is revealed (I1) | in `model` (`arv/evaluator/llmcall.go`), the `host == nil` check ("no model providers") is moved before `Require` | yes, unit only | `TestUnheldModelWithoutAHostIsDenied` |

**Result.** 6 of 6 are caught. Combined with round 1, all 53 Phase 7 mutants are now caught. The e2e suite (`./e2e`) caught none of these six. Each one is pinned only by its unit test, as the round 1 fixes table expected. The Linux run of `TestProcFSIsRefused` is still open for the merge step.
