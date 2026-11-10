# Arv Runtime: Design

Read this before any task file. Tests in every phase name the invariant they protect (for example `// I2`).

## 1. What Arv is for

Arv is a small language for concurrent operational work: fan-out health checks, data pulls, migrations, deploy steps and agent loops. The runtime, not convention, decides what the code can touch, how much it can spend, and that nothing it starts outlives it. You can embed it in a Go program (like Starlark) or run it from the command line with explicit grants (like Deno).

**Not goals:**
- A general-purpose application language.
- CPU speed. A tree-walker is roughly 50–100× slower than Go on tight loops. Arv targets I/O-bound work, where the program mostly waits on the network.
- LLM training or GPU kernels. That's Mojo, Triton and JAX territory.
- A package manager. Not before Phase 8, and maybe never.

## 2. Invariants

These are the product. Each one has tests, and every phase must keep all of them passing.

| # | Invariant | Enforced by | From |
|---|---|---|---|
| I1 | **No ambient authority.** The root task holds only what the host grants (`--allow` / `Options.Caps`). Builtins are the only way to reach the OS. | `runtime.Task.Require`, `arv.Run` | P1 |
| I2 | **Attenuation only.** A child's capabilities are a subset of its parent's, checked at `spawn`. With no `with` clause, the child holds nothing. | `CapSet.Attenuate` | P2 |
| I3 | **Check at use, on canonical requests.** Paths are made absolute with symlinks resolved; hosts are lowercased; every redirect is re-checked. | `runtime.New`, `runtime.Canonical`, `clientFor` | P1 |
| I4 | **Structure.** A scope, a task body, or the whole program doesn't finish until every task started inside it has finished. | `runtime.Scope.Wait`, `withScope` | P2 |
| I5 | **Fail fast by default.** The first failure cancels sibling tasks *and* the code inside the scope; the scope then evaluates to that failure. | `Scope.fail`, context cause | P2 |
| I6 | **Budgets only shrink.** A child's limit is never more than its parent's remaining budget. | `runtime.Budget` | P3 |
| I7 | **Effects are the only nondeterminism.** Time, network, files, environment, randomness and LLM calls all go through one effect gate. | `Interpreter.effect` | P6 |
| I8 | **No shared mutable Arv state between tasks.** Values are immutable; tasks talk through `spawn` arguments and `await` results. Until P6, locking `Environment` gives memory safety only; P6 makes this a true guarantee by freezing captured environments at spawn. | `Environment` lock (P2), freeze (P6) | P2/P6 |

## 3. Language surface (target)

```arv
// P1: capability values and effects
net("api.github.com")   net("*.github.com")   fs_read("./data")   fs_write("/tmp/out")   env("HOME")
http_get(url)           read_file(path)       write_file(path, text)                  getenv(name)

// P2: structured concurrency
scope { ... }                                  // waits for every task started inside
let t = spawn check(url) with [net("api.github.com")];
await t
sleep(250)

// P3: budgets and loops
scope (timeout_ms: 2000, tasks: 20, steps: 1000000, alloc_mb: 64) { ... }
for url in urls { spawn check(url) with [net("api.github.com")] }   // evaluates to an array
await [t1, t2]                                                       // awaits each, returns an array

// P4: supervision and error values
scope (supervise: "restart", retries: 3, backoff_ms: 200) { ... }   // or "fail_fast" (default), "collect"
match await t { ok(v) => v, err(e) => e["message"] }

// P6: durable runs (CLI)
arv run --record run.jsonl ...   arv run --replay run.jsonl ...   arv run --resume run.jsonl ...

// P7: agents
llm("anthropic:claude-sonnet-5-5")   tool("search")
ask(model, prompt)   agent(model, goal, tools)
scope (tokens: 50000, usd_cents: 50) { ... }
```

Scope options use one syntax, `scope (name: expr, ...) { ... }`, for every phase. Unknown option names are errors.

## 4. Architecture

```
 host (CLI flags or Go caller)
   │  grants: []runtime.Capability, budgets
   ▼
 arv.Run(src, Options) ──► root runtime.Task ──► root runtime.Scope
                                 │
                 evaluator.Interpreter (one per task, one goroutine)
                   │ Eval nodes; Task.Tick() each node (cancellation, budgets)
                   │ spawn → Task.Child(attenuated caps) → Scope.Go(new Interpreter)
                   ▼
              builtins / effects ── Task.Require(capability) ──► OS (net, fs, env, clock)
```

**Package map** (after Phase 2):

| Package | Responsibility | May import |
|---|---|---|
| `main` (repo root) | CLI: REPL, `arv run --allow` | `arv`, `arv/repl`, `arv/runtime` |
| `arv` | Embedding API: `Run(src, Options)` | everything below |
| `arv/token`, `arv/lexer`, `arv/ast`, `arv/parser` | Front end (from the Monkey book, extended) | each other |
| `arv/value` | Values, `Environment`, `TaskHandle`, `Capability` values | `ast`, `runtime` |
| `arv/evaluator` | `Interpreter`, builtins, effects, spawn/await/scope | all of the above |
| `arv/runtime` | `Capability`, `CapSet`, `Task`, `Scope`; later `Budget`, `Supervisor`, `EffectLog` | **standard library only** |

The rule that keeps this sane: **`runtime` never imports `value` or `evaluator`.** Anything the runtime has to store about values (Phase 6's log) is stored as encoded bytes.

## 5. Why this needs a language, not a Go library

| Need | In Go | In Arv |
|---|---|---|
| Least privilege | Any imported package can `os.Open` or dial out. Policy is code review. | Code can't name the OS except through builtins, and builtins check the task's grants (I1, I3). |
| Handing a task less authority | Not expressible. | `spawn f() with [subset]` (I2). |
| No leaked goroutines | `errgroup` is opt-in; nothing stops a bare `go`. | `spawn` only exists inside a scope; scopes join (I4). |
| Cancel the rest on failure | Manual `context` plumbing everywhere. | Default behaviour (I5). |
| Budgets that nest | Deadlines nest, but step, task and dollar budgets don't exist. | Scope options (I6). |
| Replay and resume | Only with a framework like Temporal, plus discipline. | Every effect already goes through one gate (I7, I8). |

## 6. Known limits (say these out loud)

- **Capability values aren't authority.** Arv is Deno-style permissions plus per-task attenuation, not pure object capabilities. A pure ocap design (the value *is* the authority) is possible later; the `CapSet` boundary makes that change local.
- **Network checks use the hostname.** A granted host that resolves to an internal IP (DNS rebinding, SSRF) gets through until Phase 8 adds an IP check in `DialContext`.
- **There is no live-memory limit.** `alloc_mb` caps the total bytes of Arv values a scope *allocates* (strings, arrays, hashes), not the Go heap. It's named for what it measures.
- **`Hash.Inspect` order is random** (it's a Go map). That's harmless until Phase 6, which needs deterministic output and fixes it.
- **No loops until Phase 3.** Phase 2 examples use recursion.

## 7. Prior art to read (≈2 hours, before Phase 3)

- Starlark in Go: embedding, frozen values, step limits (`Thread.SetMaxExecutionSteps`). https://github.com/google/starlark-go
- Deno's permission model and `--allow-*` flags. https://docs.deno.com/runtime/fundamentals/security/
- Nathaniel J. Smith, "Notes on structured concurrency, or: Go statement considered harmful". https://vorpus.org/blog/notes-on-structured-concurrency-or-go-statement-considered-harmful/
- Erlang/OTP supervision principles (restart strategies, intensity). https://www.erlang.org/doc/system/sup_princ.html
- Temporal's workflow determinism rules (what replay requires). https://docs.temporal.io/workflow-definition#deterministic-constraints
- Mark Miller et al., "Capability Myths Demolished". http://srl.cs.jhu.edu/pubs/SRL2003-02.pdf
- BAML (typed LLM functions as a small language). https://docs.boundaryml.com
