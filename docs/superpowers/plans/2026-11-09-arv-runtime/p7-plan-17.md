# Phase 7, task p7.17: Phase 7 notes

> **For agentic workers:** this file is one task, p7.17. Do it test first, exactly as written; if something here does not match the code you find, stop and report instead of improvising. Prerequisite: task p7.16 is committed in your tree. Start every shell command with `cd <your worktree> &&` and use absolute paths: the shell's working directory does not persist. Before committing: `gofmt -l .` prints nothing, `go vet ./...` is clean and `go test -race ./...` is green. End the commit message with a blank line and then `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`. Never use `--no-verify` and never push; keep Go files under 15 KB and markdown under 18 KB. `go.mod` says `go 1.24.0` (toolchain 1.26.2): `t.Chdir`, `min` and `max` are available; do not use `testing/synctest` or `sync.WaitGroup.Go`. Before a long test run, check `df -h /System/Volumes/Data`; below 1 GB free, stop and report (only `go clean -cache` may be deleted). This machine is heavily loaded: keep wall-clock bounds generous. No test may reach the network or spend money: never set `ARV_LIVE_ANTHROPIC`, and never put a real API key anywhere.
>
> Every code block below was applied, in task order, to a scratch copy of `feat/phases-3-8` (Phases 3, 4 and 5 merged; Phase 6 is not) and passed `gofmt -l .`, `go vet ./...` and `go test -race ./...` (including `e2e/`). Design: `14-phase7-agents.md`. Deviations from it, and why: `p7-plan-1.md`.

### Task p7.17: Phase 7 notes

Record what was built, where each guarantee is tested, and the known limits, with the numbers measured while planning. `16-implementation-notes.md` is near 18 KB, so it gets one pointer line.

**Files:**
- Create: `docs/superpowers/plans/2026-11-09-arv-runtime/16o-phase7-notes.md`
- Modify: `docs/superpowers/plans/2026-11-09-arv-runtime/16-implementation-notes.md`

- [ ] **Step 1: Write the notes**

Create `docs/superpowers/plans/2026-11-09-arv-runtime/16o-phase7-notes.md`:

```markdown
# Phase 7: agents (as built)

Built from `p7-plan-1.md` to `p7-plan-17.md`. Their deviations from `14-phase7-agents.md` are listed in `p7-plan-1.md`. This file records what Phase 7 built and its known limits. Review rounds and the mutation table for this phase go below, or in a continuation file once this one nears 18 KB.

**What Phase 7 adds:**
- **Capabilities** (`arv/runtime`): kinds `llm` (`"provider:model"`, `"provider:*"`, `"*"`) and `tool` (a name, or `"*"`), with coverage and attenuation like the others. `llm(...)` and `tool(...)` are Arv values, and `--allow llm=...`, `--allow tool=...` grant them.
- **Redaction** (`arv/runtime/redact.go`): `DeniedError.Redacted` and `RedactDenials`. Every prompt, goal and tool result passes through `RedactDenials` before a model sees it.
- **Budgets** (`arv/runtime/budget_llm.go`): `tokens` and `usd_cents` pools, reserved before each call and settled after it, and `max_turns`. Scope options `tokens`, `usd_cents`, `max_turns`; CLI flags `--tokens`, `--usd-cents`, `--max-turns`.
- **`arv/llm`**: the `Provider` interface, request and response types, `Error` with kinds, `Fake`, `Host` (providers and price overrides, passed as `arv.Options.LLM`), `DefaultPrices`, and `Anthropic` (Messages API over `net/http`).
- **Effects** (`arv/evaluator`): `ask(model, prompt)` and `agent(model, goal, tools[, descriptions])`.
- **CLI**: reads `ANTHROPIC_API_KEY`, removes it from the environment, and gives the run an Anthropic provider. `ARV_ANTHROPIC_URL` points it elsewhere (tests use a local fake).
- `examples/agent.arv`, a README section, and one opt-in live test (`ARV_LIVE_ANTHROPIC=1`).

## Where each guarantee is tested

| Guarantee | Tests |
|---|---|
| `ask` without `llm(...)` is denied, and the provider gets no call | `TestAskNeedsTheModelCapability` |
| An allowed tool runs once with the model's arguments; the final text is returned | `TestAgentRunsAnAllowedTool`, `TestAgentConversationShape` |
| A tool the task does not hold is an `is_error` result `capability denied: tool("notes")`; the run goes on | `TestAgentDeniedToolIsAToolResult`, `TestCLIAgentDeniedTool`, `TestAgentExample` |
| A tool's own effect is checked: `http_get` without `net` is an error result, nothing is dialled | `TestAgentToolEffectIsChecked` |
| `usd_cents: 1` refuses the second call before the provider sees it | `TestAskUSDBudget` |
| `tokens` refuses a call it cannot cover; settlement frees the rest | `TestAskTokensBudget`, `TestReserveAndSettleTokens` |
| `max_turns: 3` stops a model that always calls tools, with a `budget` error | `TestAgentMaxTurns` |
| No grant list, task ID or internal address reaches a model | `TestRedactDenials`, `FuzzRedactDenials`, `TestAskRedactsDenialsInThePrompt` |
| The API key is in no Arv value, output, log or error | `TestCLIKeyIsNotInTheEnvironment`, `TestCLIProviderErrorHidesTheKey`, `TestAnthropicErrors`, `TestAnthropicNeverPrintsTheKey`, `TestLiveAnthropic` |
| Provider errors become `effect` errors with a kind | `TestAskErrors`, `TestAnthropicErrors` |
| No goroutine outlives a call or a run | `TestAskIsCancelledWithItsScope`, `TestSpawnedAgentHoldsOnlyItsGrants`, `TestAnthropicCancelAndLeaks` |

## Known limits

- **A failed provider call costs nothing.** On a provider error or a cancellation, the reservation is settled to 0. A call that timed out after the provider started it may still be billed. The provider does not report usage on an error, so the budget cannot know.
- **The input estimate is bytes, not tokens.** A call reserves its text's bytes plus 512 (1024 more with tools, plus a little per block and tool) as input tokens, and 4096 output tokens. A token is at least one byte, so the estimate is high for text, but a provider that counts more (images, a long system prompt of its own) spends past the reservation. Settlement then charges the overage and the call ends with a `budget` error, after its reply came back and was paid for.
- **Concurrent calls near a limit.** Each call reserves its most before it starts, so two calls that would each fit alone may both be refused when the budget can hold only one reservation at a time.
- **Redaction works on text.** It finds a denial an Arv program passed on as a string, not one it took apart (`split(e["message"], ",")`). Everything after the first denial in a text is dropped. A net denial becomes `capability denied: net`, without the host.
- **Uncharged allocation.** Measured with a provider that returns at once: about 950 bytes per `ask` that `alloc_mb` does not see, whatever the prompt's size (the prompt is not copied). The Anthropic provider encodes each request into a body of about 2 bytes per byte of conversation (2.1 MB for a 1 MiB prompt), transiently, in the host; the conversation itself was charged when it was built. A reply's body is read up to 16 MiB before it is decoded and charged. All bounded, so Minor.
- **Prices are copied by hand.** `arv/llm/pricing.go` has the prices of 2026-11-11 (from the `claude-api` skill's table, cached 2026-11-03). A model missing from it cannot run under `usd_cents` unless `Host.Prices` names it. Cache pricing is not modelled: Arv sends no `cache_control`, and cache tokens a provider reports anyway are counted as input at the plain input price.
- **Tools take strings only.** A tool's parameters are its function's, all required strings; extra or missing arguments go back to the model as an error result. A result that is not a STRING goes back as JSON (`json(v)`), or as its printed form when JSON cannot hold it (a function).
- **Model calls are not replayable yet.** They are nondeterministic input (I7). Phase 6 is not on this base; routing `ask` and `agent` turns through `effect()` and testing that a replay makes zero provider calls is follow-up work for p67.
```

In `docs/superpowers/plans/2026-11-09-arv-runtime/16-implementation-notes.md`, after:

```markdown
Notes: [`16m`](16m-phase5-notes.md). Review round 1: [`16n`](16n-phase5-round1.md).
```

add:

```markdown

## Phase 7: agents

Notes: [`16o`](16o-phase7-notes.md).
```

- [ ] **Step 2: Check**

Run: `wc -c docs/superpowers/plans/2026-11-09-arv-runtime/16o-phase7-notes.md docs/superpowers/plans/2026-11-09-arv-runtime/16-implementation-notes.md` (each under 18432 bytes), `gofmt -l .`, `go vet ./...` and `go test -race ./...`.
Expected: sizes under 18432 bytes; PASS.


- [ ] **Step 3: Commit**

```bash
git add docs/superpowers/plans/2026-11-09-arv-runtime/16o-phase7-notes.md docs/superpowers/plans/2026-11-09-arv-runtime/16-implementation-notes.md
git commit -m "docs(notes): Phase 7 notes

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```
