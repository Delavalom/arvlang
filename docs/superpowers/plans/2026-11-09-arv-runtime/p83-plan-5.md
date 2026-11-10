# Phase 8.3, part 5 of 10: net scopes accept ports (task p83.5)

> **For agentic workers:** this file is self-contained. Implement task p83.5 exactly as written, test first. Background, which you don't need to read: `15-phase8-tooling.md` §8.3, `16-implementation-notes.md` (it overrides older plan text) and the invariants I1–I8 in `01-design.md`, all in `docs/superpowers/plans/2026-11-09-arv-runtime/`.

**Goal:** `net("host:443")` restricts a grant to one port; a bare host keeps meaning any port. This task changes only `arv/runtime` (parsing and `Covers`); p83.6 makes requests carry ports.

**Invariants touched:** I2 (a portless request, which means every port, is covered only by a portless grant), I3.

**Depends on:** p83.1–p83.4 committed on `phase/p83`. Check with `git log --oneline -12`.

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

### Task p83.5: net scopes accept ports

**Design.** The net-scope code moves out of `capability.go` into a new `arv/runtime/netscope.go` (it grows, and `capability.go` must stay small):
- `netScope` (it replaces `netHost`) splits off an optional port with `splitPort`, normalises the host with `normHost` (the old `netHost` rules), and re-joins them: `host:port`, or `[v6]:port`.
- A port is decimal, 1–65535; leading zeros are dropped (`0080` becomes `80`).
- `netCovers(g, r)`: if the grant names a port, the request must name the same port. Then `hostCovers` applies the old host rules.
- Normalisation now also rejects `..` and `host..`. Dropping one trailing dot used to leave a name ending in `.`, which a second pass rejected (`FuzzNetScope` found this; the fuzz target lands in p83.8).

**Expectations that change** (p83.10 records the reasons in `16-implementation-notes.md`): ports used to be rejected. The unit tests listed `127.0.0.1:8080`, `localhost:80` and `[::1]:80` as invalid. They now list invalid *ports* instead. The e2e case `net=127.0.0.1:1234` ("a port is not allowed") becomes two bad-port cases plus one accepted-port case.

**Files:**
- Create: `arv/runtime/netscope.go`
- Modify: `arv/runtime/capability.go` (remove `netHost`, `isSpaceOrControl`, `isNumeric`; `Covers` delegates to `netCovers`; imports)
- Test: `arv/runtime/netscope_test.go`; modify `arv/runtime/capability_test.go`, `arv/runtime/attenuate_test.go`, `arv/arv_test.go`, `e2e/hardening_test.go`

- [ ] **Step 1: Write the failing tests**

Create `arv/runtime/netscope_test.go`:

```go
package runtime

import "testing"

func TestNetScopePorts(t *testing.T) {
	for in, want := range map[string]string{
		"API.Example.com:443": "api.example.com:443",
		"localhost.:8080":     "localhost:8080",
		"127.0.0.1:0080":      "127.0.0.1:80",
		"[::1]:443":           "[::1]:443",
		"[0:0::1]:443":        "[::1]:443",
		"*:443":               "*:443",
		"*.Example.com:443":   "*.example.com:443",
		"host:65535":          "host:65535",
	} {
		c, err := New(Net, in)
		if err != nil || c.Scope != want {
			t.Errorf("New(net, %q) = %q, %v; want %q", in, c.Scope, err, want)
		}
	}
	for _, in := range []string{
		"host:", ":80", "host:0", "host:65536", "host:+80", "host:-1", "host:8a",
		"host:80:81", "[::1]:", "[::1]80", "[::1]:0", "a:b:c", "*.0.0.1:80", "host :80",
		"..", "host..", "..:80", // found by FuzzNetScope: ".." normalised to "."
	} {
		if c, err := New(Net, in); err == nil {
			t.Errorf("New(net, %q) = %s, want an error", in, c)
		}
	}
}

// A grant without a port covers every port; a grant with one covers only
// requests for that port. A portless request stands for every port (as at
// spawn), so only a portless grant covers it (I2).
func TestNetPortCoverage(t *testing.T) {
	for _, c := range []struct {
		held, want string
		ok         bool
	}{
		{"example.com", "example.com:443", true},
		{"example.com", "example.com", true},
		{"example.com:443", "example.com:443", true},
		{"example.com:443", "example.com:80", false},
		{"example.com:443", "example.com", false},
		{"example.com:443", "other.com:443", false},
		{"*.example.com:443", "api.example.com:443", true},
		{"*.example.com:443", "api.example.com:8443", false},
		{"*:443", "anything.io:443", true},
		{"*:443", "anything.io:80", false},
		{"*", "anything.io:80", true},
		{"[::1]:8080", "::1", false},
		{"::1", "[::1]:8080", true},
		{"127.0.0.1:8080", "127.0.0.1:8080", true},
	} {
		if got := mustCap(t, Net, c.held).Covers(mustCap(t, Net, c.want)); got != c.ok {
			t.Errorf("net(%q) covers net(%q) = %v, want %v", c.held, c.want, got, c.ok)
		}
	}
}

// I2: a child cannot widen a port grant into every port by asking for the
// bare host at spawn.
func TestAttenuateKeepsPorts(t *testing.T) {
	parent := NewCapSet(mustCap(t, Net, "api.example.com:443"))
	if _, err := parent.Attenuate([]Capability{mustCap(t, Net, "api.example.com")}); err == nil {
		t.Error("a portless request was admitted from a port grant")
	}
	if _, err := parent.Attenuate([]Capability{mustCap(t, Net, "api.example.com:443")}); err != nil {
		t.Errorf("the same port was refused: %v", err)
	}
}
```

In `arv/runtime/capability_test.go`, replace:

```go
// Each of these would be accepted but never match a request (url.Hostname
// has no port, scheme or brackets), or would match far more than intended.
func TestNewRejectsBadNetScopes(t *testing.T) {
	for _, in := range []string{
		"", ".", "127.0.0.1:8080", "localhost:80", "[::1]:80", "https://host",
```

with:

```go
// Each of these would be accepted but never match a request (no scheme or
// path, and a port must be 1-65535), or would match far more than intended.
func TestNewRejectsBadNetScopes(t *testing.T) {
	for _, in := range []string{
		"", ".", "127.0.0.1:0", "localhost:65536", "[::1]:x", "https://host",
```

In `arv/runtime/attenuate_test.go`, replace:

```go
{"disk", "/"}, {Net, "host:80"}} {
```

with:

```go
{"disk", "/"}, {Net, "host:0"}} {
```

In `arv/arv_test.go`, replace:

```go
{Kind: runtime.Net, Scope: "127.0.0.1:8080"},
```

with:

```go
{Kind: runtime.Net, Scope: "127.0.0.1:0"},
```

In `e2e/hardening_test.go`, replace:

```go
		bad("net=127.0.0.1:1234", "a port is not allowed"),
```

with:

```go
		bad("net=127.0.0.1:0", "must be a number from 1 to 65535"),
		bad("net=127.0.0.1:65536", "must be a number from 1 to 65535"),
```

In `e2e/hardening_test.go`, replace:

```go
			stdout: "9\n7\n3\ntrue\n42\n42\nhttp://example.test\nbig\n",
		},
	})
}
```

with:

```go
			stdout: "9\n7\n3\ntrue\n42\n42\nhttp://example.test\nbig\n",
		},
		{
			// Changed in Phase 8.3: ports used to be rejected; net=host:port
			// now restricts the grant to that port.
			name: "a port is accepted", script: "basics.arv", allow: []string{"net=127.0.0.1:1234"},
			stdout: "9\n7\n3\ntrue\n42\n42\nhttp://example.test\nbig\n",
		},
	})
}
```

- [ ] **Step 2: Run them and watch them fail**

Run: `cd /Users/delavalom/delavalom-labs/arvlang-wt/p83 && go test -count=1 ./arv/runtime/ ./e2e/`

Expected:
`arv/runtime`: `TestNetScopePorts`, `TestNetPortCoverage` and `TestAttenuateKeepsPorts` fail. For example: `New(net, "[::1]:443") = "", net: host "[::1]:443": brackets must hold exactly an IPv6 address (no port)`, and `New(net, "..") = net("."), want an error`.
`e2e`: `TestNetGrantValidation/net=127.0.0.1:0`, `/net=127.0.0.1:65536` and `/a_port_is_accepted` fail.

- [ ] **Step 3: Implement**

Create `arv/runtime/netscope.go`:

```go
package runtime

import (
	"fmt"
	"net/netip"
	"strconv"
	"strings"
	"unicode"
)

// netScope normalises a net scope: a host pattern, optionally followed by
// ":port". The host part takes the form url.Hostname() gives for the same
// host, so a grant can match a request: lower case, one trailing dot
// dropped, IP literals in netip's canonical form. A port is a decimal
// number from 1 to 65535 (leading zeros dropped). An IPv6 address with a
// port is bracketed ("[::1]:443"); without one it is bare ("::1").
//
// A scope without a port covers every port; one with a port covers only
// that port. Requests (http_get and every redirect) always name a port,
// the scheme's default when the URL has none (see Covers).
//
// It rejects anything that could never match a host (a scheme, a path,
// userinfo, whitespace, an empty or out-of-range port) and wildcards other
// than "*" alone or a leading "*." on a DNS name.
func netScope(s string) (string, error) {
	if strings.ContainsAny(s, "/@?#") || strings.IndexFunc(s, isSpaceOrControl) >= 0 {
		return "", fmt.Errorf("host %q: want a bare host name, optionally with a port (no scheme, path, userinfo or spaces)", s)
	}
	host, port, err := splitPort(s)
	if err != nil {
		return "", err
	}
	h, err := normHost(host, s)
	if err != nil || port == "" {
		return h, err
	}
	if strings.Contains(h, ":") {
		return "[" + h + "]:" + port, nil
	}
	return h + ":" + port, nil
}

// splitPort splits "host:port" or "[v6]:port" into host and normalised
// port. A scope with no port, a bracketed IPv6 address without one, or a
// bare IPv6 address returns port "".
func splitPort(s string) (host, port string, err error) {
	switch {
	case strings.HasPrefix(s, "["):
		end := strings.Index(s, "]")
		if end < 0 || end == len(s)-1 {
			return s, "", nil // normHost checks the brackets
		}
		if s[end+1] != ':' {
			return "", "", fmt.Errorf("host %q: brackets must hold exactly an IPv6 address", s)
		}
		host, port = s[:end+1], s[end+2:]
	case strings.Count(s, ":") == 1:
		host, port, _ = strings.Cut(s, ":")
	default:
		return s, "", nil
	}
	n, err := strconv.ParseUint(port, 10, 16)
	if err != nil || n == 0 || strings.Trim(port, "0123456789") != "" {
		return "", "", fmt.Errorf("host %q: port %q must be a number from 1 to 65535", s, port)
	}
	return host, strconv.FormatUint(n, 10), nil
}

// normHost normalises the host part of scope s (see netScope).
func normHost(h, s string) (string, error) {
	if h == "*" {
		return h, nil
	}
	h = strings.ToLower(h)
	if strings.HasPrefix(h, "[") {
		inner, ok := strings.CutSuffix(h[1:], "]")
		if a, err := netip.ParseAddr(inner); ok && err == nil && a.Is6() {
			return a.String(), nil
		}
		return "", fmt.Errorf("host %q: brackets must hold exactly an IPv6 address", s)
	}
	if strings.Contains(h, ":") {
		if a, err := netip.ParseAddr(h); err == nil && a.Is6() {
			return a.String(), nil
		}
		return "", fmt.Errorf("host %q: not a host name, an IPv6 address or host:port", s)
	}
	h = strings.TrimSuffix(h, ".")
	if h == "" || strings.HasSuffix(h, ".") { // "..": dropping one dot must leave a name
		return "", fmt.Errorf("host %q: empty host", s)
	}
	if a, err := netip.ParseAddr(h); err == nil {
		return a.String(), nil
	}
	name, wild := strings.CutPrefix(h, "*.")
	if name == "" || strings.Contains(name, "*") {
		return "", fmt.Errorf("host %q: a wildcard must be \"*\" or a leading \"*.\" on a domain", s)
	}
	if wild && isNumeric(name[strings.LastIndex(name, ".")+1:]) {
		// No TLD is all digits, so this is (part of) an IP literal.
		return "", fmt.Errorf("host %q: wildcards cannot cover IP addresses", s)
	}
	return h, nil
}

// splitScope splits a scope already normalised by netScope into its host
// and port ("" for any port).
func splitScope(s string) (host, port string) {
	if strings.HasPrefix(s, "[") {
		if i := strings.LastIndex(s, "]:"); i > 0 {
			return s[1:i], s[i+2:]
		}
		return strings.Trim(s, "[]"), ""
	}
	if strings.Count(s, ":") == 1 {
		host, port, _ = strings.Cut(s, ":")
		return host, port
	}
	return s, ""
}

// netCovers reports whether net grant g covers request r, both normalised.
// A grant without a port covers every port; one with a port covers only a
// request for that port, so it never covers a portless request (which
// stands for every port, e.g. at spawn).
func netCovers(g, r string) bool {
	gh, gp := splitScope(g)
	rh, rp := splitScope(r)
	if gp != "" && gp != rp {
		return false
	}
	return hostCovers(gh, rh)
}

// hostCovers reports whether host pattern g covers host r.
func hostCovers(g, r string) bool {
	if g == "*" || g == r {
		return true
	}
	if strings.HasPrefix(g, "*.") {
		if _, err := netip.ParseAddr(r); err == nil {
			return false // DNS wildcards never cover IP literals
		}
		return strings.HasSuffix(r, g[1:]) // "*.x.com" covers "a.x.com", not "x.com"
	}
	return false
}

func isSpaceOrControl(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }

func isNumeric(s string) bool {
	return s != "" && strings.Trim(s, "0123456789") == ""
}
```

In `arv/runtime/capability.go`, delete everything from the line `// netHost normalises a net scope to the form url.Hostname() gives for the` up to, but not including, the line `// Covers reports whether holding c is enough to perform request r.`. That removes the old `netHost`, `isSpaceOrControl` and `isNumeric`, which move to `netscope.go`.

In `arv/runtime/capability.go`, replace:

```go
	case Net:
		if c.Scope == "*" || c.Scope == r.Scope {
			return true
		}
		if strings.HasPrefix(c.Scope, "*.") {
			if _, err := netip.ParseAddr(r.Scope); err == nil {
				return false // DNS wildcards never cover IP literals
			}
			return strings.HasSuffix(r.Scope, c.Scope[1:]) // "*.x.com" covers "a.x.com", not "x.com"
		}
		return false
```

with:

```go
	case Net:
		return netCovers(c.Scope, r.Scope)
```

In `arv/runtime/capability.go`, replace:

```go
	"fmt"
	"net/netip"
	"path/filepath"
	"strings"
	"unicode"
)
```

with:

```go
	"fmt"
	"path/filepath"
	"strings"
)
```

In `arv/runtime/capability.go`, replace:

```go
readlink); net scopes are normalised by netHost. Host grants
```

with:

```go
readlink); net scopes are normalised by netScope. Host grants
```

In `arv/runtime/capability.go`, replace:

```go
cleaned); net scopes are normalised by netHost, which is pure.
```

with:

```go
cleaned); net scopes are normalised by netScope, which is pure.
```

In `arv/runtime/capability.go`, replace:

```go
		host, err := netHost(scope)
		if err != nil {
			return Capability{}, fmt.Errorf("%s: %w", kind, err)
		}
		scope = host
```

with:

```go
		norm, err := netScope(scope)
		if err != nil {
			return Capability{}, fmt.Errorf("%s: %w", kind, err)
		}
		scope = norm
```

- [ ] **Step 4: Run the checks and watch them pass**

```bash
cd /Users/delavalom/delavalom-labs/arvlang-wt/p83 && gofmt -l . && go vet ./... && GOOS=windows go vet ./... && go test -race -count=1 ./...
```

Expected: `gofmt` prints nothing, both vets are clean, and every package says `ok` (`arv/token` has no test files).

- [ ] **Step 5: Commit**

```bash
cd /Users/delavalom/delavalom-labs/arvlang-wt/p83 && git add arv/arv_test.go arv/runtime/attenuate_test.go arv/runtime/capability.go arv/runtime/capability_test.go arv/runtime/netscope.go arv/runtime/netscope_test.go e2e/hardening_test.go && git commit -F - <<'EOF'
runtime: net scopes accept a port

net("host:443") restricts a grant to that port; a bare host still means
every port. A portless request (every port, as at spawn) is covered
only by a portless grant (I2). Net scope parsing moves to netscope.go,
and ".." no longer normalises to ".".

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
EOF
```
