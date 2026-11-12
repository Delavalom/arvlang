# Arv

Arv is a small language for concurrent operational work: fan-out checks, data pulls, deploy steps, agent loops. The runtime, not convention, decides what the code can touch.

- **Capabilities, not ambient access.** A program can only use the network, files or environment variables the host grants with `--allow`. A spawned task holds nothing unless its parent hands it a subset.
- **Structured concurrency.** `scope { ... }` doesn't finish until every task started in it has finished. The first failure cancels the rest.
- **Budgets.** `scope (timeout_ms: 5000, tasks: 20, steps: 1000000, alloc_mb: 64) { ... }` caps time, concurrent tasks, evaluation steps and allocation. A nested scope can only shrink what encloses it.
- **Supervision and errors as values.** `scope (supervise: "collect") { ... }` keeps going when a task fails, and `scope (supervise: "restart", retries: 3) { ... }` runs a failed task again. `match` looks at an error's kind instead of crashing.
- **Agents on the same model.** `ask(model, prompt)` and `agent(model, goal, tools)` call a language model. The model is a capability (`llm("anthropic:claude-sonnet-5-5")`), each tool is one (`tool("weather")`), tokens and dollars are scope budgets, and the API key stays in the host.
- **Planned:** durable replay. See `docs/superpowers/plans/2026-11-09-arv-runtime/`.

```arv
let check = fn(url) { http_get(url)["status"] };
let urls = ["https://api.github.com/zen", "https://api.github.com/octocat"];
let statuses = scope (tasks: 20, timeout_ms: 5000) {
  await for url in urls { spawn check(url) with [net("api.github.com")] }
};
puts(statuses);
```

```bash
make build
bin/arv run --allow net=api.github.com examples/fanout.arv
# [200, 200, 200]
```

Without the grant, the program is refused before it touches the network:

```bash
bin/arv run examples/fanout.arv
# error: spawn in task root: capability denied: net("api.github.com") (holds: []) (at examples/fanout.arv:17:27)
```

## Grants

Pass `--allow kind=scope` once per grant.

| Kind | Scope | Example |
| --- | --- | --- |
| `net` | a host (`api.github.com`, `*.github.com`, `*` or an IP) with an optional `:port`, or a CIDR range | `--allow net=api.github.com:443`, `--allow 'net=*.github.com'`, `--allow net=10.0.0.0/8` |
| `fs_read` | a path | `--allow fs_read=data` |
| `fs_write` | a path | `--allow fs_write=out` |
| `env` | a variable name | `--allow env=HOME` |
| `llm` | `provider:model`, `provider:*` or `*` | `--allow llm=anthropic:claude-sonnet-5-5` |
| `tool` | a tool name, or `*` | `--allow tool=weather` |

- **Ports.** `net=api.github.com:443` allows port 443 only; a bare host allows every port. A request names the port it reaches (the scheme's default when the URL has none), and every redirect is checked again, port included. Schemes and paths are rejected.
- **Internal addresses need an IP grant.** Every connection is checked on the address it actually reaches, after DNS. Loopback, private, link-local (including the `169.254.169.254` metadata service) and other internal addresses are refused unless an IP or CIDR grant covers them (`net=127.0.0.1`, `net=10.0.0.0/8`). A host name grant, even `*`, never reaches them, so a name that resolves, or rebinds, to an internal address is refused. `http_get` uses no proxy settings from the environment. (Model calls do: their endpoint is the host's choice, not the program's.)
- **Paths are checked after symlinks are followed.** `..` is resolved after that, and grants are canonicalised, so a link or a `..` cannot step outside the granted directory. Files are then opened beneath the grant with `os.Root`, so a directory swapped for a symlink after the check cannot lead outside it either.
- **`read_file` reads regular files only,** never a file on a procfs (Linux's `/proc`, FreeBSD's `procfs` and `linprocfs`; `write_file` neither), and refuses anything over 10 MiB.
- **A task can only narrow its grants.** `spawn f(x) with [net("api.github.com")]` succeeds only if the parent already holds that capability. A denial names the task and what it holds:

```bash
# script.arv: puts(getenv("HOME"))
bin/arv run --allow env=HOME script.arv   # prints $HOME
bin/arv run script.arv                    # error: capability denied: env("HOME") in task root (holds: []) (at script.arv:1:6)
```

## Running

```bash
bin/arv                          # REPL, no capabilities
bin/arv run [--allow kind=scope]... [--timeout 30s] [--steps N] [--tasks N] [--alloc-mb N]
            [--tokens N] [--usd-cents N] [--max-turns N] [--record|--replay|--resume LOG] [--fsync] FILE
bin/arv run --manifest [--timeout 30s] [--steps N] [--tasks N] [--alloc-mb N]
            [--tokens N] [--usd-cents N] [--max-turns N] FILE
bin/arv check FILE               # the grants FILE can need; runs nothing
```

The flags set the whole run's budget. `--timeout` defaults to 10 minutes (`0` turns it off). `--tasks` caps the tasks alive at once and defaults to 1000, so runaway spawn recursion ends in an error. `--steps` and `--alloc-mb` have no default limit. A spent budget is an error that says where the program was:

```bash
cat script.arv
# let f = fn(n) { if (n == 0) { 0 } else { f(n - 1) } };
# puts(f(5000));
bin/arv run --steps 1000 script.arv
# error: budget exceeded: steps (limit 1000) (at script.arv:1:15)
```

## Knowing the grants before running

`arv check FILE` prints the grants a program can need, without running it: one `--allow` line per grant, then a `#` line for each call whose argument it cannot know before running.

```text
$ bin/arv check examples/fanout.arv
--allow net=api.github.com
# http_get at 14:23: host not statically known
```

It looks at every call of `http_get`, `read_file`, `write_file`, `getenv`, `net`, `fs_read`, `fs_write` and `env`, wherever it is, even in code that never runs. An argument is known when it is a string literal, a name bound once in the whole program by `let` to a known string, or a `+` of known strings. A builtin passed around as a value is a `#` line too. Paths print as written, relative to the working directory, as `--allow` takes them. A grant with `[`, `]`, `*` or other unusual characters prints as `$'...'` (bash, zsh and ksh read it), so it pastes as one word. What a server redirects to, and the IP grant a name that resolves to an internal address also needs, are only known when the program runs.

A program can carry its grants in a header of comments before its first line of code:

```arv
// arv:allow net=api.github.com
// arv:allow fs_write=out
```

`arv run --manifest FILE` grants exactly those lines (it can't be combined with `--allow`) and refuses to start if `arv check` finds a need outside them. `arv check` on such a file exits 1 in the same case. A `#` line doesn't stop the run: the program holds the header's grants and nothing else, so anything outside them is still denied when it is tried.

## Budgets

| Scope option | Limits | When it runs out |
| --- | --- | --- |
| `timeout_ms` | wall-clock time for the scope | the scope and its tasks are cancelled |
| `tasks` | the scope's own tasks running at once | `spawn` waits for one to finish |
| `steps` | nodes evaluated by everything under the scope | error |
| `alloc_mb` | MiB of values created under the scope: strings, arrays, hashes, closures, environments and bindings (never refunded) | error |
| `tokens` | model tokens, input and output, of every `ask` and `agent` call under the scope | the call is refused before it is sent |
| `usd_cents` | what those calls cost, in US cents, at the prices in `arv/llm/pricing.go` | the call is refused before it is sent |
| `max_turns` | model calls one `agent` may make (default 20) | error |

Option values are positive integers; an unknown or repeated option is an error. Limits nest: a scope inside `scope (steps: 1000)` can never spend more than what is left of those 1000, whatever it asks for.

## Agents

```arv
let tools = {
  "weather": fn(city) { http_get("https://api.weather.example/v1?q=" + city)["body"] },
  "notes":   fn(path) { read_file(path) }
};
let answer = scope (tokens: 50000, usd_cents: 25, timeout_ms: 60000) {
  await spawn agent("anthropic:claude-sonnet-5-5", "Should I bike to work in Lisbon today?", tools)
    with [llm("anthropic:claude-sonnet-5-5"), tool("weather"), net("api.weather.example")]
};
```

- **The model is a grant.** `ask` and `agent` need `llm(...)` for the model they name. The CLI reads `ANTHROPIC_API_KEY` and removes it from the environment before the run, so not even `--allow env=*` lets the program read it, and no file on a procfs is ever read (`/proc/self/environ` still holds the starting environment). A reply that echoes the key, even split across blocks, is refused. An fs grant that covers a file holding the key (a `.env`, a shell profile) does read it: grant narrowly. A Go host passes `Options.LLM` (`arv/llm`): its providers, with their keys; if it grants `env`, it removes the key's variable itself.
- **Each tool is a grant, and so are its effects.** The model is offered every tool in the hash; a tool's parameters are its function's, all strings. The agent above holds `tool("weather")` and not `tool("notes")`, so if the model asks for `notes` it gets the tool result `capability denied: tool("notes")` and the run goes on. `weather` runs in the agent's own task, so its `http_get` needs `net("api.weather.example")` too. A tool's error goes back to the model as an error result.
- **The model never sees your grants.** A denial that reaches a model (a prompt, a goal, a tool description or result) is cut to what was asked for: `capability denied: tool("notes")`, `capability denied: net`, `capability denied: fs_read` (no path). Task IDs are cut from tool errors.
- **Budgets are checked before each call.** A call reserves its most output (4096 tokens) plus an estimate of its input, refuses to start if `tokens` or `usd_cents` cannot cover that, and then settles to the real usage. A call that fails after it may have reached the provider (cancelled, a timeout, a network error) is charged its whole reservation; only one the API answered with a 4xx or 529 is free. `agent(model, goal, tools, descriptions)` takes an optional hash of tool descriptions.
- `examples/agent.arv` runs one end to end; the tests use a fake provider and never call the network.

## Errors and supervision

Every error has a kind:

| Kind | Comes from |
| --- | --- |
| `capability` | a missing grant, in an effect or at `spawn ... with` |
| `budget` | a scope option or `--steps`, `--timeout`, ... running out |
| `cancelled` | the task's scope was cancelled |
| `effect` | the world: connection refused, file not found, a model provider's error (`ask: anthropic: rate_limit (HTTP 429): ...`) |
| `runtime` | the program: a type error, bad arguments, division by zero |
| `user` | `error("message")` |

`match` runs one arm or the other; both are required:

```arv
match await t {
  ok(v) => v,
  err(e) => if (e["kind"] == "capability") { "denied" } else { e["message"] }
}
```

`e` is a map with `kind`, `message`, `task` (the task it arose in, such as `root/0`) and `pos` (`line:col`).

`supervise` decides what a task's failure does to its scope:

| `supervise:` | A failed task |
| --- | --- |
| `"fail_fast"` (default) | cancels the scope, its other tasks and its body; the scope fails with it. `match` can't rescue it. |
| `"collect"` | cancels nothing; `await` returns its error, to `match`. A failure nobody awaits fails the scope when it ends: `N unawaited task failures: ...`. |
| `"restart"` | runs again, as task `root/0#2`, `root/0#3`, ..., up to `retries` times (default 3), waiting `backoff_ms` (default 100) doubled each time, at most 30 s. Only the kinds in `retry_on` restart: `["effect"]` by default, `["effect", "user"]` at most. Then it fails like `fail_fast`. |

**Restart is for idempotent work:** every attempt runs the task's effects again, so a task that writes a file writes it again. Attempts share the scope's budgets; restarting refunds nothing. A spawned task sees the variables it captured as they were at `spawn`, in every attempt: a later `let` in the parent never reaches it.

## Record, replay and resume

Every effect goes through one gate that can log it: the network, files, the environment, `sleep`, `now()`, `random(n)`, `clock_ms()`, `puts`, the model calls of `ask` and `agent`, and a Go host's own effects. Which failure of a scope came first is logged too.

```bash
bin/arv run --record run.jsonl --allow net=api.github.com script.arv   # log every effect
bin/arv run --replay run.jsonl script.arv                              # nothing runs live
bin/arv run --resume run.jsonl --allow net=api.github.com script.arv   # replay, then go on live
```

`--replay` answers every effect from the log: no request, no file, no waiting, and the same output byte for byte. It takes the grants and limits from the log and prints the grants, so it needs no `--allow`, and no API key: a model's replies are in the log. A program that asks for something else stops: `replay diverged at task root/0, effect 1: the log has http_get("…/a"), the program asked for http_get("…/b")`. `--resume` continues a run that was killed. It replays each task while the log has records for it, then runs that task live and appends to the log. It needs the grants the run was recorded with. `--fsync` syncs the log after every line, so a crash loses no logged effect. A log that was edited, cut or reordered is refused (each line chains a SHA-256 of the one before). The chain is not a signature. The log can hold secrets, such as `getenv` values, so it is created with mode 0600.

## Host effects (embedding)

A Go program embeds Arv with `arv.Run(src, arv.Options{...})` and can add its own effects. Each one names the capability kind it needs and how a call's scope is found; the runtime checks the calling task's grants before it calls the host's function, and an effect without a kind can't be registered:

```go
arv.Run(src, arv.Options{
	Caps: []runtime.Capability{{Kind: "k8s", Scope: "staging"}},
	Effects: map[string]arv.Effect{
		"k8s_apply": {Kind: "k8s", Scope: namespaceArg, Fn: applyManifest},
	},
})
```

The script calls `k8s_apply("staging", manifest)` and hands the grant to a child with `spawn f() with [k8s("staging")]`. `Fn` gets the calling task (honour `t.Ctx`), returns Arv data or an error, and its errors become Arv errors of kind `effect`. `Options.Kinds` gives a kind its own coverage rule; the default is an exact match. `Scope`, `Fn` and `Covers` are called from many tasks at once and must be safe for concurrent use. `Fn` must not change its arguments (Arv values are immutable, and an integer from -128 to 1023 is one `*value.Integer` shared by every run in the process), and a value it returns belongs to the run: the host must never change it afterwards. `examples/host` is a runnable host with a fake cluster: `go run ./examples/host -allow staging`. With `Options.Record`, each host call is logged as `host:<name>`; a replay (`Options.Replay`) binds the same `Effects` and `Kinds` but never calls `Fn`.

## Development

```bash
make build   # go build -o bin/arv .
make test    # go test -race ./...
make e2e     # black-box suite that drives the real binary
make test-walk  # the same suites on the tree-walker, the reference engine
make bench-cpu  # CPU-bound speed programs (Phase 8.4)
```

Programs run as Go closures, compiled from the syntax tree once per run. The tree-walking interpreter they are compiled from stays as the reference: the test suite runs on both (`make test-walk`), and they must agree on every value, error, position, output, step and byte.

## Language

Literals are numbers, booleans, strings, arrays, maps and functions. There are `let` bindings, `if`/`else` expressions, closures and recursion. `for x in xs { ... }` evaluates to the array of its body's values, so `await for url in urls { spawn check(url) }` starts one task per URL and collects their results; `await [t1, t2]` awaits each task in order. Strings compare with `==` and `!=`. Builtins include `len`, `push`, `last`, `puts`, `error`, `http_get`, `read_file`, `write_file`, `getenv`, `sleep`, `ask` and `agent`, plus `lines(s)` (the non-blank lines of `s`, trimmed), `split(s, sep)`, `json(v)` (compact JSON, hash keys in order), `str(v)` (`v` as `puts` prints it) and `clock_ms()` (a monotonic clock, for latencies).

`r.up` is `r["up"]`. `"${n} of ${len(xs)} down"` interpolates: each `${...}` holds an expression (no string literals inside it) and prints as `puts` would; `\${` is a literal `${`. Integers compare with `<`, `>`, `<=` and `>=`. `a && b` and `a || b` short-circuit: the right side runs only when the left does not decide, and, as in `if`, only `true` counts as true, so `||` is not a default operator (`r.name || "anon"` is `"anon"` whatever the name).

`examples/uptime.arv` is the Phase 5 gate scenario: check 200 URLs, 20 at a time, with a timeout and restarts, and write a report. `bench/` compares it with an idiomatic Go version against a local farm (`make bench`) and probes both for a key-stealing change (`make escape`).

`examples/uptime-ergonomic.arv` is the same scenario written with the syntax above. It takes the same grants (`--allow fs_read=targets.txt --allow fs_write=out --allow net=127.0.0.1:PORT`) and writes the same report.

Arv started as an interpreter written while reading *Writing an Interpreter in Go*. The earlier lexer and parser experiments are at tag `v0-research`.
