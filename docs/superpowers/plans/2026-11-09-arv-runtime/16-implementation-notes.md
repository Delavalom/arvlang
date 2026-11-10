# Implementation notes: Phases 0–2 as built

**Status (2026-11-10):** Phases 0–2 are implemented on `feat/runtime`. Execution was subagent-driven: one implementer per task, plus spec and quality reviews. The branch is not pushed or merged. Read this file before expanding Phases 3–8: the code differs from plan files 02–09 in the ways listed below. The commit log is the source of truth.

## Added beyond the plan

| What | Where | Why |
|---|---|---|
| Black-box e2e suite (Tasks 1.8, 2.10) | `e2e/` | The owner asked for e2e coverage of the whole implementation. It builds the real binary and uses a local `httptest` server and temp dirs, with no internet. Server gates (`/wait`, `/arrive`, `/slow`, `/mark`) order events, so tests don't depend on wall-clock timing. `ARV_E2E_RACE=1` builds the binary with `-race`. |
| `Makefile` | root | `make build`, `make test`, `make e2e`. |
| Split parser tests | `arv/parser/parser_*_test.go` | The 20 KB per-file commit hook. |

## Security and robustness fixes found during execution

1. **Dangling symlink escape.** The e2e suite found it. `write_file` through a dangling link inside a grant wrote outside the grant. `Canonical` now walks path components with `Lstat`/`Readlink` and allows at most 40 hops. It returns `(string, error)`. Files are opened with `O_NOFOLLOW`.
2. **Scope validation.** An empty fs scope used to cover `/`, and `--allow fs_read=` used to grant the working directory. Both are now rejected. `arv.Run` validates and canonicalises the host's grants.
3. **One `..` rule.** `runtime.Resolve` joins paths without cleaning them, and `Canonical` resolves `..` the way the kernel does, after following symlinks. This applies everywhere: grants, constructors and requests.
4. **Limits.** Bodies over 10 MiB are an error instead of being truncated. `read_file` reads regular files only, so a FIFO fails fast. `sleep` has an upper bound. The parser has a nesting limit.
5. **Net scopes.** Ports, schemes, paths, `@` and whitespace are rejected. A trailing dot is stripped. Bracketed IPv6 addresses are normalised. Wildcards are allowed only as a leading `*.` on DNS names, never on IP addresses.
6. **Concurrency.**
   - A task panic now cancels and waits for the scope (I4), and the handle always finishes.
   - The first error wins, and the body's error goes through `Scope.Fail`.
   - `arv.Run` never panics into the host.
   - Division by zero is an Arv error. Empty blocks return `NIL`.
7. **Await ownership.** A task may await only handles spawned by itself or its descendants. Without this, a self-await, mutual awaits or awaiting a parent deadlocked the process.
8. **No authority, no OS access.**
   - Capability constructors don't touch the filesystem.
   - `CapSet.Admit` is the single admission point, used by `Attenuate`, `read_file` and `write_file`.
   - A denial outside every grant names the path as the script wrote it, never the resolved target or the OS error.
   - When resolution fails inside a grant, the script sees the real OS error.
   - `..` after a missing path component fails, as it does in the kernel.
   - Remaining limit: allow and deny can still reveal, one bit at a time, whether a path outside every grant exists. Canonicalising has to `lstat` it.
9. **Robustness.**
   - Total evaluator recursion is bounded at 100000 levels, so no program can overflow the Go stack. Before this, call depth multiplied by nesting depth was a fatal crash.
   - The parser counts only real nesting (`MaxNesting` 10000).
   - Infix chains are evaluated and printed iteratively.
   - `write_file` opens with `O_NONBLOCK`, so a FIFO can't hang a scope.
   - The REPL keeps one root task per session.
   - Await is allowed on handles spawned by the awaiting task or any of its descendants.

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

## Follow-through for later phases

- **Phase 3 (budgets):**
  - Release `tasks:` slots in a `defer`, so the panic path releases them too.
  - Scopes are created in `evalScope` and `Run`, not in `withScope` alone.
  - Spawn recursion is unbounded today, because each task starts at depth 0. Don't make the default task budget unlimited.
  - Check `timeout_ms` for overflow, as `sleep` now does.
  - Keep per-scope state in a frame in `in.scopes`. Don't mutate exported `Task` fields.
- **Phase 4 (supervision):**
  - `Scope.Fail` stores a Go error, so the error `Kind` is lost. Carry the `*value.Error` instead.
  - The await-ownership rule constrains supervisors. `collect` can rely on owners to know which handles were awaited.
  - `restart` re-runs against a live environment, so I8 (freeze at spawn) should land in Phase 4, not Phase 6. Today a child can see a parent rebind a name after `spawn`.
- **Phase 6 (replay):**
  - Freezing the environment at spawn must be transitive.
  - Spawn-time canonicalisation calls `lstat`. That is OS input, so log it or route it through the effect gate (I7).
  - `--replay` without `--allow` must install the grants from the log header. Otherwise every `spawn … with` is denied.
- **Phase 7 (agents):** denial messages still list the held grants. Redact them before errors reach a model.
- **Phase 8 (tooling and hardening):**
  - `net("host:443")` conflicts with the current rule that rejects ports. You would have to parse the port and compare it with each scheme's default port, in `http_get` and in `CheckRedirect`.
  - CIDR grants are rejected by the `/` check.
  - `os.Root` closes the directory-swap race between check and use, but it needs `go 1.24` or later. `go.mod` says 1.22.5. It also refuses escaping symlinks instead of following them to check where they lead, so some hardening e2e expectations will flip.
  - Moving `main.go` to `cmd/arv` breaks the e2e build target, the `Makefile` and the README.
  - DNS rebinding and SSRF are still open.
