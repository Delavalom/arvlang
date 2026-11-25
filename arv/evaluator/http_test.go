package evaluator

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/delavalom/arvlang/arv/runtime"
	"github.com/delavalom/arvlang/arv/value"
)

// echoServer answers 201 with what it got: the method, the X-A,
// Authorization and Cookie headers and the body, joined by |.
func echoServer(t *testing.T) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		body, _ := io.ReadAll(r.Body)
		w.WriteHeader(http.StatusCreated)
		fmt.Fprintf(w, "%s|%s|%s|%s|%s", r.Method, r.Header.Get("X-A"), r.Header.Get("Authorization"), r.Header.Get("Cookie"), body)
	}))
	t.Cleanup(srv.Close)
	return srv, &hits
}

// I3: http needs the net capability for the URL's host and port, like
// http_get; without it nothing is sent.
func TestHTTPNeedsNetCapability(t *testing.T) {
	srv, hits := echoServer(t)
	src := fmt.Sprintf(`http({"url": %q, "method": "POST", "body": "x"})`, srv.URL)
	got, _ := runWith(t, src)
	wantDenied(t, got, `net("`+hostPort(srv)+`")`)
	if e := got.(*value.Error); e.ErrKind() != runtime.KindCapability {
		t.Fatalf("kind %s", e.ErrKind())
	}
	got, _ = runWith(t, src, grant(t, runtime.Net, "127.0.0.1:1"))
	wantDenied(t, got, `net("`+hostPort(srv)+`")`)
	if n := hits.Load(); n != 0 {
		t.Fatalf("server saw %d requests, want 0", n)
	}
}

// A POST with headers and a body; the result has http_get's shape.
func TestHTTPPostWithHeadersAndBody(t *testing.T) {
	srv, hits := echoServer(t)
	src := fmt.Sprintf(`let r = http({"url": %q, "method": "POST", "headers": {"X-A": "1", "authorization": "Bearer t"}, "body": "hello"});
[r["status"], r["body"], r]`, srv.URL+"/p")
	got, _ := runWith(t, src, grant(t, runtime.Net, "127.0.0.1"))
	if want := "[201, POST|1|Bearer t||hello, {body: POST|1|Bearer t||hello, status: 201}]"; got.Inspect() != want {
		t.Fatalf("got %s, want %s", got.Inspect(), want)
	}
	if hits.Load() != 1 {
		t.Fatalf("hits %d", hits.Load())
	}
}

// Every method allowed is sent as written; GET is the default; HEAD has
// no body in its answer.
func TestHTTPMethods(t *testing.T) {
	srv, _ := echoServer(t)
	for method, want := range map[string]string{
		"":        "GET||||",
		"GET":     "GET||||",
		"HEAD":    "",
		"POST":    "POST||||",
		"PUT":     "PUT||||",
		"PATCH":   "PATCH||||",
		"DELETE":  "DELETE||||",
		"OPTIONS": "OPTIONS||||",
	} {
		m := ""
		if method != "" {
			m = fmt.Sprintf(`, "method": %q`, method)
		}
		src := fmt.Sprintf(`http({"url": %q%s})["body"]`, srv.URL, m)
		if got, _ := runWith(t, src, grant(t, runtime.Net, "127.0.0.1")); got.Inspect() != want {
			t.Errorf("%s: got %s, want %q", method, got.Inspect(), want)
		}
	}
}

// http_get(url) is http({"url": url}): the same result.
func TestHTTPGetIsSugar(t *testing.T) {
	srv, _ := echoServer(t)
	src := fmt.Sprintf(`str(http_get(%[1]q)) == str(http({"url": %[1]q}))`, srv.URL)
	if got, _ := runWith(t, src, grant(t, runtime.Net, "127.0.0.1")); got.Inspect() != "true" {
		t.Fatalf("got %s", got.Inspect())
	}
}

// httpIn evaluates src with req bound to a hash of the given string
// pairs and headers, under limits l and caps.
func httpIn(t *testing.T, src string, req map[string]value.Object, l runtime.Limits, caps ...runtime.Capability) value.Object {
	t.Helper()
	root := runtime.NewRoot(context.Background(), runtime.NewCapSet(caps...), io.Discard)
	in := New(root)
	env := value.NewEnvironment()
	env.Set("req", value.NewHash(req))
	return in.withScope(frame{runtime.NewScope(root.Ctx), runtime.Root(l)}, func() value.Object {
		return in.Eval(parse(t, src), env)
	})
}

func strs(m map[string]string) *value.Hash {
	h := map[string]value.Object{}
	for k, v := range m {
		h[k] = &value.String{Value: v}
	}
	return value.NewHash(h)
}

// A request is checked before anything is required or sent; each error
// names the problem. The headers that would let a program smuggle a
// request (CR, LF, Host, Content-Length, hop-by-hop) are refused.
func TestHTTPRequestErrors(t *testing.T) {
	srv, hits := echoServer(t)
	s := func(v string) value.Object { return &value.String{Value: v} }
	u := s(srv.URL)
	hdr := func(k, v string) map[string]value.Object {
		return map[string]value.Object{"url": u, "headers": strs(map[string]string{k: v})}
	}
	many := map[string]string{}
	for i := 0; i < 65; i++ {
		many[fmt.Sprintf("X-%d", i)] = "v"
	}
	for _, tt := range []struct {
		req  map[string]value.Object
		want string
	}{
		{map[string]value.Object{}, "http: the request has no url"},
		{map[string]value.Object{"url": u, "header": s("x")}, `http: unknown key "header" in the request (want url, method, headers, body)`},
		{map[string]value.Object{"url": u, "timeout_ms": s("x")}, `http: unknown key "timeout_ms" in the request (want url, method, headers, body)`},
		{map[string]value.Object{"url": &value.Integer{Value: 1}}, "http: url must be STRING, got INTEGER"},
		{map[string]value.Object{"url": u, "method": TRUE}, "http: method must be STRING, got BOOLEAN"},
		{map[string]value.Object{"url": u, "method": s("TRACE")}, `http: method "TRACE" is not one of GET, HEAD, POST, PUT, PATCH, DELETE, OPTIONS`},
		{map[string]value.Object{"url": u, "method": s("get")}, `http: method "get" is not one of GET, HEAD, POST, PUT, PATCH, DELETE, OPTIONS`},
		{map[string]value.Object{"url": u, "method": s("CONNECT")}, `http: method "CONNECT" is not one of GET, HEAD, POST, PUT, PATCH, DELETE, OPTIONS`},
		{map[string]value.Object{"url": u, "headers": &value.Array{}}, "http: headers must be HASH, got ARRAY"},
		{map[string]value.Object{"url": u, "headers": value.NewHash(map[string]value.Object{"X-A": &value.Integer{Value: 1}})}, `http: header "X-A" must be STRING, got INTEGER`},
		{map[string]value.Object{"url": u, "body": NIL}, "http: body must be STRING, got NIL"},
		{map[string]value.Object{"url": u, "body": s(strings.Repeat("b", maxBody+1))}, "http: body is 10485761 bytes long (max 10485760)"},
		{map[string]value.Object{"url": s("ftp://x/")}, `http: invalid url "ftp://x/"`},
		{map[string]value.Object{"url": s("http:///p")}, "http: URL has no host"},
		{map[string]value.Object{"url": s("http://" + strings.Repeat("a", maxURLLen))}, "http: URL is 8199 bytes long (max 8192)"},
		{hdr("X-A", "1\r\nX-B: 2"), `http: header "X-A": value has CR, LF or another control byte`},
		{hdr("X-A", "1\n"), `http: header "X-A": value has CR, LF or another control byte`},
		{hdr("X-A", "a\x00b"), `http: header "X-A": value has CR, LF or another control byte`},
		{hdr("X-A", "a\x7fb"), `http: header "X-A": value has CR, LF or another control byte`},
		{hdr("X-A\r\nX-B", "1"), `http: invalid header name "X-A\r\nX-B"`},
		{hdr("X A", "1"), `http: invalid header name "X A"`},
		{hdr("X:A", "1"), `http: invalid header name "X:A"`},
		{hdr("", "1"), `http: invalid header name ""`},
		{hdr("Host", "evil"), `http: header "Host" cannot be set`},
		{hdr("content-length", "1"), `http: header "content-length" cannot be set`},
		{hdr("Transfer-Encoding", "chunked"), `http: header "Transfer-Encoding" cannot be set`},
		{hdr("Connection", "close"), `http: header "Connection" cannot be set`},
		{hdr("Keep-Alive", "1"), `http: header "Keep-Alive" cannot be set`},
		{hdr("Proxy-Connection", "1"), `http: header "Proxy-Connection" cannot be set`},
		{hdr("Proxy-Authorization", "1"), `http: header "Proxy-Authorization" cannot be set`},
		{hdr("Proxy-Authenticate", "1"), `http: header "Proxy-Authenticate" cannot be set`},
		{hdr("TE", "trailers"), `http: header "TE" cannot be set`},
		{hdr("Trailer", "X"), `http: header "Trailer" cannot be set`},
		{hdr("Upgrade", "h2c"), `http: header "Upgrade" cannot be set`},
		{map[string]value.Object{"url": u, "headers": strs(map[string]string{"X-A": "1", "x-a": "2"})}, `http: header "x-a" given twice`},
		{map[string]value.Object{"url": u, "headers": strs(many)}, "http: 65 headers (max 64)"},
		{hdr("X-A", strings.Repeat("v", maxReqHeaderBytes)), "http: headers are 65543 bytes (max 65536)"},
	} {
		got := httpIn(t, `http(req)`, tt.req, runtime.Limits{}, grant(t, runtime.Net, "*"), grant(t, runtime.Net, "127.0.0.1"))
		e, ok := got.(*value.Error)
		if !ok || e.Message != tt.want || e.ErrKind() != runtime.KindRuntime {
			t.Errorf("http(%s): got %.200s, want runtime error %q", value.NewHash(tt.req).Inspect()[:min(80, len(value.NewHash(tt.req).Inspect()))], got.Inspect(), tt.want)
		}
	}
	for src, want := range map[string]string{
		`http()`:                "ERROR: http: want 1 argument, got 0",
		`http({"url": "a"}, 1)`: "ERROR: http: want 1 argument, got 2",
		`http("http://x/")`:     "ERROR: http: argument must be HASH, got STRING",
		`http({1: "x"})`:        "ERROR: http: request keys must be STRING, got INTEGER",
		`http({"url": "http://x/", "headers": {1: "x"}})`: "ERROR: http: header names must be STRING, got INTEGER",
	} {
		if _, got := runOut(t, src); got.Inspect() != want {
			t.Errorf("%s = %s, want %s", src, got.Inspect(), want)
		}
	}
	if n := hits.Load(); n != 0 {
		t.Fatalf("server saw %d requests, want 0", n)
	}
}

// Error text is bounded however long the header or method.
func TestHTTPErrorTextIsBounded(t *testing.T) {
	long := strings.Repeat("x", 1<<20)
	for _, req := range []map[string]value.Object{
		{"url": &value.String{Value: "http://h/"}, "method": &value.String{Value: long}},
		{"url": &value.String{Value: "http://h/"}, long: TRUE},
		{"url": &value.String{Value: "http://h/"}, "headers": strs(map[string]string{long + "\n": "v"})},
		{"url": &value.String{Value: "http://h/"}, "headers": strs(map[string]string{"Host" + long: "v\n"})},
	} {
		got := httpIn(t, `http(req)`, req, runtime.Limits{})
		if e, ok := got.(*value.Error); !ok || len(e.Message) > 400 {
			t.Errorf("got %.120s (%d bytes)", got.Inspect(), len(got.Inspect()))
		}
	}
}

// The request's body and headers are charged before it is sent: under a
// limit they do not fit, nothing reaches the server.
func TestHTTPChargesTheRequestBeforeSending(t *testing.T) {
	srv, hits := echoServer(t)
	req := map[string]value.Object{
		"url":     &value.String{Value: srv.URL},
		"method":  &value.String{Value: "PUT"},
		"headers": strs(map[string]string{"X-A": "12345"}),
		"body":    &value.String{Value: strings.Repeat("b", 1000)},
	}
	// body 1000, header 3+5+4, then the answer "PUT|12345|||" + body and
	// the record's two pairs.
	reqBytes := int64(1000 + 3 + 5 + 4)
	answer := int64(len("PUT|12345|||")+1000) + 2*hashPairSize
	got := httpIn(t, `http(req)["status"]`, req, runtime.Limits{AllocBytes: reqBytes + answer}, grant(t, runtime.Net, "127.0.0.1"))
	if got.Inspect() != "201" {
		t.Fatalf("under %d bytes: got %s", reqBytes+answer, got.Inspect())
	}
	got = httpIn(t, `http(req)["status"]`, req, runtime.Limits{AllocBytes: reqBytes - 1}, grant(t, runtime.Net, "127.0.0.1"))
	if e, ok := got.(*value.Error); !ok || e.ErrKind() != runtime.KindBudget {
		t.Fatalf("under %d bytes: got %s, want a budget error", reqBytes-1, got.Inspect())
	}
	if n := hits.Load(); n != 1 {
		t.Fatalf("server saw %d requests, want 1", n)
	}
}

// The response limits hold for http as for http_get.
func TestHTTPResponseLimit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(strings.Repeat("a", maxBody+1)))
	}))
	defer srv.Close()
	got, _ := runWith(t, fmt.Sprintf(`http({"url": %q, "method": "POST"})`, srv.URL), grant(t, runtime.Net, "127.0.0.1"))
	if e, ok := got.(*value.Error); !ok || e.Message != "http: body exceeds 10 MiB limit" || e.ErrKind() != runtime.KindEffect {
		t.Fatalf("got %.100s", got.Inspect())
	}
}

// The transport reads no proxy from the environment: the dial check must
// see the target's address, not a proxy's.
func TestHTTPUsesNoProxy(t *testing.T) {
	root := runtime.NewRoot(context.Background(), runtime.NewCapSet(), io.Discard)
	if transportFor(root).Proxy != nil {
		t.Fatal("the transport has a proxy function")
	}
}

// Both engines agree on http's errors and denials (no request is made).
func TestHTTPEnginesAgree(t *testing.T) {
	for _, src := range []string{
		`http({"url": "ftp://x/"})`,
		`http(5)`,
		`http({"url": "http://127.0.0.1:1/", "method": "POST", "body": "b"})`,
		`let r = {"url": "http://h/", "headers": {"Host": "x"}}; http(r)`,
		`match http({"url": "http://h/", "method": "TRACE"}) { ok(v) => v, err(e) => e["message"] }`,
	} {
		sameOnBothEngines(t, src)
	}
}
