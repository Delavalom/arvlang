# Phase 7, task p7.15: CLI key handling; the opt-in live test

> **For agentic workers:** this file is one task, p7.15. Do it test first, exactly as written; if something here does not match the code you find, stop and report instead of improvising. Prerequisite: task p7.14 is committed in your tree. Start every shell command with `cd <your worktree> &&` and use absolute paths: the shell's working directory does not persist. Before committing: `gofmt -l .` prints nothing, `go vet ./...` is clean and `go test -race ./...` is green. End the commit message with a blank line and then `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`. Never use `--no-verify` and never push; keep Go files under 15 KB and markdown under 18 KB. `go.mod` says `go 1.24.0` (toolchain 1.26.2): `t.Chdir`, `min` and `max` are available; do not use `testing/synctest` or `sync.WaitGroup.Go`. Before a long test run, check `df -h /System/Volumes/Data`; below 1 GB free, stop and report (only `go clean -cache` may be deleted). This machine is heavily loaded: keep wall-clock bounds generous. No test may reach the network or spend money: never set `ARV_LIVE_ANTHROPIC`, and never put a real API key anywhere.
>
> Every code block below was applied, in task order, to a scratch copy of `feat/phases-3-8` (Phases 3, 4 and 5 merged; Phase 6 is not) and passed `gofmt -l .`, `go vet ./...` and `go test -race ./...` (including `e2e/`). Design: `14-phase7-agents.md`. Deviations from it, and why: `p7-plan-1.md`.

### Task p7.15: CLI key handling; the opt-in live test

`arv run` reads `ANTHROPIC_API_KEY`, removes it from the process environment, and passes an Anthropic provider as `opts.LLM`. With no key, there is no host, and a model call is an `effect` error. `ARV_ANTHROPIC_URL` replaces the endpoint; the CLI tests point it at a local fake API that records request bodies and keys, then check that the key appears in no output, error, or request body (only in the header), and that `getenv` cannot read it even under `--allow env=*`.

`arv/live_anthropic_test.go` is the one live test. It is skipped unless `ARV_LIVE_ANTHROPIC=1`. **Do not run it**: Step 4 shows it skipped.

**Files:**
- Modify: `main.go`
- Create (test): `main_llm_test.go`
- Create (test): `arv/live_anthropic_test.go`

- [ ] **Step 1: Write the failing tests**

Create `main_llm_test.go`:

```go
package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
)

const cliKey = "sk-ant-cli-SECRET-9876543210"

// fakeAPI is a local stand-in for the Messages API: it answers the n-th
// request with replies[n] (the last one again after that) and keeps every
// request's body and API key.
type fakeAPI struct {
	*httptest.Server
	mu     sync.Mutex
	bodies []string
	keys   []string
}

func newFakeAPI(t *testing.T, status int, replies ...string) *fakeAPI {
	t.Helper()
	f := &fakeAPI{}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		f.mu.Lock()
		n := len(f.bodies)
		f.bodies = append(f.bodies, string(data))
		f.keys = append(f.keys, r.Header.Get("x-api-key"))
		f.mu.Unlock()
		w.WriteHeader(status)
		io.WriteString(w, strings.ReplaceAll(replies[min(n, len(replies)-1)], "$KEY", r.Header.Get("x-api-key")))
	}))
	t.Cleanup(f.Close)
	t.Setenv("ANTHROPIC_API_KEY", cliKey)
	t.Setenv("ARV_ANTHROPIC_URL", f.URL+"/v1/messages")
	return f
}

func textReply(text string) string {
	b, _ := json.Marshal(text)
	return `{"content":[{"type":"text","text":` + string(b) + `}],"stop_reason":"end_turn","usage":{"input_tokens":9,"output_tokens":2}}`
}

// noKey fails if the key shows up in any of texts.
func noKey(t *testing.T, texts ...string) {
	t.Helper()
	for _, s := range texts {
		if strings.Contains(s, cliKey) {
			t.Fatalf("the API key leaked into %q", s)
		}
	}
}

func TestCLIAsk(t *testing.T) {
	api := newFakeAPI(t, 200, textReply("ok"))
	script := writeScript(t, `puts(ask("anthropic:claude-haiku-5-5", "Reply with the word ok"))`)
	code, out, stderr := cli("run", "--allow", "llm=anthropic:*", script)
	if code != 0 || out != "ok\n" {
		t.Fatalf("exit %d, out %q, err %q", code, out, stderr)
	}
	if len(api.keys) != 1 || api.keys[0] != cliKey {
		t.Fatal("the request did not carry the key")
	}
	noKey(t, out, stderr, api.bodies[0])
}

// The program cannot read the key, even holding every env grant: the CLI
// removed it from the environment before the run.
func TestCLIKeyIsNotInTheEnvironment(t *testing.T) {
	newFakeAPI(t, 200, textReply("ok"))
	script := writeScript(t, `puts("[" + getenv("ANTHROPIC_API_KEY") + "]")`)
	code, out, stderr := cli("run", "--allow", "env=*", script)
	if code != 0 || out != "[]\n" {
		t.Fatalf("exit %d, out %q, err %q", code, out, stderr)
	}
	if os.Getenv("ANTHROPIC_API_KEY") != "" {
		t.Fatal("the key is still in the environment")
	}
}

// A provider error is an Arv error with a kind, and holds no key even
// when the server echoes it back.
func TestCLIProviderErrorHidesTheKey(t *testing.T) {
	newFakeAPI(t, 401, `{"type":"error","error":{"type":"authentication_error","message":"invalid x-api-key: $KEY"}}`)
	script := writeScript(t, `match ask("anthropic:claude-haiku-5-5", "hi") { ok(v) => v, err(e) => puts(e["kind"] + ": " + e["message"]) }`)
	code, out, stderr := cli("run", "--allow", "llm=anthropic:*", script)
	if code != 0 || !strings.HasPrefix(out, "effect: ask: anthropic: auth (HTTP 401): authentication_error: invalid x-api-key: [redacted]") {
		t.Fatalf("exit %d, out %q, err %q", code, out, stderr)
	}
	noKey(t, out, stderr)
}

// An agent end to end through the real provider code: a tool the task
// does not hold is refused as a tool result, the model sees no grant the
// task holds, and the key appears nowhere but in the request header.
func TestCLIAgentDeniedTool(t *testing.T) {
	dir := t.TempDir()
	api := newFakeAPI(t, 200,
		`{"content":[{"type":"tool_use","id":"t1","name":"notes","input":{"path":"/etc/passwd"}}],"stop_reason":"tool_use","usage":{"input_tokens":30,"output_tokens":9}}`,
		textReply("I may not read notes."))
	script := writeScript(t, `let tools = {"notes": fn(path) { read_file(path) }, "weather": fn(city) { "sunny" }};
puts(agent("anthropic:claude-sonnet-5-5", "Read my notes", tools))`)
	code, out, stderr := cli("run", "--allow", "llm=anthropic:*", "--allow", "tool=weather", "--allow", "fs_read="+dir, script)
	if code != 0 || out != "I may not read notes.\n" {
		t.Fatalf("exit %d, out %q, err %q", code, out, stderr)
	}
	if len(api.bodies) != 2 || !strings.Contains(api.bodies[1], `"content":"capability denied: tool(\"notes\")","is_error":true`) {
		t.Fatalf("second request = %s", api.bodies[len(api.bodies)-1])
	}
	for _, b := range api.bodies {
		if strings.Contains(b, dir) || strings.Contains(b, "holds") {
			t.Fatalf("a request names a held grant: %s", b)
		}
	}
	noKey(t, append([]string{out, stderr}, api.bodies...)...)
}
```

Create `arv/live_anthropic_test.go`:

```go
package arv

import (
	"bytes"
	"context"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/delavalom/arvlang/arv/llm"
	"github.com/delavalom/arvlang/arv/runtime"
)

// recorder passes calls to p and keeps every response.
type recorder struct {
	p     llm.Provider
	mu    sync.Mutex
	resps []llm.Response
}

func (r *recorder) Complete(ctx context.Context, req llm.Request) (llm.Response, error) {
	resp, err := r.p.Complete(ctx, req)
	r.mu.Lock()
	defer r.mu.Unlock()
	if err == nil {
		r.resps = append(r.resps, resp)
	}
	return resp, err
}

// The one test that calls the real Anthropic Messages API. It costs money,
// so it runs only with ARV_LIVE_ANTHROPIC=1 and ANTHROPIC_API_KEY set.
func TestLiveAnthropic(t *testing.T) {
	if os.Getenv("ARV_LIVE_ANTHROPIC") != "1" {
		t.Skip("set ARV_LIVE_ANTHROPIC=1 and ANTHROPIC_API_KEY to call the real API (it costs money)")
	}
	key := os.Getenv("ANTHROPIC_API_KEY")
	if key == "" {
		t.Fatal("ARV_LIVE_ANTHROPIC=1 needs ANTHROPIC_API_KEY")
	}
	rec := &recorder{p: llm.NewAnthropic(key, "")}
	opts := Options{
		LLM:  &llm.Host{Providers: map[string]llm.Provider{"anthropic": rec}},
		Caps: []runtime.Capability{{Kind: runtime.LLM, Scope: "anthropic:claude-haiku-5-5"}},
	}
	var out bytes.Buffer
	opts.Out = &out
	// Each call reserves about 4.6K tokens, so the second fits in 6000 only
	// if the first was settled to its real usage.
	src := `scope (tokens: 6000, usd_cents: 1) {
  let a = ask("anthropic:claude-haiku-5-5", "Reply with the word ok");
  let b = ask("anthropic:claude-haiku-5-5", "Reply with the word ok");
  a + " " + b
}`
	got, err := Run(src, opts)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(strings.ToLower(got.Inspect()), "ok") < 2 {
		t.Fatalf("replies = %q", got.Inspect())
	}
	if len(rec.resps) != 2 || rec.resps[0].InputTokens <= 0 || rec.resps[0].OutputTokens <= 0 {
		t.Fatalf("usage = %+v", rec.resps)
	}
	if strings.Contains(got.Inspect(), key) || strings.Contains(out.String(), key) {
		t.Fatal("the key reached Arv output")
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test -run '^(TestCLIAsk|TestCLIKeyIsNotInTheEnvironment|TestCLIProviderErrorHidesTheKey|TestCLIAgentDeniedTool|TestLiveAnthropic)$' . ./arv/`

Expected: FAIL: the CLI makes no host, so `TestCLIAsk` and the others fail; `TestLiveAnthropic` is skipped. The output includes:

```text
    main_llm_test.go:65: exit 1, out "", err "error: ask: the host configured no model providers (at $TMPDIR/TestCLIAsk2376395895/001/main.arv:1:6)\n"
    main_llm_test.go:80: exit 0, out "[sk-ant-cli-SECRET-9876543210]\n", err ""
    main_llm_test.go:94: exit 0, out "effect: ask: the host configured no model providers\n", err ""
    main_llm_test.go:111: exit 1, out "", err "error: agent: the host configured no model providers (at $TMPDIR/TestCLIAgentDeniedTool239324807/002/main.arv:2:6)\n"
```

- [ ] **Step 3: Implement**

In `main.go`, replace:

```go
	"github.com/delavalom/arvlang/arv"
	"github.com/delavalom/arvlang/arv/repl"
```

with:

```go
	"github.com/delavalom/arvlang/arv"
	"github.com/delavalom/arvlang/arv/llm"
	"github.com/delavalom/arvlang/arv/repl"
```

In `main.go`, replace:

```go
  --usd-cents N        what model calls may cost in all, in US cents (default: no limit)
  --max-turns N        model calls one agent(...) may make (default 20)`
```

with:

```go
  --usd-cents N        what model calls may cost in all, in US cents (default: no limit)
  --max-turns N        model calls one agent(...) may make (default 20)

environment:
  ANTHROPIC_API_KEY    the key for llm=anthropic:... grants; read by arv, never visible to the program`
```

In `main.go`, replace:

```go
	opts.Out = stdout
	if _, err := arv.Run(string(src), opts); err != nil {
```

with:

```go
	opts.Out = stdout
	opts.LLM = hostLLM()
	if _, err := arv.Run(string(src), opts); err != nil {
```

In `main.go`, after:

```go
	return 0
}
```

add:

```go

// hostLLM is the run's model providers: Anthropic, with the key from
// ANTHROPIC_API_KEY, or nil if that is unset. It removes the key from the
// process's environment once read, so no env grant (not even env=*) lets
// the program's getenv read it: the key lives in the Go host only.
// ARV_ANTHROPIC_URL, when set, replaces the API's URL; tests point it at a
// local fake.
func hostLLM() *llm.Host {
	key := os.Getenv("ANTHROPIC_API_KEY")
	if key == "" {
		return nil
	}
	os.Unsetenv("ANTHROPIC_API_KEY")
	p := llm.NewAnthropic(key, os.Getenv("ARV_ANTHROPIC_URL"))
	return &llm.Host{Providers: map[string]llm.Provider{"anthropic": p}}
}
```

- [ ] **Step 4: Run to verify it passes**

Run: `go test -race -run '^(TestCLIAsk|TestCLIKeyIsNotInTheEnvironment|TestCLIProviderErrorHidesTheKey|TestCLIAgentDeniedTool|TestLiveAnthropic)$' . ./arv/`, then `gofmt -l .`, `go vet ./...` and `go test -race ./...`.
Expected: PASS everywhere; `go test -v -run TestLiveAnthropic ./arv/` prints `--- SKIP`.

- [ ] **Step 5: Commit**

```bash
git add main_llm_test.go arv/live_anthropic_test.go main.go
git commit -m "feat(cli): run with an Anthropic provider from ANTHROPIC_API_KEY

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```
