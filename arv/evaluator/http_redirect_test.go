package evaluator

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/delavalom/arvlang/arv/llm"
	"github.com/delavalom/arvlang/arv/runtime"
	"github.com/delavalom/arvlang/arv/value"
)

// asLocalhost is srv's URL with localhost for 127.0.0.1: the same server
// under another host name.
func asLocalhost(srv *httptest.Server) string {
	return strings.Replace(srv.URL, "127.0.0.1", "localhost", 1)
}

// redirectTo answers every request with a redirect of the given status to
// target.
func redirectTo(t *testing.T, target string, code int) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.RedirectHandler(target, code))
	t.Cleanup(srv.Close)
	return srv
}

// both grants a request to the 127.0.0.1 servers and to localhost needs.
func both(t *testing.T) []runtime.Capability {
	return []runtime.Capability{grant(t, runtime.Net, "127.0.0.1"), grant(t, runtime.Net, "localhost")}
}

// I3: every redirect is checked again, host and port, as for http_get.
func TestHTTPRedirectToAnUngrantedHostIsDenied(t *testing.T) {
	target, hits := echoServer(t)
	r := redirectTo(t, asLocalhost(target), http.StatusFound)
	got, _ := runWith(t, fmt.Sprintf(`http({"url": %q, "method": "POST", "body": "b"})`, r.URL), grant(t, runtime.Net, "127.0.0.1"))
	wantDenied(t, got, `net("localhost:`)
	other := redirectTo(t, target.URL, http.StatusTemporaryRedirect)
	got, _ = runWith(t, fmt.Sprintf(`http({"url": %q})`, other.URL), grant(t, runtime.Net, hostPort(other)))
	wantDenied(t, got, `net("`+hostPort(target)+`")`)
	if n := hits.Load(); n != 0 {
		t.Fatalf("the redirect target saw %d requests", n)
	}
}

// I3: a granted name that resolves to an internal address is refused at
// the dial, for http as for http_get.
func TestHTTPRefusesANameThatResolvesToLoopback(t *testing.T) {
	srv, hits := echoServer(t)
	src := fmt.Sprintf(`http({"url": %q, "method": "POST", "body": "b"})["body"]`, asLocalhost(srv))
	got, _ := runWith(t, src, grant(t, runtime.Net, "localhost"))
	e, ok := got.(*value.Error)
	if !ok || !strings.Contains(e.Message, "capability denied: net(") || !strings.Contains(e.Message, "reached a loopback address") {
		t.Fatalf("got %s, want a dial-time denial", got.Inspect())
	}
	if n := hits.Load(); n != 0 {
		t.Fatalf("server saw %d requests, want 0", n)
	}
	if got, _ := runWith(t, src, both(t)...); got.Inspect() != "POST||||b" {
		t.Fatalf("with an IP grant: got %s", got.Inspect())
	}
}

// Go's rules: a 301, 302 or 303 turns a POST into a GET without its body;
// a 307 or 308 to the same host sends the method and body again.
func TestHTTPRedirectBodyRules(t *testing.T) {
	target, _ := echoServer(t)
	for _, tt := range []struct {
		code int
		want string
	}{
		{http.StatusMovedPermanently, "GET|1|||"},
		{http.StatusFound, "GET|1|||"},
		{http.StatusSeeOther, "GET|1|||"},
		{http.StatusTemporaryRedirect, "POST|1|||body"},
		{http.StatusPermanentRedirect, "POST|1|||body"},
	} {
		mux := http.NewServeMux()
		mux.Handle("/from", http.RedirectHandler("/to", tt.code))
		mux.Handle("/to", target.Config.Handler)
		srv := httptest.NewServer(mux)
		src := fmt.Sprintf(`http({"url": %q, "method": "POST", "headers": {"X-A": "1"}, "body": "body"})["body"]`, srv.URL+"/from")
		got, _ := runWith(t, src, grant(t, runtime.Net, "127.0.0.1"))
		srv.Close()
		if got.Inspect() != tt.want {
			t.Errorf("%d: got %s, want %s", tt.code, got.Inspect(), tt.want)
		}
	}
}

// A redirect never carries the request body to another host: a 307 or
// 308 that would resend it there is refused, and the target sees nothing.
func TestHTTPRedirectNeverResendsTheBodyToAnotherHost(t *testing.T) {
	target, hits := echoServer(t)
	for _, code := range []int{http.StatusTemporaryRedirect, http.StatusPermanentRedirect} {
		r := redirectTo(t, asLocalhost(target)+"/to", code)
		got, _ := runWith(t, fmt.Sprintf(`http({"url": %q, "method": "PUT", "body": "secret"})`, r.URL), both(t)...)
		e, ok := got.(*value.Error)
		want := fmt.Sprintf("a %d redirect to localhost:%s would send the request body to another host", code, target.URL[strings.LastIndex(target.URL, ":")+1:])
		if !ok || e.ErrKind() != runtime.KindEffect || !strings.Contains(e.Message, want) || !strings.HasPrefix(e.Message, "http: ") {
			t.Fatalf("%d: got %s, want an effect error with %q", code, got.Inspect(), want)
		}
	}
	// Without a body, the same redirect is followed.
	r := redirectTo(t, asLocalhost(target)+"/to", http.StatusTemporaryRedirect)
	if got, _ := runWith(t, fmt.Sprintf(`http({"url": %q, "method": "DELETE"})["body"]`, r.URL), both(t)...); got.Inspect() != "DELETE||||" {
		t.Fatalf("no body: got %s", got.Inspect())
	}
	// An empty body is no body.
	if got, _ := runWith(t, fmt.Sprintf(`http({"url": %q, "method": "POST", "body": ""})["body"]`, r.URL), both(t)...); got.Inspect() != "POST||||" {
		t.Fatalf("empty body: got %s", got.Inspect())
	}
	if n := hits.Load(); n != 2 {
		t.Fatalf("the target saw %d requests, want 2", n)
	}
}

// Go's rules: Authorization and Cookie go to the same host on a redirect,
// never to another one; other headers go to both.
func TestHTTPRedirectDropsSensitiveHeadersForAnotherHost(t *testing.T) {
	target, _ := echoServer(t)
	headers := `{"X-A": "1", "Authorization": "Bearer t", "Cookie": "s=1"}`
	away := redirectTo(t, asLocalhost(target)+"/to", http.StatusFound)
	got, _ := runWith(t, fmt.Sprintf(`http({"url": %q, "headers": %s})["body"]`, away.URL, headers), both(t)...)
	if got.Inspect() != "GET|1|||" {
		t.Fatalf("another host: got %s", got.Inspect())
	}
	mux := http.NewServeMux()
	mux.Handle("/from", http.RedirectHandler("/to", http.StatusFound))
	mux.Handle("/to", target.Config.Handler)
	same := httptest.NewServer(mux)
	defer same.Close()
	got, _ = runWith(t, fmt.Sprintf(`http({"url": %q, "headers": %s})["body"]`, same.URL+"/from", headers), both(t)...)
	if got.Inspect() != "GET|1|Bearer t|s=1|" {
		t.Fatalf("same host: got %s", got.Inspect())
	}
}

// Phase 7: a denial from http that reaches a model is redacted like
// http_get's: the model sees the kind, never the scope or the grants.
func TestAgentToolHTTPDenialIsRedacted(t *testing.T) {
	srv, hits := echoServer(t)
	f := llm.NewFake(
		llm.ToolUseReply(20, 10, llm.ToolUse("t1", "post", fmt.Sprintf(`{"url": %q}`, srv.URL))),
		llm.TextReply("refused", 40, 5),
	)
	src := `agent("anthropic:claude-sonnet-5-5", "Post it", {"post": fn(url) { http({"url": url, "method": "POST", "body": "x"})["body"] }})`
	got, _ := runLLM(t, src, fakeHost(f, nil), runtime.Limits{},
		grant(t, runtime.LLM, sonnet), grant(t, runtime.Tool, "post"), grant(t, runtime.Net, "api.private.example"))
	if got.Inspect() != "refused" {
		t.Fatalf("got %s", got.Inspect())
	}
	res := toolResults(t, f.Calls()[1])
	if len(res) != 1 || !res[0].IsError || res[0].Text != "capability denied: net" {
		t.Fatalf("tool results = %+v", res)
	}
	if hits.Load() != 0 {
		t.Fatal("the tool's request reached the server")
	}
}
