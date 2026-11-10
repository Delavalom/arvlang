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
