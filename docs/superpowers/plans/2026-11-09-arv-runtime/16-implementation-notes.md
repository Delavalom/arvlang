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

Of the Phase 8 follow-through items below, the `os.Root`, port, CIDR and SSRF ones are done here. Moving `main.go` to `cmd/arv` is left for 8.5. **Details, the review-round history, the deviations from §8.3 and the mutation table are in [`16b-phase83-notes.md`](16b-phase83-notes.md)**, kept apart so this shared file stays under the commit hook's size limit.

**Go version.** `go.mod` says `go 1.24.0`, the lowest release with `os.Root` (added in Go 1.24); the toolchain is 1.26.2. With a 1.24 module, `go vet` reports non-constant format strings, so `evalIdentifier` now calls `newError("identifier not found: %s", ...)`. Phase 3 edits the same file, so expect a one-line merge conflict there.

**Filesystem.** `read_file` and `write_file` open through an `os.Root`, which refuses a symlink that escapes the root, so a directory swapped after the check cannot redirect the open. The root is the outermost directory that any fs grant of the task's lineage names, of either kind, matched by identity (`confiningRoot`); from there `openIn` steps down to the covering grant one checked component at a time. A lone file grant keeps the direct `O_NOFOLLOW` open. `Canonical` still resolves every symlink before the check, so no e2e expectation flips; only a tree that changes between check and open, or an unreadable directory (Known limits), behaves differently.

**Net.** `net("host:443")` and `net("[::1]:443")` are valid grants; a bare host means every port. Every request names its port (the URL's, or the scheme default), in `http_get` and `CheckRedirect` alike. CIDR grants (`net=10.0.0.0/8`) are implemented, not deferred, and take no port. Each `http_get` gets its own transport whose `net.Dialer.Control` checks every address actually dialled (`Task.RequireDial`): an internal address (loopback, private, link-local and the rest of `runtime.AddrClass`) needs an IP or CIDR grant covering it and its port. This refuses SSRF through a granted name and DNS rebinding, since the check sees the connected address. `*` covers every host name but no internal IP literal or CIDR range (I2). **The explicit grant path for loopback**, used by the e2e suite, is an IP grant: `net=127.0.0.1` or `net=127.0.0.1:PORT`; a request for `localhost` pairs `net=localhost` (the name) with `net=127.0.0.1` (the dial).

### Changed expectations (each with its reason)

| Test | Before | After | Why |
|---|---|---|---|
| `e2e TestNetGrantValidation` | `net=127.0.0.1:1234` rejected | accepted; `:0`, `:65536` rejected | Ports are part of a grant now. |
| `e2e TestNet` no grant / other host / redirect to ungranted host; `TestNetHostForms` userinfo and normalised-host denials | denial names `net("127.0.0.1")`, `net("localhost")`, `net("other.invalid")` | names `host:PORT` (`other.invalid:80` for a portless URL) | A request names the port it reaches. |
| `e2e TestNet` "wildcard grant allows any host" | `net=*` fetched `http://127.0.0.1:PORT` | `net=*` plus `net=127.0.0.1` fetch `http://localhost:PORT` | `*` no longer covers internal IPs (I2), and the loopback dial needs an IP grant. The case still shows `*` covers any name. |
| `e2e TestNetHostForms` uppercase / trailing-dot cases | `net=localhost` alone | plus `net=127.0.0.1` | Only an IP grant may dial loopback; it cannot match the request name, so the cases still test name normalisation. |
| unit `TestHTTPGetNeedsNetCapability`, `TestHTTPGetChecksRedirectTargets` | denial names the host | names `host:port` | Same as above. |
| unit `TestNewRejectsBadNetScopes`, `TestLiteralDoesNotResolve`, `TestRunRejectsInvalidCaps` | `host:80`-style scopes invalid | invalid ports (`:0`, `:65536`, `:x`) instead | Valid ports are accepted now. |
| unit `TestGrantPicksTheMostSpecificCover`, renamed `TestGrantPicksTheOutermostCover` | the innermost cover | the outermost cover | Review round 1: the inner grant's directory can be swapped from inside the outer one. |
| new unit `TestSearchOnlyDirInsideGrantIsRefused`, `TestWriteOnlyGrantDirIsRefused` | a file below a search-only (`--x`) directory, or in a `-wx` grant directory, opened | `permission denied` | `os.Root` opens directories read-only (review round 4; see Known limits). |

### Known limits

- **Unreadable directories (review round 4).** `os.Root` opens every directory it walks, the root included, read-only, so any directory without the read bit between the open's root and the file fails the open with `permission denied`. This holds inside a single plain grant, not only when stepping down from an outer grant: a search-only directory (`0100`, or a `0711` home directory seen by another user) inside `fs_read=/g`, or a write-only drop-box grant (`0300`). Before Phase 8.3 search permission was enough. The failure is closed. Workaround: grant the readable directory below. A fix needs a search-only `openat` walk (`O_SEARCH`/`O_PATH`); see 16b. Pinned by `arv/evaluator/unreadabledir_unix_test.go`.
- A hard link inside a grant to a file outside it is still followed. Neither `Canonical` nor `os.Root` can see it.
- Swapping an *ancestor* of the outermost fs grant the lineage holds, by a writer outside every grant, is outside the threat model, which covers a writer inside a grant the program holds.
- A dial-time denial names the internal address a granted name resolved to. Phase 7 must redact denials before they reach a model.
- A name grant cannot be restricted to particular IP addresses. Grant the IP or range instead.
- Each `http_get` opens a fresh connection: no keep-alive, one TCP (and TLS) handshake per call. Proxies are not supported.
- Teredo addresses are refused as a class rather than decoded. The dial check drops the zone of the address it reaches, so `net=fe80::1` reaches `fe80::1` on any interface.

### Mutation checks

The independent mutation-check rounds 1-4 (every guarantee, each mutation, the test that caught it, and the two test gaps they closed) are in [`16d-phase83-mutations.md`](16d-phase83-mutations.md).

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
  - Done in Phase 8.3 (see that section above): port grants checked in `http_get` and `CheckRedirect`, CIDR grants, `os.Root` for the fs effects (`go.mod` is now `go 1.24.0`), and the dial-time check against SSRF and DNS rebinding.
  - Moving `main.go` to `cmd/arv` breaks the e2e build target, the `Makefile` and the README.

## Phase 3: budgets

Built from `p3-plan-1.md` to `p3-plan-13.md`; their deviations from `10-phase3-budgets.md` are listed in `p3-plan-1.md`. **The fixes from review rounds 1-6 and the mutation tables (rounds 1-3) are in [`16c-phase3-notes.md`](16c-phase3-notes.md), review round 7 is in [`16e-phase3-round7.md`](16e-phase3-round7.md), review round 8 is in [`16f-phase3-round8.md`](16f-phase3-round8.md), and review round 9 is in [`16g-phase3-round9.md`](16g-phase3-round9.md).**

**Recorded deviations:**
- **Timeout bounds in the acceptance tests are 3 s, not 300 ms.** The tests must pass at load averages above 300. A 3 s bound still separates the 100 ms limit from the inner 10 s timeout and the 5 s sleep, so I6 is still tested.
- **Commit 323d76e has a README edit that is not in p3-plan-13.** It adds ` (at examples/fanout.arv:17:27)` to the fanout denial line. This matches what the binary prints, so it was kept. The getenv example was updated to match the same format.

**Known limits:**
- **A timeout is reported late, not enforced early, for work that ignores cancellation.** A host builtin that ignores its context runs to the end, because the scope waits for it (I4). The scope then reports the timeout. Pure Arv work between context checks (every 256 nodes) can also run past the deadline, but no builtin, and so no effect, starts after it.
- **`alloc_mb` still bounds churn, not live memory.** Integers (8 bytes each) are not charged, because each one is kept alive only by a charged array element or binding. Rebinding a name is charged again. Live memory is bounded by a small constant factor of the limit plus a fixed amount per live task, not by the limit alone. A concatenation is charged before it is built. `read_file` and `http_get` charge their bytes as they read them, 32 KiB at a time (`value.ChargingFunction`), so each reader holds at most one uncharged chunk. They copy their chunks once into a string of exactly the result's size, so a held result keeps no uncharged slack, and a read allocates about twice its result while it runs. The first chunk is sized from the file's size or the `Content-Length`, so a small read does not allocate a whole chunk. `push` charges its copy before making it, and `for` and `await` allocate nothing for their array up front. A capability costs its scope's bytes plus 32, and so does each capability a `spawn ... with` requests, as canonicalised. `write_file` writes its text without copying it, and `puts` prints a string, or the text it builds, without copying it. An fs effect charges each stat of a lineage grant it makes (256 bytes plus the path). Checking and opening its path still churns about 5 KiB per call, uncharged, and canonicalising a spawn request about 3 KiB, charged only 32 bytes plus its scope. Both are fixed amounts per call or per request, so a loop of them can churn 10 to 15 times what it is charged. Error values are not charged, so builtins keep them small: a scope is at most 4096 bytes (`runtime.MaxScopeLen`), a host name 253, an `http_get` URL 8 KiB (a redirect's too, and a redirect's `Location` is refused past that before it is parsed), net/http's error text and an invalid URL are quoted only to 256 bytes, and a denial lists at most 8 grants. `http_get` also holds its response headers uncharged, capped at 64 KiB (`maxHeaderBytes`). At the default cap of 1000 live tasks, these per-task amounts can reach on the order of 100 MiB in all, whatever the limit. A host builtin set as a plain `Fn` is charged only after it returns, so a host that returns large values should use `Charging` instead. The text `puts` builds is charged in 64 KiB steps, so a run can go up to 64 KiB past the limit before `puts` stops. The REPL prints each result with plain `Inspect`. It runs without budgets, so nothing bounds that text, just as nothing bounds the REPL's other allocations.
- **The depth pool bounds the total depth of the live tasks, not the number of deep tasks.** At the default cap, any mix whose depths add up to more than 400000 levels fails. For example, 900 tasks each 80 Arv calls (about 560 levels) deep fail, and 900 tasks each 60 calls deep pass. A raised `--tasks` raises the pool, and with it the worst-case stack memory: about 640 bytes per level (twice that under `-race`), so about 1.3 GB at `--tasks 8000`. `MaxRunEvalDepth` and `LiveTaskDepth` are constants, not `Limits` fields.
- **A root-level panic is placed at the program's last statement.** That is where the run ended, not where the panic happened.
- **The `httptest` backpressure test does not check for leaked goroutines.** The HTTP client keeps idle keep-alive connections open.
