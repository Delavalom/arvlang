# Phase 7, task p7.7: scope options and CLI flags for model budgets

> **For agentic workers:** this file is one task, p7.7. Do it test first, exactly as written; if something here does not match the code you find, stop and report instead of improvising. Prerequisite: task p7.6 is committed in your tree. Start every shell command with `cd <your worktree> &&` and use absolute paths: the shell's working directory does not persist. Before committing: `gofmt -l .` prints nothing, `go vet ./...` is clean and `go test -race ./...` is green. End the commit message with a blank line and then `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`. Never use `--no-verify` and never push; keep Go files under 15 KB and markdown under 18 KB. `go.mod` says `go 1.24.0` (toolchain 1.26.2): `t.Chdir`, `min` and `max` are available; do not use `testing/synctest` or `sync.WaitGroup.Go`. Before a long test run, check `df -h /System/Volumes/Data`; below 1 GB free, stop and report (only `go clean -cache` may be deleted). This machine is heavily loaded: keep wall-clock bounds generous. No test may reach the network or spend money: never set `ARV_LIVE_ANTHROPIC`, and never put a real API key anywhere.
>
> Every code block below was applied, in task order, to a scratch copy of `feat/phases-3-8` (Phases 3, 4 and 5 merged; Phase 6 is not) and passed `gofmt -l .`, `go vet ./...` and `go test -race ./...` (including `e2e/`). Design: `14-phase7-agents.md`. Deviations from it, and why: `p7-plan-1.md`.

### Task p7.7: scope options and CLI flags for model budgets

`scope (tokens: N, usd_cents: N, max_turns: N)` and `arv run --tokens N --usd-cents N --max-turns N`. Values are positive integers like the other options; `usd_cents` is capped so its micro-dollar limit fits an int64, and `max_turns` at 10 000.

**Files:**
- Modify: `arv/evaluator/scope_options.go`
- Modify: `main.go`
- Create (test): `arv/evaluator/agent_options_test.go`
- Modify (test): `main_agent_test.go`

- [ ] **Step 1: Write the failing tests**

Create `arv/evaluator/agent_options_test.go`:

```go
package evaluator

import (
	"math"
	"testing"

	"github.com/delavalom/arvlang/arv/runtime"
)

func TestModelScopeOptions(t *testing.T) {
	got, _ := runWith(t, `scope (tokens: 50000, usd_cents: 25, max_turns: 3) { 1 + 1 }`)
	if got.Inspect() != "2" {
		t.Fatalf("got %s, want 2", got.Inspect())
	}
	for _, tt := range []struct{ input, msg string }{
		{`scope (tokens: 0) { 1 }`, "scope option tokens: want a positive INTEGER, got 0"},
		{`scope (usd_cents: "1") { 1 }`, "scope option usd_cents: want a positive INTEGER, got STRING"},
		{`scope (usd_cents: 922337203685478) { 1 }`, "scope option usd_cents: 922337203685478 is too large (max 922337203685477)"},
		{`scope (max_turns: 10001) { 1 }`, "scope option max_turns: 10001 is too large (max 10000)"},
	} {
		got, _ := runWith(t, tt.input)
		wantErrorAt(t, got, tt.msg, "1:8")
	}
	if runtime.MaxUSDCents != math.MaxInt64/10_000 {
		t.Fatal("MaxUSDCents changed: update the error above")
	}
}
```

In `main_agent_test.go`, after:

```go
	}
}
```

add:

```go

func TestModelBudgetFlags(t *testing.T) {
	opts, _, err := parseRun([]string{"--tokens", "50000", "--usd-cents", "25", "--max-turns", "3", "x.arv"}, "/")
	if err != nil {
		t.Fatal(err)
	}
	if l := opts.Limits; l.Tokens != 50000 || l.USDCents != 25 || l.MaxTurns != 3 {
		t.Fatalf("limits = %+v", l)
	}
	script := writeScript(t, "1")
	for _, args := range [][]string{{"--tokens", "0"}, {"--usd-cents", "922337203685478"}, {"--max-turns", "x"}} {
		code, _, stderr := cli(append(append([]string{"run"}, args...), script)...)
		if code != 2 || !strings.HasPrefix(stderr, "error: "+args[0]) {
			t.Errorf("%v: exit %d, stderr %q", args, code, stderr)
		}
	}
	for _, flag := range []string{"--tokens N", "--usd-cents N", "--max-turns N"} {
		if !strings.Contains(usage, flag) {
			t.Errorf("usage does not list %s", flag)
		}
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test -run '^(TestModelScopeOptions|TestAllowAgentGrants|TestModelBudgetFlags)$' ./arv/evaluator/ .`

Expected: FAIL: the options are unknown and the flags are not parsed. The output includes:

```text
    agent_options_test.go:13: got ERROR: unknown scope option "tokens", want 2
    main_agent_test.go:27: unknown flag "--tokens"
        usage:
          arv                    start the REPL (no capabilities)
```

- [ ] **Step 3: Implement**

In `arv/evaluator/scope_options.go`, replace:

```go
	maxBackoffMS = int64(runtime.MaxBackoff / time.Millisecond)
)
```

with:

```go
	maxBackoffMS = int64(runtime.MaxBackoff / time.Millisecond)
	maxTurns     = 10_000
)
```

In `arv/evaluator/scope_options.go`, replace:

```go
	"retries": maxRetries, "backoff_ms": maxBackoffMS,
}
```

with:

```go
	"retries": maxRetries, "backoff_ms": maxBackoffMS,
	"tokens": math.MaxInt64, "usd_cents": runtime.MaxUSDCents, "max_turns": maxTurns,
}
```

In `arv/evaluator/scope_options.go`, replace:

```go
				restart.Backoff = time.Duration(n) * time.Millisecond
			}
```

with:

```go
				restart.Backoff = time.Duration(n) * time.Millisecond
			case "tokens":
				l.Tokens = n
			case "usd_cents":
				l.USDCents = n
			case "max_turns":
				l.MaxTurns = int(n)
			}
```

In `main.go`, replace:

```go
  --tasks N            at most N tasks alive at once; one more is an error (default 1000)
  --alloc-mb N         allocate at most N MiB of Arv values in all (default: no limit)`
```

with:

```go
  --tasks N            at most N tasks alive at once; one more is an error (default 1000)
  --alloc-mb N         allocate at most N MiB of Arv values in all (default: no limit)
  --tokens N           model tokens, input and output, of all calls (default: no limit)
  --usd-cents N        what model calls may cost in all, in US cents (default: no limit)
  --max-turns N        model calls one agent(...) may make (default 20)`
```

In `main.go`, replace:

```go
			opts.Limits.AllocBytes = n << 20
		default:
```

with:

```go
			opts.Limits.AllocBytes = n << 20
		case "--tokens":
			if n, err = positive(flag, val, math.MaxInt64); err != nil {
				return opts, "", err
			}
			opts.Limits.Tokens = n
		case "--usd-cents":
			if n, err = positive(flag, val, runtime.MaxUSDCents); err != nil {
				return opts, "", err
			}
			opts.Limits.USDCents = n
		case "--max-turns":
			if n, err = positive(flag, val, math.MaxInt32); err != nil {
				return opts, "", err
			}
			opts.Limits.MaxTurns = int(n)
		default:
```

- [ ] **Step 4: Run to verify it passes**

Run: `go test -race -run '^(TestModelScopeOptions|TestAllowAgentGrants|TestModelBudgetFlags)$' ./arv/evaluator/ .`, then `gofmt -l .`, `go vet ./...` and `go test -race ./...`.
Expected: PASS everywhere.

- [ ] **Step 5: Commit**

```bash
git add arv/evaluator/agent_options_test.go main_agent_test.go arv/evaluator/scope_options.go main.go
git commit -m "feat: tokens, usd_cents and max_turns options and flags

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```
