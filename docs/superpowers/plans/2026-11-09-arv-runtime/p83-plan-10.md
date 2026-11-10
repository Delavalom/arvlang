# Phase 8.3, part 10 of 10: document 8.3 (notes, README, usage) (task p83.10)

> **Superseded (review rounds 1 and 2).** `CapSet.Grant` picks the *outermost* cover, not the most specific one, and the fs open starts from the outermost fs grant of *any* kind that is an ancestor of it by identity (`confiningRoot`, then a checked step down per component). Wherever this file says "most specific", "longest", "smallest root" or `TestGrantPicksTheMostSpecificCover`, follow the Phase 8.3 section of `16-implementation-notes.md` instead: rooting the open at an inner grant lets a directory swap escape every grant.

> **For agentic workers:** this file is self-contained. Implement task p83.10 exactly as written, test first. Background, which you don't need to read: `15-phase8-tooling.md` §8.3, `16-implementation-notes.md` (it overrides older plan text) and the invariants I1–I8 in `01-design.md`, all in `docs/superpowers/plans/2026-11-09-arv-runtime/`.

**Goal:** Write down what 8.3 built: every changed expectation with its reason, the deviations from the design file, known limits, and the mutations checked. Update the README grant rules, the CLI usage line and the known-limit line in `01-design.md`.

**Invariants touched:** none (docs). Rule from the kickoff: every changed e2e expectation needs a written reason in `16-implementation-notes.md`.

**Depends on:** p83.1–p83.9 committed on `phase/p83`. Check with `git log --oneline -12`.

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

### Task p83.10: document 8.3 (notes, README, usage)

The new section goes **before** `## Follow-through for later phases`, not at the end of the file. Phase 3 runs in parallel and will probably append its own notes at the end, so this placement keeps the merge clean. `16-implementation-notes.md` grows to about 14 KB, under the 18 KB limit.

Before committing, re-read the "Changed e2e and unit expectations" table against `git diff feat/phases-3-8 -- e2e arv/runtime/*_test.go arv/evaluator/effects_test.go arv/arv_test.go`. Every changed pre-existing expectation must have a row. If an implementer of p83.3–p83.9 had to change anything else, add a row for it.

**Files:**
- Modify: `docs/superpowers/plans/2026-11-09-arv-runtime/16-implementation-notes.md`, `docs/superpowers/plans/2026-11-09-arv-runtime/01-design.md` (one bullet)
- Modify: `README.md`, `main.go` (usage string)

- [ ] **Step 1: Implement**

In `docs/superpowers/plans/2026-11-09-arv-runtime/16-implementation-notes.md`, insert the following immediately before the line `## Follow-through for later phases`:

```markdown
## Phase 8.3: network hardening and `os.Root` (as built)

Of the Phase 8 follow-through items below, the `os.Root`, port, CIDR and SSRF ones are done here. Moving `main.go` to `cmd/arv` is left for 8.5.

**Go version.** `go.mod` says `go 1.24.0`, the lowest release with `os.Root` (added in Go 1.24); the toolchain is 1.26.2. With a 1.24 module, `go vet` reports non-constant format strings, so `evalIdentifier` now calls `newError("identifier not found: %s", ...)`. Phase 3 edits the same file, so expect a one-line merge conflict there.

**Filesystem: `os.Root`.**
- `fsRequest` returns the canonical path and the most specific grant that covers it (`CapSet.Grant`). `openIn` opens that grant as an `os.Root` and opens the path beneath it. `os.Root` follows a symlink only while it stays inside the root, and refuses one that escapes ("path escapes from parent"). So a directory swapped for a symlink after the check can no longer redirect the open outside the grant.
- A file grant (the path is the grant itself) has no directory inside the grant to confine. It keeps the old direct open with `O_NOFOLLOW`.
- During such a race, a swap to a *relative* symlink that stays inside the grant is followed. `os.Root` refuses every *absolute* symlink, even one that points inside the root.
- **No e2e expectation flips for fs.** `Canonical` still resolves every symlink before the check, so `os.Root` opens a path with no symlinks in it. The only difference shows when the tree changes between check and open. Those races are tested with the `beforeOpen` hook in `arv/evaluator/swap_unix_test.go`.

**Net ports.**
- `net("host:443")` and `net("[::1]:443")` are now valid grants. A port is 1–65535; leading zeros are dropped. A bare host still means every port.
- Every request names a port: the URL's own port, or the scheme's default (`http` 80, `https` 443). `http_get` and `CheckRedirect` both build the request with `netRequest`.
- A portless request stands for every port, so at `spawn` only a portless grant covers it (I2).

**CIDR grants (implemented, not deferred).**
- `net=10.0.0.0/8` covers the IP addresses in it and any narrower range, on every port. A range with bits set past its prefix is rejected, and the error suggests the masked form.
- A CIDR range takes no port.
- A host *name* never matches a CIDR grant, even when it resolves into the range. Requests are checked by name; CIDR and IP grants decide which internal addresses a dial may reach.

**SSRF and DNS rebinding.**
- `transportFor` gives each `http_get` its own `http.Transport`. Its `net.Dialer.Control` runs once per address actually tried, after DNS resolution, and calls `Task.RequireDial`.
- An internal address needs a net grant naming that IP address, or a CIDR range holding it, with a matching port if the grant names one. `runtime.AddrClass` defines internal: unspecified, loopback, private and ULA, link-local (which holds 169.254.169.254), multicast, 0/8, 100.64/10, 192.0.0/24, 198.18/15, 240/4, local NAT64, Teredo and site-local. IPv4-mapped, NAT64 and 6to4 addresses are classed by the IPv4 address they carry.
- The check runs on the connected address itself, so no separate resolve-then-check step exists for a rebinding answer to race.
- `*` covers every host name but no internal IP literal, and no CIDR range. Otherwise a child could attenuate `*` into `net("127.0.0.1")` and gain an internal dial its parent lacked (I2). `TestNetAttenuationNeverGrantsMore` checks this exhaustively over a vocabulary of scope shapes.
- The transport is never shared: a pooled connection checked against one task's grants must not serve another. Keep-alives are off, so no connection goroutine outlives the call (leak rule; `TestHTTPGetLeavesNoGoroutines`).
- No proxy is used. Through a proxy the dial would reach the proxy, and the check would see the proxy's address.
- **The explicit grant path for loopback** (and for the e2e suite) is an IP grant: `net=127.0.0.1`, or `net=127.0.0.1:PORT`. A test that requests `localhost` pairs `net=localhost` (for the name) with `net=127.0.0.1` (for the dial).

**Bug found by fuzzing.** `FuzzNetScope` found that `net("..")` normalised to `"."`, and `"host.."` to `"host."`. Neither form is accepted on a second pass. Normalisation now rejects a name that still ends in a dot after one trailing dot is dropped.

### Changed e2e and unit expectations (each with its reason)

| Test | Before | After | Why |
|---|---|---|---|
| `e2e TestNetGrantValidation` | `net=127.0.0.1:1234` rejected ("a port is not allowed") | accepted. Bad ports (`:0`, `:65536`) are rejected | Ports are part of a grant now. |
| `e2e TestNet` no grant / other host / redirect to ungranted host | denial names `net("127.0.0.1")` or `net("localhost")` | names `net("127.0.0.1:PORT")` or `net("localhost:PORT")` | A request names the port it reaches. |
| `e2e TestNetHostForms` userinfo and normalised-host denials | `net("other.invalid")`, `net("localhost")` | `net("other.invalid:80")`, `net("other.invalid:PORT")`, `net("localhost:PORT")` | Same reason. |
| `e2e TestNet` "wildcard grant allows any host" | `net=*` fetched `http://127.0.0.1:PORT` | `net=*` plus `net=127.0.0.1` fetch `http://localhost:PORT` | `*` no longer covers internal IPs (I2), and the dial to loopback needs an IP grant. The case still shows that `*` covers any host name. |
| `e2e TestNetHostForms` uppercase / trailing-dot cases | `net=localhost` alone | `net=localhost` plus `net=127.0.0.1` | `localhost` resolves to loopback, which only an IP grant may dial. The IP grant cannot match the request name, so these cases still test name normalisation. |
| unit `TestHTTPGetNeedsNetCapability`, `TestHTTPGetChecksRedirectTargets` | denial names the host | names `host:port` | A request names the port it reaches. |
| unit `TestNewRejectsBadNetScopes`, `TestLiteralDoesNotResolve`, `TestRunRejectsInvalidCaps` | `host:80`-style scopes were invalid | invalid ports (`:0`, `:65536`, `:x`) instead | Valid ports are accepted now. |

### Deviations from `15-phase8-tooling.md` §8.3

1. **Acceptance test uses `localhost`, not a `rebind.test` DNS stub.** The binary uses the system resolver, and the standard library has no DNS server. A stub would need a hand-written DNS codec plus a resolver hook in production code. `localhost` resolves to loopback through `/etc/hosts`, which is hermetic. Because the check runs on the connected address, it is resolution-agnostic: a stub would exercise the same code path.
2. **`net.Dialer.Control` instead of resolving inside a custom `DialContext`.** Control sees every address the dialer actually tries, including Happy Eyeballs fallbacks, so there is nothing to keep consistent between a lookup and the connect.
3. **`*` does not cover internal IP literals or CIDR ranges** (see above).
4. **CIDR grants take no port.**

### Known limits

- A hard link inside a grant to a file outside it is still followed. Neither `Canonical` nor `os.Root` can see it.
- Swapping an *ancestor* of a grant's directory is outside the threat model, which covers a writer inside the grant.
- A dial-time denial names the internal address a granted name resolved to. Phase 7 must redact denials before they reach a model.
- A name grant cannot be restricted to particular IP addresses. Grant the IP or range instead.
- Each `http_get` opens a fresh connection: no keep-alive, and one TCP (and TLS) handshake per call. Proxies are not supported.
- Teredo addresses are refused as a class rather than decoded. Zone IDs are dropped before the dial check.

### Mutations checked while planning

| Mutation | Caught by |
|---|---|
| `read_file` and `write_file` open the canonical path directly again (no `os.Root`) | `TestReadFileDirSwapCannotEscape`, `TestWriteFileDirSwapCannotEscape`, `TestReadFileLeafSwapCannotEscape` |
| `Control` always returns nil | `TestHTTPGetRefusesANameThatResolvesToLoopback`, `TestHTTPGetRedirectDialIsChecked` |
| Keep-alives on, with no `CloseIdleConnections` | `TestHTTPGetLeavesNoGoroutines` |
| `*` covers internal IP literals | `TestNetAttenuationNeverGrantsMore`, `TestNetCoverageCIDRAndInternal` |

```

In `docs/superpowers/plans/2026-11-09-arv-runtime/01-design.md`, replace:

```markdown
- **Network checks use the hostname.** A granted host that resolves to an internal IP (DNS rebinding, SSRF) gets through until Phase 8 adds an IP check in `DialContext`.
```

with:

```markdown
- **Network checks use the hostname, then the address.** Since Phase 8.3 every connection is also checked on the address it reaches: an internal address needs an IP or CIDR grant, so DNS rebinding and SSRF through a granted name are refused (see `16-implementation-notes.md`).
```

In `main.go`, replace:

```go
kinds: net=HOST (or *.domain, *), fs_read=PATH, fs_write=PATH, env=NAME`
```

with:

```go
kinds: net=HOST[:PORT] (HOST: name, *.domain, *, IP) or net=CIDR,
       fs_read=PATH, fs_write=PATH, env=NAME`
```

In `README.md`, replace:

```markdown
| `net` | a bare host, or a `*.domain` wildcard | `--allow net=api.github.com`, `--allow 'net=*.github.com'` |
```

with:

```markdown
| `net` | a host (`api.github.com`, `*.github.com`, `*` or an IP) with an optional `:port`, or a CIDR range | `--allow net=api.github.com:443`, `--allow 'net=*.github.com'`, `--allow net=10.0.0.0/8` |
```

In `README.md`, replace:

```markdown
- **Net grants are hosts only.** Ports and schemes are rejected: `--allow net=api.github.com:443` fails with `a port is not allowed`.
```

with:

```markdown
- **Ports.** `net=api.github.com:443` allows port 443 only; a bare host allows every port. A request names the port it reaches (the scheme's default when the URL has none), and every redirect is checked again, port included. Schemes and paths are rejected.
- **Internal addresses need an IP grant.** Every connection is checked on the address it actually reaches, after DNS. Loopback, private, link-local (including the `169.254.169.254` metadata service) and other internal addresses are refused unless an IP or CIDR grant covers them (`net=127.0.0.1`, `net=10.0.0.0/8`). A host name grant, even `*`, never reaches them, so a name that resolves, or rebinds, to an internal address is refused. Proxy settings from the environment are not used.
```

In `README.md`, replace:

```markdown
- **Paths are checked after symlinks are followed.** `..` is resolved after that, and grants are canonicalised, so a link or a `..` cannot step outside the granted directory.
```

with:

```markdown
- **Paths are checked after symlinks are followed.** `..` is resolved after that, and grants are canonicalised, so a link or a `..` cannot step outside the granted directory. Files are then opened beneath the grant with `os.Root`, so a directory swapped for a symlink after the check cannot lead outside it either.
```

- [ ] **Step 2: Run the checks and watch them pass**

```bash
cd /Users/delavalom/delavalom-labs/arvlang-wt/p83 && gofmt -l . && go vet ./... && GOOS=windows go vet ./... && go test -race -count=1 ./...
```

Expected: `gofmt` prints nothing, both vets are clean, and every package says `ok` (`arv/token` has no test files).

- [ ] **Step 3: Commit**

```bash
cd /Users/delavalom/delavalom-labs/arvlang-wt/p83 && git add README.md docs/superpowers/plans/2026-11-09-arv-runtime/01-design.md docs/superpowers/plans/2026-11-09-arv-runtime/16-implementation-notes.md main.go && git commit -F - <<'EOF'
docs: record Phase 8.3 network hardening and os.Root

Changed expectations with reasons, deviations from the 8.3 design,
known limits and the mutations checked; README grant rules and CLI
usage for ports, CIDR and the dial-time address check.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
EOF
```
