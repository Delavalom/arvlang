# Phase 8.3, part 6 of 10: http_get and redirects check host and port (task p83.6)

> **For agentic workers:** this file is self-contained. Implement task p83.6 exactly as written, test first. Background, which you don't need to read: `15-phase8-tooling.md` §8.3, `16-implementation-notes.md` (it overrides older plan text) and the invariants I1–I8 in `01-design.md`, all in `docs/superpowers/plans/2026-11-09-arv-runtime/`.

**Goal:** Every request names the port it reaches (the URL's own, or the scheme's default), on the first request and on every redirect hop, so a port grant holds across redirects.

**Invariants touched:** I3 (every redirect re-checked). Guarantee: net effects reach only granted hosts and ports, including across redirects.

**Depends on:** p83.1–p83.5 committed on `phase/p83`. Check with `git log --oneline -12`.

**Where:** worktree `/Users/delavalom/delavalom-labs/arvlang-wt/p83`, branch `phase/p83` (branched from `feat/phases-3-8`). If the worktree is missing: `git -C /Users/delavalom/delavalom-labs/arvlang worktree add /Users/delavalom/delavalom-labs/arvlang-wt/p83 phase/p83`. Don't edit the main checkout.

**Rules:**
- Start every shell command with `cd /Users/delavalom/delavalom-labs/arvlang-wt/p83 &&`.
- Keep Go files under 15 KB and markdown under 18 KB. A pre-commit hook blocks any staged file over about 20 KB. Never skip or bypass git hooks.
- Before committing: `gofmt -l .` prints nothing, `go vet ./...` is clean, and `go test -race -count=1 ./...` is green. Use `-count=1`: the e2e suite builds the binary itself, so the test cache can't see source changes.
- Check `df -h /System/Volumes/Data` before long runs. If less than 1 GB is free, stop and report `blocked` (disk). The only thing you may delete is the Go build cache (`go clean -cache`).
- Don't weaken a test to make it pass. If a test exposes a real bug outside this task, mark it `t.Skip("BUG: ...")` with a reproduction and report it.
- `arv/runtime` stays stdlib-only. Never push or touch remotes.
- The machine is heavily loaded (load average above 300), so keep timing bounds generous.

**Validated:** every block below was compiled and tested in a scratch copy, with p83.1–p83.10 applied in order, on go1.26.2 darwin/arm64: `gofmt`, `go vet` (darwin, linux, windows) and `go test -race -count=1 ./...`. If an `old` block doesn't match exactly, an earlier task was done differently. Read the surrounding code and make the equivalent change; don't guess.

---

### Task p83.6: http_get and redirects check host and port

**Design.** `netRequest(u *url.URL)` in a new `arv/evaluator/netreq.go` builds `runtime.New(runtime.Net, net.JoinHostPort(u.Hostname(), port))`. `port` is `u.Port()`, or `80`/`443` for `http`/`https`; any other scheme is an error. `httpGet` and `clientFor`'s `CheckRedirect` both call it instead of `runtime.New(runtime.Net, u.Hostname())`.

**Expectations that change:** a denial now names `host:port`. The unit tests `TestHTTPGetNeedsNetCapability` and `TestHTTPGetChecksRedirectTargets` are updated. So are the e2e denial strings in `TestNet` (no grant / other host / redirect to an ungranted host) and in `TestNetHostForms` (the two userinfo cases and "denial names the normalised host"). This task creates `e2e/phase83_test.go` with the `srvPort()` helper and the port cases; p83.7 and p83.9 append to it.

Run e2e with `-count=1`: the suite builds the binary itself, so Go's test cache cannot see source changes.

**Files:**
- Create: `arv/evaluator/netreq.go`
- Modify: `arv/evaluator/effects.go` (`clientFor` comment and `CheckRedirect`; `httpGet`)
- Test: `arv/evaluator/netreq_test.go`; modify `arv/evaluator/effects_test.go`
- Test: create `e2e/phase83_test.go`; modify `e2e/phase1_test.go`, `e2e/hardening_test.go`

- [ ] **Step 1: Write the failing tests**

Create `arv/evaluator/netreq_test.go`:

```go
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
```

In `arv/evaluator/effects_test.go`, replace:

```go
	got, _ := runWith(t, src)
	wantDenied(t, got, `net("127.0.0.1")`)
```

with:

```go
	got, _ := runWith(t, src)
	wantDenied(t, got, `net("`+strings.TrimPrefix(srv.URL, "http://")+`")`) // host and port
```

In `arv/evaluator/effects_test.go`, replace:

```go
	wantDenied(t, got, `net("localhost")`)
}
```

with:

```go
	wantDenied(t, got, `net("`+strings.TrimPrefix(elsewhere, "http://")+`")`)
}
```

Create `e2e/phase83_test.go`:

```go
package e2e

import (
	"strings"
	"testing"
)

// Phase 8.3: network hardening (ports, CIDR grants, the dial-time address
// check). Every case runs against the local test server on 127.0.0.1.

// srvPort is the test server's port.
func srvPort() string { return srv.URL[strings.LastIndex(srv.URL, ":")+1:] }

// net=host:port restricts the grant to that port, on the first request and
// on every redirect hop.
func TestNetPorts(t *testing.T) {
	t.Parallel()
	port := srvPort()
	other := "1"
	if port == other {
		other = "2"
	}
	get := func(name, route string, allow []string, stdout string, hitsWant map[string]int) spec {
		s := spec{
			name: name, script: "http_get.arv", vars: map[string]string{"ROUTE": route},
			allow: allow, stdout: stdout,
			verify: func(t *testing.T) {
				for path, n := range hitsWant {
					if got := hits(t, path); got != n {
						t.Errorf("server saw %d requests for %s, want %d", got, path, n)
					}
				}
			},
		}
		if stdout == "" {
			s.code = 1
		}
		return s
	}
	deny := func(s spec, want string) spec {
		s.stderrHas = []string{`capability denied: net("` + want + `")`}
		return s
	}
	runAll(t, []spec{
		get("the granted port is allowed", "/ok", []string{"net=127.0.0.1:" + port},
			"200\n"+okBody+"\n", map[string]int{"/ok": 1}),
		deny(get("another port is denied", "/ok", []string{"net=127.0.0.1:" + other},
			"", map[string]int{"/ok": 0}), "127.0.0.1:"+port),
		get("a redirect to a granted host and port succeeds", "/redirect-out",
			[]string{"net=127.0.0.1:" + port, "net=localhost:" + port},
			"200\n"+targetBody+"\n", map[string]int{"/redirect-out": 1, "/redirect-target": 1}),
		deny(get("a redirect to an ungranted port is denied", "/redirect-out",
			[]string{"net=127.0.0.1:" + port, "net=localhost:" + other},
			"", map[string]int{"/redirect-out": 1, "/redirect-target": 0}), "localhost:"+port),
	})
}
```

In `e2e/phase1_test.go`, replace:

```go
	denied := func(s spec, host string) spec {
		s.stderrHas = append(s.stderrHas, `net("`+host+`")`)
		return s
	}
```

with:

```go
	denied := func(s spec, host string) spec {
		// Phase 8.3: a request names the port it reaches.
		s.stderrHas = append(s.stderrHas, `net("`+host+`:`+srvPort()+`")`)
		return s
	}
```

In `e2e/hardening_test.go`, replace:

```go
loopback, false, 0, "other.invalid"),
		get("userinfo with a port
```

with:

```go
loopback, false, 0, "other.invalid:80"),
		get("userinfo with a port
```

In `e2e/hardening_test.go`, replace:

```go
"@other.invalid:"+port+"/ok", loopback, false, 0, "other.invalid"),
```

with:

```go
"@other.invalid:"+port+"/ok", loopback, false, 0, "other.invalid:"+port),
```

In `e2e/hardening_test.go`, replace:

```go
"http://LOCALHOST.:"+port+"/ok", loopback, false, 0, "localhost"),
```

with:

```go
"http://LOCALHOST.:"+port+"/ok", loopback, false, 0, "localhost:"+port),
```

- [ ] **Step 2: Run them and watch them fail**

Run: `cd /Users/delavalom/delavalom-labs/arvlang-wt/p83 && go test -count=1 ./arv/evaluator/ ./e2e/`

Expected:
`arv/evaluator`: build failure, `undefined: netRequest`.
`e2e`: `TestNet/{no_grant_is_denied,grant_for_another_host_is_denied,redirect_to_an_ungranted_host_is_denied}`, `TestNetHostForms/{userinfo_cannot_pose_as_the_host,userinfo_with_a_port_cannot_pose_as_the_host,denial_names_the_normalised_host}` and all four `TestNetPorts` cases fail. The request is still portless, so a port grant never covers it.

- [ ] **Step 3: Implement**

Create `arv/evaluator/netreq.go`:

```go
package evaluator

import (
	"fmt"
	"net"
	"net/url"

	"github.com/delavalom/arvlang/arv/runtime"
)

// defaultPorts are the ports a URL without one reaches, by scheme.
var defaultPorts = map[string]string{"http": "80", "https": "443"}

// netRequest is the capability a request for u needs: its host and the
// port it will reach (the scheme's default when u names none), normalised
// by runtime.New so it compares with grants. http_get checks it before the
// first request and CheckRedirect before every redirect, so a port grant
// (net("host:443")) holds across redirects too.
func netRequest(u *url.URL) (runtime.Capability, error) {
	port := u.Port()
	if port == "" {
		var ok bool
		if port, ok = defaultPorts[u.Scheme]; !ok {
			return runtime.Capability{}, fmt.Errorf("unsupported URL scheme %q", u.Scheme)
		}
	}
	return runtime.New(runtime.Net, net.JoinHostPort(u.Hostname(), port))
}
```

In `arv/evaluator/effects.go`, replace:

```go
// clientFor returns an HTTP client that re-checks the task's net capability
// on every redirect, so a granted host cannot bounce the task elsewhere.
```

with:

```go
// clientFor returns an HTTP client that re-checks the task's net capability
// (host and port, see netRequest) on every redirect, so a granted host
// cannot bounce the task to another host or port.
```

In `arv/evaluator/effects.go`, replace:

```go
			want, err := runtime.New(runtime.Net, req.URL.Hostname())
```

with:

```go
			want, err := netRequest(req.URL)
```

In `arv/evaluator/effects.go`, replace:

```go
	want, err := runtime.New(runtime.Net, u.Hostname())
```

with:

```go
	want, err := netRequest(u)
```

- [ ] **Step 4: Run the checks and watch them pass**

```bash
cd /Users/delavalom/delavalom-labs/arvlang-wt/p83 && gofmt -l . && go vet ./... && GOOS=windows go vet ./... && go test -race -count=1 ./...
```

Expected: `gofmt` prints nothing, both vets are clean, and every package says `ok` (`arv/token` has no test files).

- [ ] **Step 5: Commit**

```bash
cd /Users/delavalom/delavalom-labs/arvlang-wt/p83 && git add arv/evaluator/effects.go arv/evaluator/effects_test.go arv/evaluator/netreq.go arv/evaluator/netreq_test.go e2e/hardening_test.go e2e/phase1_test.go e2e/phase83_test.go && git commit -F - <<'EOF'
evaluator: http_get and redirects check host and port

Every request names the port it reaches (the URL's, or the scheme's
default), so net("host:443") holds on the first request and on every
redirect. Denials now name host:port.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
EOF
```
