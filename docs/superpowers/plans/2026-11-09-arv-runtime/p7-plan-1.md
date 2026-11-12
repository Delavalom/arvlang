# Phase 7, task p7.1: `llm` and `tool` capability kinds

> **For agentic workers:** this file is one task, p7.1. Do it test first, exactly as written; if something here does not match the code you find, stop and report instead of improvising. Prerequisite: your tree is `feat/phases-3-8` with Phases 3, 4 and 5 merged and no Phase 7 work yet. Start every shell command with `cd <your worktree> &&` (absolute paths). Before committing: `gofmt -l .` prints nothing, `go vet ./...` is clean and `go test -race ./...` is green. End the commit message with a blank line and then `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`. Never use `--no-verify` and never push; keep Go files under 15 KB and markdown under 18 KB. `go.mod` says `go 1.24.0` (toolchain 1.26.2): `t.Chdir`, `min` and `max` are available; do not use `testing/synctest` or `sync.WaitGroup.Go`. Before a long test run, check `df -h /System/Volumes/Data`; below 1 GB free, stop and report (only `go clean -cache` may be deleted). The machine is heavily loaded: generous wall-clock bounds. No test may reach the network or spend money: never set `ARV_LIVE_ANTHROPIC`, and never put a real API key anywhere.
>
> Every code block below was applied, in task order, to a scratch copy of `feat/phases-3-8` (Phases 3, 4 and 5 merged; Phase 6 is not) and passed `gofmt -l .`, `go vet ./...` and `go test -race ./...` (including `e2e/`). Design: `14-phase7-agents.md`. Deviations, and why: `p7-plan-1.md`.

## Phase 7 overview

**Goal:** an agent is a task. Model access is a capability, each tool is a capability, tokens and dollars are budgets, and every model call is an effect (`14-phase7-agents.md`). `ask(model, prompt)` makes one call; `agent(model, goal, tools)` runs a model in a loop with Arv functions as its tools. The API key lives in the Go host and never becomes an Arv value.

**Guarantees, and the tasks that test them:**
- `ask` or `agent` without `llm(...)` for the model is denied before the host is looked at, and the provider gets no call (p7.8).
- A tool the task does not hold is not run: the model gets the `is_error` result `capability denied: tool("notes")` and the program goes on. A held tool runs in the agent's own task, so its effects need their own grants (p7.11, p7.15, p7.16).
- Every text that reaches a model (prompt, goal, tool result) has its capability denials redacted: no held grants, task IDs or dial-time addresses (p7.3, p7.8, p7.11).
- A call that `tokens` or `usd_cents` cannot cover is refused before any request; after the call the reservation settles to the real usage; `max_turns` stops an agent with a `budget` error (p7.6, p7.9, p7.11).
- Provider failures become `effect` errors with a kind (`auth`, `rate_limit`, `overloaded`, ...) (p7.4, p7.14).
- The API key appears in no Arv value, output, log or error; the CLI removes it from the environment before the run (p7.14, p7.15).
- No goroutine outlives a call or a run (p7.8, p7.11, p7.14).
- Every test uses a fake provider or a local `httptest` server. Exactly one live test exists, skipped unless `ARV_LIVE_ANTHROPIC=1` (p7.15). Do not run it.

**Invariants touched:** I1 and I2 (two new kinds, attenuated by `spawn ... with`), I6 (two new pools that only shrink; replies and tool results charge `alloc_mb`), I7 (model calls are nondeterministic input; Phase 6 must record them).

| Task | File | What |
|---|---|---|
| p7.1 | `p7-plan-1.md` | `llm` and `tool` capability kinds in `arv/runtime` |
| p7.2 | `p7-plan-2.md` | `llm(...)`, `tool(...)` values and `--allow` |
| p7.3 | `p7-plan-3.md` | denial redaction |
| p7.4 | `p7-plan-4.md` | `arv/llm`: types, errors, `Fake` |
| p7.5 | `p7-plan-5.md` | `arv/llm`: prices and `Host` |
| p7.6 | `p7-plan-6.md` | `tokens`, `usd_cents`, `max_turns` budget pools |
| p7.7 | `p7-plan-7.md` | scope options and CLI flags for them |
| p7.8 | `p7-plan-8.md` | `ask` |
| p7.9 | `p7-plan-9.md` | `ask` under budgets; `arv.Options.LLM` |
| p7.10 | `p7-plan-10.md` | agent tool definitions and arguments |
| p7.11 | `p7-plan-11.md` | the `agent` loop |
| p7.12 | `p7-plan-12.md` | non-string tool results; conversation tests |
| p7.13 | `p7-plan-13.md` | Anthropic wire format |
| p7.14 | `p7-plan-14.md` | Anthropic HTTP provider |
| p7.15 | `p7-plan-15.md` | CLI key handling; the opt-in live test |
| p7.16 | `p7-plan-16.md` | `examples/agent.arv`, README |
| p7.17 | `p7-plan-17.md` | `16o-phase7-notes.md` |

### Deviations from `14-phase7-agents.md`, and why

1. **No replay (design task 8).** Phase 6 is not on this base; it is built in parallel. Routing `ask` and `agent` turns through `effect()`, and the test that a replay makes zero provider calls, are follow-up work for p67.
2. **`anthropic-version: 2026-10-21`**, not `2023-06-01`: the `claude-api` skill, checked on 2026-11-11, gives it as the current version. Model IDs are from the same skill (`claude-sonnet-5-5`, `claude-haiku-5-5`). Server-side model fallbacks are not used: a fallback would answer, and bill, as a model the `llm(...)` grant does not name.
3. **Prices** (`arv/llm/pricing.go`) are copied from the `claude-api` skill's pricing table (its cache is dated 2026-11-03) on 2026-11-11, and the file records both dates. A `Host` can override them. A model with no price cannot run under `usd_cents`: that is a `budget` error before the call.
4. **Budget details.** The `usd` pool counts micro-dollars; its limit is `usd_cents * 10_000`. `max_turns` is a `Limits` field: a nested scope can only lower it, and the root's is 20 unless `--max-turns` says otherwise. Every call asks for `max_tokens: 4096` (not a script option); it reserves that plus an input estimate (the request's bytes plus overhead, since a token is at least a byte), and settles to the reported usage. Usage past the reservation spends the budget and ends the call with a `budget` error. A failed call settled to 0; review round 1 changed that (`16p`).
5. **CLI flags** `--tokens`, `--usd-cents` and `--max-turns` set the run's limits, like `--steps`.
6. **Tools.** The model is offered every key of the hash, sorted by name; a tool name must be a valid API tool name (`[A-Za-z0-9_-]{1,64}`). Parameters are the function's, all required strings; missing, extra or non-string arguments go back as an error result. The design's `tool_spec` is an optional fourth argument: a hash from tool name to description. A STRING result goes back as is; any other value as JSON (`json(v)`), or its printed form when JSON cannot hold it. A `budget` or `cancelled` error inside a tool stops the agent; any other error goes back as an error result.
7. **Redaction.** The design sends "the denial message" to the model; the brief forbids leaking the grants it lists. `RedactDenials` keeps the text before the first denial and replaces the rest with what was asked for: `capability denied: tool("notes")`. A net denial becomes `capability denied: net`, so a model cannot learn which names resolve to internal addresses. Prompts and goals are redacted too.
8. **Stop reasons.** `end_turn`, `stop_sequence` and `tool_use` are normal. `refusal`, `max_tokens` and anything else (`pause_turn` needs server tools, which Arv never offers) are `effect` errors, so a truncated answer is never returned as if it were whole.
9. **How the host passes providers.** `arv.Options.LLM *llm.Host` (providers and price overrides), carried on the run's context. The CLI builds it from `ANTHROPIC_API_KEY`, then removes that from the environment (`env=*` cannot read it; procfs: `16p`). `ARV_ANTHROPIC_URL` replaces the endpoint (tests use a local fake). Model calls need no `net` grant.
10. **The HTTP client** honours proxy settings (the host configured this endpoint; `http_get`'s policy is for Arv's own requests) and disables keep-alives, so no idle connection goroutine outlives a run. Replies over 16 MiB are refused. The key is scrubbed from every error text. (As built it also never follows a redirect; `16p`.)
11. **The live test** is `TestLiveAnthropic` in package `arv`, gated on `ARV_LIVE_ANTHROPIC=1` (the brief's name, not `ARV_LIVE_TESTS`). It sends the design's prompt to Haiku twice under `tokens: 6000`, which only fits if the first call was settled to its real usage. It uses the shared `max_tokens: 4096`, not 50; a one-word reply costs the same.

### Task p7.1: `llm` and `tool` capability kinds

Two new `runtime.Kind`s. `llm`'s scope is `"provider:model"`, `"provider:*"` or `"*"`; `tool`'s is a tool name or `"*"`. Coverage is an exact match or a wildcard; `"anthropic:*"` covers every Anthropic model and nothing else. Both are pure: no name is looked up. `Literal` (and so `Parse`, `New`, the CLI and the Arv constructors) validates them; a bad scope's error quotes at most 64 bytes of it. Tool names follow the Messages API (`[A-Za-z0-9_-]{1,64}`); `ToolName` is exported because `agent` validates hash keys with it (p7.10).

**Files:**
- Create: `arv/runtime/agentcap.go`
- Modify: `arv/runtime/capability.go`
- Create (test): `arv/runtime/agentcap_test.go`

- [ ] **Step 1: Write the failing tests**

Create `arv/runtime/agentcap_test.go`:

```go
package runtime

import (
	"strings"
	"testing"
)

func TestAgentScopesValidate(t *testing.T) {
	good := []Capability{
		{LLM, "*"}, {LLM, "anthropic:*"}, {LLM, "anthropic:claude-sonnet-5-5"},
		{LLM, "fake:model.v1@2026"}, {Tool, "*"}, {Tool, "weather"}, {Tool, "get_weather-2"},
		{Tool, strings.Repeat("a", MaxToolName)},
	}
	for _, c := range good {
		got, err := Literal(c.Kind, c.Scope)
		if err != nil || got != c {
			t.Errorf("Literal(%s) = %v, %v; want it unchanged", c, got, err)
		}
	}
	bad := []Capability{
		{LLM, "anthropic"}, {LLM, ":model"}, {LLM, "Anthropic:x"}, {LLM, "anthropic:"},
		{LLM, "anthropic:-x"}, {LLM, "anthropic:a b"}, {LLM, "*:x"}, {LLM, "anthropic:x:y"},
		{Tool, "a b"}, {Tool, "a.b"}, {Tool, "we*"}, {Tool, strings.Repeat("a", MaxToolName+1)},
		{LLM, ""}, {Tool, ""},
	}
	for _, c := range bad {
		if _, err := Literal(c.Kind, c.Scope); err == nil {
			t.Errorf("Literal(%s) succeeded, want an error", c)
		}
	}
}

func TestAgentCoverage(t *testing.T) {
	cases := []struct {
		grant, req Capability
		want       bool
	}{
		{Capability{LLM, "anthropic:claude-haiku-5-5"}, Capability{LLM, "anthropic:claude-haiku-5-5"}, true},
		{Capability{LLM, "anthropic:claude-haiku-5-5"}, Capability{LLM, "anthropic:claude-sonnet-5-5"}, false},
		{Capability{LLM, "anthropic:*"}, Capability{LLM, "anthropic:claude-sonnet-5-5"}, true},
		{Capability{LLM, "anthropic:*"}, Capability{LLM, "anthropic:*"}, true},
		{Capability{LLM, "anthropic:*"}, Capability{LLM, "anthropicx:m"}, false},
		{Capability{LLM, "anthropic:*"}, Capability{LLM, "*"}, false},
		{Capability{LLM, "*"}, Capability{LLM, "fake:m"}, true},
		{Capability{LLM, "anthropic:claude-haiku-5-5"}, Capability{LLM, "anthropic:*"}, false},
		{Capability{Tool, "weather"}, Capability{Tool, "weather"}, true},
		{Capability{Tool, "weather"}, Capability{Tool, "notes"}, false},
		{Capability{Tool, "*"}, Capability{Tool, "notes"}, true},
		{Capability{Tool, "weather"}, Capability{Tool, "*"}, false},
		{Capability{Tool, "*"}, Capability{LLM, "*"}, false},
	}
	for _, c := range cases {
		if got := c.grant.Covers(c.req); got != c.want {
			t.Errorf("%s covers %s = %v, want %v", c.grant, c.req, got, c.want)
		}
	}
}

// I2: a child can narrow an llm or tool grant, never widen it.
func TestAgentAttenuation(t *testing.T) {
	parent := NewCapSet(Capability{LLM, "anthropic:*"}, Capability{Tool, "weather"})
	if _, err := parent.Attenuate([]Capability{{LLM, "anthropic:claude-haiku-5-5"}, {Tool, "weather"}}); err != nil {
		t.Fatalf("narrowing: %v", err)
	}
	for _, wider := range []Capability{{LLM, "*"}, {LLM, "other:m"}, {Tool, "*"}, {Tool, "notes"}} {
		_, err := parent.Attenuate([]Capability{wider})
		if _, ok := err.(*DeniedError); !ok {
			t.Errorf("Attenuate(%s) = %v, want *DeniedError", wider, err)
		}
	}
}

func TestParseAgentGrants(t *testing.T) {
	for in, want := range map[string]Capability{
		"llm=anthropic:*":               {LLM, "anthropic:*"},
		"llm=anthropic:claude-opus-5-5": {LLM, "anthropic:claude-opus-5-5"},
		"tool=weather":                  {Tool, "weather"},
		"tool=*":                        {Tool, "*"},
	} {
		got, err := Parse(in, "/")
		if err != nil || got != want {
			t.Errorf("Parse(%q) = %v, %v; want %v", in, got, err, want)
		}
	}
	if _, err := Parse("llm=anthropic", "/"); err == nil {
		t.Error(`Parse("llm=anthropic") succeeded`)
	}
}

func TestAgentScopeErrorIsClipped(t *testing.T) {
	_, err := Literal(Tool, strings.Repeat("x", 4000)+" ")
	if err == nil || len(err.Error()) > 200 {
		t.Fatalf("error = %v (%d bytes), want a short error", err, len(err.Error()))
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test -run '^(TestAgentScopesValidate|TestAgentCoverage|TestAgentAttenuation|TestParseAgentGrants|TestAgentScopeErrorIsClipped)$' ./arv/runtime/`

Expected: FAIL: the package does not compile. The output includes:

```text
# github.com/delavalom/arvlang/arv/runtime [github.com/delavalom/arvlang/arv/runtime.test]
arv/runtime/agentcap_test.go:10:4: undefined: LLM
arv/runtime/agentcap_test.go:11:4: undefined: LLM
arv/runtime/agentcap_test.go:11:33: undefined: Tool
```

- [ ] **Step 3: Implement**

In `arv/runtime/capability.go`, replace:

```go
	Env     Kind = "env"      // read one environment variable
)

var kinds = map[Kind]bool{Net: true, FSRead: true, FSWrite: true, Env: true}
```

with:

```go
	Env     Kind = "env"      // read one environment variable
	LLM     Kind = "llm"      // call one model: "provider:model", "provider:*" or "*"
	Tool    Kind = "tool"     // let a model call one tool: a name, or "*"
)

var kinds = map[Kind]bool{Net: true, FSRead: true, FSWrite: true, Env: true, LLM: true, Tool: true}
```

In `arv/runtime/capability.go`, before:

```go
	}
	return Capability{Kind: kind, Scope: scope}, nil
```

add:

```go
	}
	if kind == LLM || kind == Tool {
		if err := agentScope(kind, scope); err != nil {
			return Capability{}, fmt.Errorf("%s: %w", kind, err)
		}
```

In `arv/runtime/capability.go`, replace:

```go
		return c.Scope == "*" || c.Scope == r.Scope
	}
```

with:

```go
		return c.Scope == "*" || c.Scope == r.Scope
	case LLM, Tool:
		return agentCovers(c.Scope, r.Scope)
	}
```

Create `arv/runtime/agentcap.go`:

```go
package runtime

import (
	"fmt"
	"strings"
)

// MaxToolName is the longest tool name a tool grant may name: the
// Anthropic Messages API's limit for a tool's name.
const MaxToolName = 64

// maxModelLen caps the model part of an llm scope. Model IDs are short
// (claude-sonnet-5-5); this only keeps an error from quoting a long one.
const maxModelLen = 128

// agentScope validates the scope of an llm or tool capability. Both are
// pure: a model or tool name is not looked up anywhere.
//
//   - llm: "*" (any model of any provider), "provider:*" (any model of one
//     provider) or "provider:model". A provider is a lowercase name
//     ([a-z][a-z0-9_-]*, at most 32 bytes); a model is [A-Za-z0-9._@-]+,
//     at most 128 bytes, starting with a letter or digit.
//   - tool: "*" (any tool) or a name of [A-Za-z0-9_-]+, at most
//     MaxToolName bytes, as the Messages API requires of tool names.
func agentScope(kind Kind, scope string) error {
	if scope == "*" {
		return nil
	}
	if kind == Tool {
		if !ToolName(scope) {
			return fmt.Errorf("tool name %q: want 1 to %d of A-Z, a-z, 0-9, _ and -, or *", clip(scope), MaxToolName)
		}
		return nil
	}
	provider, model, ok := strings.Cut(scope, ":")
	if !ok {
		return fmt.Errorf("%q: want provider:model, provider:* or *", clip(scope))
	}
	if !providerName(provider) {
		return fmt.Errorf("provider %q: want a lowercase name of a-z, 0-9, _ and -", clip(provider))
	}
	if model != "*" && !modelName(model) {
		return fmt.Errorf("model %q: want a name of A-Z, a-z, 0-9, ., _, @ and -, or *", clip(model))
	}
	return nil
}

// ToolName reports whether s is a valid tool name (not "*").
func ToolName(s string) bool {
	if s == "" || len(s) > MaxToolName {
		return false
	}
	for i := 0; i < len(s); i++ {
		if c := s[i]; !alnum(c) && c != '_' && c != '-' {
			return false
		}
	}
	return true
}

func providerName(s string) bool {
	if s == "" || len(s) > 32 || s[0] < 'a' || s[0] > 'z' {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !(c >= 'a' && c <= 'z') && !(c >= '0' && c <= '9') && c != '_' && c != '-' {
			return false
		}
	}
	return true
}

func modelName(s string) bool {
	if s == "" || len(s) > maxModelLen || !alnum(s[0]) {
		return false
	}
	for i := 0; i < len(s); i++ {
		if c := s[i]; !alnum(c) && !strings.ContainsRune("._@-", rune(c)) {
			return false
		}
	}
	return true
}

func alnum(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
}

// clip shortens s for an error message: a scope can be MaxScopeLen bytes.
func clip(s string) string {
	if len(s) > 64 {
		return s[:64] + "..."
	}
	return s
}

// agentCovers reports whether llm or tool grant g covers request r, both
// valid: "*" covers everything, "provider:*" every model of that provider,
// and anything else only itself. A request is compared as written: a
// wildcard request is covered only by a grant at least as wide.
func agentCovers(g, r string) bool {
	if g == "*" || g == r {
		return true
	}
	if p, ok := strings.CutSuffix(g, ":*"); ok {
		rp, _, found := strings.Cut(r, ":")
		return found && rp == p
	}
	return false
}
```

- [ ] **Step 4: Run to verify it passes**

Run: `go test -race -run '^(TestAgentScopesValidate|TestAgentCoverage|TestAgentAttenuation|TestParseAgentGrants|TestAgentScopeErrorIsClipped)$' ./arv/runtime/`, then `gofmt -l .`, `go vet ./...` and `go test -race ./...`.
Expected: PASS everywhere.

- [ ] **Step 5: Commit**

```bash
git add arv/runtime/agentcap_test.go arv/runtime/capability.go arv/runtime/agentcap.go
git commit -m "feat(runtime): llm and tool capability kinds

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```
