# Phase 6, task p6.1: Ordered hashes

> **For agentic workers:** this file is one task, p6.1. Do it test first, exactly as written; if something here does not match the code you find, stop and report instead of improvising. Prerequisite: your tree is `phase/p6`, made from `feat/phases-3-8` with Phase 5 merged, and has no Phase 6 work yet. Start every shell command with `cd <your worktree> &&`. Before committing: `gofmt -l .` prints nothing, `go vet ./...` is clean and `go test -race ./...` is green. End the commit message with a blank line and then `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`. Never use `--no-verify`; keep Go files under 15 KB and markdown under 18 KB. `go.mod` says `go 1.24.0` (toolchain 1.26.2), so do not use `testing/synctest` or `sync.WaitGroup.Go`. `arv/runtime` stays stdlib-only.
>
> Every code block below was applied, in task order, to a scratch copy of the tree and passed `go vet ./...` and `go test -race ./...` (including `e2e/`). Design: `13-phase6-durable-replay.md`. Deviations from it, and why: `p6-plan-1.md`.

## Phase 6 overview

**Goal:** every effect goes through one gate that can log it. `arv run --record run.jsonl` writes the log. `--replay run.jsonl` runs the program again with every effect answered from the log: no network, no files, no waiting, the same output byte for byte. `--resume run.jsonl` replays what was logged, then goes on live and appends.

**Guarantees, and the tasks that test them:**
- I7: a replay prints the same bytes and gets the same effect results, with no live effect (p6.4, p6.8, p6.9, p6.11, p6.13).
- `--resume` after a crash reaches the uninterrupted run's result, and the server sees only the remaining requests (p6.5, p6.11, p6.13).
- A tampered, truncated or mismatched log is a clean error, never a panic and never a live effect (p6.3, p6.4, p6.11, p6.13).
- `--replay` installs the header's grants; a forged log cannot widen a spawn's grants (p6.9, p6.10, p6.12, p6.13).
- `fail_fast`, restarts and timeouts replay the same (p6.6, p6.7, p6.10, p6.11).
- No goroutine outlives a logged `arv.Run` (p6.11).

| Task | File | What |
|---|---|---|
| p6.1 | `p6-plan-1.md` | ordered hashes: `Hash.Set`, `Keys`; literals in source order |
| p6.2 | `p6-plan-2.md` | `value.Encode`, `Decode`, `EncodeRequest` |
| p6.3 | `p6-plan-3.md` | the log file: `LogRecord`, the hash chain, `ReadLog` |
| p6.4 | `p6-plan-4.md` | `EffectLog`, `NewRecorder`, `NewReplayer`, `Task.Effect`, divergence |
| p6.5 | `p6-plan-5.md` | output in recorded order (`Task.Print`); `NewResumer`; host cancel; write failure |
| p6.6 | `p6-plan-6.md` | checks: `Task.Err` and `Tick` logged, `LiveErr`, `WaitFor` |
| p6.7 | `p6-plan-7.md` | checks: `Acquire`, `Expired`, `Backoff`; `Scope.SuperviseWith` |
| p6.8 | `p6-plan-8.md` | `Builtin.Effect`, the evaluator's gate, `puts`, `now()`, `random(n)` |
| p6.9 | `p6-plan-9.md` | `arv.Options` `Record`, `Replay`, `Resume`; the header |
| p6.10 | `p6-plan-10.md` | the `spawn` effect; `await`, the live-task cap, deadlines and backoffs through the checks |
| p6.11 | `p6-plan-11.md` | resume after a crash, property tests, no live effect, no leak |
| p6.12 | `p6-plan-12.md` | CLI `--record`, `--replay`, `--resume`, `--fsync` |
| p6.13 | `p6-plan-13.md` | e2e acceptance through the binary, SIGKILL included |
| p6.14 | `p6-plan-14.md` | README section; `16o-phase6-notes.md`; 16-notes pointer |

**16-implementation-notes follow-through:**
- Freeze at spawn must be transitive: Phase 4 already did it with horizons (`runtime.Task.Horizon`, `Environment.GetAs`; `arv/evaluator/freeze_test.go` covers grandchildren), so the design's task 2 has no Phase 6 task.
- Spawn-time canonicalisation calls lstat, which is OS input: it becomes the `spawn` effect (p6.10).
- `--replay` without `--allow` installs the header's grants, through `runtime.Literal`, which reads no file (p6.9, p6.12).

**Deviations from `13-phase6-durable-replay.md`, and why** (the full text goes in `16o-phase6-notes.md`, p6.14):
- The header is `{"v":1,"header":{...}}`, with grants as `{"kind","scope"}` objects, the limits and the working directory: a replay runs under the recorded grants and limits, and relative paths depend on the directory.
- There is no `err` field: an effect's error is a value, encoded in `res`. The run's result is in a final `{"end":{"result":...}}` line.
- A hash encodes as `{"hash":[[k,v],...]}` in key order (keys need not be strings); invalid UTF-8 as `{"bytes":...}`; an error as `{"error":[message,kind,pos,task]}`.
- The gate is `effect(kind, args, do func(charge) value.Object)` in `arv/evaluator/gate.go`: what an effect charges to `alloc_mb` is logged and charged again on replay.
- Timing-dependent outcomes are logged as numbered **checks**: a cancellation seen at `Tick` or before a builtin, an `await` cut short, the live-task cap, a scope's deadline, a backoff cut short. Without them `fail_fast` output differs between runs.
- `spawn` is an effect (the lstat item above); a replay validates the logged grants with `Literal` and `Allows` (I2).
- `puts` records hold each line's index in the run's output, so a replay prints in the recorded order.
- Added: the `end` line (compared at the end of a replay, which must also use every record) and a SHA-256 hash chain over the lines.
- A changed script is a warning, so the divergence shows; a divergence reads `replay diverged at task root/0, effect 1: the log has ..., the program asked for ...`.
- `random(n)` takes a bound; `clock_ms` is an effect too; restart attempts are `root/0`, `root/0#2`, ... as Phase 4 names them.
- `--replay` refuses `--allow` and limit flags; resuming a finished log replays it; the log file is created 0600 (it may hold `getenv` values).
- "Replay in < 50 ms" is not asserted: a process start alone can take longer at a load average of 300. Replays must instead finish before the sleep they skip would have.

### Task p6.1: Ordered hashes

Replay compares output byte for byte, but a hash prints in Go's random map order, and a hash literal evaluates its pairs in that order too, so the effects inside it (a `puts`, an `http_get`) happen in a different order on each run. `Hash` now remembers the order its keys were first set in:
- `Set(k, p)` appends a new key and keeps an old key's place; `Keys()` returns that order. A hash filled by hand, without `Set`, returns its keys sorted by type and hash value: still the same every time.
- The parser keeps a literal's keys in source order (`ast.HashLiteral.Keys`), and `evalHashLiteral` evaluates them in that order.
- `NewHash` (records from builtins) sorts its field names. `json` walks `Keys()`, so its error names the first bad key, not a random one.

`hashPairSize` stays 48: the key slice adds 24 bytes per pair uncharged, a Minor known limit (p6.14), because raising it changed exact numbers in the existing alloc tests.

**Files:**
- Modify: `arv/ast/hash.go`
- Modify: `arv/evaluator/eval_functions.go`
- Modify: `arv/evaluator/json.go`
- Modify: `arv/parser/parsing_functions.go`
- Modify: `arv/value/value.go`
- Create (test): `arv/evaluator/hash_order_test.go`
- Create (test): `arv/value/hash_order_test.go`

- [ ] **Step 1: Write the failing tests**

Create `arv/evaluator/hash_order_test.go`:

```go
package evaluator

import (
	"bytes"
	"context"
	"testing"

	"github.com/delavalom/arvlang/arv/lexer"
	"github.com/delavalom/arvlang/arv/parser"
	"github.com/delavalom/arvlang/arv/runtime"
	"github.com/delavalom/arvlang/arv/value"
)

// runOut runs src in a root task with no grants and returns what it
// printed and its result.
func runOut(t *testing.T, src string) (string, value.Object) {
	t.Helper()
	p := parser.New(lexer.New(src))
	program := p.ParseProgram()
	if len(p.Errors()) > 0 {
		t.Fatalf("parse: %v", p.Errors())
	}
	var out bytes.Buffer
	root := runtime.NewRoot(context.Background(), runtime.NewCapSet(), &out)
	res := New(root).Run(program, value.NewEnvironment())
	return out.String(), res
}

// I7: a hash literal's pairs are evaluated in source order, so the
// effects in them happen in the same order on every run, and the hash
// prints in that order.
func TestHashLiteralEvaluatesInSourceOrder(t *testing.T) {
	src := `let h = {"z": puts("1"), "y": puts("2"), "x": puts("3"), "w": puts("4"), "v": puts("5")};
puts({"zeta": 1, "alpha": 2, "mid": 3, "beta": 4});
puts(json({"b": 1, "a": 2}))`
	want := "1\n2\n3\n4\n5\n{zeta: 1, alpha: 2, mid: 3, beta: 4}\n{\"a\":2,\"b\":1}\n"
	for i := 0; i < 100; i++ {
		if got, res := runOut(t, src); got != want {
			t.Fatalf("run %d: printed %q (result %s), want %q", i, got, res.Inspect(), want)
		}
	}
}

// json names the first key in order that is not a string, not a random one.
func TestJSONBadKeyIsTheFirst(t *testing.T) {
	for i := 0; i < 50; i++ {
		_, res := runOut(t, `json({"a": 1, 2: 2, true: 3})`)
		if e, ok := res.(*value.Error); !ok || e.Message != "json: hash keys must be STRING, got INTEGER" {
			t.Fatalf("run %d: %s", i, res.Inspect())
		}
	}
}
```

Create `arv/value/hash_order_test.go`:

```go
package value

import "testing"

func str(s string) *String { return &String{Value: s} }

// I7: Phase 6 replays a run and compares its output byte for byte, so a
// hash must print the same every time: in the order its keys were set.
func TestHashInspectsInInsertionOrder(t *testing.T) {
	h := &Hash{}
	for _, k := range []string{"zeta", "alpha", "mid", "beta", "omega", "kappa"} {
		h.Set(str(k).HashKey(), HashPair{Key: str(k), Value: &Integer{Value: int64(len(k))}})
	}
	h.Set(str("alpha").HashKey(), HashPair{Key: str("alpha"), Value: &Integer{Value: 9}}) // keeps its place
	want := "{zeta: 4, alpha: 9, mid: 3, beta: 4, omega: 5, kappa: 5}"
	for i := 0; i < 100; i++ {
		if got := h.Inspect(); got != want {
			t.Fatalf("run %d: Inspect = %s, want %s", i, got, want)
		}
	}
}

func TestNewHashIsSorted(t *testing.T) {
	fields := map[string]Object{"status": &Integer{Value: 200}, "body": str("ok"), "a": &Nil{}, "z": &Nil{}}
	for i := 0; i < 100; i++ {
		if got := NewHash(fields).Inspect(); got != "{a: nil, body: ok, status: 200, z: nil}" {
			t.Fatalf("run %d: NewHash = %s", i, got)
		}
	}
}

// A hash whose Pairs were filled by hand has no recorded order; it still
// prints the same every time.
func TestHandBuiltHashIsStable(t *testing.T) {
	pairs := map[HashKey]HashPair{}
	for _, k := range []string{"c", "a", "b", "d", "e", "f", "g"} {
		pairs[str(k).HashKey()] = HashPair{Key: str(k), Value: str(k)}
	}
	h := &Hash{Pairs: pairs}
	first := h.Inspect()
	for i := 0; i < 100; i++ {
		if got := h.Inspect(); got != first {
			t.Fatalf("run %d: %s, then %s", i, first, got)
		}
	}
}
```

- [ ] **Step 2: Run them and watch them fail**

Run: `cd <worktree> && go test -race ./arv/value/ ./arv/evaluator/ -run 'TestHashInspectsInInsertionOrder|TestNewHashIsSorted|TestHandBuiltHashIsStable|TestHashLiteralEvaluatesInSourceOrder|TestJSONBadKeyIsTheFirst'`

Expected: `arv/value` fails to build (`h.Set undefined`, `undefined: NewHashSize`), and `arv/evaluator` fails `TestHashLiteralEvaluatesInSourceOrder` (the `puts` lines come out of order within the first few runs).

- [ ] **Step 3: Implement**

In `arv/ast/hash.go`, replace:

```go
	Token token.Token // the '{' token
	Pairs map[Expression]Expression
}

```

with:

```go
	Token token.Token // the '{' token
	Pairs map[Expression]Expression
	Keys  []Expression // Pairs' keys in source order: the order they are evaluated in
}

```

In `arv/ast/hash.go`, replace:

```go
	var out bytes.Buffer
	pairs := []string{}
	for key, value := range hl.Pairs {
		pairs = append(pairs, key.String()+":"+value.String())
	}
	out.WriteString("{")
```

with:

```go
	var out bytes.Buffer
	pairs := []string{}
	for _, key := range hl.Keys {
		pairs = append(pairs, key.String()+":"+hl.Pairs[key].String())
	}
	out.WriteString("{")
```

In `arv/evaluator/eval_functions.go`, replace:

```go
	node *ast.HashLiteral, env *value.Environment,
) value.Object {
	pairs := make(map[value.HashKey]value.HashPair)
	for keyNode, valueNode := range node.Pairs {
		key := in.Eval(keyNode, env)
		if isError(key) {
```

with:

```go
	node *ast.HashLiteral, env *value.Environment,
) value.Object {
	h := value.NewHashSize(len(node.Keys))
	for _, keyNode := range node.Keys { // in source order: effects run in it
		valueNode := node.Pairs[keyNode]
		key := in.Eval(keyNode, env)
		if isError(key) {
```

In `arv/evaluator/eval_functions.go`, replace:

```go
			return val
		}
		hashed := hashKey.HashKey()
		pairs[hashed] = value.HashPair{Key: key, Value: val}
	}
	return in.alloc(&value.Hash{Pairs: pairs}, hashPairSize*int64(len(pairs)))
}

```

with:

```go
			return val
		}
		h.Set(hashKey.HashKey(), value.HashPair{Key: key, Value: val})
	}
	return in.alloc(h, hashPairSize*int64(len(h.Pairs)))
}

```

In `arv/evaluator/json.go`, replace:

```go
	}
	entries := make([]entry, 0, len(h.Pairs))
	for _, p := range h.Pairs {
		k, ok := p.Key.(*value.String)
		if !ok {
```

with:

```go
	}
	entries := make([]entry, 0, len(h.Pairs))
	for _, hk := range h.Keys() { // in order: the first bad key is always the same
		p := h.Pairs[hk]
		k, ok := p.Key.(*value.String)
		if !ok {
```

In `arv/parser/parsing_functions.go`, replace:

```go

		hash.Pairs[key] = value

		if !p.peekTokenIs(token.RBRACE) && !p.expectPeek(token.COMMA) {
```

with:

```go

		hash.Pairs[key] = value
		hash.Keys = append(hash.Keys, key)

		if !p.peekTokenIs(token.RBRACE) && !p.expectPeek(token.COMMA) {
```

In `arv/value/value.go`, replace:

```go
	"fmt"
	"hash/fnv"
	"strings"
	"sync/atomic"
```

with:

```go
	"fmt"
	"hash/fnv"
	"sort"
	"strings"
	"sync/atomic"
```

In `arv/value/value.go`, replace:

```go
		}
		ins.put("{")
		i := 0
		for _, pair := range o.Pairs {
			if i > 0 {
				ins.put(", ")
			}
			i++
			ins.write(pair.Key, depth+1)
			ins.put(": ")
```

with:

```go
		}
		ins.put("{")
		for i, k := range o.Keys() {
			if i > 0 {
				ins.put(", ")
			}
			pair := o.Pairs[k]
			ins.write(pair.Key, depth+1)
			ins.put(": ")
```

In `arv/value/value.go`, replace:

```go
}

type Hash struct {
	Pairs map[HashKey]HashPair
}

```

with:

```go
}

// Hash maps keys to values and remembers the order its keys were first
// set in (Set), so Inspect, json and the Phase 6 log see the same order on
// every run: Go's map order is random, and replay needs identical output.
type Hash struct {
	Pairs map[HashKey]HashPair
	keys  []HashKey // Pairs' keys in the order Set first added them
}

// Set binds k to p. A new key goes after every key already in h; setting
// a key again keeps its place.
func (h *Hash) Set(k HashKey, p HashPair) {
	if h.Pairs == nil {
		h.Pairs = make(map[HashKey]HashPair)
	}
	if _, ok := h.Pairs[k]; !ok {
		h.keys = append(h.keys, k)
	}
	h.Pairs[k] = p
}

// NewHashSize is an empty hash with room for n keys.
func NewHashSize(n int) *Hash {
	return &Hash{Pairs: make(map[HashKey]HashPair, n), keys: make([]HashKey, 0, n)}
}

// Keys is h's keys in the order Set first added them. A hash whose Pairs
// were filled without Set (by hand, in a test) has no order recorded: its
// keys come back sorted by type and hash value, which is still the same
// order every time.
func (h *Hash) Keys() []HashKey {
	if len(h.keys) == len(h.Pairs) {
		return h.keys
	}
	keys := make([]HashKey, 0, len(h.Pairs))
	for k := range h.Pairs {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].Type != keys[j].Type {
			return keys[i].Type < keys[j].Type
		}
		return keys[i].Value < keys[j].Value
	})
	return keys
}

```

In `arv/value/value.go`, replace:

```go

// NewHash builds a hash with string keys, for builtins returning records.
func NewHash(fields map[string]Object) *Hash {
	pairs := make(map[HashKey]HashPair, len(fields))
	for k, v := range fields {
		key := &String{Value: k}
		pairs[key.HashKey()] = HashPair{Key: key, Value: v}
	}
	return &Hash{Pairs: pairs}
}

```

with:

```go

// NewHash builds a hash with string keys, for builtins returning records.
// Its keys are in sorted order, so a record prints the same every time.
func NewHash(fields map[string]Object) *Hash {
	names := make([]string, 0, len(fields))
	for k := range fields {
		names = append(names, k)
	}
	sort.Strings(names)
	h := NewHashSize(len(fields))
	for _, k := range names {
		key := &String{Value: k}
		h.Set(key.HashKey(), HashPair{Key: key, Value: fields[k]})
	}
	return h
}

```

- [ ] **Step 4: Run the tests, then the full checks**

Run: `cd <worktree> && go test -race ./arv/value/ ./arv/evaluator/ -run 'TestHashInspectsInInsertionOrder|TestNewHashIsSorted|TestHandBuiltHashIsStable|TestHashLiteralEvaluatesInSourceOrder|TestJSONBadKeyIsTheFirst'`, then `cd <worktree> && gofmt -l . && go vet ./... && go test -race ./...`

Expected: `ok` for every package, and `gofmt -l .` prints nothing.

- [ ] **Step 5: Commit**

```bash
cd <worktree> && git add arv/ast/hash.go arv/evaluator/eval_functions.go arv/evaluator/hash_order_test.go arv/evaluator/json.go arv/parser/parsing_functions.go arv/value/hash_order_test.go arv/value/value.go && git commit -F - <<'MSG'
feat(value): hashes keep insertion order; literals evaluate in source order

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
MSG
```
