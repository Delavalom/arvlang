# Arv security model and threat model

This is what Arv's runtime defends, against whom, and where its protection ends. It is the input to the external review planned in [`02-verification.md`](superpowers/plans/2026-11-15-arv-next/02-verification.md) §V7. The invariants it relies on are in [`invariants.md`](invariants.md). The detailed known limits, phase by phase, are in [`18a`](superpowers/plans/2026-11-09-arv-runtime/18a-phases-3-8-fuzzing-and-limits.md). Report vulnerabilities as [`SECURITY.md`](../SECURITY.md) says.

## What Arv is, and is not

Arv runs a program inside the host's process: the `arv` CLI, or a Go program that calls `arv.Run`. The runtime decides what the program can touch, how much it can spend, and that every task it starts ends inside its scope. It does this with capability checks at each builtin, budgets checked as the program runs, and one logged gate for every effect.

**Arv is not a kernel sandbox.** It does not use OS isolation (namespaces, seccomp, a VM). Its protection holds only as long as the Go runtime, the standard library, Arv's own code and the host's effects are correct. For untrusted code at scale, run Arv inside a container, gVisor or a microVM as well.

## Assets

| Asset | Example | Main guard |
|---|---|---|
| Host secrets | `ANTHROPIC_API_KEY`, other environment variables, files such as `~/.ssh` | No ambient authority (I1); `env` grants per variable; the model key is removed from the environment, and procfs reads are refused |
| Files outside the grants | anything not under an `fs_read` or `fs_write` grant | Canonical checks at use and `os.Root` opens (I3) |
| The internal network | loopback, RFC 1918, link-local, cloud metadata (`169.254.169.254`) | A dial-time address check; a name grant, even `*`, never reaches an internal address |
| Money | model tokens and US cents | The `tokens` and `usd_cents` budgets, reserved before each call (I6) |
| The host's availability | CPU, memory, tasks, wall time | The `steps`, `alloc_mb`, `tasks` and `timeout_ms` budgets; I4 (no task outlives its scope) |
| The record of what happened | the effect log | A SHA-256 hash chain, file mode 0600, and a replay that diverges on any mismatch (I7) |

## Attackers

| Attacker | Controls | Wants | Main defence |
|---|---|---|---|
| **A1. Malicious script author** | The whole `.arv` program | To read secrets, reach the network or files outside the grants, or exhaust the host | I1, I2, I3, the budgets, `arv check` and `--manifest` before running |
| **A2. Prompt-injected model** | Model replies and tool calls inside `ask` and `agent` | To call tools it was not given, exfiltrate data, overspend | Each tool is a grant; a tool's effects need their own grants; redaction keeps grants, task IDs, paths and internal addresses out of prompts; model budgets; `max_turns` |
| **A3. Malicious data source** | HTTP bodies, file contents, tool results | To inject instructions into a model, or crash the parser | The same as A2, plus bounded reads (10 MiB files, 16 MiB replies) and fuzzed parsers |
| **A4. Forged or edited log** | A log file passed to `--replay` or `--resume` | To make a replay print anything, or raise a resume's limits | A hash chain plus divergence checks. Accepted gaps listed below; signed logs are roadmap 13a |
| **A5. Malicious module** (Phase 12, not built yet) | Imported Arv code | To use authority its importer holds | Planned: an imported module holds nothing it is not passed |

**Out of scope.** These have full authority already, or sit below the runtime:
- the host's own Go code, including host effects registered through `arv.Options.Effects`;
- the OS kernel, and a privileged local user;
- a writer outside every grant who swaps an ancestor of the outermost grant (18a, Phase 8.3);
- a model provider that lies about usage;
- side channels other than those listed below.

## What a malicious script cannot do (and the tests that show it)

| Attempt | Result | Shown by |
|---|---|---|
| Read a file with no grant, or outside its grant | `capability denied` | `e2e` grant tests; `TestUseTimeDenialRevealsNothing` |
| Escape with `..`, a symlink, or a directory swapped mid-run | Refused; files open beneath the grant | `TestReadFileDirSwapCannotEscape`, `TestNestedGrantDirSwapCannotEscape` |
| Reach the metadata service, loopback, or a name that rebinds inside | Refused at dial time | `TestNetHostForms`, the 8.3 rebinding tests |
| Read the model key | Not in the environment; procfs refused; a reply that echoes it refused | Phase 7 key tests (`arv/llm/keycheck_test.go`) |
| Widen a child's grants at `spawn` | Refused unless the parent holds them | `TestSpawnAttenuatesAgentGrants`, `TestHostEffectAttenuates` |
| Spawn without limit, loop forever, churn memory, overspend | `tasks` waits; `steps` and `timeout_ms`, `alloc_mb`, `tokens` and `usd_cents` fail with kind `budget` | `TestStepsExhaustionCancelsTasks`, `TestAllocExhaustionCancelsTasks`, Phase 7 budget tests |
| Make a model call a tool it was not given | The model gets a `capability denied` tool result, and the run goes on | Phase 7 agent tests |
| Learn its grants through a model | Denials reaching a model are cut to what was asked for | Phase 7 redaction tests |
| Exfiltrate the Phase 5 gate's canary through a changed script | Denied, 0 bytes sent (Go: 29 of 29 bytes sent) | `bench/escape`, [`gate-report.md`](superpowers/plans/2026-11-09-arv-runtime/gate-report.md) |

## Known gaps (accepted or open)

Each line names where it is recorded. None is a failing test. Each says what a guarantee does not cover.

**Files**
- An fs grant that covers a file holding a secret (a `.env`, a shell profile) reads it. Grant narrowly (README, "Agents").
- A hard link inside a grant to a file outside it is followed (18a, 8.3).
- A directory without the read bit fails closed under `os.Root` (18a, 8.3; roadmap A13).
- An admitted request shows, through `alloc_mb`, whether an outside name is a symlink and about how long its target is. The owner chose to keep this (18a, Phase 3).
- The uncharged walk of a denied request may be a timing oracle through `timeout_ms` (18a, Phase 3).

**Network**
- A name grant cannot be limited to IP addresses (18a, 8.3).
- `net=fe80::1` reaches that address on any interface: the dial check drops the zone (18a, 8.3).

**Model key**
- An embedder that grants `env` must unset the key itself (README).
- NetBSD procfs is not checked (18a, Phase 7).
- The reply key check is defence in depth, not a boundary.

**Logs**
- The hash chain is not a signature. A forged log can make a replay print anything the program could print.
- A forged header sets the limits of a resume's live part, money budgets included.
- A forged spawn record can widen a child up to its parent's grants.

  The owner accepted this risk class (18a, Phases 6 and 6x7). Roadmap 13a adds signed logs.
- The log can hold secrets: `getenv` values, response bodies, and request headers once `http` exists. The file is 0600.

**Memory and CPU**
- `alloc_mb` bounds allocation churn, not live memory (18a, Phase 3; roadmap 13c).
- Some fixed costs are not charged (18a, Phases 3, 7, 8.2).
- Compiling is not cancellable (roadmap A7).

**Concurrency**
- `restart` re-runs effects, so it is for idempotent work only (README).

## The review we want

Give a reviewer this file, `invariants.md`, the README and the code paths named in `invariants.md`. Ask them to break:
1. grant parsing and canonicalisation;
2. the `os.Root` walk and the swap races;
3. the dial-time address check (SSRF, rebinding, IPv6 forms, IPv4-mapped and IPv4-compatible addresses);
4. key handling and redaction;
5. replay and resume with forged logs, before and after signed logs;
6. budgets as a denial-of-service boundary;
7. later, module isolation and code mode.

Findings go into this file's gap list or get fixed, with a test either way.
