package evaluator

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	goruntime "runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/delavalom/arvlang/arv/runtime"
	"github.com/delavalom/arvlang/arv/value"
)

// pongServer counts requests and answers "pong".
func pongServer(t *testing.T) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		fmt.Fprint(w, "pong")
	}))
	t.Cleanup(srv.Close)
	return srv, &hits
}

// SSRF / DNS rebinding: a granted name that resolves to an internal address
// is refused at the dial, on the address actually reached. "localhost"
// resolves (through the system resolver, /etc/hosts) to loopback only.
func TestHTTPGetRefusesANameThatResolvesToLoopback(t *testing.T) {
	srv, hits := pongServer(t)
	port := srv.URL[strings.LastIndex(srv.URL, ":")+1:]
	src := fmt.Sprintf(`http_get("http://localhost:%s/")["body"]`, port)

	got, _ := runWith(t, src, grant(t, runtime.Net, "localhost"))
	e, ok := got.(*value.Error)
	if !ok || !strings.Contains(e.Message, "capability denied: net(") ||
		!strings.Contains(e.Message, "localhost:"+port+" reached a loopback address") {
		t.Fatalf("got %s, want a dial-time denial", got.Inspect())
	}
	if n := hits.Load(); n != 0 {
		t.Fatalf("server saw %d requests, want 0", n)
	}

	// The explicit grant path: the name for the request, the IP for the dial.
	got, _ = runWith(t, src, grant(t, runtime.Net, "localhost"), grant(t, runtime.Net, "127.0.0.1"))
	if got.Inspect() != "pong" {
		t.Fatalf("with an IP grant: got %s, want pong", got.Inspect())
	}
}

// The dial check also covers redirect hops: a redirect to a granted name
// that resolves to loopback is refused unless an IP grant covers it.
func TestHTTPGetRedirectDialIsChecked(t *testing.T) {
	target, hits := pongServer(t)
	elsewhere := strings.Replace(target.URL, "127.0.0.1", "localhost", 1)
	redirector := httptest.NewServer(http.RedirectHandler(elsewhere, http.StatusFound))
	defer redirector.Close()
	src := fmt.Sprintf(`http_get(%q)["body"]`, redirector.URL)

	got, _ := runWith(t, src, grant(t, runtime.Net, hostPort(redirector)), grant(t, runtime.Net, "localhost"))
	wantErrorHas(t, got, "reached a loopback address")
	if n := hits.Load(); n != 0 {
		t.Fatalf("redirect target saw %d requests, want 0", n)
	}
	got, _ = runWith(t, src, grant(t, runtime.Net, "127.0.0.1"), grant(t, runtime.Net, "localhost"))
	if got.Inspect() != "pong" {
		t.Fatalf("with an IP grant: got %s, want pong", got.Inspect())
	}
}

// clientConns counts goroutines running an HTTP client connection loop.
func clientConns() int {
	buf := make([]byte, 1<<20)
	return strings.Count(string(buf[:goruntime.Stack(buf, true)]), "net/http.(*persistConn).readLoop(")
}

// Leak rule: http_get keeps no idle connection, so no transport goroutine
// outlives the call (and so none outlives arv.Run). Nothing else in this
// package's tests makes HTTP client connections, so none may remain.
func TestHTTPGetLeavesNoGoroutines(t *testing.T) {
	srv, _ := pongServer(t)
	caps := grant(t, runtime.Net, "127.0.0.1")
	src := fmt.Sprintf(`http_get(%q)["body"]`, srv.URL)
	before := goruntime.NumGoroutine()
	for i := 0; i < 5; i++ {
		if got, _ := runWith(t, src, caps); got.Inspect() != "pong" {
			t.Fatalf("got %s", got.Inspect())
		}
	}
	deadline := time.Now().Add(30 * time.Second) // generous: the machine may be loaded
	for (clientConns() > 0 || goruntime.NumGoroutine() > before) && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if c, n := clientConns(), goruntime.NumGoroutine(); c > 0 || n > before {
		buf := make([]byte, 1<<16)
		t.Fatalf("%d client connections left; %d goroutines before, %d after:\n%s",
			c, before, n, buf[:goruntime.Stack(buf, true)])
	}
}
