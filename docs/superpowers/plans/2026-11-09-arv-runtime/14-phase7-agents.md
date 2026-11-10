# Phase 7: LLM calls and agents on the same model

**Status:** design and task outline. Depends on Phase 3 (budgets) and Phase 4 (errors as values). Phase 6 is optional but makes agent runs replayable for free.

**Goal:** In Arv, an agent is not a framework. It's a task. Model access is a capability, each tool is a capability, tokens and dollars are budgets, and every model call is an effect.

```arv
let tools = {
  "weather": fn(city) { http_get("https://api.weather.example/v1?q=" + city)["body"] },
  "notes":   fn(path) { read_file(path) }
};

let answer = scope (tokens: 50000, usd_cents: 25, timeout_ms: 60000) {
  await spawn agent("anthropic:claude-sonnet-5-5", "Should I bike to work in Lisbon today?", tools)
    with [llm("anthropic:claude-sonnet-5-5"), tool("weather"), net("api.weather.example")]
};
```

The agent above **cannot** use `notes`, even though it's in the hash: it holds no `tool("notes")` and no `fs_read`. If the model asks for it, the model gets a `tool_result` error saying `capability denied`, and the program keeps going.

## Design

### Capabilities

- `llm("provider:model")`, with wildcards like `llm("anthropic:*")`. CLI: `--allow llm=anthropic:*`.
- `tool("name")`: may the task let a model call this tool. The tool's *own* effects are still checked against the task's other grants (above: `net("api.weather.example")`). Both checks must pass.
- New `runtime.Kind`s: `LLM`, `Tool`. Coverage: exact match, or `provider:*`, or `*`.

### Secrets never enter Arv

The host (the CLI or a Go embedder) builds the provider with the API key, for example from `ANTHROPIC_API_KEY` read by `main.go`. Arv code holds `llm(...)` capabilities, never keys. Model calls don't need `net` grants, because the host configured the endpoint.

### Effects

- `ask(model, prompt)` returns a string. One Messages API call.
- `agent(model, goal, tools)` returns the final text. The loop:
  1. Send `goal` plus tool definitions. A tool's name is its hash key; its parameters come from the function's parameter names, all typed as strings in v1. An optional hash `tool_spec` adds descriptions.
  2. On `stop_reason: "tool_use"`, for each `tool_use` block: require `tool(name)`. If that's denied, reply with a `tool_result` that has `is_error: true` and the denial message. Otherwise call the Arv function **in the agent's own task**, so it has exactly the agent's authority, and send back its result (or its error message, again as an `is_error` tool result).
  3. Stop at `end_turn`, at `max_turns` (scope option, default 20), or when a budget runs out.

### Budgets

`Budget` gains two pools: `tokens` and `usd_micros`. Before each call, the runtime **reserves** `max_tokens` worth of output plus the estimated input. After the call, it settles to the real usage (`usage.input_tokens`, `usage.output_tokens`). If the reservation can't be covered, the call is refused before any request goes out: `budget exceeded: usd_cents (limit 25)`. Prices come from `arv/llm/pricing.go`. **Copy the numbers from Anthropic's pricing page on the day you implement this. Don't use remembered prices**, and record that date in the file.

### Provider interface

```go
// arv/llm/llm.go
type Provider interface {
	Complete(ctx context.Context, req Request) (Response, error)
}
type Request struct { Model string; System string; Messages []Message; Tools []ToolDef; MaxTokens int }
type Response struct { Content []Block; StopReason string; InputTokens, OutputTokens int }
```

- `arv/llm/anthropic.go`: `POST https://api.anthropic.com/v1/messages` with headers `x-api-key`, `anthropic-version: 2023-06-01` and `content-type: application/json`, over plain `net/http`. **Before writing it, load the `claude-api` skill and check the current request shape, tool-use blocks and model IDs.** Defaults: `claude-sonnet-5-5` for agents, `claude-haiku-5-5` for cheap checks.
- `arv/llm/fake.go`: a scripted provider (a queue of responses, including `tool_use`). Every test uses it, so CI never calls the network or spends money.

## Task outline

1. `LLM` and `Tool` kinds, coverage rules, CLI parsing (unit tests).
2. `llm.Provider`, `Request`/`Response`, and the fake provider.
3. `ask` effect (fake-provider tests: capability required, response returned).
4. Token and dollar pools in `Budget`; reserve/settle; refusal before the request (test: the fake provider records zero calls once the budget is exhausted).
5. Scope options `tokens`, `usd_cents`, `max_turns`.
6. `agent` loop: tool definitions from functions; dispatch; denial as an `is_error` tool result; tool errors as tool results; `max_turns`.
7. Anthropic provider + one opt-in live test (`ARV_LIVE_TESTS=1`, Haiku, `max_tokens: 50`).
8. If Phase 6 shipped: `ask` and `agent` turns go through `effect()`. Test: replaying an agent run makes zero provider calls.
9. `examples/agent.arv` and a README section.

## Acceptance tests (fake provider unless marked live)

| Test | Expect |
|---|---|
| `ask` without `llm(...)` | `capability denied: llm("anthropic:claude-sonnet-5-5")` |
| model calls an allowed tool | the tool runs once with the model's arguments; the final text is returned |
| model calls a tool the task doesn't hold | the model receives an `is_error` tool result containing `capability denied`; the program doesn't fail |
| allowed tool whose effect isn't granted | the tool's `http_get` is denied; the model receives that as an `is_error` result |
| `usd_cents: 1` with a fake price that makes 2 calls cost 2¢ | 2nd call refused before reaching the provider |
| `max_turns: 3` with a model that always calls tools | stops after 3 turns with a `budget`-kind error |
| replay (with Phase 6) | 0 provider calls, same answer |
| live (opt-in) | Haiku answers "Reply with the word ok"; usage is settled into the budget |
