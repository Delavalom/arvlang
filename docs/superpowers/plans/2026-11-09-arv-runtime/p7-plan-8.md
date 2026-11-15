# Phase 7, task p7.8: `ask`

> **For agentic workers:** this file is one task, p7.8. Do it test first, exactly as written; if something here does not match the code you find, stop and report instead of improvising. Prerequisite: task p7.7 is committed in your tree. Start every shell command with `cd <your worktree> &&` and use absolute paths: the shell's working directory does not persist. Before committing: `gofmt -l .` prints nothing, `go vet ./...` is clean and `go test -race ./...` is green. End the commit message with a blank line and then `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`. Never use `--no-verify` and never push; keep Go files under 15 KB and markdown under 18 KB. `go.mod` says `go 1.24.0` (toolchain 1.26.2): `t.Chdir`, `min` and `max` are available; do not use `testing/synctest` or `sync.WaitGroup.Go`. Before a long test run, check `df -h /System/Volumes/Data`; below 1 GB free, stop and report (only `go clean -cache` may be deleted). This machine is heavily loaded: keep wall-clock bounds generous. No test may reach the network or spend money: never set `ARV_LIVE_ANTHROPIC`, and never put a real API key anywhere.
>
> Every code block below was applied, in task order, to a scratch copy of `feat/phases-3-8` (Phases 3, 4 and 5 merged; Phase 6 is not) and passed `gofmt -l .`, `go vet ./...` and `go test -race ./...` (including `e2e/`). Design: `14-phase7-agents.md`. Deviations from it, and why: `p7-plan-1.md`.

### Task p7.8: `ask`

`ask(model, prompt)` checks `llm(model)` first, before anything about the host is revealed (I1), finds the provider in the run's `Host`, makes one call with the prompt (redacted) as the only user message, and returns the reply's text. Like `puts`, it is an interpreter builtin: `applyFunction` dispatches it by pointer, because p7.9 needs the scope's budget and p7.11's `agent` needs to call Arv functions in its own task.

`complete` is the one place a call happens; `agent` uses it too. It calls the provider in the task's context (a cancelled scope ends the call), charges the reply to `alloc_mb`, and turns a stop reason other than `end_turn`, `stop_sequence` or `tool_use` into an `effect` error. A provider error keeps its kind (`effect`), unless the task was cancelled, which wins. Budgets come in p7.9.

**Files:**
- Create: `arv/evaluator/llmcall.go`
- Create: `arv/evaluator/ask.go`
- Modify: `arv/evaluator/evaluator.go`
- Create (test): `arv/evaluator/llm_helpers_test.go`
- Create (test): `arv/evaluator/ask_test.go`

- [ ] **Step 1: Write the failing tests**

Create `arv/evaluator/llm_helpers_test.go`:

```go
package evaluator

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/delavalom/arvlang/arv/lexer"
	"github.com/delavalom/arvlang/arv/llm"
	"github.com/delavalom/arvlang/arv/parser"
	"github.com/delavalom/arvlang/arv/runtime"
	"github.com/delavalom/arvlang/arv/value"
)

// sonnet is the model the Phase 7 acceptance tests name.
const sonnet = "anthropic:claude-sonnet-5-5"

// fakeHost serves every model of "anthropic" from f, at the default prices
// unless prices overrides them.
func fakeHost(f *llm.Fake, prices map[string]llm.Price) *llm.Host {
	return &llm.Host{Providers: map[string]llm.Provider{"anthropic": f}, Prices: prices}
}

// runLLM runs input as the root task holding caps, under limits l, with
// host as the run's model providers. It returns the result and the output.
func runLLM(t *testing.T, input string, host *llm.Host, l runtime.Limits, caps ...runtime.Capability) (value.Object, string) {
	t.Helper()
	p := parser.New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) > 0 {
		t.Fatalf("parse errors: %v", p.Errors())
	}
	var out bytes.Buffer
	ctx := context.Background()
	if host != nil {
		ctx = llm.WithHost(ctx, host)
	}
	root := runtime.NewRoot(ctx, runtime.NewCapSet(caps...), &out)
	return New(root).RunLimited(program, value.NewEnvironment(), l), out.String()
}

// wantKindHas fails unless got is an error of kind whose message
// contains msg.
func wantKindHas(t *testing.T, got value.Object, kind, msg string) {
	t.Helper()
	if e := wantKind(t, got, kind); !strings.Contains(e.Message, msg) {
		t.Fatalf("error %q does not contain %q", e.Message, msg)
	}
}

// userTextOf is the text of the first block of the first message of req.
func userTextOf(req llm.Request) string {
	if len(req.Messages) == 0 || len(req.Messages[0].Content) == 0 {
		return ""
	}
	return req.Messages[0].Content[0].Text
}
```

Create `arv/evaluator/ask_test.go`:

```go
package evaluator

import (
	"context"
	goruntime "runtime"
	"strings"
	"testing"

	"github.com/delavalom/arvlang/arv/llm"
	"github.com/delavalom/arvlang/arv/runtime"
	"github.com/delavalom/arvlang/arv/value"
)

func TestAskNeedsTheModelCapability(t *testing.T) {
	f := llm.NewFake(llm.TextReply("hi", 1, 1))
	got, _ := runLLM(t, `ask("anthropic:claude-sonnet-5-5", "hello")`, fakeHost(f, nil), runtime.Limits{},
		grant(t, runtime.LLM, "anthropic:claude-haiku-5-5"))
	wantDenied(t, got, `capability denied: llm("anthropic:claude-sonnet-5-5")`)
	if n := len(f.Calls()); n != 0 {
		t.Fatalf("the provider got %d calls, want 0", n)
	}
}

func TestAskReturnsTheReply(t *testing.T) {
	f := llm.NewFake(llm.TextReply("Lisbon", 12, 3))
	got, _ := runLLM(t, `ask("anthropic:claude-sonnet-5-5", "Capital of Portugal?")`, fakeHost(f, nil), runtime.Limits{},
		grant(t, runtime.LLM, "anthropic:*"))
	if s, ok := got.(*value.String); !ok || s.Value != "Lisbon" {
		t.Fatalf("got %s, want Lisbon", got.Inspect())
	}
	calls := f.Calls()
	if len(calls) != 1 {
		t.Fatalf("calls = %d", len(calls))
	}
	c := calls[0]
	if c.Model != "claude-sonnet-5-5" || c.MaxTokens != maxTokens || len(c.Tools) != 0 ||
		len(c.Messages) != 1 || c.Messages[0].Role != llm.RoleUser || userTextOf(c) != "Capital of Portugal?" {
		t.Fatalf("request = %+v", c)
	}
}

func TestAskErrors(t *testing.T) {
	all := grant(t, runtime.LLM, "*")
	host := fakeHost(llm.NewFake(llm.Reply{Err: &llm.Error{Provider: "anthropic", Kind: llm.ErrRateLimit, Status: 429, Message: "slow down"}}), nil)
	got, _ := runLLM(t, `ask("anthropic:claude-sonnet-5-5", "x")`, host, runtime.Limits{}, all)
	wantKindHas(t, got, runtime.KindEffect, "ask: anthropic: rate_limit (HTTP 429): slow down")

	got, _ = runLLM(t, `ask("anthropic:*", "x")`, host, runtime.Limits{}, all)
	wantKindHas(t, got, runtime.KindRuntime, "name one model")
	got, _ = runLLM(t, `ask("anthropic:m", 1)`, host, runtime.Limits{}, all)
	wantKindHas(t, got, runtime.KindRuntime, "ask: want (STRING, STRING), got (STRING, INTEGER)")
	got, _ = runLLM(t, `ask("anthropic:m", "x")`, nil, runtime.Limits{}, all)
	wantKindHas(t, got, runtime.KindEffect, "the host configured no model providers")
	got, _ = runLLM(t, `ask("other:m", "x")`, host, runtime.Limits{}, all)
	wantKindHas(t, got, runtime.KindEffect, `no provider "other" is configured`)

	for _, c := range []struct {
		stop, msg string
	}{
		{llm.StopRefusal, "refused the request"}, {llm.StopMaxTokens, "reached max_tokens"}, {"pause_turn", `unexpected stop reason "pause_turn"`},
	} {
		r := llm.TextReply("partial", 1, 1)
		r.Response.StopReason = c.stop
		got, _ := runLLM(t, `ask("anthropic:m", "x")`, fakeHost(llm.NewFake(r), nil), runtime.Limits{}, all)
		wantKindHas(t, got, runtime.KindEffect, c.msg)
	}
}

// The reply's bytes are charged to alloc_mb.
func TestAskChargesTheReply(t *testing.T) {
	f := llm.NewFake(llm.TextReply(strings.Repeat("x", 2<<20), 1, 1))
	got, _ := runLLM(t, `scope (alloc_mb: 1) { ask("anthropic:m", "x") }`, fakeHost(f, nil), runtime.Limits{}, grant(t, runtime.LLM, "*"))
	wantKindHas(t, got, runtime.KindBudget, "budget exceeded: alloc")
}

// A prompt carrying a capability denial goes out redacted: the model
// never sees the grants the task holds.
func TestAskRedactsDenialsInThePrompt(t *testing.T) {
	f := llm.NewFake(llm.TextReply("ok", 1, 1))
	src := `let msg = match getenv("SECRET_NAME") { ok(v) => v, err(e) => e["message"] };
ask("anthropic:m", "it said: " + msg)`
	got, _ := runLLM(t, src, fakeHost(f, nil), runtime.Limits{}, grant(t, runtime.LLM, "*"), grant(t, runtime.Env, "PRIVATE_GRANT"))
	if got.Inspect() != "ok" {
		t.Fatalf("got %s", got.Inspect())
	}
	sent := userTextOf(f.Calls()[0])
	if sent != `it said: capability denied: env("SECRET_NAME")` || strings.Contains(sent, "PRIVATE_GRANT") {
		t.Fatalf("sent %q", sent)
	}
}

// A call blocked in the provider ends when its scope times out, and
// nothing it started outlives the run.
func TestAskIsCancelledWithItsScope(t *testing.T) {
	before := goruntime.NumGoroutine()
	f := llm.NewFake()
	f.Then = func(ctx context.Context, req llm.Request) (llm.Response, error) {
		<-ctx.Done()
		return llm.Response{}, context.Cause(ctx)
	}
	got, _ := runLLM(t, `scope (timeout_ms: 50) { ask("anthropic:m", "x") }`, fakeHost(f, nil), runtime.Limits{}, grant(t, runtime.LLM, "*"))
	wantKindHas(t, got, runtime.KindBudget, "budget exceeded: timeout (limit 50 ms)")
	settled(t, before)
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test -run '^(TestAskNeedsTheModelCapability|TestAskReturnsTheReply|TestAskErrors|TestAskChargesTheReply|TestAskRedactsDenialsInThePrompt|TestAskIsCancelledWithItsScope)$' ./arv/evaluator/`

Expected: FAIL: the package does not compile. The output includes:

```text
# github.com/delavalom/arvlang/arv/evaluator [github.com/delavalom/arvlang/arv/evaluator.test]
arv/evaluator/ask_test.go:36:54: undefined: maxTokens
```

- [ ] **Step 3: Implement**

Create `arv/evaluator/llmcall.go`:

```go
package evaluator

import (
	"github.com/delavalom/arvlang/arv/llm"
	"github.com/delavalom/arvlang/arv/runtime"
	"github.com/delavalom/arvlang/arv/value"
)

// maxTokens is the most output tokens one model call may write.
const maxTokens = 4096

// blockSize is what one content block of a model's reply costs beyond its
// bytes: the Block and its place in a slice.
const blockSize = 160

// interpreterBuiltin is a builtin that applyFunction runs on the
// interpreter itself (see interpreterBuiltins): ask and agent need the
// scope's budget, and agent calls Arv functions in its own task.
func interpreterBuiltin(name string) *value.Builtin {
	return &value.Builtin{Fn: func(t *runtime.Task, args ...value.Object) value.Object {
		return newError("%s: needs an interpreter", name)
	}}
}

// model is one model a call may use: checked against the task's grants,
// with the host's provider and price for it.
type model struct {
	spec     string // "provider:model"
	provider llm.Provider
	id       string // the provider's model ID
	price    llm.Price
	priced   bool
}

// model checks that the task holds llm(spec) and finds the host's
// provider for it. The capability is checked before anything about the
// host is revealed (I1).
func (in *Interpreter) model(fn, spec string) (model, *value.Error) {
	if _, _, err := llm.ParseModel(spec); err != nil {
		return model{}, newError("%s: %s", fn, err)
	}
	if err := in.Task.Require(runtime.Capability{Kind: runtime.LLM, Scope: spec}); err != nil {
		return model{}, errorOf(err, runtime.KindCapability)
	}
	host := llm.HostFrom(in.Task.Ctx)
	if host == nil {
		return model{}, newErrorKind(runtime.KindEffect, "%s: the host configured no model providers", fn)
	}
	p, id, err := host.Lookup(spec)
	if err != nil {
		return model{}, newErrorKind(runtime.KindEffect, "%s: %s", fn, err)
	}
	price, ok := host.Price(spec)
	return model{spec: spec, provider: p, id: id, price: price, priced: ok}, nil
}

// complete makes one call to m: it calls the provider in the task's
// context and charges the reply's bytes to alloc_mb. A refusal, a reply
// cut off at max_tokens, or a stop reason it does not know is an effect
// error.
func (in *Interpreter) complete(fn string, m model, req llm.Request) (llm.Response, *value.Error) {
	req.Model, req.MaxTokens = m.id, maxTokens
	resp, err := m.provider.Complete(in.Task.Ctx, req)
	if err != nil {
		if terr := in.Task.Err(); terr != nil {
			return llm.Response{}, newErrorKind(runtime.KindCancelled, "%s in task %s cancelled: %s", fn, in.Task.ID, terr)
		}
		return llm.Response{}, newErrorKind(runtime.ErrorKind(err, runtime.KindEffect), "%s: %s", fn, err)
	}
	if e := in.charge(replySize(resp)); e != nil {
		return llm.Response{}, e
	}
	switch resp.StopReason {
	case llm.StopEndTurn, llm.StopSequence, llm.StopToolUse:
		return resp, nil
	case llm.StopRefusal:
		return llm.Response{}, newErrorKind(runtime.KindEffect, "%s: %s refused the request", fn, m.spec)
	case llm.StopMaxTokens:
		return llm.Response{}, newErrorKind(runtime.KindEffect, "%s: the reply reached max_tokens (%d) before it ended", fn, maxTokens)
	}
	return llm.Response{}, newErrorKind(runtime.KindEffect, "%s: unexpected stop reason %q", fn, clipped(resp.StopReason))
}

// replySize is what a reply costs to keep: its blocks and their bytes.
func replySize(resp llm.Response) int64 {
	n := int64(0)
	for _, c := range resp.Content {
		n += blockSize + int64(len(c.Text)+len(c.ID)+len(c.Name)+len(c.Input)+len(c.Raw))
	}
	return n
}
```

Create `arv/evaluator/ask.go`:

```go
package evaluator

import (
	"github.com/delavalom/arvlang/arv/llm"
	"github.com/delavalom/arvlang/arv/runtime"
	"github.com/delavalom/arvlang/arv/value"
)

var askBuiltin = interpreterBuiltin("ask")

func init() { builtins["ask"] = askBuiltin }

// ask(model, prompt) sends prompt to model, which the task must hold as
// llm(model), and returns the reply's text: one call, no tools. The
// prompt goes out with any capability denial in it redacted
// (runtime.RedactDenials). The call is an effect: it is refused before
// the request if the scope's tokens or usd_cents cannot cover it.
func (in *Interpreter) ask(args []value.Object) value.Object {
	if len(args) != 2 {
		return newError("ask: want 2 arguments (model, prompt), got %d", len(args))
	}
	spec, ok1 := args[0].(*value.String)
	prompt, ok2 := args[1].(*value.String)
	if !ok1 || !ok2 {
		return newError("ask: want (STRING, STRING), got (%s, %s)", args[0].Type(), args[1].Type())
	}
	m, errv := in.model("ask", spec.Value)
	if errv != nil {
		return errv
	}
	req := llm.Request{Messages: []llm.Message{userText(prompt.Value)}}
	resp, errv := in.complete("ask", m, req)
	if errv != nil {
		return errv
	}
	return &value.String{Value: resp.Text()} // its bytes are charged in complete
}

// userText is a user message holding text, with denials redacted.
func userText(text string) llm.Message {
	return llm.Message{Role: llm.RoleUser, Content: []llm.Block{{Type: llm.BlockText, Text: runtime.RedactDenials(text)}}}
}
```

In `arv/evaluator/evaluator.go`, replace:

```go
		}
		if fn == putsBuiltin {
			return in.puts(args)
		}
```

with:

```go
		}
		switch fn {
		case putsBuiltin:
			return in.puts(args)
		case askBuiltin:
			return in.ask(args)
		}
```

- [ ] **Step 4: Run to verify it passes**

Run: `go test -race -run '^(TestAskNeedsTheModelCapability|TestAskReturnsTheReply|TestAskErrors|TestAskChargesTheReply|TestAskRedactsDenialsInThePrompt|TestAskIsCancelledWithItsScope)$' ./arv/evaluator/`, then `gofmt -l .`, `go vet ./...` and `go test -race ./...`.
Expected: PASS everywhere.

- [ ] **Step 5: Commit**

```bash
git add arv/evaluator/llm_helpers_test.go arv/evaluator/ask_test.go arv/evaluator/llmcall.go arv/evaluator/ask.go arv/evaluator/evaluator.go
git commit -m "feat(evaluator): ask

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```
