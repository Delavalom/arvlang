# Phases 9 to 15

Part of [`00-roadmap.md`](00-roadmap.md). Each phase lists its goal, scope, exit criteria and size. A phase gets its own bite-sized plan files (`p9-plan-N.md`, and so on) once the phase before it has merged, as Phases 3-8 did. Every phase also has to meet the "every phase" rules in 00 §2.

## Phase 9: ship v0.1 (M)

**Goal:** a tagged release that CI has built and tested on every target, with the Phases 3-8 open findings closed or written down as limits.

**Scope**
- Items A1 to A16 in 00 §1.
- `SECURITY.md`: how to report a vulnerability, and what is in scope. That is the capability boundary; host code is out of scope.
- `CHANGELOG.md`.
- A compatibility statement: no stability promise before 1.0, log format version 3 is the current one, and a log from another version is refused (that check exists today: 18a Phase 6).

**Exit criteria**
- CI is green on every matrix job (02 §V6) for 7 days in a row, with no retried job.
- `v0.1.0` is tagged, its goreleaser artifacts are checksummed, and `go install github.com/delavalom/arvlang/cmd/arv@v0.1.0` prints `v0.1.0`.
- Every one of the 12 open findings in 18 is closed, or is listed as a limit in the README.

## Phase 10: language core v0.2 (L)

**Goal:** write the ten scripts in 04 §1 and §2 with no workaround.

**Scope.** Each item below needs a classification in `arv/check`, a classification in the gate test, agreement between the engines, a written semantics rule (02 §V1) and docs.

**Control flow**
- `return` as fixed in A1.
- `else if`.
- `while (cond) { }` with `break` and `continue`. Each iteration ticks, so `steps` and `timeout_ms` bound it.
- `range(n)`.

**Data**
- `parse_json(text)`. Hash order is already deterministic: `arv/value/hash_order_test.go`.
- `parse_int`.
- Floats, per decision D3.

**Strings**
- `contains`, `replace`, `trim`, `upper`, `lower`, `starts_with`, `ends_with`, `slice`.
- `regex_match` on RE2. RE2 runs in linear time, so a pattern cannot defeat `steps`.
- String literals inside `${…}`.

**Collections**
- `keys`, `values`, `has`, `sort`, `slice`.
- `map`, `filter` and `reduce`, either as builtins or in an Arv prelude. Benchmark both ways (03 §B1).

**HTTP**
- `http(req)` takes a hash: `method`, `url`, `headers`, `body`, `timeout_ms`.
- The `net` grant and the dial check stay as they are. Port and redirect checks are reused from `arv/evaluator/netreq.go`.
- `http_get` stays as sugar.

**Secrets**
- A `secret` value. `secret(getenv("TOKEN"))` can be placed in a header.
- `puts`, `str`, interpolation, `json` and model prompts all refuse it.
- The log stores its SHA-256, never its text.
- This closes 18a's "the log may hold secrets" for values the program marks.

**Errors**
- A `?` suffix that returns the `err` of an `ok`/`err` value from the function. It would be sugar for `match`.

**Subprocesses**
- Per decision D4: `exec(argv)` under `proc("name")`. No shell. The environment is scrubbed, the working directory must be under an fs grant, and output is bounded and charged.

**Exit criteria**
- The ten scripts run.
- `docs/language.md` has become a reference that covers every form and builtin.
- `FuzzEnginesAgree*` covers the new forms.
- A mutation round has recorded totals, with nothing caught that the round did not explain.

## Phase 11: tooling (L)

**Goal:** someone new can write, format, test and debug Arv in an editor in 10 minutes.

**Scope**

**`arv fmt`**
- The lexer keeps comments as tokens, which `15-phase8-tooling.md` §8.6 says is needed.
- The output is idempotent and is fuzzed: `fmt(fmt(x)) == fmt(x)`, and the AST is unchanged.

**Error messages**
- Today, three bad lines gave 13 cascading parse errors.
- Instead: one primary error per statement, a source snippet with a caret, a hint ("`return` is a statement; use it inside a block"), and recovery to the next statement.
- A golden corpus of 100 messages.

**LSP (`arv lsp`)**
- Parse and check diagnostics.
- Hovering over an effect shows the grant it needs (the `arv check` engine). Hovering over a `spawn … with` shows what the child holds.
- Go to definition, rename, and a "missing grant" code action that adds a `// arv:allow` line.

**REPL**
- Multi-line input.
- `:allow kind=scope`, budgets, and `:record FILE`. This removes the 18a limit "the REPL runs without budgets".

**`arv test`**
- `test "name" { … }` blocks.
- Effects come from a recorded log, so a recorded fixture replays offline in CI with no network and no API key. This is Phase 6 used as a testing tool. No mainstream runtime does it natively.

**`arv log`**
- `verify` the hash chain.
- `show`: effects by task, model calls with tokens and cents.
- `diff` two logs, and the first divergence of a replay.

**Editors and docs**
- A tree-sitter grammar and a VS Code extension (syntax, LSP client).
- A docs site with a tutorial, a reference, a cookbook, the security model and an embedding guide.
- A playground: a `GOOS=js GOARCH=wasm` build with fake effects, and a viewer for replay logs.

**Exit criteria**
- `fmt` survives 10 minutes of fuzzing.
- The LSP works in VS Code and Neovim.
- The error corpus is green.
- `arv test` replays the `examples/agent.arv` fixture offline.
- The docs site is live, and the playground runs `fanout.arv` against fake effects.

## Phase 12: modules, stdlib, packages (L)

**Goal:** share code without sharing authority.

**Scope**

**`import`**
- `import "./lib.arv"`, and `import "github.com/x/y@v1.2.0/z.arv"` fetched by git, with no registry before 1.0.

**Imported code holds nothing (I1 at module level)**
- A module gets authority only through the arguments it is called with, or through a `spawn … with` its caller writes.
- This is decision D2: capability values become authority inside modules. That is the change `01-design.md` §6 says the `CapSet` boundary keeps local.

**`arv.lock`**
- The SHA-256 of every module, like `go.sum`. A run with a mismatched lock is refused.
- The log header records the source hashes, so a replay or resume finds code drift before its first effect. Today 18a warns only about the cwd.

**`arv check` across modules**
- It reports the grants per module and the grants that cross module boundaries.

**A stdlib written in Arv**
- `strings`, `lists`, `retry`, `http_json`, `report`.

**Exit criteria**
- A two-module example.
- An e2e test in which a malicious module cannot reach anything it was not passed: it tries `read_file`, `http_get`, `getenv`, a `spawn … with`, and a model call.
- A lock mismatch is refused.
- `arv check` output is golden-tested across modules.

## Phase 13: runtime (XL; split into 13a to 13e)

**13a. Signed logs.**
- The host holds an Ed25519 or HMAC key and signs the head of the chain at intervals and at the end.
- `--resume` and `--replay` can require a valid signature.
- This closes the forged-log class that 18a lists for Phase 6 and 6x7, which the owner accepted as a risk: raised limits, widened children, lowered usage.
- Exit: every forged-log fuzz target (`FuzzForgedReplay`, `FuzzForgedAgentResume`, …) gets a signed mode in which every forgery is refused.

**13b. Log compaction.**
- Today a model request is logged whole on every turn, so the log grows with the square of the turns (18a Phase 6).
- Store each turn as a delta, or as content-addressed blobs.
- Exit: a 50-turn agent log is within 3× the size of its final transcript, and a replay is still byte-identical.

**13c. Memory and connections.**
- A live-memory limit for a CLI run (`--mem-mb`, through `runtime/debug.SetMemoryLimit` plus a check at the scope level), keeping `alloc_mb` for churn.
- `http` keep-alive, keeping the dial check on every new connection. Today each `http_get` opens a fresh connection (18a Phase 8.3). A pooled connection must never be reused for a host that its grant did not admit.
- Exit: a memory bomb under `--mem-mb 64` fails with kind `budget`, and the benchmarks show connection reuse (03 §B6).

**13d. Durable worker.**
- `arv serve` holds runs, resumes them after a crash, and stores logs in files first, then SQLite.
- Durable `sleep` that survives a restart, with timers in the log.
- `wait_signal(name)`, a logged effect that an external call completes.
- This is where Temporal and Restate live ([Temporal](https://docs.temporal.io/workflow-definition), [Restate](https://docs.restate.dev/concepts/durable_execution)). Arv's difference is that the language enforces determinism through the effect gate (I7), with no SDK rules for the programmer to follow.
- Exit: the DST oracles in 02 §V3 pass with the worker killed at random points.

**13e. Speed, gated by decision D6.**
- Slot resolution, `spawn` and `await` on the compiled path, and lock-free reads (16p84 lists these as not done).
- Exit: 03 §B1 shows a gain, with p < 0.01 under benchstat, and nothing else changes.

## Phase 14: host and agent story (XL)

**Goal:** Arv is the safest way to let a model act. OWASP's "Excessive Agency" mitigations (minimal tools, minimal permissions, authorization outside the model, rate limits) are the Arv model: [OWASP LLM06](https://genai.owasp.org/llmrisk/llm062025-excessive-agency/).

**Scope**

**Code mode**
- A host exposes one tool, "run Arv". The model writes Arv. The host runs `arv check` against a fixed manifest, then runs the program with the host's grants and budgets, and records it.
- Cloudflare's Code Mode is the closest analogue: a V8 isolate in which tool calls go back to the host ([codemode](https://developers.cloudflare.com/agents/api-reference/codemode)). Arv adds budgets, per-task attenuation and replay.

**MCP**
- `mcp("server")` is a capability. A server's tools become Arv tools, each under `tool(...)`.
- `arv mcp-serve` exposes scripts as MCP tools, each with its own manifest.

**Providers**
- OpenAI, Gemini, and OpenAI-compatible local servers, behind `llm.Provider` (`arv/llm/llm.go:21`).
- Pricing is versioned with a source and a date. Cache pricing is modelled.

**Typed tools and output**
- Tool parameter types come from annotations, as JSON Schema. Today tools take strings only (18a).
- `ask_json(model, prompt, schema)`.
- `max_tokens` and `thinking` are configurable, and replies can stream to `puts`.

**`approve(message)`**
- A human approval as a logged effect. A run waiting for approval can be killed and resumed (13d).

**Hosts in other languages**
- `arv serve --stdio`, a JSON-RPC protocol, plus thin Python and TypeScript packages.

**The Wasm effect**
- Run a module under [wazero](https://github.com/tetratelabs/wazero), with capability-checked imports (§8.7 of the old plan).

**Agent evals**
- Run tau-bench-style tasks through Arv and report pass^k ([tau-bench](https://arxiv.org/abs/2406.12045)). A recorded run makes a failure reproducible offline.

**Exit criteria**
- The prompt-injection red-team suite in 04 §2 passes.
- MCP interop works with three public servers.
- At least two providers pass the same agent e2e suite.
- Code mode is shown with a model writing and running Arv under a $0.25 budget.

## Phase 15: distribution and v1.0 (M)

**Goal:** a 1.0 that people can depend on.

**Scope**
- Release channels: a Homebrew tap, an install script that checks checksums, cosign signatures, SLSA provenance, an SBOM, and a container image.
- Windows, per decision D5: the `os.Root`, `nofollow_other.go` and `procfs_other.go` paths need tests on Windows.
- Promises: semver for the Go API (`arv.Options`, `arv.Effect`), a frozen language spec for 1.x, and log format compatibility (a 1.x binary replays any 1.x log). A deprecation policy.
- Gleam shipped its compiler, build tool, formatter and LSP together in v1, with a semver promise ([Gleam 1.0](https://gleam.run/news/gleam-version-1/)). Arv's 1.0 should ship the Phase 11 tools the same way.

**Exit criteria**
- The "proven" table in 02 §Exit holds.
- The external audit's high and critical findings are fixed and re-tested.
- The adoption gate (04 §7) passed.
