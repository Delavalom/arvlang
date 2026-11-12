# Phase 8.4, task p84.6: random programs and cancellation on both engines; `make test-walk`

> **For agentic workers:** this file is one task, p84.6. Do it test first, exactly as written; if something here does not match the code you find, stop and report instead of improvising. Prerequisite: task p84.5 is committed on `phase/p84` in `/Users/delavalom/delavalom-labs/arvlang-wt/p84`. Start every shell command with `cd /Users/delavalom/delavalom-labs/arvlang-wt/p84 &&`. Before committing: `gofmt -l .` prints nothing, `go vet ./...` is clean and `go test -race ./...` is green (first check `df -h /System/Volumes/Data`: below 1 GB free, stop and report "blocked: disk"; the only thing you may delete is the build cache, `go clean -cache`). End the commit message with a blank line and then `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`. Never use `--no-verify`, never push. Keep Go files under 15 KB and markdown under 18 KB. Create files that contain `${` with the Write or Edit tool, not with a shell heredoc.
>
> Every code block below was applied, in task order, to a scratch copy of the tree and passed `go vet ./...` and `go test -race` on the packages it touches. Overview, guarantees S1-S6, decisions and deviations: `p84-plan-1.md`.

## Task p84.6: random programs and cancellation on both engines; `make test-walk`

The corpus (p84.2) covers each node type by hand. This task adds what a hand-written list misses, and the way to run everything on the reference engine:

- **Random programs (S1, S2).** `TestEnginesAgreeProperty` puts a random Phase 5E expression from the existing generator (`ergo_gen_test.go`: atoms, `.field`, `&&`, `||`, comparisons, arithmetic, `if`, `!`, `-`, interpolation, a printing `tick`, `await spawn`) inside a function with a `let` and a `return`, a `for`, a `match` and a spawned task, and runs it through `Run` on both engines: no limit, 150 steps and 1500 bytes. The value or error (message, kind, position), the output, and the exact steps and bytes (`eFit`) must agree. 200 seeds (30 under `-short`); `FuzzEnginesAgree` is the same with the fuzzer's bytes. `TestEnginesAgreeOnSpeedPrograms` runs the p84.1 programs on both.
- **Cancellation (S3) and the leak rule.** `TestEnginesObserveCancellationPromptly` runs a program of 10^12 loop bodies, in the root task and four spawned tasks, on each engine: under a 100 ms timeout it must end with `budget exceeded: timeout (limit 100 ms)`, and with a host cancel after 100 ms with an error naming the cancellation, each within 20 s (generous, for a load above 300). Goroutines are counted before and after with `settle` (`leak_p83_test.go`).
- **`make test-walk`** runs the whole suite with `-tags arv_treewalk`, and e2e with `ARV_E2E_TAGS=arv_treewalk`, which e2e passes to `go build` for every binary it builds (`buildFlags`).

The tests pass at once: they guard the compiled code written in p84.3 to p84.5. Step 3 shows they bite.

**Files:**
- Create (test): `arv/engine_prop_test.go`, `arv/engine_cancel_test.go`
- Modify (test): `e2e/harness_test.go`, `e2e/p82_helpers_test.go`, `e2e/p82_host_test.go`
- Modify: `Makefile`

- [ ] **Step 1: Write the tests**

Create `arv/engine_prop_test.go`:

```go
package arv

import (
	"bytes"
	"errors"
	"fmt"
	"math/rand"
	"testing"

	"github.com/delavalom/arvlang/arv/evaluator"
	"github.com/delavalom/arvlang/arv/runtime"
)

// engineProgram puts the random expression n (ergo_gen_test.go) inside a
// function, a let, a return, a loop, a match and a spawned task, so the
// node types the generator does not make run around it.
func engineProgram(n *enode) string {
	return ePrelude +
		"let f = fn(i) { let r = " + n.write(true) + "; if (i == 2) { return r } else { [i, r] } };\n" +
		"let rs = for i in [1, 2] { match f(i) { ok(v) => v, err(e) => [e.kind, e.message, e.pos] } };\n" +
		"puts(rs);\nscope { await spawn f(3) }"
}

// engineOutcome is eOutcome with the error's position, which the same
// source must give on both engines.
type engineOutcome struct {
	eOutcome
	pos string
}

// runOnEngine runs src through Run on one engine (walk: the tree-walker).
func runOnEngine(src string, walk bool, l runtime.Limits) engineOutcome {
	old := evaluator.SetTreeWalk(walk)
	defer evaluator.SetTreeWalk(old)
	var out bytes.Buffer
	v, err := Run(src, Options{Out: &out, Limits: l})
	o := engineOutcome{eOutcome: eOutcome{out: out.String()}}
	var ae *Error
	switch {
	case errors.As(err, &ae):
		o.err, o.kind, o.pos = ae.Msg, ae.Kind, ae.Pos
	case err != nil:
		o.err = "go: " + err.Error()
	default:
		o.value = v.Inspect()
	}
	return o
}

// fitOnEngine is eFit on one engine.
func fitOnEngine(src string, walk bool, limit func(int64) runtime.Limits) (int64, bool) {
	old := evaluator.SetTreeWalk(walk)
	defer evaluator.SetTreeWalk(old)
	return eFit(src, limit)
}

// checkEngines fails unless src gives the same outcome on both engines,
// with no limit and under a tight steps and bytes limit, and is charged
// the same steps and the same bytes.
func checkEngines(t *testing.T, src string) {
	t.Helper()
	for _, l := range []runtime.Limits{{}, {Steps: 150}, {AllocBytes: 1500}} {
		w, c := runOnEngine(src, true, l), runOnEngine(src, false, l)
		if w != c {
			t.Fatalf("limits %+v:\ntree-walker %+v\ncompiled    %+v\n%s", l, w, c, src)
		}
	}
	for _, k := range []struct {
		name  string
		limit func(int64) runtime.Limits
	}{{"steps", eSteps}, {"bytes", eBytes}} {
		fw, okW := fitOnEngine(src, true, k.limit)
		fc, okC := fitOnEngine(src, false, k.limit)
		if fw != fc || okW != okC {
			t.Fatalf("%s: tree-walker %d (%v), compiled %d (%v)\n%s", k.name, fw, okW, fc, okC, src)
		}
	}
}

// The engines agree on random programs: 200 seeds, fixed so a failure
// reproduces (the seed is in the subtest name).
func TestEnginesAgreeProperty(t *testing.T) {
	cases := 200
	if testing.Short() {
		cases = 30
	}
	for seed := 0; seed < cases; seed++ {
		r := rand.New(rand.NewSource(int64(1000 + seed)))
		data := make([]byte, 80)
		r.Read(data)
		t.Run(fmt.Sprint("seed", seed), func(t *testing.T) { checkEngines(t, engineProgram(decode(data))) })
	}
}

// FuzzEnginesAgree is the property with the fuzzer's bytes.
func FuzzEnginesAgree(f *testing.F) {
	for seed := 0; seed < 10; seed++ {
		r := rand.New(rand.NewSource(int64(seed)))
		data := make([]byte, 40)
		r.Read(data)
		f.Add(data)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 200 {
			return
		}
		checkEngines(t, engineProgram(decode(data)))
	})
}

// The speed programs (p84.1) agree too, under no limit.
func TestEnginesAgreeOnSpeedPrograms(t *testing.T) {
	for _, p := range speedPrograms {
		if w, c := runOnEngine(p.src, true, runtime.Limits{}), runOnEngine(p.src, false, runtime.Limits{}); w != c {
			t.Errorf("%s: tree-walker %+v, compiled %+v", p.name, w, c)
		}
	}
}
```

Create `arv/engine_cancel_test.go`:

```go
package arv

import (
	"context"
	"errors"
	goruntime "runtime"
	"strings"
	"testing"
	"time"

	"github.com/delavalom/arvlang/arv/evaluator"
	"github.com/delavalom/arvlang/arv/runtime"
)

// endless runs far longer than any test (10^12 loop bodies), in the root
// task and in four spawned tasks, so only a deadline or a cancel ends it.
var endless = func() string {
	big := "[" + strings.TrimSuffix(strings.Repeat("0, ", 10000), ", ") + "]"
	return "let big = " + big + ";\n" +
		"let spin = fn() { for a in big { for b in big { for c in big { a + b + c } } } };\n" +
		"let ts = for i in [1, 2, 3, 4] { spawn spin() };\nspin()"
}()

// Compiled code observes a deadline and a host's cancel as promptly as
// the tree-walker: each run ends with the budget's timeout or the
// cancellation well within the bound (generous: the machine may be under
// heavy load), and no goroutine outlives Run.
func TestEnginesObserveCancellationPromptly(t *testing.T) {
	for _, walk := range []bool{true, false} {
		old := evaluator.SetTreeWalk(walk)
		before := settle(0)

		start := time.Now()
		_, err := Run(endless, Options{Limits: runtime.Limits{Timeout: 100 * time.Millisecond}})
		var ae *Error
		if !errors.As(err, &ae) || ae.Msg != "budget exceeded: timeout (limit 100 ms)" {
			t.Errorf("walk=%v: err = %v, want the timeout", walk, err)
		}
		if d := time.Since(start); d > 20*time.Second {
			t.Errorf("walk=%v: the timeout took %v", walk, d)
		}

		ctx, cancel := context.WithCancel(context.Background())
		time.AfterFunc(100*time.Millisecond, cancel)
		start = time.Now()
		if _, err := Run(endless, Options{Ctx: ctx}); err == nil || !strings.Contains(err.Error(), "cancel") {
			t.Errorf("walk=%v: err = %v, want a cancellation", walk, err)
		}
		if d := time.Since(start); d > 20*time.Second {
			t.Errorf("walk=%v: the cancel took %v", walk, d)
		}
		cancel()

		evaluator.SetTreeWalk(old)
		if after := settle(before); after > before {
			buf := make([]byte, 1<<20)
			t.Fatalf("walk=%v: %d goroutines before, %d after:\n%s", walk, before, after, buf[:goruntime.Stack(buf, true)])
		}
	}
}
```

- [ ] **Step 2: Run them**

Run: `cd /Users/delavalom/delavalom-labs/arvlang-wt/p84 && go test -race ./arv/ -run 'TestEnginesAgree|TestEnginesObserveCancellationPromptly' -count=1 -v 2>&1 | grep -E '^(--- |ok|FAIL)'`
Expected: `--- PASS` for `TestEnginesAgreeOnSpeedPrograms`, `TestEnginesAgreeProperty` and `TestEnginesObserveCancellationPromptly`, then `ok`.

- [ ] **Step 3: Show that they bite**

In `arv/evaluator/compile.go` (in `compileIf`), change `if condition == TRUE {` to `if condition != FALSE {`, then run `cd /Users/delavalom/delavalom-labs/arvlang-wt/p84 && go test ./arv/ -run TestEnginesAgreeProperty 2>&1 | head -3`.
Expected: FAIL at `seed0` (`1 && ...` takes the consequence on the compiled engine only). Change it back.

- [ ] **Step 4: Build e2e binaries with tags, and add `make test-walk`**

In `e2e/harness_test.go`, replace:

```go
func runMain(m *testing.M) int {
```

with:

```go
// buildFlags are the go build flags of every binary the suite builds:
// -race under ARV_E2E_RACE (the binary itself runs under the race
// detector), and -tags with the value of ARV_E2E_TAGS (arv_treewalk runs
// the suite on the tree-walker, the reference engine of Phase 8.4).
func buildFlags() []string {
	var flags []string
	if os.Getenv("ARV_E2E_RACE") != "" {
		flags = append(flags, "-race")
	}
	if tags := os.Getenv("ARV_E2E_TAGS"); tags != "" {
		flags = append(flags, "-tags", tags)
	}
	return flags
}

func runMain(m *testing.M) int {
```

In the same file, replace:

```go
	buildArgs := []string{"build", "-o", arvBin}
	if os.Getenv("ARV_E2E_RACE") != "" { // run the binary itself under the race detector
		buildArgs = append(buildArgs, "-race")
	}
```

with:

```go
	buildArgs := append([]string{"build", "-o", arvBin}, buildFlags()...)
```

In `e2e/p82_helpers_test.go`, replace:

```go
// p82Host builds e2e/p82host once (with -race under ARV_E2E_RACE), next to
```

with:

```go
// p82Host builds e2e/p82host once (with buildFlags), next to
```

and replace:

```go
		args := []string{"build", "-o", p82Bin}
		if os.Getenv("ARV_E2E_RACE") != "" {
			args = append(args, "-race")
		}
```

with:

```go
		args := append([]string{"build", "-o", p82Bin}, buildFlags()...)
```

In `e2e/p82_host_test.go`, replace:

```go
	args := []string{"build", "-o", bin}
	if os.Getenv("ARV_E2E_RACE") != "" {
		args = append(args, "-race")
	}
```

with:

```go
	args := append([]string{"build", "-o", bin}, buildFlags()...)
```

and delete the line `	"os"` from that file's imports (nothing else in it uses `os`).

In `Makefile`, replace:

```make
.PHONY: build test e2e bench-test bench escape bench-cpu
```

with:

```make
.PHONY: build test test-walk e2e bench-test bench escape bench-cpu
```

and replace:

```make
e2e:
	go test -race -count=1 ./e2e/
```

with:

```make
e2e:
	go test -race -count=1 ./e2e/

# Phase 8.4: the whole suite, e2e included, on the tree-walker, the
# reference engine (programs run as compiled code by default).
test-walk:
	go test -race -tags arv_treewalk ./...
	ARV_E2E_TAGS=arv_treewalk go test -race -count=1 ./e2e/
```

Run: `cd /Users/delavalom/delavalom-labs/arvlang-wt/p84 && gofmt -l . && go vet ./... && ARV_E2E_TAGS=arv_treewalk go test -race -count=1 ./e2e/ -run 'TestP82HostExample|TestNet$'`
Expected: no gofmt output, vet clean, `ok`.

- [ ] **Step 5: Check and commit**

Run: `cd /Users/delavalom/delavalom-labs/arvlang-wt/p84 && go test -race ./... && make test-walk`
Expected: every package `ok`, both times. The suite is long, and the machine is shared: `TestResumeReplaysAFailedAttemptToItsEnd` and the other spin-calibrated `resume_*` tests are timing tests from Phase 6 that can fail when the load average jumps in the middle of a run (seen once in the scratch copy at a load of 790, on the tree-walker; it passed when re-run alone). If one fails, re-run it alone with `-count=3` before treating it as a finding.

```bash
cd /Users/delavalom/delavalom-labs/arvlang-wt/p84 && git add arv/engine_prop_test.go arv/engine_cancel_test.go e2e Makefile && git commit -m "test(arv): engines agree on random programs and cancellation; make test-walk

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```
