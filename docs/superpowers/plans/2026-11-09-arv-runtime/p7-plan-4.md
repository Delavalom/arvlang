# Phase 7, task p7.4: `arv/llm`: types, errors and the fake provider

> **For agentic workers:** this file is one task, p7.4. Do it test first, exactly as written; if something here does not match the code you find, stop and report instead of improvising. Prerequisite: task p7.3 is committed in your tree. Start every shell command with `cd <your worktree> &&` and use absolute paths: the shell's working directory does not persist. Before committing: `gofmt -l .` prints nothing, `go vet ./...` is clean and `go test -race ./...` is green. End the commit message with a blank line and then `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`. Never use `--no-verify` and never push; keep Go files under 15 KB and markdown under 18 KB. `go.mod` says `go 1.24.0` (toolchain 1.26.2): `t.Chdir`, `min` and `max` are available; do not use `testing/synctest` or `sync.WaitGroup.Go`. Before a long test run, check `df -h /System/Volumes/Data`; below 1 GB free, stop and report (only `go clean -cache` may be deleted). This machine is heavily loaded: keep wall-clock bounds generous. No test may reach the network or spend money: never set `ARV_LIVE_ANTHROPIC`, and never put a real API key anywhere.
>
> Every code block below was applied, in task order, to a scratch copy of `feat/phases-3-8` (Phases 3, 4 and 5 merged; Phase 6 is not) and passed `gofmt -l .`, `go vet ./...` and `go test -race ./...` (including `e2e/`). Design: `14-phase7-agents.md`. Deviations from it, and why: `p7-plan-1.md`.

### Task p7.4: `arv/llm`: types, errors and the fake provider

The design's `Provider` interface and its request and response types, in a new package that imports only the standard library and `arv/runtime`. A `Block` keeps its `Raw` JSON so that blocks the evaluator does not read (thinking) go back to the API unchanged. `*Error` is a provider failure with a kind (`auth`, `billing`, `not_found`, `invalid_request`, `rate_limit`, `overloaded`, `server`, `network`, `decode`); it reports `ErrorKind() == "effect"`, so the evaluator's `runtime.ErrorKind` makes it an Arv `effect` error. Its text is clipped to 512 bytes. `ParseModel` splits `"provider:model"` and refuses wildcards: a call names one model.

`Fake` answers from a queue and records deep copies of the requests; `Then`, when set, answers instead (tests use it to block until the context ends). It returns `context.Cause(ctx)` once the context is done.

**Files:**
- Create: `arv/llm/llm.go`
- Create: `arv/llm/fake.go`
- Create (test): `arv/llm/llm_test.go`

- [ ] **Step 1: Write the failing tests**

Create `arv/llm/llm_test.go`:

```go
package llm

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/delavalom/arvlang/arv/runtime"
)

func TestParseModel(t *testing.T) {
	p, m, err := ParseModel("anthropic:claude-haiku-5-5")
	if err != nil || p != "anthropic" || m != "claude-haiku-5-5" {
		t.Fatalf("ParseModel = %q, %q, %v", p, m, err)
	}
	for _, bad := range []string{"anthropic:*", "*", "anthropic", "", "Anthropic:x"} {
		if _, _, err := ParseModel(bad); err == nil {
			t.Errorf("ParseModel(%q) succeeded", bad)
		}
	}
}

func TestErrorText(t *testing.T) {
	e := &Error{Provider: "anthropic", Kind: ErrRateLimit, Status: 429, Message: "slow down"}
	if got := e.Error(); got != "anthropic: rate_limit (HTTP 429): slow down" {
		t.Fatalf("Error() = %q", got)
	}
	long := &Error{Provider: "p", Kind: ErrNetwork, Message: strings.Repeat("x", 5000)}
	if n := len(long.Error()); n > maxErrorText+64 {
		t.Fatalf("Error() is %d bytes, want it clipped", n)
	}
	if runtime.ErrorKind(e, runtime.KindRuntime) != runtime.KindEffect {
		t.Fatal("a provider error must be of kind effect")
	}
}

func TestResponseText(t *testing.T) {
	r := Response{Content: []Block{{Type: BlockText, Text: "a"}, ToolUse("1", "t", "{}"), {Type: BlockText, Text: "b"}}}
	if r.Text() != "ab" {
		t.Fatalf("Text() = %q", r.Text())
	}
}

func TestFakeAnswersInOrderAndRecords(t *testing.T) {
	f := NewFake(TextReply("one", 3, 1), Reply{Err: errors.New("boom")})
	req := Request{Model: "m", MaxTokens: 10, Messages: []Message{{Role: RoleUser, Content: []Block{{Type: BlockText, Text: "hi"}}}}}
	r, err := f.Complete(context.Background(), req)
	if err != nil || r.Text() != "one" || r.InputTokens != 3 || r.OutputTokens != 1 {
		t.Fatalf("first = %+v, %v", r, err)
	}
	req.Messages[0].Content[0].Text = "changed" // the record is a copy
	if _, err := f.Complete(context.Background(), req); err == nil || err.Error() != "boom" {
		t.Fatalf("second err = %v", err)
	}
	if _, err := f.Complete(context.Background(), req); !errors.Is(err, ErrNoReply) {
		t.Fatalf("third err = %v, want ErrNoReply", err)
	}
	calls := f.Calls()
	if len(calls) != 3 || calls[0].Messages[0].Content[0].Text != "hi" || calls[1].Messages[0].Content[0].Text != "changed" {
		t.Fatalf("calls = %+v", calls)
	}
}

func TestFakeThenAndCancel(t *testing.T) {
	f := NewFake()
	f.Then = func(ctx context.Context, req Request) (Response, error) {
		return ToolUseReply(1, 1, ToolUse("t1", "weather", `{"city":"Lisbon"}`)).Response, nil
	}
	r, err := f.Complete(context.Background(), Request{})
	if err != nil || r.StopReason != StopToolUse || r.Content[0].Name != "weather" {
		t.Fatalf("Then reply = %+v, %v", r, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := f.Complete(ctx, Request{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled err = %v", err)
	}
	if len(f.Calls()) != 2 {
		t.Fatalf("calls = %d, want 2", len(f.Calls()))
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test -run '^(TestParseModel|TestErrorText|TestResponseText|TestFakeAnswersInOrderAndRecords|TestFakeThenAndCancel)$' ./arv/llm/`

Expected: FAIL: the package does not compile. The output includes:

```text
# github.com/delavalom/arvlang/arv/llm [github.com/delavalom/arvlang/arv/llm.test]
arv/llm/llm_test.go:13:15: undefined: ParseModel
```

- [ ] **Step 3: Implement**

Create `arv/llm/llm.go`:

```go
// Package llm is how Arv calls language models: a Provider interface the
// host implements or picks (Anthropic, or Fake in tests), the request and
// response shapes the evaluator's ask and agent effects build, and the
// prices that turn usage into the usd_cents budget. The host builds the
// providers, with their API keys; Arv code names models only through
// llm("provider:model") capabilities and never sees a key.
package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/delavalom/arvlang/arv/runtime"
)

// Provider sends one request to a model and returns its reply. It must
// return once ctx ends, and must never put a credential into an error.
type Provider interface {
	Complete(ctx context.Context, req Request) (Response, error)
}

// Request is one call: the conversation so far, the tools the model may
// ask for, and the most output tokens it may write.
type Request struct {
	Model     string // the provider's model ID, without the "provider:" prefix
	System    string // "" for none
	Messages  []Message
	Tools     []ToolDef
	MaxTokens int
}

// Roles of a message.
const (
	RoleUser      = "user"
	RoleAssistant = "assistant"
)

// Message is one turn of the conversation.
type Message struct {
	Role    string
	Content []Block
}

// Block types the evaluator reads or writes. A provider keeps any other
// block it returns (thinking) in Raw and sends it back unchanged.
const (
	BlockText       = "text"
	BlockToolUse    = "tool_use"
	BlockToolResult = "tool_result"
)

// Block is one piece of a message.
type Block struct {
	Type    string
	Text    string          // text; a tool_result's content
	ID      string          // tool_use: its ID; tool_result: the ID of the tool_use it answers
	Name    string          // tool_use: the tool's name
	Input   json.RawMessage // tool_use: the arguments, a JSON object
	IsError bool            // tool_result: the content is an error
	Raw     json.RawMessage // a block of another type, as the provider returned it
}

// ToolDef describes a tool to the model. In v1 every parameter is a
// required string.
type ToolDef struct {
	Name        string
	Description string
	Params      []string
}

// Stop reasons a Response may carry.
const (
	StopEndTurn   = "end_turn"
	StopSequence  = "stop_sequence"
	StopToolUse   = "tool_use"
	StopMaxTokens = "max_tokens"
	StopRefusal   = "refusal"
)

// Response is a model's reply and what it cost, in tokens.
type Response struct {
	Content      []Block
	StopReason   string
	InputTokens  int
	OutputTokens int
}

// Text is the reply's text blocks, joined.
func (r Response) Text() string {
	var b strings.Builder
	for _, c := range r.Content {
		if c.Type == BlockText {
			b.WriteString(c.Text)
		}
	}
	return b.String()
}

// Kinds of Error.
const (
	ErrAuth       = "auth"            // 401, 403: the key is missing, wrong or not allowed
	ErrBilling    = "billing"         // 402
	ErrNotFound   = "not_found"       // 404: often an unknown model
	ErrInvalid    = "invalid_request" // 400, 413 and other 4xx
	ErrRateLimit  = "rate_limit"      // 429
	ErrOverloaded = "overloaded"      // 529
	ErrServer     = "server"          // other 5xx
	ErrNetwork    = "network"         // no response
	ErrDecode     = "decode"          // a response that could not be read
)

// Error is a provider's failure. Its message never holds a credential: a
// provider builds it from the response's status and error message only.
type Error struct {
	Provider string
	Kind     string // one of the Err* constants
	Status   int    // the HTTP status, 0 if there was none
	Message  string // the provider's own message, clipped to maxErrorText bytes
}

// maxErrorText caps the provider message an Error keeps: an error value is
// not charged to alloc_mb.
const maxErrorText = 512

func (e *Error) Error() string {
	status := ""
	if e.Status != 0 {
		status = fmt.Sprintf(" (HTTP %d)", e.Status)
	}
	msg := e.Message
	if len(msg) > maxErrorText {
		msg = msg[:maxErrorText] + "..."
	}
	if msg != "" {
		msg = ": " + msg
	}
	return fmt.Sprintf("%s: %s%s%s", e.Provider, e.Kind, status, msg)
}

// ErrorKind makes a provider failure an Arv effect error: the world
// refused or failed, and restart may retry it.
func (e *Error) ErrorKind() string { return runtime.KindEffect }

// ParseModel splits "provider:model" into its parts. It refuses a wildcard:
// a call names exactly one model (a grant may name many).
func ParseModel(spec string) (provider, model string, err error) {
	c, err := runtime.Literal(runtime.LLM, spec)
	if err != nil {
		return "", "", err
	}
	provider, model, _ = strings.Cut(c.Scope, ":")
	if c.Scope == "*" || model == "*" {
		return "", "", fmt.Errorf("model %q: name one model, not a wildcard", spec)
	}
	return provider, model, nil
}
```

Create `arv/llm/fake.go`:

```go
package llm

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
)

// Fake is a scripted Provider for tests: it answers each call with the
// next Reply in its queue, then with Then (if set), and records every
// request it receives. It makes no network call and starts no goroutine.
type Fake struct {
	// Then answers once the queue is empty; nil makes that an error.
	Then func(ctx context.Context, req Request) (Response, error)

	mu      sync.Mutex
	replies []Reply
	calls   []Request
}

// Reply is one scripted answer: a response, or an error.
type Reply struct {
	Response Response
	Err      error
}

// NewFake returns a Fake that answers with replies, in order.
func NewFake(replies ...Reply) *Fake {
	return &Fake{replies: append([]Reply(nil), replies...)}
}

// ErrNoReply is the Fake's error when nothing is scripted for a call.
var ErrNoReply = errors.New("fake: no reply scripted")

// Complete records req and returns the next reply. Once ctx has ended it
// returns ctx's cause, still recording the call.
func (f *Fake) Complete(ctx context.Context, req Request) (Response, error) {
	f.mu.Lock()
	f.calls = append(f.calls, cloneRequest(req))
	var next *Reply
	if len(f.replies) > 0 {
		next = &f.replies[0]
		f.replies = f.replies[1:]
	}
	then := f.Then
	f.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return Response{}, context.Cause(ctx)
	}
	switch {
	case next != nil:
		return next.Response, next.Err
	case then != nil:
		return then(ctx, req)
	}
	return Response{}, ErrNoReply
}

// Calls returns a copy of every request received so far, in order.
func (f *Fake) Calls() []Request {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]Request, len(f.calls))
	for i, r := range f.calls {
		out[i] = cloneRequest(r)
	}
	return out
}

// TextReply is a final answer: text, stop reason end_turn, and the usage.
func TextReply(text string, in, out int) Reply {
	return Reply{Response: Response{
		Content:    []Block{{Type: BlockText, Text: text}},
		StopReason: StopEndTurn, InputTokens: in, OutputTokens: out,
	}}
}

// ToolUseReply asks for tools: stop reason tool_use, the given tool_use
// blocks (see ToolUse), and the usage.
func ToolUseReply(in, out int, uses ...Block) Reply {
	return Reply{Response: Response{
		Content: append([]Block(nil), uses...), StopReason: StopToolUse, InputTokens: in, OutputTokens: out,
	}}
}

// ToolUse is a tool_use block with the given ID, tool name and JSON input.
func ToolUse(id, name, input string) Block {
	return Block{Type: BlockToolUse, ID: id, Name: name, Input: json.RawMessage(input)}
}

// cloneRequest copies req deeply, so a caller that keeps appending to its
// conversation cannot change what was recorded.
func cloneRequest(req Request) Request {
	c := req
	c.Tools = append([]ToolDef(nil), req.Tools...)
	for i := range c.Tools {
		c.Tools[i].Params = append([]string(nil), req.Tools[i].Params...)
	}
	c.Messages = make([]Message, len(req.Messages))
	for i, m := range req.Messages {
		blocks := make([]Block, len(m.Content))
		for j, b := range m.Content {
			b.Input = append(json.RawMessage(nil), b.Input...)
			b.Raw = append(json.RawMessage(nil), b.Raw...)
			blocks[j] = b
		}
		c.Messages[i] = Message{Role: m.Role, Content: blocks}
	}
	return c
}
```

- [ ] **Step 4: Run to verify it passes**

Run: `go test -race -run '^(TestParseModel|TestErrorText|TestResponseText|TestFakeAnswersInOrderAndRecords|TestFakeThenAndCancel)$' ./arv/llm/`, then `gofmt -l .`, `go vet ./...` and `go test -race ./...`.
Expected: PASS everywhere.

- [ ] **Step 5: Commit**

```bash
git add arv/llm/llm_test.go arv/llm/llm.go arv/llm/fake.go
git commit -m "feat(llm): provider interface, errors and a fake provider

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```
