# Phase 7: agents (as built)

Built from `p7-plan-1.md` to `p7-plan-17.md`. Their deviations from `14-phase7-agents.md` are listed in `p7-plan-1.md`. This file records what Phase 7 built and its known limits. Review round 1 is in [`16p`](16p-phase7-round1.md); it changed some rows and limits below, which are updated here.

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
| A call that may have been billed keeps its reservation; only a provably unprocessed one is free | `TestCancelledCallKeepsItsReservation`, `TestUnprocessedCallCostsNothing`, `TestFailedCallMayBeBilled`, `TestCostNothing`, `TestBadUsageIsChargedInFull` |
| Nested `tokens`, `usd_cents`, `max_turns` only shrink | `TestModelLimitsOnlyShrinkProperty`, `budget_llm_test.go` |
| `tokens` refuses a call it cannot cover; settlement frees the rest | `TestAskTokensBudget`, `TestReserveAndSettleTokens` |
| `max_turns: 3` stops a model that always calls tools, with a `budget` error | `TestAgentMaxTurns` |
| No grant list, task ID, host path or internal address reaches a model | `TestRedactDenials`, `FuzzRedactDenials`, `TestRedactTaskIDs`, `TestAskRedactsDenialsInThePrompt`, `TestAgentRedactsToolDescriptions`, `TestAgentRedactsTheGoal`, `TestAgentToolErrorsNameNoTask`, `TestAgentFSDenialNamesNoPath` |
| The API key is in no Arv value, output, log or error | `TestCLIKeyIsNotInTheEnvironment`, `TestCLIProviderErrorHidesTheKey`, `TestCLIReplyEchoingTheKeyFails`, `TestAnthropicErrors`, `TestAnthropicReplyEchoingTheKeyIsRefused`, `TestAnthropicNeverPrintsTheKey`, `TestAnthropicDoesNotFollowRedirects`, `TestProcFSIsRefused` (Linux), `TestLiveAnthropic` |
| Provider errors become `effect` errors with a kind | `TestAskErrors`, `TestAnthropicErrors` |
| No goroutine outlives a call or a run | `TestAskIsCancelledWithItsScope`, `TestAgentStopsWhenATimeoutHitsATool`, `TestAgentStopsWhenATimeoutHitsATurn`, `TestSpawnedAgentHoldsOnlyItsGrants`, `TestAnthropicCancelAndLeaks` |

## Known limits

- **A failed call is charged its maximum** (review round 1). A call that fails after it may have reached the provider (a cancellation, a timeout, a network error, an unreadable 200, a 5xx other than 529) keeps its whole reservation: the provider reports no usage on an error, and the program controls cancellation. Only `llm.CostNothing` errors (a redirect, a 4xx or 529, a request refused before sending) are refunded. So a budget can run out on calls that were in fact free (a dial failure counts as maybe billed). Negative reported usage is an `effect` error charged in full; counts too large to add count as the most there is.
- **The input estimate is bytes, not tokens.** A call reserves its text's bytes plus 512 (1024 more with tools, plus a little per block and tool) as input tokens, and 4096 output tokens. A token is at least one byte, so the estimate is high for text, but a provider that counts more (images, a long system prompt of its own) spends past the reservation. Settlement then charges the overage and the call ends with a `budget` error, after its reply came back and was paid for.
- **Concurrent calls near a limit.** Each call reserves its most before it starts, so two calls that would each fit alone may both be refused when the budget can hold only one reservation at a time.
- **Redaction works on text.** It finds a denial an Arv program passed on as a string, not one it took apart (`split(e["message"], ",")`). Everything after the first denial in a text is dropped. A net denial becomes `capability denied: net`, without the host, and an fs denial `capability denied: fs_read` (or `fs_write`), without the path (review round 1). Task IDs are removed from tool error results and from the text kept before a denial, not from prompts, goals or a tool's ok result (program data). A tool's other effect errors keep their text, so a missing file inside a grant still names its absolute path (`read_file: open /abs/...`): that path is inside a grant the agent holds.
- **Uncharged allocation.** Measured with a provider that returns at once: about 950 bytes per `ask` that `alloc_mb` does not see, whatever the prompt's size (the prompt is not copied). The Anthropic provider encodes each request into a body of about 2 bytes per byte of conversation (2.1 MB for a 1 MiB prompt), transiently, in the host; the conversation itself was charged when it was built. A reply's body is read up to 16 MiB before it is decoded and charged. All bounded, so Minor.
- **Prices are copied by hand.** `arv/llm/pricing.go` has the prices of 2026-11-11 (from the `claude-api` skill's table, cached 2026-11-03). A model missing from it cannot run under `usd_cents` unless `Host.Prices` names it. Cache pricing is not modelled: Arv sends no `cache_control`, and cache tokens a provider reports anyway are counted as input at the plain input price.
- **Tools take strings only.** A tool's parameters are its function's, all required strings; extra or missing arguments go back to the model as an error result. A result that is not a STRING goes back as JSON (`json(v)`), or as its printed form when JSON cannot hold it (a function).
- **Model calls are not replayable yet.** They are nondeterministic input (I7). Phase 6 is not on this base; routing `ask` and `agent` turns through `effect()` and testing that a replay makes zero provider calls is follow-up work for p67.
- **`max_tokens` is 4096 for every call** (deviation 4). Requests send no `thinking` parameter, and current models (Opus 5.5, Sonnet 5.5, Haiku 5.5) then think adaptively, counting thinking against `max_tokens`; the `claude-api` skill suggests about 16000 for non-streaming calls. A hard turn can therefore stop at `max_tokens`, which is an `effect` error (never a truncated answer). The trade-off is the reservation: 16000 output tokens of Opus 5.5 reserve 32 cents per call, more than `--usd-cents 25` allows, and a `tokens` budget below about 4.6K already runs no call. A `max_tokens` scope option is the follow-up if this bites.
- **Key exposure outside the provider.** The key never reaches Arv through `ask`, `agent`, errors, `getenv` under the CLI, or (Linux) procfs. An fs grant that covers a file holding the key (a `.env`, a shell profile) still reads it, and an embedder that grants `env` must unset the variable itself (`arv.Options` doc). `TestProcFSIsRefused` is compile-checked here (darwin) but runs only on Linux.
- **Reply key check cost.** `holdsKey` decodes each 200 reply a second time (into `any`) in the host, up to the 16 MiB reply cap: bounded per call, uncharged, Minor.
