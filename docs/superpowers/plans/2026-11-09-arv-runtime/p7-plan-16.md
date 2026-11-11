# Phase 7, task p7.16: `examples/agent.arv` and the README

> **For agentic workers:** this file is one task, p7.16. Do it test first, exactly as written; if something here does not match the code you find, stop and report instead of improvising. Prerequisite: task p7.15 is committed in your tree. Start every shell command with `cd <your worktree> &&` and use absolute paths: the shell's working directory does not persist. Before committing: `gofmt -l .` prints nothing, `go vet ./...` is clean and `go test -race ./...` is green. End the commit message with a blank line and then `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`. Never use `--no-verify` and never push; keep Go files under 15 KB and markdown under 18 KB. `go.mod` says `go 1.24.0` (toolchain 1.26.2): `t.Chdir`, `min` and `max` are available; do not use `testing/synctest` or `sync.WaitGroup.Go`. Before a long test run, check `df -h /System/Volumes/Data`; below 1 GB free, stop and report (only `go clean -cache` may be deleted). This machine is heavily loaded: keep wall-clock bounds generous. No test may reach the network or spend money: never set `ARV_LIVE_ANTHROPIC`, and never put a real API key anywhere.
>
> Every code block below was applied, in task order, to a scratch copy of `feat/phases-3-8` (Phases 3, 4 and 5 merged; Phase 6 is not) and passed `gofmt -l .`, `go vet ./...` and `go test -race ./...` (including `e2e/`). Design: `14-phase7-agents.md`. Deviations from it, and why: `p7-plan-1.md`.

### Task p7.16: `examples/agent.arv` and the README

The design's example, made runnable: `weather` reads a local forecast file instead of calling a weather API, and the agent holds `tool("weather")` and `fs_read` of that one file, but not `tool("notes")`. Its test runs it through the CLI against the fake API from p7.15: the model asks for `notes`, gets `capability denied: tool("notes")`, asks for `weather`, and answers. Without an `llm` grant the run fails with the denial.

**Files:**
- Create: `examples/agent.arv`
- Create: `examples/weather.txt`
- Modify: `README.md`
- Create (test): `example_agent_test.go`

- [ ] **Step 1: Write the failing test**

Create `example_agent_test.go`:

```go
package main

import (
	"strings"
	"testing"
)

// examples/agent.arv runs as its header says, against a local fake of the
// Messages API: the model asks for both tools, weather runs with the
// agent's grants, notes is refused as a tool result, and the answer is
// printed.
func TestAgentExample(t *testing.T) {
	api := newFakeAPI(t, 200,
		`{"content":[{"type":"tool_use","id":"n","name":"notes","input":{"path":"/etc/hosts"}},`+
			`{"type":"tool_use","id":"w","name":"weather","input":{"city":"Lisbon"}}],"stop_reason":"tool_use","usage":{"input_tokens":300,"output_tokens":40}}`,
		textReply("Yes: it is sunny and dry in Lisbon."))
	code, out, stderr := cli("run", "--allow", "llm=anthropic:claude-sonnet-5-5", "--allow", "tool=weather",
		"--allow", "fs_read=examples/weather.txt", "examples/agent.arv")
	if code != 0 || out != "Yes: it is sunny and dry in Lisbon.\n" {
		t.Fatalf("exit %d, out %q, err %q", code, out, stderr)
	}
	if len(api.bodies) != 2 {
		t.Fatalf("%d requests, want 2", len(api.bodies))
	}
	second := api.bodies[1]
	for _, want := range []string{
		`"tool_use_id":"n"`, `capability denied: tool(\"notes\")`,
		`"tool_use_id":"w"`, `Forecast for Lisbon: Sunny, 22 C`,
		`"description":"Read a file of personal notes"`,
	} {
		if !strings.Contains(second, want) {
			t.Errorf("second request lacks %s:\n%s", want, second)
		}
	}
	noKey(t, append([]string{out, stderr}, api.bodies...)...)
	// Without the model grant the spawn is refused before any request.
	newFakeAPI(t, 200, textReply("unused"))
	code, _, stderr = cli("run", "--allow", "tool=weather", "--allow", "fs_read=examples/weather.txt", "examples/agent.arv")
	if code != 1 || !strings.Contains(stderr, `capability denied: llm("anthropic:claude-sonnet-5-5")`) {
		t.Fatalf("exit %d, err %q", code, stderr)
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test -run '^(TestAgentExample)$' .`

Expected: FAIL: `examples/agent.arv` does not exist. The output includes:

```text
    example_agent_test.go:20: exit 1, out "", err "error: open examples/agent.arv: no such file or directory\n"
```

- [ ] **Step 3: Implement**

Create `examples/agent.arv`:

```arv
// An agent is a task. It may call the model it holds as llm(...), and a
// tool only if it holds tool(name); the tool's own effects need their own
// grants. The model is offered both tools below, but this agent holds only
// tool("weather") and read access to one file, so if the model asks for
// notes it gets "capability denied" back as the tool's result, and the
// run goes on. Tokens, dollars and time are scope budgets.
//
//   ANTHROPIC_API_KEY=... arv run --allow llm=anthropic:claude-sonnet-5-5 \
//     --allow tool=weather --allow fs_read=examples/weather.txt examples/agent.arv

let model = "anthropic:claude-sonnet-5-5";

let tools = {
  "weather": fn(city) { "Forecast for " + city + ": " + read_file("examples/weather.txt") },
  "notes":   fn(path) { read_file(path) }
};
let about = {
  "weather": "Today's weather forecast for a city",
  "notes":   "Read a file of personal notes"
};

let answer = scope (tokens: 50000, usd_cents: 25, timeout_ms: 60000, max_turns: 5) {
  await spawn agent(model, "Should I bike to work in Lisbon today? Answer in one sentence.", tools, about)
    with [llm(model), tool("weather"), fs_read("examples/weather.txt")]
};
puts(answer);
```

Create `examples/weather.txt`:

```text
Sunny, 22 C, light wind from the north, no rain expected.
```

In `README.md`, replace:

```markdown
- **Supervision and errors as values.** `scope (supervise: "collect") { ... }` keeps going when a task fails, and `scope (supervise: "restart", retries: 3) { ... }` runs a failed task again. `match` looks at an error's kind instead of crashing.
- **Planned:** token and dollar budgets, durable replay, and LLM agents whose tools are capabilities. See `docs/superpowers/plans/2026-11-09-arv-runtime/`.
```

with:

```markdown
- **Supervision and errors as values.** `scope (supervise: "collect") { ... }` keeps going when a task fails, and `scope (supervise: "restart", retries: 3) { ... }` runs a failed task again. `match` looks at an error's kind instead of crashing.
- **Agents on the same model.** `ask(model, prompt)` and `agent(model, goal, tools)` call a language model. The model is a capability (`llm("anthropic:claude-sonnet-5-5")`), each tool is one (`tool("weather")`), tokens and dollars are scope budgets, and the API key stays in the host.
- **Planned:** durable replay. See `docs/superpowers/plans/2026-11-09-arv-runtime/`.
```

In `README.md`, after:

```markdown
| `env` | a variable name | `--allow env=HOME` |
```

add:

```markdown
| `llm` | `provider:model`, `provider:*` or `*` | `--allow llm=anthropic:claude-sonnet-5-5` |
| `tool` | a tool name, or `*` | `--allow tool=weather` |
```

In `README.md`, replace:

````markdown
bin/arv                          # REPL, no capabilities
bin/arv run [--allow kind=scope]... [--timeout 30s] [--steps N] [--tasks N] [--alloc-mb N] FILE
```
````

with:

````markdown
bin/arv                          # REPL, no capabilities
bin/arv run [--allow kind=scope]... [--timeout 30s] [--steps N] [--tasks N] [--alloc-mb N]
            [--tokens N] [--usd-cents N] [--max-turns N] FILE
```
````

In `README.md`, replace:

```markdown
| `alloc_mb` | MiB of values created under the scope: strings, arrays, hashes, closures, environments and bindings (never refunded) | error |

Option values are positive integers; an unknown or repeated option is an error. Limits nest: a scope inside `scope (steps: 1000)` can never spend more than what is left of those 1000, whatever it asks for.
```

with:

````markdown
| `alloc_mb` | MiB of values created under the scope: strings, arrays, hashes, closures, environments and bindings (never refunded) | error |
| `tokens` | model tokens, input and output, of every `ask` and `agent` call under the scope | the call is refused before it is sent |
| `usd_cents` | what those calls cost, in US cents, at the prices in `arv/llm/pricing.go` | the call is refused before it is sent |
| `max_turns` | model calls one `agent` may make (default 20) | error |

Option values are positive integers; an unknown or repeated option is an error. Limits nest: a scope inside `scope (steps: 1000)` can never spend more than what is left of those 1000, whatever it asks for.

## Agents

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

- **The model is a grant.** `ask` and `agent` need `llm(...)` for the model they name. The CLI reads `ANTHROPIC_API_KEY` and removes it from the environment before the run, so not even `--allow env=*` lets the program read it. A Go host passes `Options.LLM` (`arv/llm`): its providers, with their keys.
- **Each tool is a grant, and so are its effects.** The model is offered every tool in the hash; a tool's parameters are its function's, all strings. The agent above holds `tool("weather")` and not `tool("notes")`, so if the model asks for `notes` it gets the tool result `capability denied: tool("notes")` and the run goes on. `weather` runs in the agent's own task, so its `http_get` needs `net("api.weather.example")` too. A tool's error goes back to the model as an error result.
- **The model never sees your grants.** A denial that reaches a model (a tool result, a prompt) is cut to what was asked for: `capability denied: tool("notes")`, `capability denied: net`.
- **Budgets are checked before each call.** A call reserves its most output (4096 tokens) plus an estimate of its input, refuses to start if `tokens` or `usd_cents` cannot cover that, and then settles to the real usage. `agent(model, goal, tools, descriptions)` takes an optional hash of tool descriptions.
- `examples/agent.arv` runs one end to end; the tests use a fake provider and never call the network.
````

In `README.md`, replace:

```markdown
| `cancelled` | the task's scope was cancelled |
| `effect` | the world: connection refused, file not found |
| `runtime` | the program: a type error, bad arguments, division by zero |
```

with:

```markdown
| `cancelled` | the task's scope was cancelled |
| `effect` | the world: connection refused, file not found, a model provider's error (`ask: anthropic: rate_limit (HTTP 429): ...`) |
| `runtime` | the program: a type error, bad arguments, division by zero |
```

In `README.md`, replace:

```markdown
Literals are numbers, booleans, strings, arrays, maps and functions. There are `let` bindings, `if`/`else` expressions, closures and recursion. `for x in xs { ... }` evaluates to the array of its body's values, so `await for url in urls { spawn check(url) }` starts one task per URL and collects their results; `await [t1, t2]` awaits each task in order. Strings compare with `==` and `!=`. Builtins include `len`, `push`, `last`, `puts`, `error`, `http_get`, `read_file`, `write_file`, `getenv` and `sleep`, plus `lines(s)` (the non-blank lines of `s`, trimmed), `split(s, sep)`, `json(v)` (compact JSON, hash keys in order) and `clock_ms()` (a monotonic clock, for latencies).
```

with:

```markdown
Literals are numbers, booleans, strings, arrays, maps and functions. There are `let` bindings, `if`/`else` expressions, closures and recursion. `for x in xs { ... }` evaluates to the array of its body's values, so `await for url in urls { spawn check(url) }` starts one task per URL and collects their results; `await [t1, t2]` awaits each task in order. Strings compare with `==` and `!=`. Builtins include `len`, `push`, `last`, `puts`, `error`, `http_get`, `read_file`, `write_file`, `getenv`, `sleep`, `ask` and `agent`, plus `lines(s)` (the non-blank lines of `s`, trimmed), `split(s, sep)`, `json(v)` (compact JSON, hash keys in order) and `clock_ms()` (a monotonic clock, for latencies).
```

- [ ] **Step 4: Run to verify it passes**

Run: `go test -race -run '^(TestAgentExample)$' .`, then `gofmt -l .`, `go vet ./...` and `go test -race ./...`.
Expected: PASS everywhere.

- [ ] **Step 5: Commit**

```bash
git add example_agent_test.go examples/agent.arv examples/weather.txt README.md
git commit -m "docs: agent example and README section

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```
