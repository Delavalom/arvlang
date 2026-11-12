# Phase 6, task p6.8: The evaluator gate: effect builtins, puts, now and random

> **For agentic workers:** this file is one task, p6.8. Do it test first, exactly as written; if something here does not match the code you find, stop and report instead of improvising. Prerequisite: tasks p6.1 to p6.7 are committed in your tree. Start every shell command with `cd <your worktree> &&`. Before committing: `gofmt -l .` prints nothing, `go vet ./...` is clean and `go test -race ./...` is green. End the commit message with a blank line and then `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`. Never use `--no-verify`; keep Go files under 15 KB and markdown under 18 KB. `go.mod` says `go 1.24.0` (toolchain 1.26.2), so do not use `testing/synctest` or `sync.WaitGroup.Go`. `arv/runtime` stays stdlib-only.
>
> Every code block below was applied, in task order, to a scratch copy of the tree and passed `go vet ./...` and `go test -race ./...` (including `e2e/`). Design: `13-phase6-durable-replay.md`. Deviations from it, and why: `p6-plan-1.md`.

### Task p6.8: The evaluator gate: effect builtins, puts, now and random

`value.Builtin.Effect` names an effect builtin. `applyFunction` sends every call of one through `callEffect`, then `effect(kind, args, do)` (`arv/evaluator/gate.go`):
- with no log, `do` runs as before, charging `alloc_mb` directly;
- with one, `Task.Effect` records the encoded arguments, the encoded result, the bytes `do` charged and whether the result is a cancellation; replayed, it charges the logged bytes and decodes the logged result. A divergence is an error of kind `replay`.

The effects: `http_get`, `read_file`, `write_file`, `getenv`, `sleep`, `clock_ms`, and the new `now()` (Unix milliseconds) and `random(n)` (0 to n-1). `puts` renders its lines first, then prints them as one `Task.Print`. `TestEveryBuiltinIsPureOrAnEffect` makes every future builtin choose a class.

**Files:**
- Modify: `arv/evaluator/builtins.go`
- Modify: `arv/evaluator/clock.go`
- Modify: `arv/evaluator/effects.go`
- Modify: `arv/evaluator/evaluator.go`
- Create: `arv/evaluator/gate.go`
- Modify: `arv/value/value.go`
- Create (test): `arv/evaluator/gate_replay_test.go`
- Create (test): `arv/evaluator/gate_test.go`

- [ ] **Step 1: Write the failing tests**

Create `arv/evaluator/gate_replay_test.go`:

```go
package evaluator

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/delavalom/arvlang/arv/lexer"
	"github.com/delavalom/arvlang/arv/parser"
	"github.com/delavalom/arvlang/arv/runtime"
	"github.com/delavalom/arvlang/arv/value"
)

// runLogged runs src in a root task holding caps, with the effect log l
// printing to out, then finishes l. It returns the run's result and
// Finish's error.
func runLogged(t *testing.T, src string, l *runtime.EffectLog, out *bytes.Buffer, caps ...runtime.Capability) (value.Object, error) {
	t.Helper()
	p := parser.New(lexer.New(src))
	program := p.ParseProgram()
	if len(p.Errors()) > 0 {
		t.Fatalf("parse: %v", p.Errors())
	}
	root := runtime.NewRoot(l.Start(context.Background()), runtime.NewCapSet(caps...), out)
	root.SetLog(l)
	res := New(root).Run(program, value.NewEnvironment())
	return res, l.Finish(value.EncodeRequest([]value.Object{res}))
}

// recordLogged records src; it returns what it printed, its result and
// its log.
func recordLogged(t *testing.T, src string, caps ...runtime.Capability) (string, value.Object, []byte) {
	t.Helper()
	var out, log bytes.Buffer
	l, err := runtime.NewRecorder(&log, runtime.Header{}, &out, nil)
	if err != nil {
		t.Fatal(err)
	}
	res, err := runLogged(t, src, l, &out, caps...)
	if err != nil {
		t.Fatalf("recording: %v", err)
	}
	return out.String(), res, log.Bytes()
}

// replayLogged replays log for src with the root holding caps.
func replayLogged(t *testing.T, src string, log []byte, caps ...runtime.Capability) (string, value.Object, error) {
	t.Helper()
	read, err := runtime.ReadLog(bytes.NewReader(log))
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	l, err := runtime.NewReplayer(read, &out)
	if err != nil {
		t.Fatal(err)
	}
	res, err := runLogged(t, src, l, &out, caps...)
	return out.String(), res, err
}

// I7 in the evaluator: every effect goes through the log, and a replay
// answers each from it: the environment, the clock and the random numbers
// of the recorded run, and its output, whatever the world says now.
func TestEffectsAreReplayed(t *testing.T) {
	t.Setenv("ARV_P6_GATE", "recorded")
	env, _ := runtime.New(runtime.Env, "ARV_P6_GATE")
	src := `puts(getenv("ARV_P6_GATE")); puts(random(1000000000)); let t = now(); sleep(1);
puts(clock_ms() > -1); [t, getenv("ARV_P6_GATE"), {"b": 1, "a": 2}]`
	printed, res, log := recordLogged(t, src, env)
	if !strings.HasPrefix(printed, "recorded\n") || isError(res) {
		t.Fatalf("recording printed %q, returned %s", printed, res.Inspect())
	}
	t.Setenv("ARV_P6_GATE", "changed")
	replayed, rres, err := replayLogged(t, src, log, env)
	if err != nil || replayed != printed || rres.Inspect() != res.Inspect() {
		t.Fatalf("replay: %v, printed %q, returned %s; want %q, %s", err, replayed, rres.Inspect(), printed, res.Inspect())
	}
	_, rres, err = replayLogged(t, `puts(getenv("ARV_P6_OTHER"))`, log, env)
	e, ok := rres.(*value.Error)
	want := `at task root, effect 1: the log has getenv("ARV_P6_GATE"), the program asked for getenv("ARV_P6_OTHER")`
	if !ok || e.ErrKind() != runtime.KindReplay || !strings.Contains(e.Message, want) || err == nil {
		t.Fatalf("an edited program: %s, %v", rres.Inspect(), err)
	}
}
```

Create `arv/evaluator/gate_test.go`:

```go
package evaluator

import (
	"slices"
	"testing"
)

// pureBuiltins are the builtins whose result depends only on their
// arguments. The capability constructors resolve a relative path against
// the working directory, which a log's header records (see Phase 6 notes).
var pureBuiltins = []string{"len", "last", "push", "error", "json", "split", "lines", "net", "fs_read", "fs_write", "env"}

// I7: every builtin is either pure or an effect that goes through the
// gate under its own name. A new builtin must be put in one class or the
// other here, so none reaches the world unlogged.
func TestEveryBuiltinIsPureOrAnEffect(t *testing.T) {
	effects := []string{"http_get", "read_file", "write_file", "getenv", "sleep", "clock_ms", "now", "random", "puts"}
	for name, b := range builtins {
		switch {
		case slices.Contains(pureBuiltins, name):
			if b.Effect != "" {
				t.Errorf("%s is listed as pure but is the effect %q", name, b.Effect)
			}
		case slices.Contains(effects, name):
			if b.Effect != name {
				t.Errorf("%s is an effect logged as %q", name, b.Effect)
			}
		default:
			t.Errorf("builtin %s is neither pure nor an effect: classify it", name)
		}
	}
	for _, name := range append(effects, pureBuiltins...) {
		if builtins[name] == nil {
			t.Errorf("no builtin %s", name)
		}
	}
}

func TestNowAndRandom(t *testing.T) {
	for _, tt := range []struct{ src, want string }{
		{`now() > 1700000000000`, "true"},
		{`let r = random(3); if (r > -1) { r < 3 } else { false }`, "true"},
		{`random(1)`, "0"},
		{`random(0)`, "ERROR: random: want a positive INTEGER bound"},
		{`random("x")`, "ERROR: random: want a positive INTEGER bound"},
		{`now(1)`, "ERROR: now: want 0 arguments, got 1"},
	} {
		if _, got := runOut(t, tt.src); got.Inspect() != tt.want {
			t.Errorf("%s = %s, want %s", tt.src, got.Inspect(), tt.want)
		}
	}
}
```

- [ ] **Step 2: Run them and watch them fail**

Run: `cd <worktree> && go test -race ./arv/evaluator/ -run 'TestEveryBuiltinIsPureOrAnEffect|TestNowAndRandom|TestEffectsAreReplayed'`

Expected: the package fails to build: `b.Effect undefined (type *value.Builtin has no field or method Effect)`.

- [ ] **Step 3: Implement**

In `arv/evaluator/builtins.go`, replace:

```go
		// Under an interpreter, applyFunction calls Interpreter.puts
		// instead, which renders within the scope's budgets.
		Fn: func(t *runtime.Task, args ...value.Object) value.Object {
			for _, arg := range args {
```

with:

```go
		// Under an interpreter, applyFunction calls Interpreter.puts
		// instead, which renders within the scope's budgets.
		Effect: "puts",
		Fn: func(t *runtime.Task, args ...value.Object) value.Object {
			for _, arg := range args {
```

In `arv/evaluator/builtins.go`, replace:

```go
		return nil
	}
	for _, arg := range args {
		// A string is already charged, and the text is charged as it is
```

with:

```go
		return nil
	}
	if in.Task.Log() != nil {
		return in.putsLogged(args, check)
	}
	for _, arg := range args {
		// A string is already charged, and the text is charged as it is
```

In `arv/evaluator/builtins.go`, replace:

```go
	return NIL
}
```

with:

```go
	return NIL
}

// putsLogged is puts in a run with an effect log: it renders every line
// first, then prints them as one effect (runtime.Task.Print), so a replay
// checks them against the log and prints them in the recorded order.
func (in *Interpreter) putsLogged(args []value.Object, check func(int64) error) value.Object {
	lines := make([]string, len(args))
	req := make([]value.Object, len(args))
	for i, arg := range args {
		s, ok := arg.(*value.String)
		if !ok {
			text, err := value.InspectChecked(arg, check)
			if err != nil {
				return errorValue(err)
			}
			s = &value.String{Value: text}
		}
		lines[i], req[i] = s.Value, s
	}
	if err := in.Task.Print(value.EncodeRequest(req), lines); err != nil {
		return errorOf(err, runtime.KindReplay)
	}
	return NIL
}
```

In `arv/evaluator/clock.go`, replace:

```go

func init() {
	builtins["clock_ms"] = &value.Builtin{Fn: clockMS}
}

// clock_ms() is the milliseconds since the process started, on a
// monotonic clock: only the difference of two readings means anything,
// as a latency. Like sleep it needs no capability. It is an effect (I7):
// Phase 6 must record it.
func clockMS(t *runtime.Task, args ...value.Object) value.Object {
	if len(args) != 0 {
```

with:

```go

func init() {
	builtins["clock_ms"] = &value.Builtin{Fn: clockMS, Effect: "clock_ms"}
}

// clock_ms() is the milliseconds since the process started, on a
// monotonic clock: only the difference of two readings means anything,
// as a latency. Like sleep it needs no capability. It is an effect (I7).
func clockMS(t *runtime.Task, args ...value.Object) value.Object {
	if len(args) != 0 {
```

In `arv/evaluator/effects.go`, replace:

```go
		builtins[name] = &value.Builtin{Fn: capabilityConstructor(kind)}
	}
	builtins["http_get"] = &value.Builtin{Charging: httpGet}
	builtins["read_file"] = &value.Builtin{Charging: readFile}
	builtins["write_file"] = &value.Builtin{Charging: writeFile}
	builtins["getenv"] = &value.Builtin{Fn: getenv}
	builtins["sleep"] = &value.Builtin{Fn: sleep}
}

```

with:

```go
		builtins[name] = &value.Builtin{Fn: capabilityConstructor(kind)}
	}
	builtins["http_get"] = &value.Builtin{Charging: httpGet, Effect: "http_get"}
	builtins["read_file"] = &value.Builtin{Charging: readFile, Effect: "read_file"}
	builtins["write_file"] = &value.Builtin{Charging: writeFile, Effect: "write_file"}
	builtins["getenv"] = &value.Builtin{Fn: getenv, Effect: "getenv"}
	builtins["sleep"] = &value.Builtin{Fn: sleep, Effect: "sleep"}
}

```

In `arv/evaluator/evaluator.go`, replace:

```go
			return in.puts(args)
		}
		if fn.Charging != nil {
			return fn.Charging(in.Task, in.chargeBytes, args...)
```

with:

```go
			return in.puts(args)
		}
		if fn.Effect != "" {
			return in.callEffect(fn, args)
		}
		if fn.Charging != nil {
			return fn.Charging(in.Task, in.chargeBytes, args...)
```

Create `arv/evaluator/gate.go`:

```go
package evaluator

import (
	"math/rand/v2"
	"time"

	"github.com/delavalom/arvlang/arv/runtime"
	"github.com/delavalom/arvlang/arv/value"
)

func init() {
	builtins["now"] = &value.Builtin{Fn: now, Effect: "now"}
	builtins["random"] = &value.Builtin{Fn: random, Effect: "random"}
}

// now() is the wall-clock time in milliseconds since the Unix epoch. It
// needs no capability; it is an effect (I7), so a replay sees the time
// the recorded run saw.
func now(t *runtime.Task, args ...value.Object) value.Object {
	if len(args) != 0 {
		return newError("now: want 0 arguments, got %d", len(args))
	}
	return &value.Integer{Value: time.Now().UnixMilli()}
}

// random(n) is a random integer from 0 to n-1. It needs no capability;
// it is an effect (I7), so a replay draws the same numbers.
func random(t *runtime.Task, args ...value.Object) value.Object {
	if len(args) != 1 {
		return newError("random: want 1 argument, got %d", len(args))
	}
	n, ok := args[0].(*value.Integer)
	if !ok || n.Value < 1 {
		return newError("random: want a positive INTEGER bound")
	}
	return &value.Integer{Value: rand.Int64N(n.Value)}
}

// callEffect calls fn, an effect builtin, through the gate. A plain Fn's
// result is charged after it returns, replayed or not, as for any other
// builtin; a Charging one charges itself (the gate replays its charges).
func (in *Interpreter) callEffect(fn *value.Builtin, args []value.Object) value.Object {
	result := in.effect(fn.Effect, args, func(charge func(int64) error) value.Object {
		if fn.Charging != nil {
			return fn.Charging(in.Task, charge, args...)
		}
		return fn.Fn(in.Task, args...)
	})
	if fn.Charging != nil || fn.Shares || isError(result) {
		return result
	}
	return in.alloc(result, allocSize(result))
}

// effect is the gate every effect goes through (I7). Without a log it
// runs do, which charges what it allocates to charge. With one, the task's
// log records it (runtime.Task.Effect): its arguments, its result, the
// bytes it charged and whether it was cancelled; or a replay answers it
// from the log without running it, charging the recorded bytes and
// decoding the recorded result. A replay that diverges fails with kind
// replay, and the whole run stops.
func (in *Interpreter) effect(kind string, args []value.Object, do func(charge func(int64) error) value.Object) value.Object {
	if in.Task.Log() == nil {
		return do(in.chargeBytes)
	}
	var result value.Object
	o, err := in.Task.Effect(kind, value.EncodeRequest(args), func() runtime.Outcome {
		var charged int64
		result = do(func(n int64) error { charged += n; return in.chargeBytes(n) })
		res, err := value.Encode(result)
		if err != nil { // a bug: no effect returns what the codec refuses
			result = newError("%s: cannot log its result: %s", kind, err)
			res, _ = value.Encode(result)
		}
		e, failed := result.(*value.Error)
		return runtime.Outcome{Res: res, Charged: charged, Cancelled: failed && e.ErrKind() == runtime.KindCancelled}
	})
	if err != nil {
		return errorOf(err, runtime.KindReplay)
	}
	if !o.Replayed {
		return result
	}
	in.chargeBytes(o.Charged) // a failure here was the recorded result's own
	v, err := value.Decode(o.Res)
	if err != nil {
		return errorOf(in.Task.Log().Diverge("at task %s: the log's result for %s does not decode: %s", in.Task.ID, kind, err), runtime.KindReplay)
	}
	return v
}
```

In `arv/value/value.go`, replace:

```go
	// charged to the allocation budget.
	Shares bool
}

```

with:

```go
	// charged to the allocation budget.
	Shares bool
	// Effect, unless empty, names an effect builtin (I7): its result
	// depends on the world (network, files, environment, time,
	// randomness, output), so every call goes through the run's effect
	// log under this name, to be recorded or replayed.
	Effect string
}

```

- [ ] **Step 4: Run the tests, then the full checks**

Run: `cd <worktree> && go test -race ./arv/evaluator/ -run 'TestEveryBuiltinIsPureOrAnEffect|TestNowAndRandom|TestEffectsAreReplayed'`, then `cd <worktree> && gofmt -l . && go vet ./... && go test -race ./...`

Expected: `ok` for every package, and `gofmt -l .` prints nothing.

- [ ] **Step 5: Commit**

```bash
cd <worktree> && git add arv/evaluator/builtins.go arv/evaluator/clock.go arv/evaluator/effects.go arv/evaluator/evaluator.go arv/evaluator/gate.go arv/evaluator/gate_replay_test.go arv/evaluator/gate_test.go arv/value/value.go && git commit -F - <<'MSG'
feat(evaluator): every effect goes through the gate; now() and random(n)

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
MSG
```
