# Phase 2c: Fan-out demo, end-to-end test, README

**Goal:** The scenario that motivates the project works end to end through the CLI: many concurrent HTTP checks, each task holding only `net` to its one host, finishing in about the time of one request.

---

### Task 2.7: `len` on arrays

The demo walks arrays with recursion (loops arrive in Phase 3), which needs `len`.

**Files:**
- Modify: `arv/evaluator/builtins.go` (the `len` builtin)

- [ ] **Step 1: Add a failing case.** Append to `arv/evaluator/interpreter_test.go`:

```go
func TestLenOfArray(t *testing.T) {
	got, _ := runWith(t, `len([1, 2, 3])`)
	if got.Inspect() != "3" {
		t.Fatalf("got %s", got.Inspect())
	}
}
```

Run: `go test ./arv/evaluator/ -run TestLenOfArray`
Expected: FAIL, because `len` reports that ARRAY is not supported.

- [ ] **Step 2: Implement.** In the `len` builtin's type switch, after the `*value.String` case:

```go
			case *value.Array:
				return &value.Integer{Value: int64(len(arg.Elements))}
```

Run: `go test ./arv/evaluator/ -run TestLenOfArray`
Expected: PASS.

---

### Task 2.8: Example script and end-to-end test

**Files:**
- Create: `examples/fanout.arv`
- Modify: `main_test.go` (add `TestFanOutEndToEnd` and its imports)

- [ ] **Step 1: Write the end-to-end test.** Add `"fmt"`, `"net/http"`, `"net/http/httptest"`, `"sync/atomic"` and `"time"` to the imports in `main_test.go`, then append:

```go
func TestFanOutEndToEnd(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		time.Sleep(100 * time.Millisecond)
		fmt.Fprint(w, "ok")
	}))
	defer srv.Close()

	script := writeScript(t, fmt.Sprintf(`
		let n = 20;
		let check = fn(i) { http_get(%q)["status"] };
		let startAll = fn(i, ts) { if (i == n) { ts } else { startAll(i + 1, push(ts, spawn check(i) with [net("127.0.0.1")])) } };
		let awaitAll = fn(i, ts, out) { if (i == len(ts)) { out } else { awaitAll(i + 1, ts, push(out, await ts[i])) } };
		puts(len(scope { awaitAll(0, startAll(0, []), []) }));`, srv.URL))

	var out, errOut bytes.Buffer
	start := time.Now()
	if code := runCLI([]string{"run", "--allow", "net=127.0.0.1", script}, &out, &errOut); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	if out.String() != "20\n" || hits.Load() != 20 {
		t.Fatalf("out %q, hits %d", out.String(), hits.Load())
	}
	// 20 requests x 100ms each would take 2s sequentially.
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("fan-out took %s; requests were not concurrent", elapsed)
	}
}
```

- [ ] **Step 2: Run it**

Run: `go test -race . -run TestFanOutEndToEnd -v`
Expected: PASS. 20 requests with 100 ms of server latency each finish in well under 1 s.

- [ ] **Step 3: Write the example**

```arv
// Check many URLs at once. Each check runs as its own task holding only
// net access to the one host it needs; nothing else can touch the network,
// the filesystem or the environment.
//
//   arv run --allow net=api.github.com examples/fanout.arv

let urls = [
  "https://api.github.com/zen",
  "https://api.github.com/octocat",
  "https://api.github.com/rate_limit"
];

let check = fn(url) { http_get(url)["status"] };

// No loops yet: start one task per URL with recursion.
let startAll = fn(i, tasks) {
  if (i == len(urls)) { tasks } else {
    startAll(i + 1, push(tasks, spawn check(urls[i]) with [net("api.github.com")]))
  }
};
let awaitAll = fn(i, tasks, out) {
  if (i == len(tasks)) { out } else { awaitAll(i + 1, tasks, push(out, await tasks[i])) }
};

let statuses = scope { awaitAll(0, startAll(0, []), []) };
puts(statuses);
```

- [ ] **Step 4: Try it against the real API**

```bash
go build -o bin/arv .
bin/arv run examples/fanout.arv
# error: spawn in task root: capability denied: net("api.github.com") (holds: []) ; exit 1
bin/arv run --allow net=api.github.com examples/fanout.arv
# [200, 200, 200]
```

(GitHub's unauthenticated rate limit is 60 requests an hour, so don't loop this.)

---

### Task 2.9: README

**Files:**
- Replace: `README.md` (keep `assets/image.png`; the new README doesn't use it, but `git log` will still show the old README)

- [ ] **Step 1: Replace `README.md`**

````markdown
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
  [await a, await b]
}
```

```bash
go build -o bin/arv .
bin/arv run --allow net=api.github.com examples/fanout.arv
```

Arv started as an interpreter written while reading *Writing an Interpreter in Go*. The earlier lexer and parser experiments are at tag `v0-research`.
````

- [ ] **Step 2: Final check for the phase**

Run: `gofmt -l . && go vet ./... && go test -race -count=5 ./...`
Expected: `gofmt` prints nothing; every package is `ok` five times.

- [ ] **Step 3: Commit**

```bash
git add examples main_test.go arv/evaluator README.md
git commit -m "feat: fan-out example, end-to-end test, README for the runtime direction"
```

- [ ] **Step 4: Open the Phase 0–2 PR** (when the owner asks for it)

```bash
git push -u origin feat/runtime
gh pr create --title "Arv runtime: capabilities + structured concurrency (phases 0-2)" \
  --body "Implements docs/superpowers/plans/2026-11-09-arv-runtime files 02-09. Invariants I1-I5 tested."
```
