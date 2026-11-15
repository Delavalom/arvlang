# Phase 3, task p3.13: Fan-out example and README

> **For agentic workers:** this file is one task, p3.13. Do it test first, exactly as written; if something here does not match the code you find, stop and report instead of improvising. Prerequisite: tasks p3.1 to p3.12 are committed in your tree. Start every shell command with `cd <your worktree> &&`. Before committing: `gofmt -l .` prints nothing, `go vet ./...` is clean and `go test -race ./...` is green. End the commit message with a blank line and then `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`. Never use `--no-verify`; keep Go files under 15 KB. `go.mod` says 1.22.5, so do not use `os.Root`, `t.Chdir`, `testing/synctest` or `sync.WaitGroup.Go`.
>
> Every code block below was applied, in task order, to a scratch copy of the tree and passed `go vet ./...` and `go test -race ./...` (including `e2e/`). Design: `10-phase3-budgets.md`. Deviations from it, and why: `p3-plan-1.md`.

### Task p3.13: Fan-out example and README

`examples/fanout.arv` uses `scope (tasks: 20, timeout_ms: 5000)` and `await for …` instead of recursion. This is the "phase done" condition in the design. The README documents budgets, the flags and the new syntax. The example was run against the real api.github.com while this plan was written and printed `[200, 200, 200]`.

**Files:**
- Modify: `README.md`
- Create (test): `example_test.go`
- Modify: `examples/fanout.arv`

- [ ] **Step 1: Write the failing tests**

Create `example_test.go`:

```go
package main

import (
	"os"
	"strings"
	"testing"

	"github.com/delavalom/arvlang/arv/lexer"
	"github.com/delavalom/arvlang/arv/parser"
)

// The example parses and uses the Phase 3 forms: for, await on an array,
// and scope options.
func TestFanoutExample(t *testing.T) {
	src, err := os.ReadFile("examples/fanout.arv")
	if err != nil {
		t.Fatal(err)
	}
	p := parser.New(lexer.New(string(src)))
	p.ParseProgram()
	if len(p.Errors()) > 0 {
		t.Fatalf("parse errors: %v", p.Errors())
	}
	for _, want := range []string{"scope (tasks: 20", "await for url in urls"} {
		if !strings.Contains(string(src), want) {
			t.Errorf("example lacks %q", want)
		}
	}
	if strings.Contains(string(src), "startAll") {
		t.Error("example still walks arrays with recursion")
	}
}
```

- [ ] **Step 2: Run them and watch them fail**

Run: `cd <worktree> && go test . -run Fanout`

Expected: `TestFanoutExample` fails with `example lacks "scope (tasks: 20"`, `example lacks "await for url in urls"` and `example still walks arrays with recursion`.

- [ ] **Step 3: Implement**

In `README.md`, replace

~~~~markdown

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
~~~~

with

~~~~markdown

- **Capabilities, not ambient access.** A program can only use the network, files or environment variables the host grants with `--allow`. A spawned task holds nothing unless its parent hands it a subset.
- **Structured concurrency.** `scope { ... }` doesn't finish until every task started in it has finished. The first failure cancels the rest.
- **Budgets.** `scope (timeout_ms: 5000, tasks: 20, steps: 1000000, alloc_mb: 64) { ... }` caps time, concurrent tasks, evaluation steps and allocation. A nested scope can only shrink what encloses it.
- **Planned:** token and dollar budgets, supervision strategies, durable replay, and LLM agents whose tools are capabilities. See `docs/superpowers/plans/2026-11-09-arv-runtime/`.

```arv
let check = fn(url) { http_get(url)["status"] };
let urls = ["https://api.github.com/zen", "https://api.github.com/octocat"];
let statuses = scope (tasks: 20, timeout_ms: 5000) {
  await for url in urls { spawn check(url) with [net("api.github.com")] }
};
puts(statuses);
```

```bash
~~~~

In `README.md`, replace

~~~~markdown

```bash
bin/arv                          # REPL, no capabilities
bin/arv run [--allow kind=scope]... FILE
```

## Development

```bash
~~~~

with

~~~~markdown

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
| `alloc_mb` | MiB of strings, arrays and hashes created under the scope (never refunded) | error |

Option values are positive integers; an unknown or repeated option is an error. Limits nest: a scope inside `scope (steps: 1000)` can never spend more than what is left of those 1000, whatever it asks for.

## Development

```bash
~~~~

In `README.md`, replace

~~~~markdown

## Language

Literals are numbers, booleans, strings, arrays, maps and functions. There are `let` bindings, `if`/`else` expressions, closures and recursion. Loops are not in yet, so `examples/fanout.arv` walks arrays with recursion. Builtins include `len`, `push`, `last`, `puts`, `http_get`, `read_file`, `write_file`, `getenv` and `sleep`.

Arv started as an interpreter written while reading *Writing an Interpreter in Go*. The earlier lexer and parser experiments are at tag `v0-research`.
~~~~

with

~~~~markdown

## Language

Literals are numbers, booleans, strings, arrays, maps and functions. There are `let` bindings, `if`/`else` expressions, closures and recursion. `for x in xs { ... }` evaluates to the array of its body's values, so `await for url in urls { spawn check(url) }` starts one task per URL and collects their results; `await [t1, t2]` awaits each task in order. Builtins include `len`, `push`, `last`, `puts`, `http_get`, `read_file`, `write_file`, `getenv` and `sleep`.

Arv started as an interpreter written while reading *Writing an Interpreter in Go*. The earlier lexer and parser experiments are at tag `v0-research`.
~~~~


Replace the whole of `examples/fanout.arv` with:

```arv
// Check many URLs at once. Each check runs as its own task holding only
// net access to the one host it needs; nothing else can touch the network,
// the filesystem or the environment. The scope runs at most 20 checks at a
// time and stops them all if they take longer than 5 seconds together.
//
//   arv run --allow net=api.github.com examples/fanout.arv

let urls = [
  "https://api.github.com/zen",
  "https://api.github.com/octocat",
  "https://api.github.com/rate_limit"
];

let check = fn(url) { http_get(url)["status"] };

let statuses = scope (tasks: 20, timeout_ms: 5000) {
  await for url in urls { spawn check(url) with [net("api.github.com")] }
};
puts(statuses);
```

- [ ] **Step 4: Run the tests, then the full checks**

Run: `cd <worktree> && go test . -run Fanout`, then `cd <worktree> && gofmt -l . && go vet ./... && go test -race ./...`

Expected: `ok` for every package, and `gofmt -l .` prints nothing.

Finish with the phase's whole suite, e2e included: `go test -race ./...` and `ARV_E2E_RACE=1 go test -race ./e2e/`. Every Phase 0–2 e2e expectation passes unchanged.

- [ ] **Step 5: Commit**

```bash
cd <worktree> && git add README.md example_test.go examples/fanout.arv && git commit -F - <<'MSG'
docs: fan-out example uses for and scope options; README covers budgets

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
MSG
```
