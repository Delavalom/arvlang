package evaluator

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/delavalom/arvlang/arv/runtime"
)

func TestNetRequestUsesTheSchemeDefaultPort(t *testing.T) {
	for raw, want := range map[string]string{
		"http://Example.com/x":      "example.com:80",
		"https://example.com/":      "example.com:443",
		"https://example.com:8443/": "example.com:8443",
		"http://example.com:0080/":  "example.com:80",
		"http://[::1]/":             "[::1]:80",
		"http://localhost.:81/":     "localhost:81",
	} {
		u, err := url.Parse(raw)
		if err != nil {
			t.Fatal(err)
		}
		got, err := netRequest(u)
		if err != nil || got.Scope != want {
			t.Errorf("netRequest(%s) = %s, %v; want scope %q", raw, got, err, want)
		}
	}
	for _, raw := range []string{"ftp://example.com/", "http://example.com:70000/"} {
		u, _ := url.Parse(raw)
		if got, err := netRequest(u); err == nil {
			t.Errorf("netRequest(%s) = %s, want an error", raw, got)
		}
	}
}

// hostPort is srv's "127.0.0.1:port".
func hostPort(srv *httptest.Server) string { return strings.TrimPrefix(srv.URL, "http://") }

func TestHTTPGetChecksThePort(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		fmt.Fprint(w, "pong")
	}))
	defer srv.Close()
	other := httptest.NewServer(http.NotFoundHandler()) // a port that is not srv's
	defer other.Close()
	src := fmt.Sprintf(`http_get(%q)["body"]`, srv.URL)

	got, _ := runWith(t, src, grant(t, runtime.Net, hostPort(srv)))
	if got.Inspect() != "pong" {
		t.Fatalf("grant for the right port: got %s", got.Inspect())
	}
	got, _ = runWith(t, src, grant(t, runtime.Net, hostPort(other)))
	wantDenied(t, got, `net("`+hostPort(srv)+`")`)
	if n := hits.Load(); n != 1 {
		t.Fatalf("server saw %d requests, want 1", n)
	}
}

// I3: a port grant holds across redirects: a redirect to the same host on
// another port is denied before it is followed.
func TestHTTPGetChecksRedirectPorts(t *testing.T) {
	var reached atomic.Bool
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached.Store(true)
	}))
	defer target.Close()
	redirector := httptest.NewServer(http.RedirectHandler(target.URL, http.StatusFound))
	defer redirector.Close()

	got, _ := runWith(t, fmt.Sprintf(`http_get(%q)`, redirector.URL), grant(t, runtime.Net, hostPort(redirector)))
	wantDenied(t, got, `net("`+hostPort(target)+`")`)
	if reached.Load() {
		t.Fatal("the redirect to an ungranted port was followed")
	}
}
