# Arv

Arv is a small language for concurrent operational work: fan-out checks, data pulls, deploy steps, agent loops. The runtime, not convention, decides what the code can touch.

- **Capabilities, not ambient access.** A program can only use the network, files or environment variables the host grants with `--allow`. A spawned task holds nothing unless its parent hands it a subset.
- **Structured concurrency.** `scope { ... }` doesn't finish until every task started in it has finished. The first failure cancels the rest.
- **Planned:** budgets (time, steps, tasks, tokens, dollars), supervision strategies, durable replay, and LLM agents whose tools are capabilities. See `docs/superpowers/plans/2026-11-09-arv-runtime/`.

```arv
let check = fn(url) { http_get(url)["status"] };
scope {
  let a = spawn check("https://api.github.com/zen") with [net("api.github.com")];
  let b = spawn check("https://api.github.com/octocat") with [net("api.github.com")];
  puts([await a, await b])
}
```

```bash
make build
bin/arv run --allow net=api.github.com examples/fanout.arv
# [200, 200, 200]
```

Without the grant, the program is refused before it touches the network:

```bash
bin/arv run examples/fanout.arv
# error: spawn in task root: capability denied: net("api.github.com") (holds: [])
```

## Grants

Pass `--allow kind=scope` once per grant.

| Kind | Scope | Example |
| --- | --- | --- |
| `net` | a bare host, or a `*.domain` wildcard | `--allow net=api.github.com`, `--allow 'net=*.github.com'` |
| `fs_read` | a path | `--allow fs_read=data` |
| `fs_write` | a path | `--allow fs_write=out` |
| `env` | a variable name | `--allow env=HOME` |

- **Net grants are hosts only.** Ports and schemes are rejected: `--allow net=api.github.com:443` fails with `a port is not allowed`.
- **Paths are checked after symlinks are followed.** `..` is resolved after that, and grants are canonicalised, so a link or a `..` cannot step outside the granted directory.
- **`read_file` reads regular files only,** and refuses anything over 10 MiB.
- **A task can only narrow its grants.** `spawn f(x) with [net("api.github.com")]` succeeds only if the parent already holds that capability. A denial names the task and what it holds:

```bash
bin/arv run --allow env=HOME script.arv   # getenv("HOME") works
bin/arv run script.arv                    # error: capability denied: env("HOME") in task root (holds: [])
```

## Running

```bash
bin/arv                          # REPL, no capabilities
bin/arv run [--allow kind=scope]... FILE
```

## Development

```bash
make build   # go build -o bin/arv .
make test    # go test -race ./...
make e2e     # black-box suite that drives the real binary
```

## Language

Literals are numbers, booleans, strings, arrays, maps and functions. There are `let` bindings, `if`/`else` expressions, closures and recursion. Loops are not in yet, so `examples/fanout.arv` walks arrays with recursion. Builtins include `len`, `push`, `last`, `puts`, `http_get`, `read_file`, `write_file`, `getenv` and `sleep`.

Arv started as an interpreter written while reading *Writing an Interpreter in Go*. The earlier lexer and parser experiments are at tag `v0-research`.
