# Phase 6: Durable execution (record, replay, resume)

**Status:** design and task outline. Do this only if the Phase 5 gate says continue.

**Goal:** Every effect goes through one gate that can log it. `arv run --record run.jsonl` writes the log. `--replay run.jsonl` re-runs the program with every effect answered from the log: no network, no files, no waiting, identical output. `--resume run.jsonl` replays what's logged, then continues live, which is Temporal-style durable execution built into the language.

**Invariants:** I7 becomes true here. I8 becomes a real guarantee instead of memory safety only.

## Why replay is sound in Arv

Replay needs every task's sequence of effects to be a deterministic function of its inputs. Arv gets there because:
- **Task IDs are deterministic.** A task spawns sequentially, so `root/0/2` is the same task on every run, whatever the scheduling.
- **Tasks share no mutable state** (I8, completed in 6.2). Values are immutable, and the only channels between tasks are `spawn` arguments and `await` results.
- **All nondeterminism is an effect** (I7). That includes time (`sleep`, the new `now()`), randomness (the new `random()`), output (`puts`) and Phase 4 backoff jitter, which is derived from task IDs.

So the log is keyed by **(task ID, per-task sequence number)**. Interleaving across tasks doesn't matter.

## Design

### Log record (JSON Lines)

```json
{"v":1,"task":"root/0","seq":3,"kind":"http_get","req":["https://api.github.com/zen"],"res":{"hash":{"status":200,"body":"..."}},"err":null}
```

The first line is a header: `{"v":1,"header":true,"script_sha256":"…","grants":["net(\"api.github.com\")"],"started":"…"}`.

### The effect gate

```go
// arv/evaluator/effect.go
func (in *Interpreter) effect(kind string, req []value.Object, do func() value.Object) value.Object
```

- `record`: run `do`, append `{task, seq, kind, req, res|err}`, return.
- `replay`: look up `(task, seq)`. If `kind` or `req` differs, return a **divergence error**: `replay diverged at root/0#1 seq 3: log has http_get("…/zen"), program asked for http_get("…/octocat")`. Otherwise return the logged result without calling `do`.
- `resume`: replay while the task has logged records, then switch that task to `record` and append.

`runtime.EffectLog` stores encoded bytes. The runtime never sees `value` types (see the architecture rule).

### Value codec

`value.Encode(Object) ([]byte, error)` / `Decode`. It covers integers, strings, booleans, nil, arrays, hashes and errors (including `Kind`). Functions, tasks and capabilities are never effect results; encoding one is a bug and returns an error.

### Replay and grants

`--replay` needs **no** `--allow` flags: nothing real happens, which makes offline debugging and CI possible. It prints the grants from the header. `--resume` needs the same grants as the original run, because live effects will happen.

## Task outline

1. **Ordered hashes:** `Hash` keeps insertion order, so `Inspect` and the codec are deterministic. Test: inspecting the same hash 100 times gives one string.
2. **Freeze at spawn (completes I8):** `spawn` copies the function's captured environment chain into one frozen snapshot. Functions inside it are re-pointed at the snapshot, so recursion still works. Test: the parent rebinds `x` after `spawn`; the child still sees the old `x`; `-race` stays clean.
3. **Value codec** with round-trip tests for every encodable type.
4. **`effect()` gate** and migrating every effect through it (`http_get`, `read_file`, `write_file`, `getenv`, `sleep`, `puts`) plus new `now()` and `random()`. A test asserts that every builtin listed as an effect goes through the gate.
5. **`runtime.EffectLog`:** append-only JSONL writer with optional `fsync` per record; reader; header.
6. **Replay** and divergence errors.
7. **Resume.**
8. **Property test:** generate small random Arv programs (spawn trees, sleeps, `http_get` against a local farm, `puts`), record, replay, and assert identical results and output. Use `testing/quick` or a small hand-rolled generator; 200 cases per run.
9. **CLI flags:** `--record`, `--replay`, `--resume`, `--fsync`.

## Acceptance tests

| Test | Expect |
|---|---|
| record → stop server → replay | same output, 0 requests, finishes in < 50 ms even though the program sleeps 2 s |
| edit the script's URL → replay | divergence error naming the task, sequence number and both requests |
| crash after 3 of 5 sequential effects (cancel when the log has 3 records) → resume | the server sees exactly 2 new requests; the final output equals an uninterrupted run |
| replay with restarts | attempts `root/0#1`, `#2` replay in order |
| property test | 200/200 programs replay identically |
