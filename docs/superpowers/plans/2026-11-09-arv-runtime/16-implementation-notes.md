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
   - Remaining limit: allow and deny can still reveal, one bit at a time, whether a path outside every grant exists. Canonicalising has to `lstat` it. `alloc_mb` adds no other route: a denied request is charged its text's cost alone (review round 11, `16i`).
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
- **Phase 4 (supervision):** all three done in Phase 4; see [`16j-phase4-notes.md`](16j-phase4-notes.md).
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

Built from `p3-plan-1.md` to `p3-plan-13.md`; their deviations from `10-phase3-budgets.md` are listed in `p3-plan-1.md`. **The fixes from review rounds 1-6 and the mutation tables (rounds 1-3) are in [`16c-phase3-notes.md`](16c-phase3-notes.md), review round 7 is in [`16e-phase3-round7.md`](16e-phase3-round7.md), review round 8 is in [`16f-phase3-round8.md`](16f-phase3-round8.md), review round 9 is in [`16g-phase3-round9.md`](16g-phase3-round9.md), review round 10 is in [`16h-phase3-round10.md`](16h-phase3-round10.md), and review round 11 is in [`16i-phase3-round11.md`](16i-phase3-round11.md).**

Its recorded deviations and known limits are in [`16p-phase3-summary.md`](16p-phase3-summary.md).

## Phase 4: supervision

Notes: [`16j`](16j-phase4-notes.md), [`16k`](16k-phase4-hardening.md). Mutation tables (round 1: 45 mutations, 42 caught, one test gap; round 2: the gap and the clock jitter now caught, one equivalent): [`16l-phase4-mutations.md`](16l-phase4-mutations.md).

| Guarantee | Mutation | Caught | Test |
|---|---|---|---|
| 30 s backoff cap (round 3) | drop `d = min(d, MaxBackoff)` | no (equivalent, see 16l) | none possible |
| 30 s backoff cap (round 4) | drop `d = min(d, MaxBackoff)` | yes, unit only (not equivalent) | `TestDelayClampsBeforeJitter` |

## Phase 5: gate

Notes: [`16m`](16m-phase5-notes.md). Review round 1: [`16n`](16n-phase5-round1.md).

## Phase 6: replay

Notes: [`16o`](16o-phase6-notes.md). Review round 1 (fixes, dispositions, mutation checks): [`16q`](16q-phase6-round1.md). Review round 2 (deadlines in the log: design, dispositions, mutation checks, and the known limits on time): [`16r`](16r-phase6-round2.md).

**Known limits added in review round 1** (each is Minor, or a bound on a fix):
- **Quiet checks (resume).** While any task still has records, a resumed task past its own records does not see a failure it has not seen before. Since round 2 it does see a deadline, unless it is still behind the log (a spawn it has still to make has records). It can go on to print or write after a sibling's failure that a live run would have stopped it at, only in that window. A resumed run whose script changed can leave records nobody uses. It then stays quiet, and a loop that ends only by a failure's cancellation runs until the host cancels the run.
- **Resumed deadlines are bounds.** Round 2 replaced the round-1 rule (`TimeoutLeft`): see [`16r`](16r-phase6-round2.md). A resumed deadline is never sooner than the recorded one, and can be later by the unlogged time between a scope opening and the record that bounds it.
- **Output order across a resume.** `puts` takes its output index before its record is written. If task A takes index k, B takes k+1 and is logged, and the run is killed before A's record, then the resumed A prints after B. Replays of the resumed log keep that order.
- **A forged log in `--resume`.** The header's limits are taken as they are: the CLI and `arv.Run` refuse limit flags on a resume, so a forged header can set `steps`, `alloc_bytes` or `timeout_ms` to 0 (no limit) for the live part. A spawn record's grants are checked against the parent's grants (I2), not against what the spawn asked for. So a forged record can give a resumed child more than the program's own narrowing, but never more than its parent holds. Requests are not canonical, and checking them strictly would need an lstat.
- **A forged `cancelled` record hangs a replay** until the host cancels it. Since round 2 no timer runs in a replay, so the header's timeout does not end it.
