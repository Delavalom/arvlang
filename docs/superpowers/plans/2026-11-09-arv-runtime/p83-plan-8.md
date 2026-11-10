# Phase 8.3, part 8 of 10: dial-time address policy in the runtime (task p83.8)

> **For agentic workers:** this file is self-contained. Implement task p83.8 exactly as written, test first. Background, which you don't need to read: `15-phase8-tooling.md` §8.3, `16-implementation-notes.md` (it overrides older plan text) and the invariants I1–I8 in `01-design.md`, all in `docs/superpowers/plans/2026-11-09-arv-runtime/`.

**Goal:** Add the pure policy for the dial-time check (`CapSet.AllowsDial`, `Task.RequireDial`), which p83.9 wires into the HTTP transport. Prove I2 for net grants with an exhaustive property test, and fuzz net scope parsing.

**Invariants touched:** I2 (property-tested), I3. Guarantees: attenuation never grants more; scope parsing rejects malformed scopes (fuzzed).

**Depends on:** p83.1–p83.7 committed on `phase/p83`. Check with `git log --oneline -12`.

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

### Task p83.8: dial-time address policy in the runtime

**Design** (new file `arv/runtime/dial.go`):
- `CapSet.AllowsDial(addr)`: a public address is always allowed, because the request's name was already checked. An internal one (`AddrClass`, p83.7) needs `s.Allows(net("ip:port"))`, and only IP and CIDR grants can satisfy that. The zone is dropped and IPv4-mapped addresses are unmapped first.
- `Task.RequireDial(dialed, addr)` returns a `*DeniedError` with a new `Note` field, which `Error()` prints after the task. Example: `capability denied: net("127.0.0.1:8080") in task root: localhost:8080 reached a loopback address, which only an IP or CIDR grant covers (holds: [net("localhost")])`. Reusing `DeniedError` means `http_get`'s existing `errors.As(err, &denied)` path reports it unchanged.
- `TestNetAttenuationNeverGrantsMore` takes a vocabulary of scope shapes (names, wildcards, public and internal IPs, CIDR ranges, with and without ports). For every pair where `g.Covers(c)`, as `Attenuate` checks, it asserts that every request `c` covers and every address `c` may dial is also allowed by `g`. While planning, the mutation "`*` covers internal IPs" made it fail.
- `FuzzNetScope` checks that normalisation is idempotent, that `Covers` is reflexive, and that `Covers` never panics on hand-built scopes.

**Files:**
- Create: `arv/runtime/dial.go`
- Modify: `arv/runtime/capability.go` (`DeniedError.Note`)
- Test: `arv/runtime/dial_test.go`, `arv/runtime/netprop_test.go`

- [ ] **Step 1: Write the failing tests**

Create `arv/runtime/dial_test.go`:

```go
package runtime

import (
	"errors"
	"net/netip"
	"strings"
	"testing"
)

func TestAllowsDial(t *testing.T) {
	set := func(scopes ...string) CapSet {
		var caps []Capability
		for _, s := range scopes {
			caps = append(caps, mustCap(t, Net, s))
		}
		return NewCapSet(caps...)
	}
	for _, c := range []struct {
		held []string
		addr string
		ok   bool
	}{
		{[]string{"example.com"}, "93.184.215.14:443", true},
		{[]string{"localhost"}, "127.0.0.1:80", false},
		{[]string{"*"}, "[::1]:80", false},
		{[]string{"*"}, "169.254.169.254:80", false},
		{[]string{"*"}, "[::ffff:127.0.0.1]:80", false},
		{[]string{"localhost", "127.0.0.1"}, "127.0.0.1:80", true},
		{[]string{"127.0.0.1:8080"}, "127.0.0.1:8080", true},
		{[]string{"127.0.0.1:8080"}, "127.0.0.1:9090", false},
		{[]string{"10.0.0.0/8"}, "10.9.9.9:5432", true},
		{[]string{"10.0.0.0/8"}, "[::ffff:10.9.9.9]:5432", true},
		{[]string{"::1"}, "[::1%lo0]:80", true},
	} {
		if got := set(c.held...).AllowsDial(netip.MustParseAddrPort(c.addr)); got != c.ok {
			t.Errorf("%v AllowsDial(%s) = %v, want %v", c.held, c.addr, got, c.ok)
		}
	}
}

func TestRequireDialNamesTheGrantItNeeds(t *testing.T) {
	task := &Task{ID: "root", Caps: NewCapSet(mustCap(t, Net, "localhost"))}
	err := task.RequireDial("localhost:8080", netip.MustParseAddrPort("127.0.0.1:8080"))
	var denied *DeniedError
	if !errors.As(err, &denied) {
		t.Fatalf("got %v, want *DeniedError", err)
	}
	for _, frag := range []string{`net("127.0.0.1:8080")`, "in task root", "localhost:8080 reached a loopback address", `holds: [net("localhost")]`} {
		if !strings.Contains(err.Error(), frag) {
			t.Errorf("error %q lacks %q", err, frag)
		}
	}
	if err := task.RequireDial("example.com:443", netip.MustParseAddrPort("93.184.215.14:443")); err != nil {
		t.Errorf("public address refused: %v", err)
	}
}
```

Create `arv/runtime/netprop_test.go`:

```go
package runtime

import (
	"net/netip"
	"strings"
	"testing"
)

// netVocab is every net scope shape: names, wildcards, IPs (public and
// internal), CIDR ranges, each with and without a port where allowed.
func netVocab(t *testing.T) []Capability {
	hosts := []string{"*", "*.example.com", "example.com", "api.example.com", "localhost",
		"127.0.0.1", "10.1.2.3", "8.8.8.8", "::1", "fd00::1"}
	cidrs := []string{"10.0.0.0/8", "10.1.0.0/16", "0.0.0.0/0", "::/0", "fd00::/8", "8.8.8.0/24"}
	var caps []Capability
	for _, h := range hosts {
		for _, port := range []string{"", "443", "8080"} {
			s := h
			if port != "" {
				if strings.Contains(h, ":") {
					s = "[" + h + "]"
				}
				s += ":" + port
			}
			caps = append(caps, mustCap(t, Net, s))
		}
	}
	for _, c := range cidrs {
		caps = append(caps, mustCap(t, Net, c))
	}
	return caps
}

// I2, property form: whenever a parent grant g admits a child grant c
// (g.Covers(c), as Attenuate checks), every request c covers is covered by
// g, and every address c may dial, g may dial too. The vocabulary is small
// enough to check every pair exhaustively.
func TestNetAttenuationNeverGrantsMore(t *testing.T) {
	vocab := netVocab(t)
	var probes []Capability
	var dials []netip.AddrPort
	for _, h := range []string{"example.com", "api.example.com", "localhost", "127.0.0.1",
		"10.1.2.3", "10.200.0.1", "8.8.8.8", "8.8.8.9", "[::1]", "[fd00::1]", "169.254.169.254"} {
		for _, port := range []string{"80", "443", "8080"} {
			probes = append(probes, mustCap(t, Net, h+":"+port))
		}
	}
	for _, a := range []string{"127.0.0.1", "10.1.2.3", "10.200.0.1", "8.8.8.8", "::1", "fd00::1", "169.254.169.254"} {
		for _, port := range []uint16{80, 443, 8080} {
			dials = append(dials, netip.AddrPortFrom(netip.MustParseAddr(a), port))
		}
	}
	for _, g := range vocab {
		if !g.Covers(g) {
			t.Errorf("%s does not cover itself", g)
		}
		for _, c := range vocab {
			if !g.Covers(c) {
				continue
			}
			for _, p := range probes {
				if c.Covers(p) && !g.Covers(p) {
					t.Errorf("child %s of %s covers %s, which the parent does not", c, g, p)
				}
			}
			for _, d := range dials {
				if NewCapSet(c).AllowsDial(d) && !NewCapSet(g).AllowsDial(d) {
					t.Errorf("child %s of %s may dial %s, which the parent may not", c, g, d)
				}
			}
		}
	}
}

// FuzzNetScope: normalising is idempotent, a valid scope covers itself,
// and Covers never panics on hand-built scopes.
func FuzzNetScope(f *testing.F) {
	for _, s := range []string{"api.example.com", "*.example.com:443", "[::1]:80", "::1",
		"10.0.0.0/8", "fd00::/8", "*", "*:443", "127.0.0.1:8080", "host:", "[::1]x", "a:b:c", "..", "host..:80"} {
		f.Add(s, "example.com:443")
	}
	f.Fuzz(func(t *testing.T, s, other string) {
		(Capability{Kind: Net, Scope: s}).Covers(Capability{Kind: Net, Scope: other})
		c, err := New(Net, s)
		if err != nil {
			return
		}
		again, err := New(Net, c.Scope)
		if err != nil || again != c {
			t.Fatalf("New(net, %q) = %s, but New of that = %s, %v", s, c, again, err)
		}
		if !c.Covers(c) {
			t.Fatalf("%s does not cover itself", c)
		}
	})
}
```

- [ ] **Step 2: Run them and watch them fail**

Run: `cd /Users/delavalom/delavalom-labs/arvlang-wt/p83 && go test ./arv/runtime/`

Expected:
Build failure: `AllowsDial undefined (type CapSet has no field or method AllowsDial)` and `task.RequireDial undefined`.

- [ ] **Step 3: Implement**

Create `arv/runtime/dial.go`:

```go
package runtime

import (
	"fmt"
	"net/netip"
)

// AllowsDial reports whether s may connect to addr, the address a dial
// actually reached after DNS resolution. The request's host name has
// already been checked (http_get, CheckRedirect); a public address needs
// nothing more. An internal address (see AddrClass) needs a net grant for
// that IP address or a CIDR range holding it, with a matching port if the
// grant names one: a host name grant, even "*", never reaches one. So a
// granted name that resolves, or re-resolves, to an internal address
// (DNS rebinding, SSRF) is refused at the dial.
func (s CapSet) AllowsDial(addr netip.AddrPort) bool {
	return dialDenied(s, addr) == nil
}

// dialDenied returns the capability addr would need, or nil if s may dial it.
func dialDenied(s CapSet, addr netip.AddrPort) *Capability {
	a := addr.Addr().Unmap().WithZone("")
	if AddrClass(a) == "" {
		return nil
	}
	want := Capability{Kind: Net, Scope: netip.AddrPortFrom(a, addr.Port()).String()}
	if s.Allows(want) {
		return nil
	}
	return &want
}

// RequireDial is the dial-time check (see AllowsDial). dialed is what the
// request asked for ("host:port"); the error names the capability that
// would allow addr and says which kind of internal address it is.
func (t *Task) RequireDial(dialed string, addr netip.AddrPort) error {
	want := dialDenied(t.Caps, addr)
	if want == nil {
		return nil
	}
	return &DeniedError{
		Task: t.ID, Want: *want, Held: t.Caps.List(),
		Note: fmt.Sprintf("%s reached a %s address, which only an IP or CIDR grant covers",
			dialed, AddrClass(addr.Addr())),
	}
}
```

In `arv/runtime/capability.go`, replace:

```go
	Want Capability
	Held []Capability
}
```

with:

```go
	Want Capability
	Held []Capability
	Note string // why, when Want alone does not say (e.g. a dial-time refusal)
}
```

In `arv/runtime/capability.go`, replace:

```go
		where = " in task " + e.Task
	}
```

with:

```go
		where = " in task " + e.Task
	}
	if e.Note != "" {
		where += ": " + e.Note
	}
```

- [ ] **Step 4: Run the checks and watch them pass**

```bash
cd /Users/delavalom/delavalom-labs/arvlang-wt/p83 && gofmt -l . && go vet ./... && GOOS=windows go vet ./... && go test -race -count=1 ./...
```

Expected: `gofmt` prints nothing, both vets are clean, and every package says `ok` (`arv/token` has no test files).

Then fuzz for at least 2 minutes. Check disk first, and stop and report `blocked` if less than 1 GB is free:
```bash
cd /Users/delavalom/delavalom-labs/arvlang-wt/p83 && df -h /System/Volumes/Data && go test -run XXX -fuzz FuzzNetScope -fuzztime 2m ./arv/runtime/
```
Expected: `PASS`. If it finds a failure, fix the bug, never the property. Keep the crasher under `arv/runtime/testdata/fuzz/FuzzNetScope/` as a regression seed, and add the input to the bad list in `TestNetScopePorts`. If nothing fails, make sure no `testdata/fuzz` directory is left behind.

- [ ] **Step 5: Commit**

```bash
cd /Users/delavalom/delavalom-labs/arvlang-wt/p83 && git add arv/runtime/capability.go arv/runtime/dial.go arv/runtime/dial_test.go arv/runtime/netprop_test.go && git commit -F - <<'EOF'
runtime: dial-time address policy, I2 property test, net fuzzing

AllowsDial/RequireDial let an internal address be dialled only under
an IP or CIDR grant covering it; a public address needs nothing more
than the name check. DeniedError gains a Note for why. Attenuation is
checked exhaustively over net scope shapes, and FuzzNetScope covers
net scope parsing.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
EOF
```
