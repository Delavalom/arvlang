# Phase 8.3, part 9 of 10: check every dialled address (SSRF / DNS rebinding) (task p83.9)

> **For agentic workers:** this file is self-contained. Implement task p83.9 exactly as written, test first. Background, which you don't need to read: `15-phase8-tooling.md` §8.3, `16-implementation-notes.md` (it overrides older plan text) and the invariants I1–I8 in `01-design.md`, all in `docs/superpowers/plans/2026-11-09-arv-runtime/`.

**Goal:** Refuse at dial time any connection to an internal address unless an IP or CIDR grant covers it, so a granted name that resolves, or re-resolves, to loopback, private or metadata addresses is refused. Leave no connection goroutine behind.

**Invariants touched:** I1, I3, leak rule. Guarantee: a hostname that resolves (or re-resolves) to a disallowed address is refused at dial time.

**Depends on:** p83.1–p83.8 committed on `phase/p83`. Check with `git log --oneline -12`.

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

### Task p83.9: check every dialled address (SSRF / DNS rebinding)

**Design.** `transportFor(t)` (added to `arv/evaluator/netreq.go`) returns a fresh `*http.Transport` for each `http_get`:
- `DialContext` builds a `net.Dialer` whose `Control` hook parses the address actually being connected to and calls `t.RequireDial(addr, ap)` (p83.8). `Control` runs after DNS resolution, once for each address tried, Happy Eyeballs fallbacks included. So there is no resolve-then-check step for a rebinding answer to race. This deviates from the design file, which says to resolve in a custom `DialContext`: `Control` covers every attempt by construction.
- `DisableKeepAlives: true`, and `httpGet` defers `client.CloseIdleConnections()`. No connection goroutine outlives the call, which is the leak rule. A shared, pooled transport would also let a connection checked against one task's grants serve another task.
- No `Proxy`: through a proxy, the check would see the proxy's address.
- The transport is built by hand, not by cloning `http.DefaultTransport`, because a host program may have replaced that.

**The explicit grant path for loopback** (and for the e2e suite) is an IP grant: `net=127.0.0.1` or `net=127.0.0.1:PORT`. Most e2e cases already request `http://127.0.0.1:PORT` under `net=127.0.0.1`. The cases that request `localhost` now pair `net=localhost` (for the name check) with `net=127.0.0.1` (for the dial).

**Acceptance (deviation from 15-phase8 section 8.3):** the design asks for a `rebind.test` name served by a local DNS stub. The binary uses the system resolver, and the stdlib has no DNS server. `localhost`, which resolves to loopback through `/etc/hosts`, is the hermetic stand-in. The check runs on the connected address, so it is resolution-agnostic.

**Expectations that change:** e2e `TestNetHostForms` "uppercase request host", "trailing-dot request host" and "uppercase trailing-dot grant" used `net=localhost` alone. They now add `net=127.0.0.1` through a `named()` helper. The IP grant cannot match the request host, so these cases still test name normalisation.

**Files:**
- Modify: `arv/evaluator/netreq.go` (imports, `transportFor`), `arv/evaluator/effects.go` (`clientFor`, `httpGet`)
- Test: `arv/evaluator/netdial_test.go`
- Test: append to `e2e/phase83_test.go`; modify `e2e/hardening_test.go`

- [ ] **Step 1: Write the failing tests**

Create `arv/evaluator/netdial_test.go`:

```go
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
```

In `e2e/hardening_test.go`, replace:

```go
	loopback := []string{"net=127.0.0.1"}
	runAll(t, []spec{
		get("userinfo cannot pose as the host",
```

with:

```go
	loopback := []string{"net=127.0.0.1"}
	// Changed in Phase 8.3: "localhost" resolves to a loopback address, which
	// only an IP grant may dial, so each name grant is paired with net=127.0.0.1.
	// That grant cannot match the request host, so the name rules are still
	// what these cases test.
	named := func(grant string) []string { return []string{grant, "net=127.0.0.1"} }
	runAll(t, []spec{
		get("userinfo cannot pose as the host",
```

In `e2e/hardening_test.go`, replace:

```go
"http://LOCALHOST:"+port+"/ok", []string{"net=localhost"}, true
```

with:

```go
"http://LOCALHOST:"+port+"/ok", named("net=localhost"), true
```

In `e2e/hardening_test.go`, replace:

```go
"http://localhost.:"+port+"/ok", []string{"net=localhost"}, true
```

with:

```go
"http://localhost.:"+port+"/ok", named("net=localhost"), true
```

In `e2e/hardening_test.go`, replace:

```go
"http://localhost:"+port+"/ok", []string{"net=LocalHost."}, true
```

with:

```go
"http://localhost:"+port+"/ok", named("net=LocalHost."), true
```

Append to the end of `e2e/phase83_test.go`:

```go
// SSRF / DNS rebinding (acceptance for 8.3): a granted name that resolves to
// an internal address is refused at the dial. "localhost" stands in for a
// rebinding name: the system resolver maps it to loopback from /etc/hosts,
// so no DNS stub is needed. An IP grant for the address is the explicit
// path that lets the e2e suite reach its 127.0.0.1 server.
func TestNetDialChecksResolvedAddress(t *testing.T) {
	t.Parallel()
	port := srvPort()
	get := func(name string, allow []string) spec {
		return spec{
			name: name, script: "http_get_url.arv",
			vars:  map[string]string{"URL": "http://localhost:" + port + "/ok"},
			allow: allow,
		}
	}
	refused := get("a name that resolves to loopback is refused", []string{"net=localhost"})
	refused.code = 1
	refused.stderrHas = []string{"capability denied: net(", "localhost:" + port + " reached a loopback address"}
	refused.verify = func(t *testing.T) {
		if n := hits(t, "/ok"); n != 0 {
			t.Errorf("server saw %d requests for /ok, want 0", n)
		}
	}
	allowed := get("an IP grant lets the dial reach it", []string{"net=localhost", "net=127.0.0.1"})
	allowed.stdout = "200\n" + okBody + "\n"
	portOnly := get("an IP grant for another port does not", []string{"net=localhost", "net=127.0.0.1:1"})
	portOnly.code = 1
	portOnly.stderrHas = []string{"reached a loopback address"}
	runAll(t, []spec{refused, allowed, portOnly})
}
```

- [ ] **Step 2: Run them and watch them fail**

Run: `cd /Users/delavalom/delavalom-labs/arvlang-wt/p83 && go test -count=1 ./arv/evaluator/ ./e2e/`

Expected:
`arv/evaluator`: `TestHTTPGetRefusesANameThatResolvesToLoopback`, `TestHTTPGetRedirectDialIsChecked` and `TestHTTPGetLeavesNoGoroutines` fail. The leak test waits about 30 s, then reports `1 client connections left`.
`e2e`: `TestNetDialChecksResolvedAddress/{a_name_that_resolves_to_loopback_is_refused,an_IP_grant_for_another_port_does_not}` fail.

- [ ] **Step 3: Implement**

In `arv/evaluator/netreq.go`, replace:

```go
import (
	"fmt"
	"net"
	"net/url"

	"github.com/delavalom/arvlang/arv/runtime"
)
```

with:

```go
import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"syscall"
	"time"

	"github.com/delavalom/arvlang/arv/runtime"
)
```

Append to the end of `arv/evaluator/netreq.go`:

```go
// transportFor returns a transport for one http_get by task t. Every
// connection it opens is checked after DNS resolution, on the address it
// actually reaches (net.Dialer.Control runs once per address tried): an
// internal address needs an IP or CIDR grant (runtime.Task.RequireDial),
// so a granted name that resolves, or re-resolves, to one is refused
// (SSRF, DNS rebinding). There is no separate resolve-then-check step for
// a rebinding answer to race.
//
// It is never shared: a pooled connection checked against one task's
// grants must not serve another task. Keep-alives are off, so no idle
// connection (and its goroutines) outlives the call, and no proxy is used,
// since the dial would then reach the proxy and the check would see the
// proxy's address instead of the target's.
func transportFor(t *runtime.Task) *http.Transport {
	return &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			d := net.Dialer{
				Timeout: 30 * time.Second,
				Control: func(_, address string, _ syscall.RawConn) error {
					ap, err := netip.ParseAddrPort(address)
					if err != nil {
						return fmt.Errorf("dial %s: unexpected address %q: %w", addr, address, err)
					}
					return t.RequireDial(addr, ap)
				},
			}
			return d.DialContext(ctx, network, addr)
		},
		DisableKeepAlives:     true,
		ForceAttemptHTTP2:     true,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
}
```

In `arv/evaluator/effects.go`, replace:

```go
// cannot bounce the task to another host or port.
func clientFor(t *runtime.Task) *http.Client {
	return &http.Client{
		Timeout: 30 * time.Second,
```

with:

```go
// cannot bounce the task to another host or port, and checks every address
// it connects to (transportFor). The caller must CloseIdleConnections.
func clientFor(t *runtime.Task) *http.Client {
	return &http.Client{
		Transport: transportFor(t),
		Timeout:   30 * time.Second,
```

In `arv/evaluator/effects.go`, replace:

```go
	resp, err := clientFor(t).Do(req)
```

with:

```go
	client := clientFor(t)
	defer client.CloseIdleConnections()
	resp, err := client.Do(req)
```

- [ ] **Step 4: Run the checks and watch them pass**

```bash
cd /Users/delavalom/delavalom-labs/arvlang-wt/p83 && gofmt -l . && go vet ./... && GOOS=windows go vet ./... && go test -race -count=1 ./...
```

Expected: `gofmt` prints nothing, both vets are clean, and every package says `ok` (`arv/token` has no test files).

- [ ] **Step 5: Commit**

```bash
cd /Users/delavalom/delavalom-labs/arvlang-wt/p83 && git add arv/evaluator/effects.go arv/evaluator/netdial_test.go arv/evaluator/netreq.go e2e/hardening_test.go e2e/phase83_test.go && git commit -F - <<'EOF'
evaluator: check every dialled address (SSRF, DNS rebinding)

Each http_get gets its own transport whose dialer checks, after DNS,
the address it actually connects to: an internal address needs an IP
or CIDR grant. Keep-alives are off and no proxy is used, so no
connection outlives the call or is shared between tasks. e2e cases
that request localhost now also grant net=127.0.0.1 for the dial.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
EOF
```
