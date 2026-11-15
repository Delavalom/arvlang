# Phase 8.3, part 7 of 10: CIDR grants and internal address classes (task p83.7)

> **For agentic workers:** this file is self-contained. Implement task p83.7 exactly as written, test first. Background, which you don't need to read: `15-phase8-tooling.md` §8.3, `16-implementation-notes.md` (it overrides older plan text) and the invariants I1–I8 in `01-design.md`, all in `docs/superpowers/plans/2026-11-09-arv-runtime/`.

**Goal:** Implement CIDR grants instead of deferring them. Classify internal addresses, and stop `*` from covering internal IP literals, so attenuation can never turn a name grant into an internal-address grant (I2).

**Invariants touched:** I2, I3. Guarantee: scope parsing rejects malformed scopes.

**Depends on:** p83.1–p83.6 committed on `phase/p83`. Check with `git log --oneline -12`.

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

### Task p83.7: CIDR grants and internal address classes

**Design.**
- `netScope` accepts a CIDR range first (`netip.ParsePrefix`) and stores `p.String()`. A range with bits set past the prefix (`127.0.0.1/8`) is rejected, and the error suggests the masked form. A CIDR range takes no port.
- `hostCovers`: a CIDR grant covers the addresses in it and narrower ranges. An IP address is covered by the same address, by a range holding it, and by `*` **only if it is public**. No other pattern covers a CIDR range, so `*` covers no range. Reason: an IP or CIDR grant will let a task dial an internal address (p83.8 and p83.9). If `*` covered `127.0.0.1`, a child could attenuate `*` into `net("127.0.0.1")` and gain an internal dial its parent lacked.
- `AddrClass(a)` (new file `arv/runtime/netaddr.go`) names the internal class, or returns `""` for a public address. Internal covers netip's unspecified, loopback, private (with ULA, which holds AWS's fd00:ec2::254), link-local (with 169.254.169.254) and multicast, plus 0/8, 100.64/10 (CGNAT), 192.0.0/24, 198.18/15, 240/4, 64:ff9b:1::/48, 2001::/32 (Teredo) and fec0::/10. IPv4-mapped addresses are unmapped first; NAT64 (64:ff9b::/96) and 6to4 (2002::/16) are classed by the IPv4 address they embed.

**Expectation that changes:** e2e `TestNet` "wildcard grant allows any host" fetched `http://127.0.0.1:PORT` with `net=*`. Now `*` does not cover that internal IP literal. The case becomes "wildcard grant allows any host name": it fetches `http://localhost:PORT` with `net=*` and `net=127.0.0.1`. That still exercises `*`, since only `*` covers the name `localhost`. The IP grant is needed once p83.9 checks the dial.

**Files:**
- Create: `arv/runtime/netaddr.go`
- Modify: `arv/runtime/netscope.go` (CIDR in `netScope`, doc, `hostCovers`)
- Test: `arv/runtime/netaddr_test.go`
- Test: append to `e2e/phase83_test.go`; modify `e2e/phase1_test.go`

- [ ] **Step 1: Write the failing tests**

Create `arv/runtime/netaddr_test.go`:

```go
package runtime

import (
	"net/netip"
	"testing"
)

func TestAddrClass(t *testing.T) {
	for in, want := range map[string]string{
		"8.8.8.8": "", "2606:4700::1111": "", "64:ff9b::808:808": "", "2002:808:808::1": "",
		"127.0.0.1": "loopback", "127.1.2.3": "loopback", "::1": "loopback",
		"::ffff:127.0.0.1": "loopback", "64:ff9b::7f00:1": "loopback", "2002:7f00:1::": "loopback",
		"10.1.2.3": "private", "172.16.0.1": "private", "192.168.1.1": "private",
		"fd00:ec2::254": "private", "169.254.169.254": "link-local", "fe80::1": "link-local",
		"0.0.0.0": "unspecified", "::": "unspecified", "0.1.2.3": "this-network",
		"224.0.0.1": "multicast", "ff02::1": "multicast", "100.100.100.200": "shared (CGNAT)",
		"192.0.0.192": "IETF protocol", "198.18.0.1": "benchmarking",
		"255.255.255.255": "reserved", "fec0::1": "site-local", "2001::1": "Teredo",
	} {
		if got := AddrClass(netip.MustParseAddr(in)); got != want {
			t.Errorf("AddrClass(%s) = %q, want %q", in, got, want)
		}
	}
}

func TestNetScopeCIDR(t *testing.T) {
	for in, want := range map[string]string{
		"10.0.0.0/8": "10.0.0.0/8", "FD00::/8": "fd00::/8", "0.0.0.0/0": "0.0.0.0/0",
		"10.1.2.3/32": "10.1.2.3/32",
	} {
		c, err := New(Net, in)
		if err != nil || c.Scope != want {
			t.Errorf("New(net, %q) = %q, %v; want %q", in, c.Scope, err, want)
		}
	}
	for _, in := range []string{"10.0.0.1/8", "10.0.0.0/33", "10.0.0.0/8:443", "[fd00::]/8", "host/8", "10.0.0.0/ 8"} {
		if c, err := New(Net, in); err == nil {
			t.Errorf("New(net, %q) = %s, want an error", in, c)
		}
	}
}

func TestNetCoverageCIDRAndInternal(t *testing.T) {
	for _, c := range []struct {
		held, want string
		ok         bool
	}{
		{"10.0.0.0/8", "10.1.2.3:443", true},
		{"10.0.0.0/8", "10.1.2.3", true},
		{"10.0.0.0/8", "10.1.0.0/16", true},
		{"10.1.0.0/16", "10.0.0.0/8", false},
		{"10.0.0.0/8", "11.0.0.1:80", false},
		{"10.0.0.0/8", "ten.example:80", false},
		{"fd00::/8", "[fd00::1]:80", true},
		{"*", "8.8.8.8:443", true},
		{"*", "127.0.0.1:80", false},
		{"*", "10.0.0.0/8", false},
		{"*", "8.8.8.0/24", false},
		{"*:443", "169.254.169.254:443", false},
		{"127.0.0.1", "127.0.0.1:8080", true},
		{"localhost", "127.0.0.1:80", false},
	} {
		if got := mustCap(t, Net, c.held).Covers(mustCap(t, Net, c.want)); got != c.ok {
			t.Errorf("net(%q) covers net(%q) = %v, want %v", c.held, c.want, got, c.ok)
		}
	}
}
```

In `e2e/phase1_test.go`, replace:

```go
		get("wildcard grant allows any host", "/ok", []string{"net=*"}, "200\n"+okBody+"\n"),
```

with:

```go
		{
			// Changed in Phase 8.3: "*" covers every host name but no internal
			// IP address, so the request goes through the name "localhost",
			// and net=127.0.0.1 lets the dial reach the loopback server.
			name: "wildcard grant allows any host name", script: "http_get_url.arv",
			vars:  map[string]string{"URL": "http://localhost:" + srvPort() + "/ok"},
			allow: []string{"net=*", "net=127.0.0.1"}, stdout: "200\n" + okBody + "\n",
		},
```

Append to the end of `e2e/phase83_test.go`:

```go
// A CIDR grant covers the IP addresses in it; "*" covers every host name
// but no internal address, so it cannot reach the loopback server.
func TestNetCIDRAndWildcard(t *testing.T) {
	t.Parallel()
	port := srvPort()
	url := "http://127.0.0.1:" + port + "/ok"
	get := func(name string, allow []string, ok bool) spec {
		s := spec{name: name, script: "http_get_url.arv", vars: map[string]string{"URL": url}, allow: allow}
		if ok {
			s.stdout = "200\n" + okBody + "\n"
		} else {
			s.code = 1
			s.stderrHas = []string{`capability denied: net("127.0.0.1:` + port + `")`}
			s.verify = func(t *testing.T) {
				if n := hits(t, "/ok"); n != 0 {
					t.Errorf("server saw %d requests for /ok, want 0", n)
				}
			}
		}
		return s
	}
	runAll(t, []spec{
		get("a CIDR grant covers an address in it", []string{"net=127.0.0.0/8"}, true),
		get("a CIDR grant does not cover an address outside it", []string{"net=10.0.0.0/8"}, false),
		get("the wildcard does not cover a loopback address", []string{"net=*"}, false),
		{
			name: "a CIDR grant with bits past its prefix is rejected", script: "basics.arv",
			allow: []string{"net=127.0.0.1/8"}, code: 2, stderrHas: []string{"did you mean 127.0.0.0/8"},
		},
	})
}
```

- [ ] **Step 2: Run them and watch them fail**

Run: `cd /Users/delavalom/delavalom-labs/arvlang-wt/p83 && go test -count=1 ./arv/runtime/ ./e2e/`

Expected:
`arv/runtime`: build failure, `undefined: AddrClass`.
`e2e`: all four `TestNetCIDRAndWildcard` cases fail. CIDR scopes are still rejected by the `/` check, and `*` still covers 127.0.0.1.

- [ ] **Step 3: Implement**

Create `arv/runtime/netaddr.go`:

```go
package runtime

import "net/netip"

// reserved lists internal ranges that netip's predicates do not cover.
var reserved = []struct {
	prefix netip.Prefix
	class  string
}{
	{netip.MustParsePrefix("0.0.0.0/8"), "this-network"}, // 0.0.0.0 reaches localhost on Linux
	{netip.MustParsePrefix("100.64.0.0/10"), "shared (CGNAT)"},
	{netip.MustParsePrefix("192.0.0.0/24"), "IETF protocol"},
	{netip.MustParsePrefix("198.18.0.0/15"), "benchmarking"},
	{netip.MustParsePrefix("240.0.0.0/4"), "reserved"}, // includes 255.255.255.255
	{netip.MustParsePrefix("64:ff9b:1::/48"), "local NAT64"},
	{netip.MustParsePrefix("2001::/32"), "Teredo"},
	{netip.MustParsePrefix("fec0::/10"), "site-local"},
}

var (
	nat64     = netip.MustParsePrefix("64:ff9b::/96") // IPv4 in the last 4 bytes
	sixToFour = netip.MustParsePrefix("2002::/16")    // IPv4 in bytes 2-5
)

// AddrClass names the kind of internal address a is, or returns "" if a
// is public. Internal means unspecified, loopback, private (RFC 1918 and
// IPv6 ULA, which holds AWS's fd00:ec2::254), link-local (which holds the
// 169.254.169.254 metadata service), multicast, or one of the reserved
// ranges above. IPv4-mapped IPv6 addresses are classed as their IPv4 form,
// and NAT64 and 6to4 addresses by the IPv4 address they embed.
func AddrClass(a netip.Addr) string {
	a = a.Unmap()
	switch {
	case !a.IsValid():
		return "invalid"
	case a.IsUnspecified():
		return "unspecified"
	case a.IsLoopback():
		return "loopback"
	case a.IsPrivate():
		return "private"
	case a.IsLinkLocalUnicast():
		return "link-local"
	case a.IsMulticast():
		return "multicast"
	}
	if a.Is6() {
		b := a.As16()
		switch {
		case nat64.Contains(a):
			return AddrClass(netip.AddrFrom4([4]byte(b[12:16])))
		case sixToFour.Contains(a):
			return AddrClass(netip.AddrFrom4([4]byte(b[2:6])))
		}
	}
	for _, r := range reserved {
		if r.prefix.Contains(a) {
			return r.class
		}
	}
	return ""
}
```

In `arv/runtime/netscope.go`, replace:

```go
func netScope(s string) (string, error) {
	if strings.ContainsAny(s, "/@?#")
```

with:

```go
func netScope(s string) (string, error) {
	if p, err := netip.ParsePrefix(s); err == nil {
		if p != p.Masked() {
			return "", fmt.Errorf("host %q: a CIDR range must not set bits past its prefix (did you mean %s?)", s, p.Masked())
		}
		return p.String(), nil // a CIDR range takes no port
	}
	if strings.ContainsAny(s, "/@?#")
```

In `arv/runtime/netscope.go`, replace:

```go
// It rejects anything that could never match a host (a scheme, a path,
```

with:

```go
// A CIDR range ("10.0.0.0/8", "fd00::/8") covers the IP addresses in it,
// on every port. Together with IP literals, it is the only kind of grant
// that reaches an internal address (see AddrClass and Task.RequireDial).
//
// It rejects anything that could never match a host (a scheme, a path,
```

In `arv/runtime/netscope.go`, replace:

```go
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
```

with:

```go
// hostCovers reports whether host pattern g covers host r. A CIDR grant
// covers the addresses and the narrower ranges inside it. An IP address is
// covered by the same address, by a CIDR range holding it, and by "*" only
// if it is public: "*" grants every host name, but no internal address, so
// a child cannot attenuate "*" into an IP grant that reaches one (I2). No
// other pattern covers a CIDR range.
func hostCovers(g, r string) bool {
	if p, err := netip.ParsePrefix(g); err == nil {
		if a, err := netip.ParseAddr(r); err == nil {
			return p.Contains(a)
		}
		q, err := netip.ParsePrefix(r)
		return err == nil && q.Bits() >= p.Bits() && p.Contains(q.Addr())
	}
	if a, err := netip.ParseAddr(r); err == nil {
		return g == r || (g == "*" && AddrClass(a) == "")
	}
	if strings.Contains(r, "/") {
		return false
	}
	if g == "*" || g == r {
		return true
	}
	if strings.HasPrefix(g, "*.") {
		return strings.HasSuffix(r, g[1:]) // "*.x.com" covers "a.x.com", not "x.com"
	}
	return false
}
```

- [ ] **Step 4: Run the checks and watch them pass**

```bash
cd /Users/delavalom/delavalom-labs/arvlang-wt/p83 && gofmt -l . && go vet ./... && GOOS=windows go vet ./... && go test -race -count=1 ./...
```

Expected: `gofmt` prints nothing, both vets are clean, and every package says `ok` (`arv/token` has no test files).

- [ ] **Step 5: Commit**

```bash
cd /Users/delavalom/delavalom-labs/arvlang-wt/p83 && git add arv/runtime/netaddr.go arv/runtime/netaddr_test.go arv/runtime/netscope.go e2e/phase1_test.go e2e/phase83_test.go && git commit -F - <<'EOF'
runtime: CIDR grants and internal address classes

net=10.0.0.0/8 covers the addresses in it. AddrClass names internal
addresses (loopback, private, link-local and metadata, CGNAT, ...).
"*" covers every host name but no internal IP literal and no CIDR
range, so attenuation cannot turn it into an internal-address grant.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
EOF
```
