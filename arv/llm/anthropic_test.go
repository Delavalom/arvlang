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

// A redirect is not followed: net/http would re-send the x-api-key header
// to the redirect's host (it strips only Authorization and cookies), so a
// 3xx is an error and the other host never sees the key.
func TestAnthropicDoesNotFollowRedirects(t *testing.T) {
	other := newAPIServer(t, 200, `{"content":[],"stop_reason":"end_turn","usage":{}}`)
	redir := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL+"/v1/messages", http.StatusTemporaryRedirect)
	}))
	defer redir.Close()
	_, err := NewAnthropic(testKey, redir.URL).Complete(context.Background(), Request{Model: "m", MaxTokens: 1})
	var e *Error
	if !errors.As(err, &e) || e.Kind != ErrInvalid || e.Status != http.StatusTemporaryRedirect || !strings.Contains(e.Message, "redirect") {
		t.Fatalf("err = %v, want an invalid_request error for the redirect", err)
	}
	if other.header != nil {
		t.Fatalf("the redirect was followed; the other host saw x-api-key %q", other.header.Get("x-api-key"))
	}
}
