# Phase 4, task p4.12: Scope options `supervise`, `retries`, `backoff_ms`, `retry_on`

> **For agentic workers:** this file is one task, p4.12. Do it test first, exactly as written; if something here does not match the code you find, stop and report instead of improvising. Prerequisite: tasks p4.1 to p4.11 are committed in your tree. Start every shell command with `cd <your worktree> &&`. Before committing: `gofmt -l .` prints nothing, `go vet ./...` is clean and `go test -race ./...` is green. End the commit message with a blank line and then `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`. Never use `--no-verify`; keep Go files under 15 KB and markdown under 18 KB. `go.mod` says `go 1.24.0` (toolchain 1.26.2), so do not use `testing/synctest` or `sync.WaitGroup.Go`.
>
> Every code block below was applied, in task order, to a scratch copy of the tree and passed `go vet ./...` and `go test -race ./...` (including `e2e/`). Design: `11-phase4-supervision.md`. Deviations from it, and why: `p4-plan-1.md`.

### Task p4.12: Scope options `supervise`, `retries`, `backoff_ms`, `retry_on`

`scope (supervise: "restart", retries: 5, backoff_ms: 50, retry_on: ["effect", "user"]) { ... }`. `scopeLimits` becomes `scopeOptions`, returning the limits and the `runtime.Policy`. `evalScope` hands the policy to the new scope.
- `supervise` is `"fail_fast"`, `"collect"` or `"restart"`.
- `retries` is 1 to 1000; `backoff_ms` is 1 to 30000 (the cap). Values are positive, as for the Phase 3 options.
- `retry_on` is an array of `"effect"` and `"user"` only, and defaults to `["effect"]`. The other kinds would fail the same way again (`capability`, `budget`, `runtime`) or must not be retried (`cancelled`).
- `retries`, `backoff_ms` and `retry_on` without `supervise: "restart"` are errors, rather than options silently ignored. Duplicates and unknown options stay errors.

Every error is positioned at the option's name.

**Files:**
- Modify: `arv/evaluator/concurrency.go`
- Modify: `arv/evaluator/scope_options.go`
- Create (test): `arv/evaluator/supervise_options_test.go`
- Create (test): `arv/evaluator/supervise_syntax_test.go`

- [ ] **Step 1: Write the failing tests**

Create `arv/evaluator/supervise_options_test.go`:

```go
package evaluator

import "testing"

func TestValidSuperviseOptions(t *testing.T) {
	for _, src := range []string{
		`scope (supervise: "fail_fast") { 1 }`,
		`scope (supervise: "collect", tasks: 2) { 1 }`,
		`scope (supervise: "restart") { 1 }`,
		`scope (supervise: "restart", retries: 1000, backoff_ms: 30000, retry_on: ["effect", "user"]) { 1 }`,
		`scope (retry_on: ["user"], supervise: "restart", backoff_ms: 1) { 1 }`, // any order
	} {
		if got, _ := runWith(t, src); got.Inspect() != "1" {
			t.Errorf("%s = %s, want 1", src, got.Inspect())
		}
	}
}

// Bad supervision options fail before the body runs, at the option's name.
func TestSuperviseOptionErrors(t *testing.T) {
	for _, tt := range []struct{ input, msg, pos string }{
		{`scope (supervise: "retry") { 1 }`, `scope option supervise: want "fail_fast", "collect" or "restart", got "retry"`, "1:8"},
		{`scope (supervise: 1) { 1 }`, `scope option supervise: want "fail_fast", "collect" or "restart", got INTEGER`, "1:8"},
		{`scope (supervise: "collect", supervise: "restart") { 1 }`, `duplicate scope option "supervise"`, "1:30"},
		{`scope (retries: 2) { 1 }`, `scope option retries needs supervise: "restart"`, "1:8"},
		{`scope (supervise: "collect", backoff_ms: 5) { 1 }`, `scope option backoff_ms needs supervise: "restart"`, "1:30"},
		{`scope (retry_on: ["user"]) { 1 }`, `scope option retry_on needs supervise: "restart"`, "1:8"},
		{`scope (supervise: "restart", retries: 0) { 1 }`, "scope option retries: want a positive INTEGER, got 0", "1:30"},
		{`scope (supervise: "restart", retries: 1001) { 1 }`, "scope option retries: 1001 is too large (max 1000)", "1:30"},
		{`scope (supervise: "restart", backoff_ms: 30001) { 1 }`, "scope option backoff_ms: 30001 is too large (max 30000)", "1:30"},
		{`scope (supervise: "restart", retry_on: "user") { 1 }`, `scope option retry_on: want an ARRAY of error kinds, such as ["effect", "user"], got "user"`, "1:30"},
		{`scope (supervise: "restart", retry_on: []) { 1 }`, `scope option retry_on: want an ARRAY of error kinds, such as ["effect", "user"], got ARRAY`, "1:30"},
		{`scope (supervise: "restart", retry_on: [1]) { 1 }`, `scope option retry_on: INTEGER is not a kind a restart retries (want "effect" or "user")`, "1:30"},
		{`scope (supervise: "restart", retry_on: ["effect", "capability"]) { 1 }`, `scope option retry_on: "capability" is not a kind a restart retries (want "effect" or "user")`, "1:30"},
		{`scope (supervise: "restart", retry_on: ["budget"]) { 1 }`, `scope option retry_on: "budget" is not a kind a restart retries (want "effect" or "user")`, "1:30"},
	} {
		got, out := runWith(t, tt.input+`; puts("after")`)
		wantErrorAt(t, got, tt.msg, tt.pos)
		if out != "" {
			t.Errorf("%s: printed %q after a bad option", tt.input, out)
		}
	}
}
```

Create `arv/evaluator/supervise_syntax_test.go`:

```go
package evaluator

import (
	"fmt"
	"testing"

	"github.com/delavalom/arvlang/arv/runtime"
)

// supervise: "restart" in a scope's options restarts its tasks, with the
// options' retries and retry_on; each scope has its own policy.
func TestRestartThroughOptions(t *testing.T) {
	srv, hits := flakyServer(t, 2)
	got, _ := runWith(t, check+fmt.Sprintf(`
		scope (supervise: "restart", retries: 3, backoff_ms: 1, retry_on: ["effect", "user"]) {
			await spawn check(%q) with [net("127.0.0.1")]
		}`, srv.URL), grant(t, runtime.Net, "127.0.0.1"))
	if got.Inspect() != "up" || hits.Load() != 3 {
		t.Fatalf("got %s after %d hits, want up after 3", got.Inspect(), hits.Load())
	}
	got, _ = runWith(t, `let f = fn(n) { error("again") };
		for n in [1, 2, 3] {
			match scope (supervise: "restart", retries: 3, backoff_ms: 1, retry_on: ["user"]) { await spawn f(n) } { ok(v) => v, err(e) => e["task"] }
		}`)
	if got.Inspect() != "[root/0#4, root/1#4, root/2#4]" {
		t.Fatalf("got %s", got.Inspect())
	}
	// Without retry_on, a user error is not restarted.
	_, out := runWith(t, `let f = fn() { puts("attempt"); error("down") };
		scope (supervise: "restart", backoff_ms: 1) { await spawn f() }`)
	if out != "attempt\n" {
		t.Fatalf("default retry_on restarted a user error: %q", out)
	}
}
```

- [ ] **Step 2: Run them and watch them fail**

Run: `cd <worktree> && go test -race ./arv/evaluator/ -run 'TestValidSuperviseOptions|TestSuperviseOptionErrors|TestRestartThroughOptions'`

Expected: FAIL. The tests fail: (the first lines; ports and times vary)

```text
--- FAIL: TestValidSuperviseOptions (0.00s)
    supervise_options_test.go:14: scope (supervise: "fail_fast") { 1 } = ERROR: unknown scope option "supervise", want 1
--- FAIL: TestSuperviseOptionErrors (0.00s)
    supervise_options_test.go:38: got error "unknown scope option \"supervise\"" at "1:8", want "scope option supervise: want \"fail_fast\", \"collect
--- FAIL: TestRestartThroughOptions (0.00s)
    supervise_syntax_test.go:19: got ERROR: unknown scope option "supervise" after 0 hits, want up after 3
```

- [ ] **Step 3: Implement**

In `arv/evaluator/concurrency.go`, replace:

```go
// evalScope opens a scope with the limits its options set, as a new level
// of the budget tree under the enclosing scope's (I6).
func (in *Interpreter) evalScope(node *ast.ScopeExpression, env *value.Environment) value.Object {
	limits, errv := in.scopeLimits(node.Options, env)
	if errv != nil {
		return errv
	}
	f := frame{newScope(in.Task.Ctx, limits), in.top().budget.Child(limits)}
	return in.withScope(f, func() value.Object {
		return in.evalBlockStatement(node.Body, env)
```

with:

```go
// evalScope opens a scope with the limits its options set, as a new level
// of the budget tree under the enclosing scope's (I6), supervising its
// tasks as its supervise option says.
func (in *Interpreter) evalScope(node *ast.ScopeExpression, env *value.Environment) value.Object {
	limits, policy, errv := in.scopeOptions(node.Options, env)
	if errv != nil {
		return errv
	}
	s := newScope(in.Task.Ctx, limits)
	s.SetPolicy(policy)
	f := frame{s, in.top().budget.Child(limits)}
	return in.withScope(f, func() value.Object {
		return in.evalBlockStatement(node.Body, env)
```

In `arv/evaluator/scope_options.go`, replace:

```go
// Largest values the scope options accept: beyond them a Duration, a byte
// count or a slot count would overflow.
const (
	maxTimeoutMS = math.MaxInt64 / int64(time.Millisecond)
	maxAllocMB   = math.MaxInt64 >> 20
	maxTasks     = math.MaxInt32
)

// scopeLimits evaluates a scope's options in env, in order, and turns them
// into limits. Each option may appear once and must be a positive INTEGER;
// an unknown name is an error, so a typo never silently means "no limit".
func (in *Interpreter) scopeLimits(opts []ast.ScopeOption, env *value.Environment) (runtime.Limits, *value.Error) {
	var l runtime.Limits
	seen := map[string]bool{}
	for _, o := range opts {
		var max int64
		switch o.Name {
		case "timeout_ms":
			max = maxTimeoutMS
		case "tasks":
			max = maxTasks
		case "steps":
			max = math.MaxInt64
		case "alloc_mb":
			max = maxAllocMB
		default:
			return l, optionError(o, "unknown scope option %q", o.Name)
		}
		if seen[o.Name] {
			return l, optionError(o, "duplicate scope option %q", o.Name)
		}
		seen[o.Name] = true
		v := in.Eval(o.Value, env)
		if e, ok := v.(*value.Error); ok {
			return l, e
		}
		n, ok := v.(*value.Integer)
		if !ok {
			return l, optionError(o, "scope option %s: want a positive INTEGER, got %s", o.Name, v.Type())
		}
		if n.Value <= 0 {
			return l, optionError(o, "scope option %s: want a positive INTEGER, got %d", o.Name, n.Value)
		}
		if n.Value > max {
			return l, optionError(o, "scope option %s: %d is too large (max %d)", o.Name, n.Value, max)
		}
		switch o.Name {
		case "timeout_ms":
			l.Timeout = time.Duration(n.Value) * time.Millisecond
		case "tasks":
			l.Tasks = int(n.Value)
		case "steps":
			l.Steps = n.Value
		case "alloc_mb":
			l.AllocBytes = n.Value << 20
		}
	}
	return l, nil
}
```

with:

```go
// Largest values the scope options accept: beyond them a Duration, a byte
// count or a slot count would overflow. A restart waits at most
// runtime.MaxBackoff, and runs at most maxRetries more times.
const (
	maxTimeoutMS = math.MaxInt64 / int64(time.Millisecond)
	maxAllocMB   = math.MaxInt64 >> 20
	maxTasks     = math.MaxInt32
	maxRetries   = 1000
	maxBackoffMS = int64(runtime.MaxBackoff / time.Millisecond)
)

// integerOptions are the options whose value is a positive INTEGER, with
// the largest each accepts.
var integerOptions = map[string]int64{
	"timeout_ms": maxTimeoutMS, "tasks": maxTasks, "steps": math.MaxInt64, "alloc_mb": maxAllocMB,
	"retries": maxRetries, "backoff_ms": maxBackoffMS,
}

// restartOnly are the options that configure supervise: "restart".
var restartOnly = []string{"retries", "backoff_ms", "retry_on"}

// scopeOptions evaluates a scope's options in env, in order, and turns
// them into limits and a supervision policy. Each option may appear once;
// an unknown name is an error, so a typo never silently means "no limit".
// Budgets and retries/backoff_ms are positive INTEGERs; supervise is
// "fail_fast" (the default), "collect" or "restart"; retry_on is an ARRAY
// of the kinds a restart retries, "effect" (the default) and "user".
func (in *Interpreter) scopeOptions(opts []ast.ScopeOption, env *value.Environment) (runtime.Limits, runtime.Policy, *value.Error) {
	var l runtime.Limits
	var p runtime.Policy
	restart := runtime.RestartPolicy()
	seen := map[string]ast.ScopeOption{}
	for _, o := range opts {
		max, isInt := integerOptions[o.Name]
		if !isInt && o.Name != "supervise" && o.Name != "retry_on" {
			return l, p, optionError(o, "unknown scope option %q", o.Name)
		}
		if _, dup := seen[o.Name]; dup {
			return l, p, optionError(o, "duplicate scope option %q", o.Name)
		}
		seen[o.Name] = o
		v := in.Eval(o.Value, env)
		if e, ok := v.(*value.Error); ok {
			return l, p, e
		}
		var errv *value.Error
		switch o.Name {
		case "supervise":
			p.Strategy, errv = strategy(o, v)
		case "retry_on":
			restart.RetryOn, errv = retryOn(o, v)
		default:
			var n int64
			if n, errv = positive(o, v, max); errv != nil {
				break
			}
			switch o.Name {
			case "timeout_ms":
				l.Timeout = time.Duration(n) * time.Millisecond
			case "tasks":
				l.Tasks = int(n)
			case "steps":
				l.Steps = n
			case "alloc_mb":
				l.AllocBytes = n << 20
			case "retries":
				restart.Retries = int(n)
			case "backoff_ms":
				restart.Backoff = time.Duration(n) * time.Millisecond
			}
		}
		if errv != nil {
			return l, p, errv
		}
	}
	if p.Strategy != runtime.Restart {
		for _, name := range restartOnly {
			if o, ok := seen[name]; ok {
				return l, p, optionError(o, "scope option %s needs supervise: \"restart\"", name)
			}
		}
		return l, p, nil
	}
	return l, restart, nil
}

// positive is v as a positive INTEGER no larger than max.
func positive(o ast.ScopeOption, v value.Object, max int64) (int64, *value.Error) {
	n, ok := v.(*value.Integer)
	if !ok {
		return 0, optionError(o, "scope option %s: want a positive INTEGER, got %s", o.Name, v.Type())
	}
	if n.Value <= 0 {
		return 0, optionError(o, "scope option %s: want a positive INTEGER, got %d", o.Name, n.Value)
	}
	if n.Value > max {
		return 0, optionError(o, "scope option %s: %d is too large (max %d)", o.Name, n.Value, max)
	}
	return n.Value, nil
}

var strategies = map[string]runtime.Strategy{
	"fail_fast": runtime.FailFast, "collect": runtime.Collect, "restart": runtime.Restart,
}

// strategy is v as a supervise option.
func strategy(o ast.ScopeOption, v value.Object) (runtime.Strategy, *value.Error) {
	if s, ok := v.(*value.String); ok {
		if st, ok := strategies[s.Value]; ok {
			return st, nil
		}
	}
	return 0, optionError(o, `scope option supervise: want "fail_fast", "collect" or "restart", got %s`, quoteOption(v))
}

// retryOn is v as a retry_on option: a non-empty ARRAY of the kinds a
// restart may retry. The others fail the same way every time (capability,
// budget, runtime) or must stop the task (cancelled).
func retryOn(o ast.ScopeOption, v value.Object) (map[string]bool, *value.Error) {
	arr, ok := v.(*value.Array)
	if !ok || len(arr.Elements) == 0 {
		return nil, optionError(o, `scope option retry_on: want an ARRAY of error kinds, such as ["effect", "user"], got %s`, quoteOption(v))
	}
	kinds := map[string]bool{}
	for _, el := range arr.Elements {
		s, ok := el.(*value.String)
		if !ok || (s.Value != runtime.KindEffect && s.Value != runtime.KindUser) {
			return nil, optionError(o, `scope option retry_on: %s is not a kind a restart retries (want "effect" or "user")`, quoteOption(el))
		}
		kinds[s.Value] = true
	}
	return kinds, nil
}

// quoteOption names v in an option error: a short string quoted, else its
// type, so an error never copies a long value.
func quoteOption(v value.Object) string {
	if s, ok := v.(*value.String); ok && len(s.Value) <= 32 {
		return fmt.Sprintf("%q", s.Value)
	}
	return string(v.Type())
}
```

- [ ] **Step 4: Run the tests, then the full checks**

Run: `cd <worktree> && go test -race ./arv/evaluator/ -run 'TestValidSuperviseOptions|TestSuperviseOptionErrors|TestRestartThroughOptions'`, then `cd <worktree> && gofmt -l . && go vet ./... && go test -race ./...`

Expected: `ok` for every package, and `gofmt -l .` prints nothing.

- [ ] **Step 5: Commit**

```bash
cd <worktree> && git add arv/evaluator/concurrency.go arv/evaluator/scope_options.go arv/evaluator/supervise_options_test.go arv/evaluator/supervise_syntax_test.go && git commit -F - <<'MSG'
feat(evaluator): supervise, retries, backoff_ms and retry_on scope options

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
MSG
```
