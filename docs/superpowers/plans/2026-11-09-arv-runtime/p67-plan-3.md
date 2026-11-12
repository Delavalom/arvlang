# Phase 6x7, task p67.3: model budget reservations are logged (`llm_reserve`)

> **For agentic workers:** this file is one task, p67.3. Do it test first, exactly as written; if something here does not match the code you find, stop and report instead of improvising. Prerequisite: tasks p67.1 to p67.2 are committed on `phase/p67`. Work in `/Users/delavalom/delavalom-labs/arvlang-wt/p67` on branch `phase/p67`. Start every shell command with `cd /Users/delavalom/delavalom-labs/arvlang-wt/p67 &&`: the shell's working directory does not persist. Before committing: `gofmt -l .` prints nothing, `go vet ./...` is clean and `go test -race ./...` is green. End the commit message with a blank line and then `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`. Never use `--no-verify`, never push, never touch remotes; keep Go files under 15 KB and markdown under 18 KB. `go.mod` says `go 1.24.0` (toolchain 1.26.2): `min`, `max` and `t.Chdir` are available; do not use `testing/synctest` or `sync.WaitGroup.Go`. Before a long test run, check `df -h /System/Volumes/Data`; below 1 GB free, stop and report status "blocked", reason "disk" (only `go clean -cache` may be deleted). The machine is shared and heavily loaded: keep wall-clock bounds generous and order events with server gates, not sleeps. No test may reach the network or spend money: every test uses the fake provider or a local `httptest` server; never set `ARV_LIVE_ANTHROPIC`, never put a real API key anywhere. If a test exposes a real bug outside this task, mark it `t.Skip("BUG: ...")` with a reproduction and report it; never weaken a test to make it pass.
>
> Every code block below was applied, in task order, to a scratch copy of `feat/phases-3-8` at `b516ca2` (Phases 3 to 8.3 and 5E merged) and passed `gofmt -l .`, `go vet ./...` and `go test -race ./...` (including `e2e/`); each "expected failure" was seen. Designs: `13-phase6-durable-replay.md`, `14-phase7-agents.md`; what was built overrides them (`16-implementation-notes.md`, `16o-phase-index.md`). Phase overview and deviations: `p67-plan-1.md`.

### Task p67.3: model budget reservations are logged (`llm_reserve`)

Two tasks under one `tokens` or `usd_cents` scope race for a pool that holds one call's reservation. `Budget.Reserve` (`arv/runtime/budget_llm.go`) is not an effect, so a replay, which runs tasks in its own order, can grant the other task: its `llm` effect then finds the log's next record is the task's end, and the run diverges. The fix makes the reservation an effect, `llm_reserve`, whenever the run keeps a log and some level limits tokens or usd_cents (`Budget.LimitsModels`). Its request is the tokens and micro-dollars; its result `nil` (granted) or the budget error. Recording or live, it calls `Reserve`. Replayed, a logged error is returned as it is and takes nothing, and a logged grant is taken whatever the pool holds (`Budget.ReserveLogged`, which subtracts at every level and never refuses). Every level then ends where the recording left it, which the test checks through a resume cut before a third call, `c`: the resume runs `c` live, and `c` is refused only if the replayed part spent `a`'s tokens. Runs with no model limit, or no log, take the old path and log nothing new.

The test orders the recording with two host effects: `second` waits in `k8s_wait` until `first` calls `k8s_done` after its call; a replay answers both from the log at once while `first` spins (`spinSrc`, `arv/spin_test.go`, about 300 ms), so `second` reaches the budget first. The price override makes a call cost 10 micro-dollars a token, so `usd_cents: 7` holds one reservation (about 4630 tokens, 4.6 cents) and `a`'s 3000 tokens (3 cents) leave 4 cents. `newOrderedPair` and `orderedPairSrc` are reused by p67.4.

**Files:**
- Create (test): `arv/runtime/budget_llm_log_test.go`
- Create (test): `arv/replay_budget_test.go`
- Create: `arv/runtime/budget_llm_log.go`
- Create: `arv/evaluator/llmbudget.go`
- Modify: `arv/evaluator/llmcall.go`

- [ ] **Step 1: Write the failing tests**

Create `arv/runtime/budget_llm_log_test.go`:

```go
package runtime

import (
	"errors"
	"testing"
)

// A replayed reservation is taken even past the limit, and a level ends
// where the recorded run's did once the settlements are made too.
func TestReserveLogged(t *testing.T) {
	root := Root(Limits{})
	if root.LimitsModels() {
		t.Fatal("a budget with no model limit limits models")
	}
	b := root.Child(Limits{Tokens: 100, USDCents: 1})
	if !b.LimitsModels() || !b.Child(Limits{}).LimitsModels() {
		t.Fatal("a level under a tokens limit does not limit models")
	}
	r1 := b.ReserveLogged(80, 5000)
	r2 := b.ReserveLogged(80, 5000) // past both limits: a replay's order
	if _, err := b.Reserve(1, 0); !isExceeded(err, "tokens") {
		t.Fatalf("Reserve while past the limit: %v", err)
	}
	if !r1.Covers(80, 5000) || r1.Covers(81, 0) || r1.Covers(0, 5001) || !r1.Covers(-1, -1) {
		t.Fatal("Covers")
	}
	r1.Settle(30, 2000)
	r2.Settle(30, 2000)
	// 100 - 60 tokens and 10000 - 4000 micro-dollars are left.
	if _, err := b.Reserve(40, 6000); err != nil {
		t.Fatalf("what is left after settling: %v", err)
	}
	if _, err := b.Reserve(1, 0); !isExceeded(err, "tokens") {
		t.Fatalf("one token past the rest: %v", err)
	}
}

func isExceeded(err error, kind string) bool {
	var ex *ExceededError
	return errors.As(err, &ex) && ex.Kind == kind
}
```

Create `arv/replay_budget_test.go`:

```go
package arv

import (
	"bytes"
	"context"
	"sync"
	"testing"

	"github.com/delavalom/arvlang/arv/llm"
	"github.com/delavalom/arvlang/arv/runtime"
	"github.com/delavalom/arvlang/arv/value"
)

// orderedPair is two tasks, a and b, calling the same model under one
// scope's model budget. The recording orders them: b waits in the host
// effect k8s_wait until a has called k8s_done. A replay answers both host
// effects from the log, so b runs at once, while a first spins: b now
// reaches the model budget first. The log, not the replay's order, must
// decide what each call got (I7).
type orderedPair struct {
	effects map[string]Effect
	host    *llm.Host
	fake    *llm.Fake
	caps    []runtime.Capability
}

func newOrderedPair(then func(ctx context.Context, req llm.Request, aDone <-chan struct{}) (llm.Response, error)) *orderedPair {
	done := make(chan struct{})
	var once sync.Once
	str := func(s string) value.Object { return &value.String{Value: s} }
	p := &orderedPair{effects: map[string]Effect{
		"k8s_done": {Kind: "k8s", Scope: nsScope, Fn: func(t *runtime.Task, a ...value.Object) (value.Object, error) {
			once.Do(func() { close(done) })
			return str("done"), nil
		}},
		"k8s_wait": {Kind: "k8s", Scope: nsScope, Fn: func(t *runtime.Task, a ...value.Object) (value.Object, error) {
			select {
			case <-done:
			case <-t.Ctx.Done():
			}
			return str("waited"), nil
		}},
	}, caps: []runtime.Capability{{Kind: runtime.LLM, Scope: "anthropic:*"}, staging}}
	p.fake = &llm.Fake{Then: func(ctx context.Context, req llm.Request) (llm.Response, error) { return then(ctx, req, done) }}
	p.host = &llm.Host{Providers: map[string]llm.Provider{"anthropic": p.fake},
		Prices: map[string]llm.Price{"anthropic:claude-haiku-5-5": {Input: 10_000_000, Output: 10_000_000}}}
	return p
}

// orderedPairSrc runs a and b under the scope options opts; each prints
// its answer, or its error's kind and message.
func orderedPairSrc(opts string) string {
	return spinSrc() + `let m = "anthropic:claude-haiku-5-5";
let try = fn(p) { match scope { ask(m, p) } { ok(v) => v, err(e) => e["kind"] + ": " + e["message"] } };
let first = fn() { spin(); let r = try("a"); k8s_done("staging"); r };
let second = fn() { k8s_wait("staging"); try("b") };
scope (` + opts + `) {
  let ta = spawn first() with [llm(m), k8s("staging")];
  let tb = spawn second() with [llm(m), k8s("staging")];
  puts(await ta); puts(await tb);
  puts(try("c"));
}`
}

// replaysAlike replays log three times; each must print want and end well.
func replaysAlike(t *testing.T, src, want string, log []byte, effects map[string]Effect) {
	t.Helper()
	for i := range 3 {
		l, err := runtime.ReadLog(bytes.NewReader(log))
		if err != nil {
			t.Fatal(err)
		}
		var out bytes.Buffer
		_, err = Run(src, Options{Replay: l, Out: &out, Effects: effects})
		if err != nil || out.String() != want {
			t.Fatalf("replay %d: %v; printed %q, want %q", i, err, out.String(), want)
		}
	}
}

// Two tasks race for a model budget that holds one call's reservation:
// whichever reserves first is answered and the other refused. The replay
// refuses the same one, so it prints what the recording printed. Then c
// asks for more than the 4000 tokens (or 4 cents) a left: a resume cut
// before c runs c live, against the budget its replayed part spent, and
// is refused too.
func TestReplayOfConcurrentModelBudgets(t *testing.T) {
	for _, opts := range []string{"tokens: 7000", "usd_cents: 7"} {
		t.Run(opts, func(t *testing.T) {
			p := newOrderedPair(func(ctx context.Context, req llm.Request, _ <-chan struct{}) (llm.Response, error) {
				return llm.TextReply(req.Messages[0].Content[0].Text+" answered", 2000, 1000).Response, nil
			})
			src := orderedPairSrc(opts)
			out, log, err := recordRun(t, src, Options{LLM: p.host, Caps: p.caps, Effects: p.effects})
			refused := "budget: budget exceeded: " + map[string]string{"tokens: 7000": "tokens (limit 7000)", "usd_cents: 7": "usd_cents (limit 7)"}[opts] + "\n"
			want := "a answered\n" + refused + refused
			if err != nil || out != want || len(p.fake.Calls()) != 1 {
				t.Fatalf("recording: %v, %d calls; printed %q, want %q", err, len(p.fake.Calls()), out, want)
			}
			replaysAlike(t, src, out, log, p.effects)
			if len(p.fake.Calls()) != 1 {
				t.Fatalf("the replays called the provider")
			}
			crashed := cutAfter(t, log, `"kind":"puts","req":["budget`)
			l, err := runtime.ReadLog(bytes.NewReader(crashed))
			if err != nil {
				t.Fatal(err)
			}
			var got bytes.Buffer
			_, err = Run(src, Options{LLM: p.host, Caps: p.caps, Effects: p.effects, Resume: l, Record: bytes.NewBuffer(bytes.Clone(crashed)), Out: &got})
			if err != nil || got.String() != want || len(p.fake.Calls()) != 1 {
				t.Fatalf("resume: %v, %d calls; printed %q, want %q", err, len(p.fake.Calls()), got.String(), want)
			}
		})
	}
}
```

- [ ] **Step 2: Run it and see it fail**

Run: `cd /Users/delavalom/delavalom-labs/arvlang-wt/p67 && go test ./arv/runtime -run TestReserveLogged -count=1; go test ./arv -run TestReplayOfConcurrentModelBudgets -count=1`

Expected: FAIL. The runtime test does not build (`b.LimitsModels undefined`). Both subtests of the arv test fail at the first replay with `replay diverged at task root/1, effect 3: the log has the task's end, the program asked for llm({"model":"anthropic:claude-haiku-5-5",...`.

- [ ] **Step 3: Log the reservation**

Create `arv/runtime/budget_llm_log.go`:

```go
package runtime

// LimitsModels reports whether some level at or above b limits tokens or
// usd_cents. Only then can a model call's reservation be refused, or its
// settlement fail, and only then does that depend on what other tasks
// reserved first.
func (b *Budget) LimitsModels() bool { return b.tokensAt != nil || b.usdAt != nil }

// ReserveLogged is Reserve for a replayed call whose recorded reservation
// was granted: it sets aside tokens and usdMicros at every level from b up
// and never refuses. A replay runs tasks in its own order, so a level can
// hold less here, for a while, than it held when the recorded run
// reserved; once every logged reservation and settlement has been acted
// out, each level holds what it held in the recorded run.
func (b *Budget) ReserveLogged(tokens, usdMicros int64) *Reservation {
	tokens, usdMicros = max(tokens, 0), max(usdMicros, 0)
	tripped("tokens", b.tokensAt, tokens, (*Budget).tokensPool)
	tripped("usd_cents", b.usdAt, usdMicros, (*Budget).usdPool)
	return &Reservation{b: b, tokens: tokens, usd: usdMicros}
}

// Covers reports whether a call that used tokens and usdMicros stayed
// within r: settling it then only gives back, and cannot fail.
func (r *Reservation) Covers(tokens, usdMicros int64) bool {
	return max(tokens, 0) <= r.tokens && max(usdMicros, 0) <= r.usd
}
```

Create `arv/evaluator/llmbudget.go`:

```go
package evaluator

import (
	"github.com/delavalom/arvlang/arv/runtime"
	"github.com/delavalom/arvlang/arv/value"
)

// A model call reserves from the tokens and usd_cents pools before it is
// sent, and settles after (complete). When concurrent tasks share a pool,
// whether a reservation fits depends on which task reserved first: that
// is scheduling, which a replay does not reproduce (I7). So when a pool
// limits the call and the run keeps a log, the reservation is the effect
// "llm_reserve" (request: the tokens and micro-dollars), whose result
// says whether it was granted. A replay acts the outcome out: a granted
// reservation is taken whatever the pool holds (Budget.ReserveLogged),
// and a refused one takes nothing and fails with the logged error. Every
// call is then refused where the recorded run refused it, and the pools
// end where they ended.
const effectReserve = "llm_reserve"

// reserve is b.Reserve for one model call, through the log when the
// outcome can depend on other tasks.
func (in *Interpreter) reserve(b *runtime.Budget, tokens, usdMicros int64) (*runtime.Reservation, *value.Error) {
	if in.Task.Log() == nil || !b.LimitsModels() {
		r, err := b.Reserve(tokens, usdMicros)
		if err != nil {
			return nil, errorOf(err, runtime.KindBudget)
		}
		return r, nil
	}
	var r *runtime.Reservation
	v := in.effect(effectReserve, amounts(tokens, usdMicros), func(func(int64) error) value.Object {
		got, err := b.Reserve(tokens, usdMicros)
		if err != nil {
			return errorOf(err, runtime.KindBudget)
		}
		r = got
		return NIL
	})
	switch v := v.(type) {
	case *value.Error:
		return nil, v
	case *value.Nil:
	default:
		return nil, errorOf(in.Task.Log().Diverge("at task %s: the log's %s is %s, not nil or an error", in.Task.ID, effectReserve, v.Type()), runtime.KindReplay)
	}
	if r == nil { // replayed: the recorded run's reservation was granted
		r = b.ReserveLogged(tokens, usdMicros)
	}
	return r, nil
}

// amounts is a reservation's or a settlement's request: its tokens and
// micro-dollars.
func amounts(tokens, usdMicros int64) []value.Object {
	return []value.Object{&value.Integer{Value: tokens}, &value.Integer{Value: usdMicros}}
}
```

In `arv/evaluator/llmcall.go`, replace:

```go
	maxTok, maxUSD := int64(est+req.MaxTokens), m.price.Cost(est, req.MaxTokens)
	r, err := b.Reserve(maxTok, maxUSD)
	if err != nil {
		return llm.Response{}, errorOf(err, runtime.KindBudget)
	}
```

with:

```go
	maxTok, maxUSD := int64(est+req.MaxTokens), m.price.Cost(est, req.MaxTokens)
	r, errv := in.reserve(b, maxTok, maxUSD) // an effect when a pool limits it (llmbudget.go)
	if errv != nil {
		return llm.Response{}, errv
	}
```

- [ ] **Step 4: Run it and see it pass**

Run: `cd /Users/delavalom/delavalom-labs/arvlang-wt/p67 && go test ./arv/runtime -run TestReserveLogged -count=1 && go test ./arv -run 'TestReplayOfConcurrentModelBudgets|TestReplayOfModelCalls|TestResume.*Model' -count=1`

Expected: PASS. `TestReplayOfModelCalls` and the resume tests of `arv/resume_model_test.go` still pass: their logs gain `llm_reserve` records only under a limit.

- [ ] **Step 5: Check and commit**

```bash
cd /Users/delavalom/delavalom-labs/arvlang-wt/p67 && gofmt -l . && go vet ./... && go test -race ./...
cd /Users/delavalom/delavalom-labs/arvlang-wt/p67 && git add -A && git commit -m "fix(evaluator): model budget reservations are logged, so concurrent agents replay alike

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```
