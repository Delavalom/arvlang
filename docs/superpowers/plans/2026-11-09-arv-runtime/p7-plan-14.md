# Phase 7, task p7.14: Anthropic HTTP provider

> **For agentic workers:** this file is one task, p7.14. Do it test first, exactly as written; if something here does not match the code you find, stop and report instead of improvising. Prerequisite: task p7.13 is committed in your tree. Start every shell command with `cd <your worktree> &&` and use absolute paths: the shell's working directory does not persist. Before committing: `gofmt -l .` prints nothing, `go vet ./...` is clean and `go test -race ./...` is green. End the commit message with a blank line and then `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`. Never use `--no-verify` and never push; keep Go files under 15 KB and markdown under 18 KB. `go.mod` says `go 1.24.0` (toolchain 1.26.2): `t.Chdir`, `min` and `max` are available; do not use `testing/synctest` or `sync.WaitGroup.Go`. Before a long test run, check `df -h /System/Volumes/Data`; below 1 GB free, stop and report (only `go clean -cache` may be deleted). This machine is heavily loaded: keep wall-clock bounds generous. No test may reach the network or spend money: never set `ARV_LIVE_ANTHROPIC`, and never put a real API key anywhere.
>
> Every code block below was applied, in task order, to a scratch copy of `feat/phases-3-8` (Phases 3, 4 and 5 merged; Phase 6 is not) and passed `gofmt -l .`, `go vet ./...` and `go test -race ./...` (including `e2e/`). Design: `14-phase7-agents.md`. Deviations from it, and why: `p7-plan-1.md`.

### Task p7.14: Anthropic HTTP provider

`NewAnthropic(key, url)` (url `""` means `https://api.anthropic.com/v1/messages`) is a `Provider` over plain `net/http`: `POST` with `x-api-key`, `anthropic-version: 2026-10-21` and `content-type: application/json`. Non-200 replies become `*Error` with a kind from the status and the API's `type: message`; a transport failure is `network`, an unreadable or over-16 MiB body `decode`. The key is replaced by `[redacted]` in any error text, even one the server echoes, and the provider prints as `llm.Anthropic(url)` under every `fmt` verb, so it cannot leak through logging either. The client honours proxies and disables keep-alives; the leak test checks that no goroutine survives a cancelled call. Every test talks to an `httptest` server; none reaches the network.

**Files:**
- Create: `arv/llm/anthropic.go`
- Create (test): `arv/llm/anthropic_test.go`

- [ ] **Step 1: Write the failing tests**

Create `arv/llm/anthropic_test.go`:

```go
package llm

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	goruntime "runtime"
	"strings"
	"testing"
	"time"
)

const testKey = "sk-ant-test-SECRET-0123456789"

// apiServer answers every request with status and body ($KEY in it
// becomes the request's x-api-key, as a careless server might echo it),
// and records the last request's path, headers and body.
type apiServer struct {
	*httptest.Server
	path   string
	header http.Header
	body   []byte
}

func newAPIServer(t *testing.T, status int, body string) *apiServer {
	t.Helper()
	s := &apiServer{}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.body, _ = io.ReadAll(r.Body)
		s.path, s.header = r.URL.Path, r.Header.Clone()
		w.WriteHeader(status)
		io.WriteString(w, strings.ReplaceAll(body, "$KEY", r.Header.Get("x-api-key")))
	}))
	t.Cleanup(s.Close)
	return s
}

// The request carries the key and the API version in headers, and the
// body is encodeRequest's; the reply is decodeResponse's.
func TestAnthropicRoundTrip(t *testing.T) {
	srv := newAPIServer(t, 200, `{"content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{"input_tokens":5,"output_tokens":1}}`)
	a := NewAnthropic(testKey, srv.URL+"/v1/messages")
	req := Request{Model: "claude-haiku-5-5", MaxTokens: 100, Messages: []Message{{Role: RoleUser, Content: []Block{{Type: BlockText, Text: "Bike?"}}}},
		Tools: []ToolDef{{Name: "weather", Params: []string{"city"}}}}
	r, err := a.Complete(context.Background(), req)
	if err != nil || r.Text() != "ok" || r.InputTokens != 5 || r.OutputTokens != 1 || r.StopReason != StopEndTurn {
		t.Fatalf("Complete = %+v, %v", r, err)
	}
	if srv.path != "/v1/messages" || srv.header.Get("x-api-key") != testKey || srv.header.Get("anthropic-version") != AnthropicVersion ||
		srv.header.Get("content-type") != "application/json" {
		t.Fatalf("path %q, headers %v", srv.path, srv.header)
	}
	want, _ := encodeRequest(req)
	if string(srv.body) != string(want) {
		t.Fatalf("body = %s\nwant %s", srv.body, want)
	}
	if NewAnthropic(testKey, "").url != AnthropicURL {
		t.Fatal("the default URL is not the Messages API")
	}
}

// Every failure is an *Error of the right kind, and none holds the key,
// even when the server echoes it.
func TestAnthropicErrors(t *testing.T) {
	for _, c := range []struct {
		status     int
		body, kind string
	}{
		{401, `{"type":"error","error":{"type":"authentication_error","message":"invalid x-api-key $KEY"}}`, ErrAuth},
		{402, `{"type":"error","error":{"type":"billing_error","message":"pay"}}`, ErrBilling},
		{404, `{"type":"error","error":{"type":"not_found_error","message":"model: claude-x"}}`, ErrNotFound},
		{400, `{"type":"error","error":{"type":"invalid_request_error","message":"bad $KEY"}}`, ErrInvalid},
		{429, `{"type":"error","error":{"type":"rate_limit_error","message":"slow down"}}`, ErrRateLimit},
		{529, `{"type":"error","error":{"type":"overloaded_error","message":"busy"}}`, ErrOverloaded},
		{500, `not json $KEY`, ErrServer},
		{200, `{"content": 5}`, ErrDecode},
		{200, `{"stop_reason":"end_turn"}`, ErrDecode},
		{200, `$KEY`, ErrDecode},
	} {
		srv := newAPIServer(t, c.status, c.body)
		_, err := NewAnthropic(testKey, srv.URL).Complete(context.Background(), Request{Model: "m", MaxTokens: 1})
		var e *Error
		if !errors.As(err, &e) || e.Kind != c.kind || (c.status != 200 && e.Status != c.status) {
			t.Errorf("%d %s: err = %v, want kind %s", c.status, c.body, err, c.kind)
		}
		if err != nil && strings.Contains(err.Error(), testKey) {
			t.Errorf("%d %s: the error holds the key: %v", c.status, c.body, err)
		}
	}
	srv := newAPIServer(t, 200, "")
	srv.Close()
	_, err := NewAnthropic(testKey, srv.URL).Complete(context.Background(), Request{Model: "m"})
	var e *Error
	if !errors.As(err, &e) || e.Kind != ErrNetwork || strings.Contains(err.Error(), testKey) {
		t.Fatalf("closed server: %v", err)
	}
}

func TestAnthropicReplyTooLarge(t *testing.T) {
	srv := newAPIServer(t, 200, `{"content":[{"type":"text","text":"`+strings.Repeat("x", maxResponse)+`"}]}`)
	_, err := NewAnthropic(testKey, srv.URL).Complete(context.Background(), Request{Model: "m"})
	var e *Error
	if !errors.As(err, &e) || e.Kind != ErrDecode || !strings.Contains(e.Message, "over 16 MiB") {
		t.Fatalf("err = %v", err)
	}
}

// A call ends with its context, with the context's cause, and no
// goroutine of it outlives it.
func TestAnthropicCancelAndLeaks(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body) // so the server sees the client hang up
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()
	defer close(release)
	ok := newAPIServer(t, 200, `{"content":[],"stop_reason":"end_turn","usage":{}}`)
	before := goruntime.NumGoroutine()
	ctx, cancel := context.WithCancelCause(context.Background())
	cause := errors.New("scope ended")
	time.AfterFunc(50*time.Millisecond, func() { cancel(cause) })
	_, err := NewAnthropic(testKey, srv.URL).Complete(ctx, Request{Model: "m"})
	if !errors.Is(err, cause) {
		t.Fatalf("err = %v, want the context's cause", err)
	}
	if _, err := NewAnthropic(testKey, ok.URL).Complete(context.Background(), Request{Model: "m"}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for goruntime.NumGoroutine() > before {
		if time.Now().After(deadline) {
			buf := make([]byte, 1<<16)
			t.Fatalf("goroutines: %d, before: %d\n%s", goruntime.NumGoroutine(), before, buf[:goruntime.Stack(buf, true)])
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// Printing the provider, as a pointer or a value, with any verb, never
// prints the key.
func TestAnthropicNeverPrintsTheKey(t *testing.T) {
	a := NewAnthropic(testKey, "")
	h := &Host{Providers: map[string]Provider{"anthropic": a}}
	for _, verb := range []string{"%v", "%+v", "%#v", "%s", "%q", "%x"} {
		for _, v := range []any{a, *a, h, *h} {
			if s := fmt.Sprintf(verb, v); strings.Contains(s, testKey) || strings.Contains(s, fmt.Sprintf("%x", testKey)) {
				t.Errorf("Sprintf(%s, %T) = %s", verb, v, s)
			}
		}
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test -run '^(TestAnthropicRoundTrip|TestAnthropicErrors|TestAnthropicReplyTooLarge|TestAnthropicCancelAndLeaks|TestAnthropicNeverPrintsTheKey)$' ./arv/llm/`

Expected: FAIL: the package does not compile. The output includes:

```text
# github.com/delavalom/arvlang/arv/llm [github.com/delavalom/arvlang/arv/llm.test]
arv/llm/anthropic_test.go:45:7: undefined: NewAnthropic
arv/llm/anthropic_test.go:52:116: undefined: AnthropicVersion
arv/llm/anthropic_test.go:60:5: undefined: NewAnthropic
```

- [ ] **Step 3: Implement**

Create `arv/llm/anthropic.go`:

```go
package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// The Anthropic Messages API, as the claude-api skill described it on
// 2026-11-11: POST /v1/messages with x-api-key, anthropic-version and a
// JSON body; tool definitions carry a JSON Schema; replies are content
// blocks, sent back unchanged on the next turn.
const (
	AnthropicURL     = "https://api.anthropic.com/v1/messages"
	AnthropicVersion = "2026-10-21"
)

// maxResponse caps the bytes of one reply the provider reads. A reply is
// at most maxTokens tokens of output; a larger body is an error.
const maxResponse = 16 << 20

// Anthropic calls the Anthropic Messages API over plain net/http. The key
// stays in an unexported field: no method returns it, it is never put in
// an error, and printing the provider with any verb prints no key.
type Anthropic struct {
	key    string
	url    string
	client *http.Client
}

// NewAnthropic returns a provider that sends key to url (AnthropicURL if
// url is ""). Each call opens its own connection (keep-alives are off), so
// no connection, and no goroutine serving one, outlives the call.
func NewAnthropic(key, url string) *Anthropic {
	if url == "" {
		url = AnthropicURL
	}
	tr := &http.Transport{Proxy: http.ProxyFromEnvironment, DisableKeepAlives: true, TLSHandshakeTimeout: 30 * time.Second}
	return &Anthropic{key: key, url: url, client: &http.Client{Transport: tr, Timeout: 10 * time.Minute}}
}

// String, GoString and Format keep the key out of fmt's output, for every
// verb (%+v and %#v included), whether the provider is printed as a
// pointer or as a value: fmt would print an unexported field otherwise.
func (a Anthropic) String() string                { return "llm.Anthropic(" + a.url + ")" }
func (a Anthropic) GoString() string              { return a.String() }
func (a Anthropic) Format(f fmt.State, verb rune) { io.WriteString(f, a.String()) }

// Complete sends req and decodes the reply. Errors are *Error, except that
// once ctx has ended it returns ctx's cause.
func (a *Anthropic) Complete(ctx context.Context, req Request) (Response, error) {
	body, err := encodeRequest(req)
	if err != nil {
		return Response{}, &Error{Provider: "anthropic", Kind: ErrInvalid, Message: err.Error()}
	}
	hreq, err := http.NewRequestWithContext(ctx, http.MethodPost, a.url, bytes.NewReader(body))
	if err != nil {
		return Response{}, a.fail(ErrInvalid, 0, err.Error())
	}
	hreq.Header.Set("x-api-key", a.key)
	hreq.Header.Set("anthropic-version", AnthropicVersion)
	hreq.Header.Set("content-type", "application/json")
	resp, err := a.client.Do(hreq)
	if err != nil {
		if ctx.Err() != nil {
			return Response{}, context.Cause(ctx)
		}
		return Response{}, a.fail(ErrNetwork, 0, err.Error())
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponse+1))
	if err != nil {
		if ctx.Err() != nil {
			return Response{}, context.Cause(ctx)
		}
		return Response{}, a.fail(ErrNetwork, resp.StatusCode, err.Error())
	}
	if len(data) > maxResponse {
		return Response{}, a.fail(ErrDecode, resp.StatusCode, fmt.Sprintf("reply over %d MiB", maxResponse>>20))
	}
	if resp.StatusCode != http.StatusOK {
		return Response{}, a.fail(statusKind(resp.StatusCode), resp.StatusCode, apiErrorMessage(data))
	}
	out, err := decodeResponse(data)
	if err != nil {
		return Response{}, a.fail(ErrDecode, resp.StatusCode, err.Error())
	}
	return out, nil
}

// fail is an *Error whose message cannot hold the key, even if a server
// or a transport error echoed it.
func (a *Anthropic) fail(kind string, status int, msg string) *Error {
	if a.key != "" {
		msg = strings.ReplaceAll(msg, a.key, "[redacted]")
	}
	return &Error{Provider: "anthropic", Kind: kind, Status: status, Message: msg}
}

// statusKind is the Error kind for an HTTP status the API answered with.
func statusKind(status int) string {
	switch {
	case status == 401 || status == 403:
		return ErrAuth
	case status == 402:
		return ErrBilling
	case status == 404:
		return ErrNotFound
	case status == 429:
		return ErrRateLimit
	case status == 529:
		return ErrOverloaded
	case status >= 500:
		return ErrServer
	}
	return ErrInvalid
}

// apiErrorMessage is the message of an API error body
// ({"type":"error","error":{"type":...,"message":...}}), or "" if the body
// is not one.
func apiErrorMessage(data []byte) string {
	var e struct {
		Error struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(data, &e) != nil {
		return ""
	}
	if e.Error.Type != "" && e.Error.Message != "" {
		return e.Error.Type + ": " + e.Error.Message
	}
	return e.Error.Message
}
```

- [ ] **Step 4: Run to verify it passes**

Run: `go test -race -run '^(TestAnthropicRoundTrip|TestAnthropicErrors|TestAnthropicReplyTooLarge|TestAnthropicCancelAndLeaks|TestAnthropicNeverPrintsTheKey)$' ./arv/llm/`, then `gofmt -l .`, `go vet ./...` and `go test -race ./...`.
Expected: PASS everywhere.

- [ ] **Step 5: Commit**

```bash
git add arv/llm/anthropic_test.go arv/llm/anthropic.go
git commit -m "feat(llm): Anthropic provider over net/http

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```
