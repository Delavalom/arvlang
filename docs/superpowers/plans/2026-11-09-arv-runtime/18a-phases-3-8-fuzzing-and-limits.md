# Phases 3-8 report, part 2: fuzzing and known limits

Part 1 is [`18-phases-3-8-report.md`](18-phases-3-8-report.md). This part collects, from the per-phase notes, the fuzzing done and the known limits, and lists the changes to the Phases 0-2 e2e cases. Each line names the note it comes from. The notes are the record; this file only gathers them.

## Fuzzing per target

Times are wall seconds of `-fuzztime`. "n/r" means the note gives no number. Unless a row says otherwise, the run found nothing.

| Phase | Target | Seconds | Execs | Found | Note |
|---|---|---|---|---|---|
| 3 | `FuzzCanonical` | 60 | n/r | nothing | 16f |
| 3 | `FuzzAdmit` | 45, then 30 | n/r | nothing | 16f, 16h |
| 3 | `FuzzNetCovers` | 30 | n/r | nothing | 16h |
| 8.3 | `FuzzCanonical` | 120 | 6,594,482 | nothing | 16b |
| 8.3 | `FuzzAdmit` | 120 | 9,713,086 | nothing | 16b |
| 8.3 | `FuzzNetCovers` | 120 | 23,237,841 | nothing | 16b |
| 8.3 | `FuzzNetScope` | 120 | 22,450,847 | `net("..")` and `"host.."` normalised to forms that were not idempotent (fixed) | 16b |
| 4 | `FuzzMatchSyntax` | 120 | 2,285,807 | nothing (after a harness fix) | 16k |
| 4 | `FuzzScopeOptionsSyntax` | 120 | 3,601,196 | in 2 s: the parser accepted a block left open at EOF, a bug from before Phase 3 (fixed) | 16k |
| 4 | `FuzzRunSupervise` | 120 | 1,199,900 | nothing | 16k |
| 4 | `FuzzCollectTree` | 120 | 381,527 | nothing | 16k |
| 5 | none | | | | 16m, 16n |
| 8.1 | `FuzzCheck` | 120, then 125 | 1,959,109, then 2,993,301 | nothing | 16p81b, 16p81d |
| 8.1 | `FuzzCheckNoEffects` | 126 | 331,812 | nothing | 16p81d |
| 8.1 | `FuzzCheckSound` | 126 | 69,354 | two defects in the test oracle (kept as corpus) | 16p81d |
| 8.1 | `FuzzSoundGen` | 125 | 138,958 | nothing | 16p81d |
| 8.2 | `FuzzHostKindCovers` | 120.4 | 10,339,644 | nothing | 16-phase82-hardening |
| 8.2 | `FuzzHostBindings` | 120.7 | 4,095,741 | nothing | 16-phase82-hardening |
| 8.2 | `FuzzRunHostScope` | 120.5 | 2,556,633 | nothing | 16-phase82-hardening |
| 7 | `FuzzAnthropicReply` | 120 | 2,855,594 | nothing | 16r-phase7 |
| 7 | `FuzzAgentToolCall` | 120 | 2,815,452 | one false positive in the test (fixed, kept as a seed) | 16r-phase7 |
| 7 | `FuzzToolArgs` | 120 | 5,350,251 | nothing | 16r-phase7 |
| 6 | `FuzzForgedReplay` | 90, then 124 | about 700k, then 2,369,184 | nothing | 16q-phase6, 16z |
| 6 | `FuzzCodecRoundTrip` | 121 | 3,175,409 | nothing | 16z |
| 6 | `FuzzDecode` | 122 | 3,105,611 | nothing | 16z |
| 6 | `FuzzReadLogTamper` | 121 | 4,411,147 | nothing | 16z |
| 6 | `FuzzReadLog` | 122 | 10,257,200 | nothing | 16z |
| 5E | `FuzzLexerOperators` | 120 | 27,795,052 | nothing | 16pe-hardening |
| 5E | `FuzzLexerNewAt` | 120 | 16,019,443 | nothing | 16pe-hardening |
| 5E | `FuzzLogicalPrecedence` | 121 | 1,444,203 | nothing | 16pe-hardening |
| 5E | `FuzzInterpolationParse` | 121 | 1,929,491 | nothing | 16pe-hardening |
| 5E | `FuzzDesugarEquivalence` | 121 | 194,965 | nothing | 16pe-hardening |
| 6x7 | `FuzzReadLogAgentRecords` | 130.4 | 4,446,669 | nothing | 16ze |
| 6x7 | `FuzzForgedAgentReplay` | 131.6 | 27,858 | nothing | 16ze |
| 6x7 | `FuzzForgedAgentResume` | 131.4 | 1,667,868 | nothing (a first run of 71 execs under load was discarded) | 16ze |
| 8.4 | `FuzzEnginesAgree` | 61, then 121.5 | 16,172, then 55,031 | nothing | 16p84, 16p84d |
| 8.4 | `FuzzEnginesAgreeSource` | 121.4 | 2,583,929 | nothing | 16p84d |
| 8.4 | `FuzzEnginesAgreeStatements` | 121.5 | 108,898 | nothing | 16p84d |
| 8.4 | `FuzzDesugarEquivalence` | 120.7 | 180,722 | nothing | 16p84d |
| 8.5 | `FuzzPseudoRevision` | 28, then 121.4 | n/r, then 6,627,129 | first run: a regexp in the test oracle lacked `(?s)` (fixed) | 16p85b |
| 8.5 | `FuzzVersionText` | 120.5 | 6,819,125 | nothing | 16p85b |

In all: 36 distinct targets and about 4,980 s (83 minutes) of recorded fuzzing, counting each listed run once. Fuzzing found two product bugs (the EOF block in Phase 4, the `..` host in Phase 8.3), both fixed. Every other hit was in a test oracle or harness.

**`FuzzMatchSyntax` harness bug (open).** 16pe-ergonomics reports that `FuzzMatchSyntax` fails within a minute, on the base too. The cause is the harness: `isName` accepts a part that holds a NUL byte, and the NUL ends the composed program. The parser is right. No later note records a fix. See part 1, "Open findings".

## Known limits, by phase

### Phase 3: budgets (16p-phase3-summary, 16f to 16i)

- A timeout is reported late for work that ignores cancellation. Pure Arv work can run up to 256 nodes past it. No effect starts after the deadline.
- `steps` does not bound the syscalls of one effect. A deep path costs hundreds of lstats, about 30 ms.
- `alloc_mb` bounds churn, not live memory. Live memory is a constant factor of the limit plus about 100 MiB at 1000 tasks.
- Some fixed costs are not charged: about 570 B per non-fs spawn request (41 B charged), about 25 KiB of net/http per `http_get`, and the nested-grant `os.Root` walk (about 2.5x the canonicalisation charge).
- A host builtin set as a plain `Fn` is charged only after it returns. `puts` text is charged in 64 KiB steps, so a run can go 64 KiB past the limit.
- The REPL runs without budgets.
- The depth pool bounds total depth, not the number of deep tasks: about 1.3 GB at `--tasks 8000`.
- A root-level panic is placed at the program's last statement.
- A denied fs request pays as if every component existed: 5.5 MB for a 4 KB path of 2000 components. The part of a symlink's cost past the prepaid bound is not charged on a denial.
- An admitted request shows, through `alloc_mb`, whether an outside name is a symlink and about how long its target is. The owner chose to keep this (16i).
- The uncharged walk of a denied request may be a timing oracle through `timeout_ms`.

### Phase 8.3: network hardening (16-implementation-notes, 16b)

- `os.Root` needs the read bit on every directory it walks. A search-only (`0711`, `0100`) or `0300` directory fails closed with `permission denied`. A fix needs a search-only `openat` walk.
- A hard link inside a grant to a file outside it is still followed.
- A writer outside every grant that swaps an ancestor of the outermost grant is outside the threat model.
- A name grant cannot be restricted to IP addresses. A dial-time denial names the internal address (Phase 7 redacts it).
- Each `http_get` opens a fresh connection: no keep-alive, no proxy.
- Teredo addresses are refused as a class. The dial check drops the zone, so `net=fe80::1` reaches it on any interface.
- During a race, a swap to a relative symlink that stays inside the grant is followed.
- The e2e swap-race test is probabilistic. `0177.0.0.1` is not in the e2e suite.

### Phase 4: supervision (16j, 16l)

- Under `fail_fast`, `match` cannot rescue a task's failure. Use `collect`.
- No error is caught inside a budget that has run out.
- String `==` costs one step, but its CPU grows with length.
- A cancel during a restart backoff fails as `cancelled`, and the last attempt's error is lost.
- `collect` reports unawaited failures only when the scope would otherwise succeed. `await` of an array under `collect` stops at its first failure.
- Restart re-runs effects, so it is for idempotent work only. `retry_on` accepts only `effect` and `user`.
- Each restart attempt has about 1.35 KB of uncharged overhead. The clock is process-wide.

### Phase 5: gate builtins and farm (16m, 16n)

- `json` charges in 64 KiB chunks and copies the text once more at the end. It does not escape `<`, `>`, `&`, U+2028 or U+2029. Nesting past 10000 levels is an error.
- `lines` and `split` pieces keep the whole text alive. `split` refuses an empty separator.
- `clock_ms` needs no capability and is nondeterministic input (logged since Phase 6).
- `farm.Expected` is wrong for farms slower than the 500 ms cap.
- Root `go test ./...` does not run `bench/uptime-go`'s tests.
- A target outside the grants stops the Arv run before `report.json` is written, while Go reports it as down.

### Phase 8.1: arv check (16p81, 16p81b, 16p81c, 16p81d)

- Redirect hosts are not reported. Dead code is reported.
- A name that resolves to an internal address gets only the name grant.
- Host effects (8.2) are not seen by the CLI check. Notes do not stop `--manifest`.
- With a header, `arv check` lstats named paths. Under `/net/<host>` on macOS this can wake the automounter (not reproduced).
- The `--manifest` pre-check runs before `--timeout` starts (2.1-2.4 s for a 7.9 MB script).
- `read_file("missing/../x")` prints a grant that `--allow` refuses. It fails closed.
- The "too long once resolved" note depends on the working directory.

### Phase 8.2: host effects (16-phase82)

- A host `Fn` that ignores `t.Ctx` delays its scope's end.
- A result is charged only after `Fn` returns, plus about 104 B of uncharged closures per call. `hostWalk`'s memo costs about 126 B per node.
- A result belongs to the run once returned. A host that mutates it races with readers.
- `Scope`, `Fn` and `Covers` must be safe for concurrent use. `Covers` must be reflexive and transitive. `Fn` must not change its arguments.
- Host kinds are not available to `arv run --allow`, and `arv check` does not know them.

### Phase 7: LLM, tools, agents (16o-phase7)

- A failed call is charged its whole reservation. A provider that reports zero usage settles to zero.
- The input estimate counts bytes, not tokens. Concurrent calls near a limit can both be refused.
- Redaction works on text only. A denial the program took apart is not found. Text after the first denial is dropped. Effect errors inside a grant keep their absolute path.
- Prices are copied by hand (2026-11-11). Cache pricing is not modelled.
- Tools take strings only. `max_tokens` is 4096 for every call, with no `thinking` parameter.
- Key exposure: NetBSD procfs is not checked. An fs grant over a file that holds the key still reads it. An embedder that grants `env` must unset the key. The reply key check is defence in depth, not a boundary.
- About 950 B uncharged per `ask`. Replies are read up to 16 MiB.

### Phase 6: replay and resume (16o-phase6, 16o-phase-index, 16r, 16u to 16y)

- `steps` and `alloc_mb` races, and late timeouts, are detected (a divergence), not reproduced.
- The cwd is read, not logged. The CLI warns when it differs. An embedder must compare `Header.Cwd` itself.
- The hash chain is not a signature. A forged log can make a replay print anything the program could print. A forged header sets every limit for a resume's live part, money budgets included. A forged spawn record can widen a child up to its parent's grants.
- The log may hold secrets (`getenv` values, bodies). The file is 0600.
- Quiet checks can let a resumed task print or write after a sibling's failure. Resumed deadlines and open times are bounds. Output order can change across a resume.
- A deadline the log does not name is not ordered by it. A slot can stay held across a resume.
- Recording a result costs about three uncharged copies. Model requests are logged whole on every turn, so the log grows with the square of the turns.
- A live effect in a resume can run before a logged deadline fires. A resume needs the crashed run's host model configuration.
- A forged resume can run until the header's `timeout_ms` and then report `budget exceeded: timeout`, not a divergence. Log format versions 1 and 2 are refused (now version 3).

### Phase 5E: ergonomics (16pe-ergonomics, 16pe-hardening)

- `str` of a large non-string value allocates about 6.8x its text while it runs.
- No string literal can appear inside `${...}`.
- `${}` of a string costs 2 steps more than `+` (11 against 9).
- After a runtime error the root scope does not charge its last partial chunk of steps.

### Phase 6x7: replayable agents (16zb, 16zc, 16zd, 16ze)

- A live model step in a resume waits for every logged reservation, even in an unrelated budget.
- A replayed trailing grant is checked against the level plus failed-settlement excess. A forged grant up to 750 tokens was not caught in the probe.
- A forged log can still raise what a resume's live part spends, in four ways (drop records, lift a header limit, lower usage, mark a settlement failed). The owner accepted this risk class.
- About 3.1 KB uncharged per limited model call.
- The concurrent-budget tests rely on CPU timing, so under load a regression could pass.
- The 200-program property test is not extended to agents.

### Phase 8.4: speed (16p84, 16p84b, 16p84d)

- The gain is 1.24x to 1.69x, not the design's 2-5x. Slot resolution, lock-free reads and a bytecode VM were not done.
- `spawn`, `await`, scope options and functions written inside them run on the tree-walker.
- Compiled code is not charged to `alloc_mb`. Compiling is not cancellable (1.30-1.84 s for a 17 MB program).
- Shared small integers are immutable by convention, not by code.

### Phase 8.5: distribution (16p85, 16p85b)

- A plain `go build` in a git worktree is not stamped. `go install` of a tag prints `commit unknown`.
- `goreleaser check` was never run. Release builds are CGO-off.
- `TestP85GoInstallLatest` uses a local proxy. Other-architecture binaries are checked for format, not run.

### Final fixes and test environment (16zg)

- A `return` inside a `let` value, a call argument or `${...}` does not leave the function. Both engines agree. This is base behaviour.
- On darwin a race-built test binary can crash (exit 66, `ThreadSanitizer: CHECK failed`, or `signal: segmentation fault` with empty stderr) or hang in fork. Run long `-race` suites with `-timeout`.

## Phases 0-2 e2e changes

Every change that `git diff origin/main feat/phases-3-8 -- e2e/` makes to a test file or script that existed on `origin/main`, with the note that gives its reason.

| Change | Reason (written in) |
|---|---|
| `TestNetGrantValidation`: `net=127.0.0.1:1234` now accepted; `:0` and `:65536` rejected | Ports are part of a grant (Phase 8.3; [16-implementation-notes](16-implementation-notes.md) §8.3) |
| `TestNet`, `TestNetHostForms`: denials name `host:PORT` | A request names the port it reaches (same table) |
| `TestNet` wildcard case: `net=*` plus `net=127.0.0.1`, through `localhost` | `*` covers no internal IP, and a loopback dial needs an IP grant (same table) |
| `TestNetHostForms` uppercase and trailing-dot cases: plus `net=127.0.0.1` | Only an IP grant may dial loopback (same table) |
| `TestAwaitCycles` (three cases) and `robust_await_mutual.arv` rewritten | Environments are frozen at spawn (I8), so a task cannot name its own handle or a later sibling's (Phase 4; [16j](16j-phase4-notes.md)) |
| `harness_test.go`: builds `cmd/arv`, tracks connections, `spec.bin` | Not an expectation. The command moved (Phase 8.5), connections are drained (16zg), one engine per binary (Phase 8.4) |
| `gates_test.go`: two more route sets | Not an expectation. New routes for Phases 3 and 4 |
