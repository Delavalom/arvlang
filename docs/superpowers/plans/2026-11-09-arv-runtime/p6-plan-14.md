# Phase 6, task p6.14: Docs: README, Phase 6 notes

> **For agentic workers:** this file is one task, p6.14. Do it test first, exactly as written; if something here does not match the code you find, stop and report instead of improvising. Prerequisite: tasks p6.1 to p6.13 are committed in your tree. Start every shell command with `cd <your worktree> &&`. Before committing: `gofmt -l .` prints nothing, `go vet ./...` is clean and `go test -race ./...` is green. End the commit message with a blank line and then `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`. Never use `--no-verify`; keep Go files under 15 KB and markdown under 18 KB. `go.mod` says `go 1.24.0` (toolchain 1.26.2), so do not use `testing/synctest` or `sync.WaitGroup.Go`. `arv/runtime` stays stdlib-only.
>
> Every code block below was applied, in task order, to a scratch copy of the tree and passed `go vet ./...` and `go test -race ./...` (including `e2e/`). Design: `13-phase6-durable-replay.md`. Deviations from it, and why: `p6-plan-1.md`.

### Task p6.14: Docs: README, Phase 6 notes

The README gets a "Record, replay and resume" section and the new flags; `16o-phase6-notes.md` records what Phase 6 built, its deviations from the design and its known limits (with the measured numbers); `16-implementation-notes.md` points to it. `16-implementation-notes.md` is near 18 KB: add only the pointer.

**Files:**
- Modify: `README.md`
- Modify: `docs/superpowers/plans/2026-11-09-arv-runtime/16-implementation-notes.md`
- Create: `docs/superpowers/plans/2026-11-09-arv-runtime/16o-phase6-notes.md`
- [ ] **Step 1: Write the docs**

In `README.md`, replace:

````markdown
```bash
bin/arv                          # REPL, no capabilities
bin/arv run [--allow kind=scope]... [--timeout 30s] [--steps N] [--tasks N] [--alloc-mb N] FILE
```

````

with:

````markdown
```bash
bin/arv                          # REPL, no capabilities
bin/arv run [--allow kind=scope]... [--timeout 30s] [--steps N] [--tasks N] [--alloc-mb N] [--record|--replay|--resume LOG] [--fsync] FILE
```

````

In `README.md`, replace:

```markdown
**Restart is for idempotent work:** every attempt runs the task's effects again, so a task that writes a file writes it again. Attempts share the scope's budgets; restarting refunds nothing. A spawned task sees the variables it captured as they were at `spawn`, in every attempt: a later `let` in the parent never reaches it.

## Development

```

with:

````markdown
**Restart is for idempotent work:** every attempt runs the task's effects again, so a task that writes a file writes it again. Attempts share the scope's budgets; restarting refunds nothing. A spawned task sees the variables it captured as they were at `spawn`, in every attempt: a later `let` in the parent never reaches it.

## Record, replay and resume

Every effect goes through one gate that can log it: the network, files, the environment, `sleep`, `now()`, `random(n)`, `clock_ms()` and `puts`.

```bash
bin/arv run --record run.jsonl --allow net=api.github.com script.arv   # log every effect
bin/arv run --replay run.jsonl script.arv                              # nothing runs live
bin/arv run --resume run.jsonl --allow net=api.github.com script.arv   # replay, then go on live
```

`--replay` answers every effect from the log: no request, no file, no waiting, and the same output byte for byte. It takes the grants and limits from the log and prints the grants, so it needs no `--allow`. A program that asks for something else stops: `replay diverged at task root/0, effect 1: the log has http_get("…/a"), the program asked for http_get("…/b")`. `--resume` continues a run that was killed. It replays each task while the log has records for it, then runs that task live and appends to the log. It needs the grants the run was recorded with. `--fsync` syncs the log after every line, so a crash loses no logged effect. A log that was edited, cut or reordered is refused (each line chains a SHA-256 of the one before). The chain is not a signature. The log can hold secrets, such as `getenv` values, so it is created with mode 0600.

## Development

````

In `docs/superpowers/plans/2026-11-09-arv-runtime/16-implementation-notes.md`, replace:

```markdown

Notes: [`16m`](16m-phase5-notes.md). Review round 1: [`16n`](16n-phase5-round1.md).
```

with:

```markdown

Notes: [`16m`](16m-phase5-notes.md). Review round 1: [`16n`](16n-phase5-round1.md).

## Phase 6: replay

Notes: [`16o`](16o-phase6-notes.md).
```

Create `docs/superpowers/plans/2026-11-09-arv-runtime/16o-phase6-notes.md`:

````markdown
# Phase 6: record, replay, resume (as built)

Built from `p6-plan-1.md` to `p6-plan-14.md`. This file records what those tasks built, how it differs from `13-phase6-durable-replay.md`, and its known limits. Review rounds and the mutation table for this phase go below, or in a continuation file once this one nears 18 KB.

**What Phase 6 adds:**
- **Ordered hashes** (`arv/value`): `Hash.Set`, `Keys`, `NewHashSize`. A hash literal evaluates its pairs in source order and prints in that order; `NewHash` records are sorted; `json`'s first bad key is the first in order.
- **The value codec** (`value.Encode`, `Decode`, `EncodeRequest`).
- **The log file** (`runtime.LogRecord`, `Header`, `ReadLog`), the **gate** (`runtime.EffectLog`, `Task.Effect`, `Task.Print`) and the **checks** (`Task.Err`, `WaitFor`, `Acquire`, `Expired`, `Backoff`; `Scope.SuperviseWith`).
- **The evaluator side**: `value.Builtin.Effect`, `Interpreter.effect` and `callEffect`, `now()`, `random(n)`, and the `spawn` effect (`attenuate`, `loggedCaps`).
- **The host side**: `arv.Options.Record`, `RecordSync`, `Replay` and `Resume`, plus the CLI flags `--record`, `--replay`, `--resume` and `--fsync`.

## The log

```
{"v":1,"header":{"script_sha256":"12af…","grants":[{"kind":"net","scope":"127.0.0.1"}],"limits":{"timeout_ms":600000},"cwd":"/work","started":"2026-11-11T23:01:04Z"},"h":"6ff7…"}
{"task":"root","seq":1,"kind":"spawn","req":["net(\"127.0.0.1\")"],"res":[["net","127.0.0.1"]],"charged":41,"h":"627e…"}
{"task":"root","seq":2,"kind":"puts","req":["started"],"out":[0],"h":"0824…"}
{"task":"root/0","seq":1,"kind":"http_get","req":["http://127.0.0.1:18777/"],"res":{"hash":[["body","…"],["status",200]]},"charged":168512,"h":"…"}
{"task":"root","seq":3,"kind":"puts","req":["200"],"out":[1],"h":"76fe…"}
{"end":{"result":[null]},"h":"8dcf…"}
```

A check line looks like `{"task":"root/1","seq":3,"kind":"cancelled","check":7,"h":"…"}`. Each line between the header and the end holds one effect or one check of one task, keyed by (task ID, per-task `seq`). `h` is SHA-256 over the previous line's `h` and this line without `h`.

## Deviations from `13-phase6-durable-replay.md`, and why

- **Task 2, freeze at spawn, was already done.** Phase 4 built it transitively with horizons (`runtime.Task.Horizon`, `Environment.GetAs`). `arv/evaluator/freeze_test.go` covers grandchildren. No Phase 6 task repeats it.
- **The header is `{"v":1,"header":{...}}`,** not `"header":true` beside the fields. The grants are `{"kind","scope"}` objects, so a reader does not parse `net("…")`. The header also holds the limits and the working directory: a replay or resume runs under the recorded limits, and relative paths depend on the directory.
- **There is no `err` field.** An effect's error is a value, so it is encoded in `res` (`{"error":[message,kind,pos,task]}`). The run's own result, error or not, is in the `end` line.
- **The codec writes a hash as `{"hash":[[k,v],...]}`,** in key order, because keys need not be strings. A string that is not valid UTF-8 is `{"bytes":"<base64>"}`. Floats and duplicate keys are rejected.
- **The gate is `effect(kind, args, do func(charge func(int64) error) value.Object)`** in `arv/evaluator/gate.go`. `do` takes the charge function, so the bytes an effect charges to `alloc_mb` are logged (`charged`) and charged again on replay.
- **Checks are logged.** Some outcomes depend on timing: a cancellation seen at `Tick` or before a builtin, an `await` cut short, the live-task cap, a scope deadline at close, a restart backoff cut short. Each is a numbered check, logged only when it saw something. A replay sees a cancellation at the same check, after this run's own cancellation has happened. Without checks, `fail_fast` output differs from run to run.
- **`spawn` is an effect.** Canonicalising its grants calls lstat and readlink, which is OS input (the 16-notes item on I7). A replay takes the child's grants from the log and checks each with `runtime.Literal` and `Caps.Allows`, so a forged log cannot widen them (I2).
- **Output is ordered globally.** A `puts` record holds each line's index in the run's output, and a replay prints lines in that order.
- **Added: the `end` line and the hash chain.** A replay compares its result with the end line and must use every record. The chain detects edited, dropped and reordered lines.
- **A changed script is a warning,** not an error, so the replay shows where it diverges. The divergence names the task and the effect: `replay diverged at task root/0, effect 1: the log has http_get("…/a"), the program asked for http_get("…/zen")`.
- **`random(n)` takes a bound,** and `clock_ms` is an effect too. `now()` is Unix milliseconds.
- **Restart attempts are `root/0`, `root/0#2`, `root/0#3`,** as Phase 4 names them.
- **`--replay` refuses `--allow` and the limit flags.** It installs the header's grants through `runtime.Literal`, which does not touch the filesystem. `--resume` needs the recorded grants and refuses limit flags. Resuming a finished log replays it.
- **The acceptance bound "replay in < 50 ms" is not asserted.** Starting a process takes longer than that under a load average of 300. In process, a replay of a 2 s sleep must finish in under 2 s. Through the binary, a replay of a 6 s sleep must finish in under 6 s.

## The 16-notes follow-through

- Freeze is transitive: done in Phase 4 (above).
- Spawn-time lstat goes through the gate as the `spawn` effect (above).
- `--replay` without `--allow` installs the header's grants and prints them: `replay: grants: net("127.0.0.1")`.

## Known limits

- **Some outcomes are detected, not reproduced.** If two tasks in one `fail_fast` scope really fail at once, a shared budget runs out under concurrency, or a timeout arrives late, the replay can reach a different first failure. The `end` comparison then fails it as a divergence. It does not print wrong output silently.
- **A replay of a timed-out run waits for the real deadline.** A check that recorded a cancellation waits for this run's own cancellation.
- **The working directory is read, not logged.** Capability constructors resolve relative paths against it, so they are pure only within one directory. The header records the directory, and the CLI warns when it differs.
- **The hash chain is not a signature.** Whoever can write the file can recompute it. A forged log can make a replay print anything the program could print, or wait until the host cancels it. It cannot widen grants or run an effect live.
- **The log may hold secrets.** `getenv` values and response bodies are logged. The CLI creates the file with mode 0600.
- **Restart re-runs effects (at least once).** A resumed task re-runs the effect it was in when the run was killed.
- **Minor, uncharged (`alloc_mb`):** each hash pair keeps its key twice, 24 bytes more than the 48 it is charged (`hashPairSize` stays 48: raising it changed the alloc tests' exact numbers). `EncodeRequest` copies an effect's arguments, bounded by values already charged, and frees the copy after the effect. `ReadLog` holds the whole log in memory and allocates about 2.7 KB per record while it reads (measured: 20000 `puts` records, a 2.9 MB file).
- **A load-sensitive test outside Phase 6:** with the whole suite under `-race` at a load average above 100, `TestConcurrentConcatStopsAtAllocLimit` failed twice in about 20 full runs (`charged_operands` once, `free_literal` once). A `spawn` charge failed first, because the 300 ms sleeps ended before all 500 tasks were spawned. Those runs keep no log, and every code path they take is unchanged by Phase 6. Run alone, or twelve times beside the base tree's copy, it passes.
````

- [ ] **Step 2: Run the tests, then the full checks**

Run: `cd <worktree> && gofmt -l . && go vet ./... && go test -race ./...`

Expected: `ok` for every package, and `gofmt -l .` prints nothing.

- [ ] **Step 3: Commit**

```bash
cd <worktree> && git add README.md docs/superpowers/plans/2026-11-09-arv-runtime/16-implementation-notes.md docs/superpowers/plans/2026-11-09-arv-runtime/16o-phase6-notes.md && git commit -F - <<'MSG'
docs: Phase 6 record, replay and resume; 16o notes

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
MSG
```
