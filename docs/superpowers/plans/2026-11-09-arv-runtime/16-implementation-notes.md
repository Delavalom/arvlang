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

- **Unreadable directories (review round 4).** `os.Root` opens every directory it walks, the root included, read-only, so a directory without the read bit between the open's root and the file fails with `permission denied`, even inside one plain grant: a search-only directory (`0100`, a `0711` home seen by another user) or a `0300` drop-box grant. Before Phase 8.3 search permission was enough. The failure is closed; grant the readable directory below. A fix needs a search-only `openat` walk; see 16b. Pinned by `arv/evaluator/unreadabledir_unix_test.go`.
- A hard link inside a grant to a file outside it is still followed. Neither `Canonical` nor `os.Root` can see it.
- Swapping an *ancestor* of the outermost fs grant the lineage holds, by a writer outside every grant, is outside the threat model, which covers a writer inside a grant the program holds.
- A dial-time denial names the internal address a granted name resolved to. Phase 7 redacts it before a model sees it.
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
- **Phase 4 (supervision):** done in Phase 4 ([`16j`](16j-phase4-notes.md)): `Scope.Fail` keeps the error's kind, `collect` relies on await ownership, and I8 (freeze at spawn) landed there, since `restart` re-runs against a live environment.
- **Phase 6 (replay):**
  - Freezing the environment at spawn must be transitive.
  - Spawn-time canonicalisation calls `lstat`. That is OS input, so log it or route it through the effect gate (I7).
  - `--replay` without `--allow` must install the grants from the log header. Otherwise every `spawn … with` is denied.
- **Phase 7 (agents):** done in Phase 7 ([`16o`](16o-phase7-notes.md)): denials are redacted before a model sees them.
- **Phase 8 (tooling and hardening):**
  - Done in Phase 8.3 (see that section above): port grants checked in `http_get` and `CheckRedirect`, CIDR grants, `os.Root` for the fs effects (`go.mod` is now `go 1.24.0`), and the dial-time check against SSRF and DNS rebinding.
  - Moving `main.go` to `cmd/arv` breaks the e2e build target, the `Makefile` and the README.

## Phase 3: budgets

Built from `p3-plan-1.md` to `p3-plan-13.md`; their deviations from `10-phase3-budgets.md` are listed in `p3-plan-1.md`. **The fixes from review rounds 1-6 and the mutation tables (rounds 1-3) are in [`16c-phase3-notes.md`](16c-phase3-notes.md), review round 7 is in [`16e-phase3-round7.md`](16e-phase3-round7.md), round 8 in [`16f-phase3-round8.md`](16f-phase3-round8.md), round 9 in [`16g-phase3-round9.md`](16g-phase3-round9.md), round 10 in [`16h-phase3-round10.md`](16h-phase3-round10.md), and round 11 in [`16i-phase3-round11.md`](16i-phase3-round11.md).**

Its recorded deviations and known limits are in [`16p-phase3-summary.md`](16p-phase3-summary.md).

## Phases 4 onwards

Each later phase's notes, review rounds and mutation tables are indexed in [`16o-phase-index.md`](16o-phase-index.md) (Phase 4, Phase 5, Phase 7, Phase 8.1, Phase 8.2, ...). Add new phase sections there.

## Phase 6x7: replayable agents

Notes and guarantees: [`16zb-p67-notes.md`](16zb-p67-notes.md). Review round 1 (a resume's live model-budget steps wait until every logged reservation has been acted out; a forged trailing grant diverges before its call is sent; log format version 3) and the known limits: [`16zc-p67-round1.md`](16zc-p67-round1.md).

## Phase 6: replay

Its notes, review rounds and known limits are indexed in the Phase 6 section of [`16o-phase-index.md`](16o-phase-index.md). Review round 3 (the fail-first race, host deadlines, forged logs that stall, shared values in the codec, the Phase 7 and 8.2 effects through the gate) is in [`16u-phase6-round3.md`](16u-phase6-round3.md). Review round 4 (host results that alias program values, a resume inside a model call, stall detection in a resume, a live failure ahead of a logged deadline, and their known limits) is in [`16v-phase6-round4.md`](16v-phase6-round4.md). Review round 5 (a forged `llm_model` in a resume, an honest log that stalled its replay through `liveErr`, forged logs that drain a resume, and their known limits) is in [`16w-phase6-round5.md`](16w-phase6-round5.md). Review round 6 (a task's end logged before its slot is freed, live waits on other tasks parked in a resume, and their known limits) is in [`16x-phase6-round6.md`](16x-phase6-round6.md). Review round 7 (log format version 2; the forged-resume known limit extended) is in [`16y-phase6-round7.md`](16y-phase6-round7.md).

**Mutation check, round 1** (2026-11-12; the full table is in [`16za-phase6-mutations.md`](16za-phase6-mutations.md)): 33 mutations covering I1, I2, I3, I5 and I7, record, replay, resume, tamper and truncation detection, header grants and goroutine leaks. 31 were caught, 16 of them by the e2e suite as well as by unit tests. Two were not caught:

| Guarantee | Mutation | Caught | Catching test |
|---|---|---|---|
| resume: same result (time) | `NewResumer` does not back-date the run's start, so `clock_ms` read after a resume can be lower than a reading the crashed run logged | no | none: no test reads `clock_ms` on both sides of a late cut |
| --replay: canonical header grants | `LogGrantsKinds` drops `c.Scope != g.Scope` | no | none: the "uncanonical" forge case (`127.0.0.1:0`) is rejected by `Literal` itself |

`TestHostDeadlineIsNotLogged` fails on the unmutated tree without `-race` when the machine is lightly loaded (with `-race` it passes): the 10 000-call depth limit is reached before its 20 ms host deadline. It is a timing flaw in the test, not a runtime bug (see 16za). *Fixed in Phase 6x7 ([`16zb`](16zb-p67-notes.md)): the test now loops over a bounded recursion.*

**Mutation check, round 2** (2026-11-12): this round re-checks only M10 and M24, the two mutants round 1 left uncaught. A fixer has since added tests for them (commit `605ce8f`). Each mutant was applied alone in `arvlang-wt/p6` on `phase/p6`. The run covered `go test -count=1 ./arv/...` and then `./e2e` and the CLI package (`.`). The mutant was then restored with `git checkout -- <file>`, and `git status` was clean before the next one. Nothing was committed. Load average was about 7, with 2.9 GB free.

| Guarantee | Mutation | Caught | Catching test |
|---|---|---|---|
| resume: same result (time) | M10: in `NewResumer` (`arv/runtime/effectlog.go`), `start: time.Now()` replaces the start back-dated by `log.at` | yes, unit only | `TestClockMSCountsTheCrashedRunsTimeAfterALateCut` (the resume printed `false` where the uninterrupted run printed `true`) |
| --replay: canonical header grants | M24: in `LogGrantsKinds` (`arv/logged.go`), `c.Scope != g.Scope` is dropped | yes, unit only | `TestForgedLogsAreRefused` cases "port not canonical" (`127.0.0.1:080`) and "prefix not canonical" (`::ffff:127.0.0.1/128`) |

**Result.** Both are caught. Combined with round 1, all 33 Phase 6 mutants are now caught. The e2e suite and the CLI package passed under both mutants, so each one is caught only by its unit test. `TestHostDeadlineIsNotLogged` failed on the unmutated tree too, in three runs out of three without `-race` (the script hits the 10 000-call depth limit before the 20 ms host deadline). This is the timing flaw in the test that round 1 recorded (fixed in Phase 6x7). It is not a runtime bug and is not related to either mutant.
