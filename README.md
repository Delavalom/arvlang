# Arv

Arv is a small language for concurrent operational work: fan-out checks, data pulls, deploy steps, agent loops. The runtime, not convention, decides what the code can touch.

- **Capabilities, not ambient access.** A program can only use the network, files or environment variables the host grants with `--allow`. A spawned task holds nothing unless its parent hands it a subset.
- **Structured concurrency.** `scope { ... }` doesn't finish until every task started in it has finished. The first failure cancels the rest.
- **Budgets.** `scope (timeout_ms: 5000, tasks: 20, steps: 1000000, alloc_mb: 64) { ... }` caps time, concurrent tasks, evaluation steps and allocation. A nested scope can only shrink what encloses it.
- **Supervision and errors as values.** `scope (supervise: "collect") { ... }` keeps going when a task fails, and `scope (supervise: "restart", retries: 3) { ... }` runs a failed task again. `match` looks at an error's kind instead of crashing.
- **Planned:** token and dollar budgets, durable replay, and LLM agents whose tools are capabilities. See `docs/superpowers/plans/2026-11-09-arv-runtime/`.

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

- **Ports.** `net=api.github.com:443` allows port 443 only; a bare host allows every port. A request names the port it reaches (the scheme's default when the URL has none), and every redirect is checked again, port included. Schemes and paths are rejected.
- **Internal addresses need an IP grant.** Every connection is checked on the address it actually reaches, after DNS. Loopback, private, link-local (including the `169.254.169.254` metadata service) and other internal addresses are refused unless an IP or CIDR grant covers them (`net=127.0.0.1`, `net=10.0.0.0/8`). A host name grant, even `*`, never reaches them, so a name that resolves, or rebinds, to an internal address is refused. Proxy settings from the environment are not used.
- **Paths are checked after symlinks are followed.** `..` is resolved after that, and grants are canonicalised, so a link or a `..` cannot step outside the granted directory. Files are then opened beneath the grant with `os.Root`, so a directory swapped for a symlink after the check cannot lead outside it either.
- **`read_file` reads regular files only,** and refuses anything over 10 MiB.
- **A task can only narrow its grants.** `spawn f(x) with [net("api.github.com")]` succeeds only if the parent already holds that capability. A denial names the task and what it holds:

```bash
# script.arv: puts(getenv("HOME"))
bin/arv run --allow env=HOME script.arv   # prints $HOME
bin/arv run script.arv                    # error: capability denied: env("HOME") in task root (holds: []) (at script.arv:1:6)
```

## Running

```bash
bin/arv                          # REPL, no capabilities
bin/arv run [--allow kind=scope]... [--timeout 30s] [--steps N] [--tasks N] [--alloc-mb N] FILE
```

The flags set the whole run's budget. `--timeout` defaults to 10 minutes (`0` turns it off). `--tasks` caps the tasks alive at once and defaults to 1000, so runaway spawn recursion ends in an error. `--steps` and `--alloc-mb` have no default limit. A spent budget is an error that says where the program was:

```bash
cat script.arv
# let f = fn(n) { if (n == 0) { 0 } else { f(n - 1) } };
# puts(f(5000));
bin/arv run --steps 1000 script.arv
# error: budget exceeded: steps (limit 1000) (at script.arv:1:15)
```

## Budgets

| Scope option | Limits | When it runs out |
| --- | --- | --- |
| `timeout_ms` | wall-clock time for the scope | the scope and its tasks are cancelled |
| `tasks` | the scope's own tasks running at once | `spawn` waits for one to finish |
| `steps` | nodes evaluated by everything under the scope | error |
| `alloc_mb` | MiB of values created under the scope: strings, arrays, hashes, closures, environments and bindings (never refunded) | error |

Option values are positive integers; an unknown or repeated option is an error. Limits nest: a scope inside `scope (steps: 1000)` can never spend more than what is left of those 1000, whatever it asks for.

## Errors and supervision

Every error has a kind:

| Kind | Comes from |
| --- | --- |
| `capability` | a missing grant, in an effect or at `spawn ... with` |
| `budget` | a scope option or `--steps`, `--timeout`, ... running out |
| `cancelled` | the task's scope was cancelled |
| `effect` | the world: connection refused, file not found |
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

The script calls `k8s_apply("staging", manifest)` and hands the grant to a child with `spawn f() with [k8s("staging")]`. `Fn` gets the calling task (honour `t.Ctx`), returns Arv data or an error, and its errors become Arv errors of kind `effect`. `Options.Kinds` gives a kind its own coverage rule; the default is an exact match. `Scope`, `Fn` and `Covers` are called from many tasks at once and must be safe for concurrent use. `Fn` must not change its arguments, and a value it returns belongs to the run: the host must never change it afterwards. `examples/host` is a runnable host with a fake cluster: `go run ./examples/host -allow staging`.

## Development

```bash
make build   # go build -o bin/arv .
make test    # go test -race ./...
make e2e     # black-box suite that drives the real binary
```

## Language

Literals are numbers, booleans, strings, arrays, maps and functions. There are `let` bindings, `if`/`else` expressions, closures and recursion. `for x in xs { ... }` evaluates to the array of its body's values, so `await for url in urls { spawn check(url) }` starts one task per URL and collects their results; `await [t1, t2]` awaits each task in order. Strings compare with `==` and `!=`. Builtins include `len`, `push`, `last`, `puts`, `error`, `http_get`, `read_file`, `write_file`, `getenv` and `sleep`, plus `lines(s)` (the non-blank lines of `s`, trimmed), `split(s, sep)`, `json(v)` (compact JSON, hash keys in order) and `clock_ms()` (a monotonic clock, for latencies).

`examples/uptime.arv` is the Phase 5 gate scenario: check 200 URLs, 20 at a time, with a timeout and restarts, and write a report. `bench/` compares it with an idiomatic Go version against a local farm (`make bench`) and probes both for a key-stealing change (`make escape`).

Arv started as an interpreter written while reading *Writing an Interpreter in Go*. The earlier lexer and parser experiments are at tag `v0-research`.
