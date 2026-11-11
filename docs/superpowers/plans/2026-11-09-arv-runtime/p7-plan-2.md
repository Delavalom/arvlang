# Phase 7, task p7.2: `llm(...)`, `tool(...)` values and `--allow`

> **For agentic workers:** this file is one task, p7.2. Do it test first, exactly as written; if something here does not match the code you find, stop and report instead of improvising. Prerequisite: task p7.1 is committed in your tree. Start every shell command with `cd <your worktree> &&` and use absolute paths: the shell's working directory does not persist. Before committing: `gofmt -l .` prints nothing, `go vet ./...` is clean and `go test -race ./...` is green. End the commit message with a blank line and then `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`. Never use `--no-verify` and never push; keep Go files under 15 KB and markdown under 18 KB. `go.mod` says `go 1.24.0` (toolchain 1.26.2): `t.Chdir`, `min` and `max` are available; do not use `testing/synctest` or `sync.WaitGroup.Go`. Before a long test run, check `df -h /System/Volumes/Data`; below 1 GB free, stop and report (only `go clean -cache` may be deleted). This machine is heavily loaded: keep wall-clock bounds generous. No test may reach the network or spend money: never set `ARV_LIVE_ANTHROPIC`, and never put a real API key anywhere.
>
> Every code block below was applied, in task order, to a scratch copy of `feat/phases-3-8` (Phases 3, 4 and 5 merged; Phase 6 is not) and passed `gofmt -l .`, `go vet ./...` and `go test -race ./...` (including `e2e/`). Design: `14-phase7-agents.md`. Deviations from it, and why: `p7-plan-1.md`.

### Task p7.2: `llm(...)`, `tool(...)` values and `--allow`

Arv code builds the new grants with `llm("anthropic:claude-sonnet-5-5")` and `tool("weather")`, like `net(...)`, and `spawn ... with` attenuates them (I2). The CLI already accepts `--allow llm=...` through `runtime.Parse`; its usage text now lists the kinds.

**Files:**
- Modify: `arv/evaluator/effects.go`
- Modify: `main.go`
- Create (test): `arv/evaluator/agentcap_test.go`
- Create (test): `main_agent_test.go`

- [ ] **Step 1: Write the failing tests**

Create `arv/evaluator/agentcap_test.go`:

```go
package evaluator

import (
	"testing"

	"github.com/delavalom/arvlang/arv/runtime"
	"github.com/delavalom/arvlang/arv/value"
)

func TestAgentCapabilityValues(t *testing.T) {
	for src, want := range map[string]string{
		`llm("anthropic:claude-sonnet-5-5")`: `llm("anthropic:claude-sonnet-5-5")`,
		`llm("anthropic:*")`:                 `llm("anthropic:*")`,
		`tool("weather")`:                    `tool("weather")`,
	} {
		got, _ := runWith(t, src)
		if c, ok := got.(*value.Capability); !ok || c.Inspect() != want {
			t.Errorf("%s = %v, want %s", src, got, want)
		}
	}
	got, _ := runWith(t, `tool("no spaces")`)
	wantErrorHas(t, got, "tool name")
}

// I2: spawn hands a child an llm or tool grant only if the parent holds it.
func TestSpawnAttenuatesAgentGrants(t *testing.T) {
	src := `scope { await spawn len("x") with [llm("anthropic:claude-haiku-5-5"), tool("weather")] }`
	got, _ := runWith(t, src, grant(t, runtime.LLM, "anthropic:*"), grant(t, runtime.Tool, "*"))
	if i, ok := got.(*value.Integer); !ok || i.Value != 1 {
		t.Fatalf("got %v, want 1", got)
	}
	got, _ = runWith(t, src, grant(t, runtime.LLM, "anthropic:claude-opus-5-5"), grant(t, runtime.Tool, "*"))
	wantDenied(t, got, `llm("anthropic:claude-haiku-5-5")`)
}
```

Create `main_agent_test.go`:

```go
package main

import (
	"strings"
	"testing"

	"github.com/delavalom/arvlang/arv/runtime"
)

func TestAllowAgentGrants(t *testing.T) {
	opts, file, err := parseRun([]string{"--allow", "llm=anthropic:*", "--allow", "tool=weather", "x.arv"}, "/")
	if err != nil || file != "x.arv" {
		t.Fatalf("parseRun: %v, %q", err, file)
	}
	want := []runtime.Capability{{Kind: runtime.LLM, Scope: "anthropic:*"}, {Kind: runtime.Tool, Scope: "weather"}}
	if len(opts.Caps) != 2 || opts.Caps[0] != want[0] || opts.Caps[1] != want[1] {
		t.Fatalf("caps = %v, want %v", opts.Caps, want)
	}
	if !strings.Contains(usage, "llm=PROVIDER:MODEL") || !strings.Contains(usage, "tool=NAME") {
		t.Fatal("usage does not list the llm and tool kinds")
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test -run '^(TestAgentCapabilityValues|TestSpawnAttenuatesAgentGrants|TestAllowAgentGrants)$' ./arv/evaluator/ .`

Expected: FAIL: `identifier not found: llm`, and the usage check. The output includes:

```text
    agentcap_test.go:18: llm("anthropic:claude-sonnet-5-5") = &{identifier not found: llm  1:1 runtime root}, want llm("anthropic:claude-sonnet-5-5")
    agentcap_test.go:18: llm("anthropic:*") = &{identifier not found: llm  1:1 runtime root}, want llm("anthropic:*")
    agentcap_test.go:18: tool("weather") = &{identifier not found: tool  1:1 runtime root}, want tool("weather")
    agentcap_test.go:22: got ERROR ERROR: identifier not found: tool, want an error mentioning "tool name"
```

- [ ] **Step 3: Implement**

In `arv/evaluator/effects.go`, replace:

```go
		"net": runtime.Net, "fs_read": runtime.FSRead, "fs_write": runtime.FSWrite, "env": runtime.Env,
	} {
```

with:

```go
		"net": runtime.Net, "fs_read": runtime.FSRead, "fs_write": runtime.FSWrite, "env": runtime.Env,
		"llm": runtime.LLM, "tool": runtime.Tool,
	} {
```

In `main.go`, replace:

```go
                       kinds: net=HOST[:PORT] (HOST: name, *.domain, *, IP) or net=CIDR,
                              fs_read=PATH, fs_write=PATH, env=NAME
  --timeout D          stop the run after D, e.g. 30s (default 10m; 0 for no limit)
```

with:

```go
                       kinds: net=HOST[:PORT] (HOST: name, *.domain, *, IP) or net=CIDR,
                              fs_read=PATH, fs_write=PATH, env=NAME,
                              llm=PROVIDER:MODEL (or PROVIDER:*, *), tool=NAME (or *)
  --timeout D          stop the run after D, e.g. 30s (default 10m; 0 for no limit)
```

- [ ] **Step 4: Run to verify it passes**

Run: `go test -race -run '^(TestAgentCapabilityValues|TestSpawnAttenuatesAgentGrants|TestAllowAgentGrants)$' ./arv/evaluator/ .`, then `gofmt -l .`, `go vet ./...` and `go test -race ./...`.
Expected: PASS everywhere.

- [ ] **Step 5: Commit**

```bash
git add arv/evaluator/agentcap_test.go main_agent_test.go arv/evaluator/effects.go main.go
git commit -m "feat(evaluator): llm and tool capability values

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```
