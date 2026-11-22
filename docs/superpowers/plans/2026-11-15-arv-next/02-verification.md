# Verification: from tested to proven

Part of [`00-roadmap.md`](00-roadmap.md).

**Where things stand today.** The invariants are tested well:
- 1,142 top-level tests passed five times under `-race`.
- 36 fuzz targets ran for about 83 recorded minutes.
- 431 mutants were tried in round 1; after the follow-up rounds all are caught except one equivalent (18 §Mutation testing).

That is evidence for the cases someone thought to try. It is not proof. The A1 `return` bug survived all of it, because no test, fuzzer or mutant put a `return` in a value position. A written semantics would have had to give that case a rule.

**What "proven" means here.** For each invariant I1 to I8:
1. a precise statement over a formal model of Arv;
2. a machine-checked proof on that model, or for the concurrent parts an exhaustive model check up to stated bounds;
3. evidence that the Go implementation behaves like the model: differential testing against a reference interpreter built from the model, and deterministic simulation;
4. continuous checks that keep 1 to 3 true as the code changes.

A proof about the Go code itself is out of reach: there is no verified Go toolchain. The honest claim is **"proved on the spec, with conformance tested in the implementation"**, which is the WebAssembly model ([SpecTec](https://webassembly.org/news/2025-03-27-spectec/), [WasmCert](https://awesome.ecosyste.ms/projects/github.com%2Fwasmcert%2Fwasmcert-coq)).

## V0. Traceability matrix and threat model (S, Phase 9)

- **Matrix.** `docs/invariants.md` lists, for each invariant: its statement, the semantics rules that carry it (V1), the tests tagged `// In`, the mutants that target it (V5), and the fuzz targets that reach it.
  - A script counts the tags. Today's counts: I1 13, I2 31, I3 16, I4 16, I5 14, I6 31, I7 31, I8 9.
  - CI fails if any invariant has no test or no mutant.
- **Threat model.** `docs/security-model.md`.
  - **Assets:** host secrets, files outside the grants, the internal network, money (tokens and cents), and the host process.
  - **Attackers:** a malicious script author, a prompt-injected model, a malicious imported module (Phase 12), and a forged log.
  - **Out of scope:** the host's own Go code, the OS kernel, and a writer outside every grant (18a Phase 8.3).
  - **Gaps and caveats:** list every accepted gap from 18a, such as an fs grant that covers a file holding the key, hard links, and forged logs until 13a. Arv runs in the host's process, so it is not a process sandbox. For untrusted code at scale, run it inside a container or gVisor as defence in depth.
- **Exit:** both docs are reviewed by the owner, and the matrix script runs in CI.

## V1. Formal semantics of Core Arv (L, Phase 10-11)

**Model in Lean 4** ([Lean](https://lean-lang.org/)). The model covers:
- the sequential core: literals, `let`, `fn`, calls, `if`, `return`, `match`, errors and their kinds, arrays and hashes, and `for`;
- budgets, as fuel: steps and allocation;
- effects, as an oracle: a list of (request, response) pairs.

Lean is both a prover and a programming language, so the same definitions give an executable reference interpreter (V2). K, with its executable semantics, is the alternative; KEVM is the precedent ([KEVM](https://fsl.cs.illinois.edu/publications/hildenbrandt-saxena-zhu-rodrigues-daian-guth-moore-zhang-park-rosu-2018-csf)). Lean is preferred because the proofs and the interpreter live in one tool.

**Theorems on the sequential core:**

| # | Statement | Invariant |
|---|---|---|
| T1 | Determinism modulo effects: for the same program, grants, limits and effect-response list, evaluation gives the same value, output and effect-request list. This is why replay is faithful | I7 |
| T2 | No authority without a grant: every effect request the model makes is covered by the issuing task's capability set | I1, I3 (on canonical requests) |
| T3 | Attenuation: the capability set of any task is a subset of its parent's | I2 |
| T4 | Budgets only shrink: a child's limit is never more than the parent's remaining budget, and total spending never exceeds the root limit | I6 |
| T5 | Control: `return` leaves the innermost function from any position; an error value propagates by its kind. This is the rule A1 broke | — |
| T6 | Immutability: no evaluation step changes a value reachable from another task's environment after spawn | I8 |

**Concurrency and the log protocol in TLA+ or P.** A model of scopes, tasks, `fail_fast`/`collect`/`restart`, cancellation, the effect log, crashes and resume. Checked with TLC or Apalache up to 3 tasks, 6 effects and 2 crashes. AWS uses TLA+ and P on S3, EBS and DynamoDB, and checks production logs against P specs ([CACM 2015](https://cacm.acm.org/magazines/2015/4/184701-how-amazon-web-services-uses-formal-methods/fulltext), [CACM, Systems Correctness Practices at AWS](https://cacm.acm.org/practice/systems-correctness-practices-at-amazon-web-services)). Properties:
- **I4, structure:** no scope reaches its end while it has a live child.
- **I5, fail fast:** after the first failure, no sibling starts a new effect.
- **Resume:** resuming any prefix of an honest log gives a log that extends the prefix. No logged effect runs again, except the one in flight, as documented. Replaying the result reproduces its output.
- **Liveness:** an honest resume finishes, which is the round 6 slot hang in 16x.

**Exit criteria:**
- T1 to T6 are proved, with no `sorry`.
- The TLA+/P model checks clean at the bounds above.
- Every rule is cited from `docs/language.md`.
- When the code changes the semantics, the PR changes the model too. A CI job builds the Lean project.

## V2. Reference interpreter and differential testing (M, after V1)

- Today the tree-walker is the reference for the closure compiler (Phase 8.4), but they share the lexer, parser, values and authors. The Lean interpreter from V1 is independent.
- **Three engines** (Lean, tree-walker, compiled) run each generated program and compare:
  - the value or error kind,
  - the output,
  - the ordered effect requests,
  - the number of steps, where the model counts them alike.
- **Generator:**
  - extend the `FuzzEnginesAgreeSource` corpus with a generator for well-scoped programs, in the style of Csmith and YARPGen (325 and 220 compiler bugs found: [Csmith](https://www-old.cs.utah.edu/~regehr/papers/pldi11-preprint.pdf), [YARPGen](https://users.cs.utah.edu/~regehr/yarpgen-oopsla20.pdf));
  - add an IR-level mutator in the style of Fuzzilli ([NDSS'23](https://www.ndss-symposium.org/wp-content/uploads/2023-290-paper.pdf)).
  - It must put `return`, `match`, errors and budget edges in every value position.
- **Exit criteria:**
  - 10⁷ generated programs per week, with no unexplained divergence.
  - Each divergence is triaged as a spec bug or an implementation bug, and keeps a seed in the corpus.

## V3. Deterministic simulation testing of replay and resume (L, Phase 10-13)

**The harness:**
- **Scheduler:** a seam in `arv/runtime`, for test builds only. One task runs at a time, chosen by a seeded PRNG at every effect and every tick boundary.
- **Clock:** a simulated clock. A `testing/synctest` bubble fakes Go's `time` ([Go 1.25](https://tip.golang.org/doc/go1.25)).
- **World:** a simulated network, files and LLM provider, with seeded latency and faults: ECONNRESET, a 529, slow bodies, ENOSPC, a failed fsync, a torn last log line.
- **Crashes:** at every effect boundary and inside every log write.

**Oracles:**
- **O1:** `replay(record(P, seed))` gives byte-identical output and no live effect.
- **O2:** `resume(kill(P, seed, k))` finishes, repeats no logged effect except the one in flight, and its output is the output of some valid uninterrupted schedule.
- **O3:** invariants are checked online at each step: capabilities are subsets, spending is within budgets, and no task outlives its scope.
- **O4:** a forged-log mutator never makes a replay perform a live effect. With 13a, signed logs refuse every forgery.

**Precedents:**
- FoundationDB simulates a whole cluster in one thread, with fault injection ([FDB testing](https://apple.github.io/foundationdb/testing.html)).
- TigerBeetle's VOPR reproduces any run from a seed plus a commit ([VOPR](https://docs.tigerbeetle.com/about/vopr)).
- Antithesis does this at the hypervisor level, as a service. Consider it once the in-process harness exists ([Antithesis](https://antithesis.com/resources/deterministic_simulation_testing/)).

Seed 40019 (A11) is exactly the kind of bug a seed-reproducible harness turns into a regression test.

**Exit criteria:**
- 10⁶ seeds per night on Linux.
- Any failure reproduces from `(commit, seed)`.
- The timed property tests (`arv/replay_timed_prop_test.go`) and the 200-program property test are ported, and the property test is extended to agents (an 18a Phase 6x7 limit).

## V4. Wider property and fuzz coverage (S to set up, then continuous)

- **Continuous fuzzing** of all 36 targets with [ClusterFuzzLite](https://google.github.io/clusterfuzzlite/running-clusterfuzzlite/github-actions/): a 10-minute run on each PR, a nightly batch run, and the corpus checked in.
- The goal is 24 CPU-hours of fuzzing a week, against about 83 minutes recorded so far.
- **Fuzz coverage** of `arv/evaluator` and `arv/runtime` is published every week, and a target that stops gaining coverage is reviewed.
- **New property tests:**
  - budget conservation across `restart`;
  - `arv check` soundness for every new builtin from Phase 10 (`FuzzCheckSound` exists);
  - `fmt` idempotence (Phase 11);
  - module isolation (Phase 12).

## V5. Continuous mutation testing (M)

- **Mutant regression suite.** Turn the recorded mutants (431 in round 1, plus later rounds) into patch files under `testdata/mutants/<phase>/`, each naming the test that must catch it. A nightly job applies each patch, runs the named packages, and fails if the suite passes.
  - The one-time manual work of Phases 3-8 becomes a permanent asset.
  - Equivalent mutants stay in the suite, marked equivalent, so a later change that makes one observable is noticed.
- **New code.** [Gremlins](https://github.com/go-gremlins/gremlins) runs on the packages a PR changes. The tool is slow on large modules, so it runs per package.
  - The aim is no surviving mutant in an invariant-tagged function without a written reason.
- **A12** (Phase 3 rounds 7-11) is re-run here, with its totals recorded.
- **Exit:** the nightly suite is green. Each PR's survivors are listed in the PR, with a test or a written reason for each.

## V6. Cross-platform CI and sanitizers (S, Phase 9)

| Job | Runs | Why |
|---|---|---|
| linux/amd64 | `go test -race -count=2 -timeout 60m ./...`, race e2e, `make test-walk` | Main gate; Linux procfs tests (A4) |
| linux/arm64 | unit + e2e | Release target |
| linux/386 | `GOARCH=386 go test ./...` on an amd64 runner | 32-bit (A4); no `-race` on 386 |
| macOS arm64 | unit `-race -timeout`; e2e without race | darwin fork hang (A14) |
| windows/amd64 | build, vet, unit tests (tier 2, D5) | It already compiles |
| go-min | the same as linux/amd64 on the `go.mod` minimum (1.24) | The compatibility the module claims |
| nightly | `ARV_LONG=1`, DST seeds, fuzz batch, mutant suite | A11, V3 to V5 |

- **Static analysis:** `go vet`, `staticcheck`, `govulncheck`.
- **Sanitizers:** the race detector everywhere it is supported. ASan and MSan need cgo, and release builds are CGO-off (`.goreleaser.yaml`), so they are not used.
- The existing goroutine-leak tests (`arv/leak_*_test.go`) run in every job.

## V7. External security review of the capability model (M, plus vendor time; Phase 14)

**Scope**
- Grant parsing and canonicalisation (`arv/runtime/canonical.go`, `grant.go`).
- The `os.Root` walk (`arv/evaluator/fsopen.go`).
- The dial-time address check, including SSRF and rebinding.
- The procfs refusals.
- API key handling and redaction (`arv/llm/keycheck.go`).
- Forged-log behaviour, before and after 13a.
- Budgets as a denial-of-service boundary.
- Module isolation (Phase 12).
- Code mode (Phase 14).

**Process**
- Give the vendor V0's threat model and the malicious-script suite from 04 §2.
- [OSTIF](https://ostif.org/ostif-helps-foundations/) arranges audits for open-source projects. AWS-funded OSTIF audits averaged about $74,000 each, which is a fair budget anchor.
- Publish the report, then fix and re-test every high and critical finding.
- After the report, run a public "capture the canary" challenge: read a canary file through a script given a narrow grant.

## Exit: when to say "proven"

| Invariant | Spec proof (V1) | Model check (V1) | Conformance (V2, V3) | Kept (V4-V6) |
|---|---|---|---|---|
| I1, I3 | T2 | — | 3-engine diff, O3 | fuzz, mutants |
| I2 | T3 | spawn under resume | O3, forged-spawn O4 | mutants |
| I4, I5 | — | TLA+/P | O2, O3 | mutants |
| I6 | T4 | across restart | O3 | property |
| I7 | T1 | resume prefix | O1, O2 | nightly DST |
| I8 | T6 | — | 3-engine diff | `-race` |

Arv may say "the invariants are proven on the spec and conformance-tested" once:
- every cell in the table holds;
- V7's high and critical findings are fixed;
- the nightly jobs have been green for 30 days.

Until then, the README says "tested", not "proven".
