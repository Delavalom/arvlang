# Phase 7: agents, e2e and hardening

What the e2e and hardening pass added on `phase/p7` after review round 2 ([`16q`](16q-phase7-round2.md)). It found no defect in the product code. Its one fuzz failure was a false positive in the new test (see Fuzzing).

## E2E (`e2e/p7_*_test.go`, 68 cases)

Each case runs the real binary against its own local fake of the Messages API (`ARV_ANTHROPIC_URL`). The fake records every request and also counts it at the gate server's `llm` gate, so a script can wait for a model call to be in flight. `runLLM` fails a case if the key (`e2eKey`) appears in stdout, in stderr or in any request body, or if the binary crashes. Tool runs are counted by the gate server (`/arrive`), so "ran once" and "never ran" are observed from outside the process.

| File | Covers |
|---|---|
| `p7_accept_test.go` | Every acceptance row of `14-phase7-agents.md` except replay (Phase 6 is not on this base) and live (`TestLiveAnthropic`, opt-in). Also covers `usd_cents` and `max_turns` set from the CLI, the default `max_turns` of 20, and the design's example program (a denial and a result in one user message, in the model's order). |
| `p7_budget_test.go` | I6 for `tokens`, `usd_cents` and `max_turns`: a larger inner limit never raises the run's, and a smaller one binds only inside its scope. Also: settlement frees the unused part of a reservation; usage past the limit leaves the budget spent; a 500 or an unreadable 200 keeps its reservation; a 429 is free; an agent's turns share one pool; `max_turns` counts per agent call. Six parallel asks under `--tokens 20000` always give exactly 4 ok and 2 budget results (any four reservations fit and no fifth can, whatever the order). |
| `p7_secret_test.go` | `env=*` cannot read the key. A reply holding the key (plainly, JSON-escaped, or in a tool argument) fails the call before any tool runs, and errors that echo the key are redacted. There is a kind for every provider status and stop reason, and for a network error. Redaction of a caught denial in a prompt, a goal, a description and a tool result (fs, net, spawn): no grant list, task ID, host path or address reaches the model. |
| `p7_conc_test.go` | A scope timeout, or a failing sibling (I5), cancels model calls in flight: the fake sees each connection close. A tool in flight is cancelled too, with no further turn. The program waits for a spawned agent (I4). Parallel agents each run only their own tools. Attenuation (I2): a task cannot pass on `tool(...)` or `llm(...)` it does not hold, a spawn without `with` holds nothing, and `tool("*")` covers every tool. Malformed tool calls become error results. |

## Fuzzing

| Target | Package | Run | Execs | Found |
|---|---|---|---|---|
| `FuzzAnthropicReply` (new): arbitrary status and body through `Anthropic.Complete` over an in-memory transport; `$KEY`, `$K1` and `$K2` place the key, whole or in halves | `arv/llm` | 2m0s | 2,855,594 | nothing |
| `FuzzAgentToolCall` (new): two arbitrary tool calls from a model through `arv.Run` | `arv` | 2m0s | 2,815,452 | one false positive in the test (below) |
| `FuzzToolArgs` (existing): a tool call's input decoded into arguments | `arv/evaluator` | 2m0s | 5,350,251 | nothing |

`FuzzAgentToolCall`'s first run flagged `"ok:holds"`. That is the model's own argument (`{"a":"holds"}`) coming back in an ok result, which is program data, not a leak. The check now ignores words the model itself sent. The input is kept as a seed (`arv/testdata/fuzz/FuzzAgentToolCall/e0a65d2a6204ee8d`). After that fix the target ran its full 2 minutes clean (the row above).

## Properties and leaks

- `TestModelBudgetsOnlyShrinkThroughCallsProperty` (`arv/p7_budget_prop_test.go`, 120 rounds): random ask and agent sequences in random nests of `tokens`, `usd_cents` and `max_turns` scopes, with the run's limits sometimes as the outermost level. Reported spending stays within the tightest limit, and no agent makes more turns than the tightest `max_turns`. The nest produces the same output, errors and provider calls as one scope holding its tightest limits, and adding a far larger innermost level changes nothing. A mutation (doubling the flat scope's tokens) makes it fail.
- `TestDeniedToolNeverExecutesProperty` (`arv/p7_tool_prop_test.go`, 300 rounds): random tool hashes, root grants, spawned subsets and `tool("*")`, with random multi-turn model calls. A call runs if and only if its tool is offered, held and well formed. It runs once, in order, and every other call gets an error result; an unheld tool's result is exactly `capability denied: tool(...)`.
- `TestP7NoGoroutineOutlivesRun` (`arv/leak_p7_test.go`): the real Anthropic provider against a local fake, through `arv.Run`. It covers parallel agents with tools, a call cut off by a scope timeout, parallel calls cut off by a failing sibling, a tool cut off by a timeout, refusals from a shared budget, `max_turns`, and the host's context cancelling an agent and a call. The goroutine count returns to the baseline (settle loop, 30 s).

## Checks

`gofmt -l .` prints nothing and `go vet ./...` is clean. `go test -race -count=5 ./...` and `ARV_E2E_RACE=1 go test -race ./e2e/` are green on darwin/arm64.
