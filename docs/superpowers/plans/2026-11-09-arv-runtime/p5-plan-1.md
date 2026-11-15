# Phase 5, task p5.1: `lines` and `split`

> **For agentic workers:** this file is one task, p5.1. Do it test first, exactly as written; if something here does not match the code you find, stop and report instead of improvising. Prerequisite: your tree is `feat/phases-3-8` with Phases 3 and 4 merged and no Phase 5 work yet. Start every shell command with `cd <your worktree> &&` and use absolute paths: the shell's working directory does not persist. Before committing: `gofmt -l .` prints nothing, `go vet ./...` is clean and `go test -race ./...` is green. End the commit message with a blank line and then `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`. Never use `--no-verify` and never push; keep Go files under 15 KB and markdown under 18 KB. `go.mod` says `go 1.24.0` (toolchain 1.26.2): `t.Chdir`, `min` and `max` are available; do not use `testing/synctest` or `sync.WaitGroup.Go`. Before a long test run, check `df -h /System/Volumes/Data`; below 1 GB free, stop and report (only `go clean -cache` may be deleted). This machine is heavily loaded: keep wall-clock bounds generous.
>
> Every code block below was applied, in task order, to a scratch copy of `feat/phases-3-8` (Phases 3 and 4 merged) and passed `gofmt -l .`, `go vet ./...` and `go test -race ./...` (including `e2e/`), and `bench/uptime-go`'s own `go vet` and `go test -race`. Design: `12-phase5-gate.md`. Deviations from it, and why: `p5-plan-1.md`. **Do not measure or write the gate verdict**: a separate step does that.

## Phase 5 overview

**Goal:** build what the gate measures, exactly as `12-phase5-gate.md` lists it: a local target farm, the Arv uptime checker, an idiomatic Go baseline, the benchmark script and the escape probe. Measuring and deciding are a separate step; nothing in these tasks writes numbers or a verdict.

**The scenario (fixed):** read 200 URLs from `targets.txt`; check each, at most 20 in flight, with a 2 s timeout per check and up to 2 restarts on effect errors; write `out/report.json` with status and latency per URL; exit non-zero if more than 5% are down.

**Guarantees, and the tasks that test them:**
- Both versions write the same report (latency aside) against the same farm, and print the same summary (p5.7, p5.8; every `bench/run.sh` run checks it again, p5.9).
- Peak in-flight requests are at most 20 for both, measured by the farm (p5.4, p5.7, p5.8, p5.9).
- The farm is local only (loopback addresses) and deterministic given a seed (p5.4, p5.6).
- The Go baseline is idiomatic: `errgroup` with `SetLimit` as the semaphore, `context.WithTimeout`, a retry loop with jittered backoff, a cloned transport (p5.8).
- The Arv escape probe is refused with `capability denied` under the same `--allow` flags, and nothing reaches the other host; the Go probe reaches it (p5.10).
- No goroutine of an Arv run outlives it (p5.7).

**Invariants touched:** I1 and I2 (the probe; each check holds only its own host), I4 (scopes join), I6 (`json`, `lines` and `split` charge `alloc_mb`), I7 (`clock_ms` is a new nondeterministic input; Phase 6 must record it).

| Task | File | What |
|---|---|---|
| p5.1 | `p5-plan-1.md` | `lines(s)` and `split(s, sep)` builtins |
| p5.2 | `p5-plan-2.md` | `clock_ms()` builtin |
| p5.3 | `p5-plan-3.md` | `json(v)` builtin |
| p5.4 | `p5-plan-4.md` | `bench/farm`: plan, handler, `/stats`, `/reset`, `/sink` |
| p5.5 | `p5-plan-5.md` | `bench/farm`: expected report, strict parse, compare |
| p5.6 | `p5-plan-6.md` | `bench/farm/cmd/farm`: the server binary and `farm compare` |
| p5.7 | `p5-plan-7.md` | `examples/uptime.arv` and its in-process test |
| p5.8 | `p5-plan-8.md` | `bench/uptime-go` (own module) and the same-report test |
| p5.9 | `p5-plan-9.md` | `bench/run.sh`, `bench/results-template.md` |
| p5.10 | `p5-plan-10.md` | `bench/escape/probe.sh` |
| p5.11 | `p5-plan-11.md` | `16m-phase5-notes.md`, README |

### Deviations from `12-phase5-gate.md`, and why

1. **Four new builtins: `lines`, `split`, `json`, `clock_ms`** (p5.1 to p5.3). The scenario cannot be written without them: Arv had no way to split text, read a clock, or turn a number into text (needed for `report.json`). They are library functions, not the syntax (`&&`, `>=`, interpolation, `.field`) that the design names as the remedy if criterion 6 fails, so the gate still measures today's syntax. `json` writes hash keys in order, so the same value always gives the same text. All three that build values charge `alloc_mb` before building (`json` one 64 KiB chunk at a time).
2. **The farm is a library plus a command.** `bench/farm` is package `farm`; the binary is `bench/farm/cmd/farm/main.go`, not `bench/farm/main.go`. The Arv example's test, the Go baseline's test and the escape test all need the farm in process.
3. **Farm details the design leaves open.**
   - Flags as listed, plus `-seed`, `-addr` (a loopback IP only), `-targets` (writes `targets.txt`) and `-expect` (writes the expected report).
   - Exact counts: `round(n * fail)` targets are down and `round(n * flaky)` flaky, picked by a seeded permutation, so a seed fixes the farm. Half the down targets answer 503 and half drop the connection, so both "error status" and "retries run out" are exercised.
   - A flaky target drops its first request after each reset, then answers 200. `POST /reset` zeroes the counters, so every benchmark run sees the same farm.
   - `/sink` is the escape probe's "host not in the list" (a second farm on another loopback port).
   - A request leaves the in-flight count before any of its response is written, so a checker that sends its next request right after an answer is never counted twice.
4. **What "restart on effect errors" means, for both versions.** A transport error is retried: no response, a dropped connection, or a failed body read. An HTTP error status is an answer, not an effect error, so it is not retried. The 2 s timeout covers the whole check, retries and backoff included. A timeout is final in both: in Arv it is a `budget` error, which `restart` does not retry. Backoff is 100 ms, doubling, plus up to 10% jitter (Arv's restart policy; Go mirrors it with random jitter).
5. **The report format.** A JSON array, in target order, of `{"url", "status", "up", "latency_ms"}`. `status` is 0 when no response came, `up` means a 2xx status, and `latency_ms` is the last attempt's. `farm compare` (p5.6) compares two reports field by field and ignores latency and field order.
6. **Summary and exit code.** Both print `{"down":N,"total":M}`, and fail with `more than 5% of the targets are down` when `down * 20 > total`. The default farm has 20 of 200 targets down, so both exit 1 by design, and the exit code is part of correctness.
7. **`bench/uptime-go` is its own module**, requiring `golang.org/x/sync` v0.19.0 (already in the module cache; `go 1.24.0`), so the main module stays dependency-free. `go test ./...` at the root does not run its tests; `make bench-test` does. Its test is the design's "test asserting the same report": it runs the Arv example in process against the same farm. The semaphore is `errgroup`'s `SetLimit(20)`, the idiomatic form; a separate `x/sync/semaphore` would add lines without changing behaviour.
8. **The escape probe.** `probe.sh` makes the one-line changes itself, from the current sources. Arv has no POST, so the Arv probe sends the key in a GET query string. A second Arv probe only sends, to show the network is closed independently of the read. `HOME` points at a temp dir holding a canary key; the real `~/.ssh` is never read.
9. **Results table.** The template is `bench/results-template.md`. `docs/research/2026-phase5-gate.md` is not written in this phase.
10. **`run.sh` alternates** which version runs first in each round, so machine drift does not favour one side.

### Task p5.1: `lines` and `split`

`lines(s)` is the non-blank lines of `s`, trimmed, so a trailing newline or `\r\n` adds nothing (the uptime checker reads `targets.txt` with it). `split(s, sep)` is Go's `strings.Split` with a non-empty separator (the checker names each URL's host with `split(url, "/")[2]`).

Both are `Charging` builtins. Each piece costs `pieceSize` (its array slot and a new String header), charged before the array is built; the pieces share the input's bytes. `lines` charges the most pieces the text can have.

Arv string literals have no escape sequences, so the tests put real newlines into the Arv source with Go strings.

**Files:**
- Create: `arv/evaluator/text.go`
- Create (test): `arv/evaluator/text_test.go`

- [ ] **Step 1: Write the failing test**

Create `arv/evaluator/text_test.go`:

```go
package evaluator

import (
	"testing"

	"github.com/delavalom/arvlang/arv/runtime"
)

func TestSplit(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{`split("a,b,c", ",")`, `[a, b, c]`},
		{`split("a,b,", ",")`, `[a, b, ]`},
		{`split("", ",")`, `[]`},
		{`split("abc", ",")`, `[abc]`},
		{`split("a::b", "::")`, `[a, b]`},
		{`split("http://127.0.0.1:8080/t/3", "/")[2]`, `127.0.0.1:8080`},
		{`len(split("a,b,c", ","))`, `3`},
	} {
		if got := testEval(tc.in); got.Inspect() != tc.want {
			t.Errorf("%s = %s, want %s", tc.in, got.Inspect(), tc.want)
		}
	}
}

func TestLines(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		// Arv string literals have no escapes; Go puts the real
		// newlines, tabs and carriage returns into the source.
		{"lines(\"a\nb\n\")", `[a, b]`},
		{"lines(\"  a  \r\n\n \t\nb\")", `[a, b]`},
		{`lines("")`, `[]`},
		{"lines(\"\n\n\")", `[]`},
		{"len(lines(\"x\ny\nz\n\"))", `3`},
	} {
		if got := testEval(tc.in); got.Inspect() != tc.want {
			t.Errorf("%s = %s, want %s", tc.in, got.Inspect(), tc.want)
		}
	}
}

func TestSplitAndLinesErrors(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{`split("a")`, `split: want 2 arguments, got 1`},
		{`split("a", 1)`, `split: want (STRING, STRING), got (STRING, INTEGER)`},
		{`split("a", "")`, `split: separator is empty`},
		{`lines(1)`, `lines: argument must be STRING, got INTEGER`},
		{`lines()`, `lines: want 1 argument, got 0`},
	} {
		got := testEval(tc.in)
		if !isError(got) || got.Inspect() != "ERROR: "+tc.want {
			t.Errorf("%s = %s, want error %q", tc.in, got.Inspect(), tc.want)
		}
	}
}

// Each piece costs its slot and a String header (pieceSize); the bytes
// are shared with the input. split charges before it builds, so a limit
// one byte short refuses the whole call.
func TestSplitAndLinesCharge(t *testing.T) {
	for _, tc := range []struct {
		src  string
		cost int64
	}{
		{`len(split("a,b,c", ","))`, 3 * pieceSize},
		// lines charges the most lines the text can have: 3 newlines.
		{"len(lines(\"a\n\nb\n\"))", 4 * pieceSize},
	} {
		if got := runCapsIn(t, tc.cost, runtime.NewCapSet(), tc.src); isError(got) {
			t.Errorf("%s under %d bytes: %s", tc.src, tc.cost, got.Inspect())
		}
		got := runCapsIn(t, tc.cost-1, runtime.NewCapSet(), tc.src)
		if !isError(got) || got.(interface{ ErrKind() string }).ErrKind() != runtime.KindBudget {
			t.Errorf("%s under %d bytes: got %s, want a budget error", tc.src, tc.cost-1, got.Inspect())
		}
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test -run 'TestSplit|TestLines' ./arv/evaluator/`
Expected: FAIL, build error `undefined: pieceSize`.

- [ ] **Step 3: Implement**

Create `arv/evaluator/text.go`:

```go
package evaluator

import (
	"strings"

	"github.com/delavalom/arvlang/arv/runtime"
	"github.com/delavalom/arvlang/arv/value"
)

// pieceSize is what one piece of split or lines costs: its array slot and
// a new String header. The piece's bytes are not copied: it shares the
// input string, which is already charged.
const pieceSize = arrayElemSize + 16

func init() {
	builtins["split"] = &value.Builtin{Charging: split}
	builtins["lines"] = &value.Builtin{Charging: lines}
}

// split(s, sep) is the array of the pieces of s between each sep, as Go's
// strings.Split: split("a,b,", ",") is ["a", "b", ""]. sep must not be
// empty. It charges every piece before it builds the array.
func split(t *runtime.Task, charge func(int64) error, args ...value.Object) value.Object {
	if len(args) != 2 {
		return newError("split: want 2 arguments, got %d", len(args))
	}
	s, ok1 := args[0].(*value.String)
	sep, ok2 := args[1].(*value.String)
	if !ok1 || !ok2 {
		return newError("split: want (STRING, STRING), got (%s, %s)", args[0].Type(), args[1].Type())
	}
	if sep.Value == "" {
		return newError("split: separator is empty")
	}
	n := strings.Count(s.Value, sep.Value) + 1
	if err := charge(pieceSize * int64(n)); err != nil {
		return errorOf(err, runtime.KindBudget)
	}
	out := make([]value.Object, 0, n)
	rest := s.Value
	for {
		i := strings.Index(rest, sep.Value)
		if i < 0 {
			break
		}
		out = append(out, &value.String{Value: rest[:i]})
		rest = rest[i+len(sep.Value):]
	}
	out = append(out, &value.String{Value: rest})
	return &value.Array{Elements: out}
}

// lines(s) is the array of the lines of s with surrounding white space
// trimmed, leaving out blank lines, so a file's trailing newline or a
// "\r\n" ending adds nothing. It charges the most pieces s can have
// before it builds the array.
func lines(t *runtime.Task, charge func(int64) error, args ...value.Object) value.Object {
	text, errObj := oneString("lines", args)
	if errObj != nil {
		return errObj
	}
	n := strings.Count(text, "\n") + 1
	if err := charge(pieceSize * int64(n)); err != nil {
		return errorOf(err, runtime.KindBudget)
	}
	out := make([]value.Object, 0, n)
	for len(text) > 0 {
		line := text
		if i := strings.IndexByte(text, '\n'); i >= 0 {
			line, text = text[:i], text[i+1:]
		} else {
			text = ""
		}
		if line = strings.TrimSpace(line); line != "" {
			out = append(out, &value.String{Value: line})
		}
	}
	return &value.Array{Elements: out}
}
```

- [ ] **Step 4: Run to verify it passes**

Run: `go test -race -run 'TestSplit|TestLines' ./arv/evaluator/`, then `gofmt -l .`, `go vet ./...` and `go test -race ./...`.
Expected: PASS everywhere.

- [ ] **Step 5: Commit**

```bash
git add arv/evaluator/text.go arv/evaluator/text_test.go
git commit -m "feat(evaluator): lines and split builtins

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```
