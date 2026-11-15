# Phase 8.2: e2e and hardening

Continues `16-phase82.md` (what was built). This file records the e2e cases, fuzz targets, property and leak tests added on `phase/p82` by the e2e and hardening pass, and what they found.

## E2E through a real host binary

Host effects exist only for Go hosts, so the arv CLI cannot exercise them (`TestP82CLIHasNoHostKinds` shows it knows no host kind). `e2e/p82host` is a Go host built by the suite (with `-race` under `ARV_E2E_RACE=1`) next to the arv binary. Each of its effects tests one guarantee:

| Effect (kind) | What it does |
|---|---|
| `k8s_apply(ns, m)` (k8s, exact) | returns `{namespace, applied}` |
| `team_read(path)` (team, subtree `Covers`) | returns `"read " + path` |
| `k8s_wait(ns)` | arrives at the server's gate `fn`, waits for `t.Ctx`, logs that it saw the cancel |
| `k8s_call(ns)` | calls the server with the host's secret, returns only the body's length |
| `k8s_fail`, `k8s_deny`, `k8s_panic` | an error, a `*runtime.DeniedError`, a panic |
| `k8s_big(ns, n)`, `k8s_echo(ns, x)`, `k8s_func(ns)` | an n-byte string, its argument, a builtin (not data) |

Every `Fn` appends `name(scope) task=ID` to a call log before anything else, so a test proves a denied call never reached `Fn`. After `arv.Run` the host waits (up to 20 s) for the goroutine count to return to its value before `Run` and exits 3 with every stack if it does not: every e2e case is a leak check. `-setup NAME` installs a deliberately bad setup.

| File | Cases |
|---|---|
| `e2e/p82_accept_test.go` | acceptance (denied, allowed, other namespace, matchable denial, no cross-kind cover); coverage (exact match without normalisation, subtree, scopes kept as written, empty and over-long scopes, a bad scope argument); 12 refused setups and 2 refused grants (nothing printed, no `Fn`); capability values give no authority; the host's secret never reaches output and a builtin result is refused; the CLI has no host kinds |
| `e2e/p82_invariants_test.go` | I2 (handed grant, no `with`, past the grant, a parent handing on what it lacks, subtree narrowing, a grandchild widening back, three levels, siblings); errors (effect, matched with kind, task and position; `DeniedError` as capability; panics at the root and in a child; a cut message; a restart retries an effect error 3 times with attempt IDs `root/0#2`, `#3`; a restart never retries a denial and `Fn` never runs; collect); budgets (run, scope and child steps; alloc_mb in a scope and in the run; results add up; an echoed 2^40-path DAG charged per node); cancellation ordered by gates (a failing sibling, eight waiting calls, a scope timeout, the run's timeout, a panicking child), I4 shown by each cancelled `Fn` logging before the host exits; 50 concurrent calls and echoed values unchanged (I8) |
| `e2e/p82_host_test.go` | (from the build) `examples/host` |

"Recorded and replayed" waited for Phase 6 (`16-phase82.md`, the seam). *Resolved in Phase 6x7 ([`16zb`](16zb-p67-notes.md)).*

## Fuzz targets

| Target | Package | Checks |
|---|---|---|
| `FuzzHostKindCovers` | `arv/runtime` | `NewKinds` accepts exactly the documented names; `Allows` matches an independent oracle for four reflexive, transitive rules (exact, subtree, domain suffix, any); grants of another host kind or a built-in kind never count; `Covers` is never called with an empty scope, nor for exact match; `Admit` agrees, keeps the scope as written and names the request when it denies; `Attenuate` agrees, the child keeps the registry, holds exactly the request and allows nothing the parent does not (I2); without the registry nothing of a host kind is allowed; host grants never change a built-in answer |
| `FuzzHostBindings` | `arv/evaluator` | `HostBindings` succeeds exactly when the effect names a declared host kind, has `Scope` and `Fn`, and every name is a bindable identifier (oracle written apart: its own keyword table); the bound effect reaches `Fn` only for a task holding a covering grant, never with built-in grants or a near-miss scope |
| `FuzzRunHostScope` | `arv` | end to end through `Run`, scopes reaching the program through an effect (any bytes, unescaped): a call reaches `Fn` exactly when the root's grant covers it, or, through `spawn ... with` (one and two levels), when the root covers the child's grant and that covers the call; every failure is an `*arv.Error`; no goroutine outlives `Run` |

Runs (`go test -run='^$' -fuzz='^NAME$' -fuzztime=2m -fuzzminimizetime=10s -parallel=4`, darwin/arm64, load average about 30):

| Target | Seconds | Execs | New interesting | Found |
|---|---|---|---|---|
| `FuzzHostKindCovers` | 120.4 | 10,339,644 | 71 | nothing |
| `FuzzHostBindings` | 120.7 | 4,095,741 | 189 | nothing |
| `FuzzRunHostScope` | 120.5 | 2,556,633 | 175 | nothing |

An earlier `FuzzHostKindCovers` run with the default 60 s minimisation spent most of its 121 s minimising (379,091 execs, nothing found), hence `-fuzzminimizetime=10s`. No crash, so no corpus entry to commit.

Mutation checks (made by hand, then reverted): `admitHost` skipping the cover check fails `FuzzHostKindCovers` and `TestPropAttenuateHostKindsNeverGrantsMore` on their seeds; `Kinds.covers` ignoring the request's kind and empty scope fails both and `TestAttenuateHostKindsNeverWidens`.

## Property and leak tests

- `TestPropAttenuateHostKindsNeverGrantsMore` (`arv/runtime/i2host_prop_test.go`): 20,000 random chains of up to five attenuations mixing two host kinds (random rules) and `env`. Every level allows nothing its parent, or the root, does not; `Attenuate` fails exactly when `Admit` refuses a request; a child holds only its requests. It complements `TestAttenuateHostKindsNeverWidens` (one level, fixed scopes).
- `TestNoGoroutineOutlivesRunWithHostEffects` (`arv/leak_p82_test.go`): scope, nested and run timeouts with up to 21 tasks waiting in `Fn`; a host result spending alloc_mb while siblings wait; a failing call failing waiting siblings; restart and collect over failing calls. After each run, calls entered equal calls returned (no `Fn` still running, I4) and no goroutine is left.

## Findings

No bug in the phase's code. Three things the e2e work ran into, none a runtime defect:

- **A race in the test host, not the runtime.** `k8s_wait` first returned the error of its `/arrive` request when the cancel it led to came before the response was read, so it never logged "cancelled" (7 of 8 in one run). The host now counts a cancel seen there too. The runtime did wait for every `Fn`: the host's goroutine check passed each time, and `TestNoGoroutineOutlivesRunWithHostEffects` counts calls entered against calls returned.
- **Capability values compare by identity**, built-in and host alike (`net("a") == net("a")` is false), so the e2e suite does not test equality of host capability values. This is the language as built, not a Phase 8.2 change.
- **A spawn denial reads `spawn in task T: capability denied: k(s) (holds: [...])`** and a restart's attempts are `root/0`, `root/0#2`, `root/0#3`; the e2e expectations use those forms.

Was open for the 6x7 integration: record and replay of `examples/host` and of `e2e/p82host` once host calls go through the Phase 6 gate. *Resolved in Phase 6x7 ([`16zb`](16zb-p67-notes.md)): `TestHostExampleRecordsAndReplays`, `TestP67HostRecordAndReplay`, `TestP67HostResumeAfterSIGKILL`.*
