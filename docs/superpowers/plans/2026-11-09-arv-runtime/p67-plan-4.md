# Phase 6x7, task p67.4: settlements past a reservation are logged (`llm_settle`)

> **For agentic workers:** this file is one task, p67.4. Do it test first, exactly as written; if something here does not match the code you find, stop and report instead of improvising. Prerequisite: tasks p67.1 to p67.3 are committed on `phase/p67`. Work in `/Users/delavalom/delavalom-labs/arvlang-wt/p67` on branch `phase/p67`. Start every shell command with `cd /Users/delavalom/delavalom-labs/arvlang-wt/p67 &&`: the shell's working directory does not persist. Before committing: `gofmt -l .` prints nothing, `go vet ./...` is clean and `go test -race ./...` is green. End the commit message with a blank line and then `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`. Never use `--no-verify`, never push, never touch remotes; keep Go files under 15 KB and markdown under 18 KB. `go.mod` says `go 1.24.0` (toolchain 1.26.2): `min`, `max` and `t.Chdir` are available; do not use `testing/synctest` or `sync.WaitGroup.Go`. Before a long test run, check `df -h /System/Volumes/Data`; below 1 GB free, stop and report status "blocked", reason "disk" (only `go clean -cache` may be deleted). The machine is shared and heavily loaded: keep wall-clock bounds generous and order events with server gates, not sleeps. No test may reach the network or spend money: every test uses the fake provider or a local `httptest` server; never set `ARV_LIVE_ANTHROPIC`, never put a real API key anywhere. If a test exposes a real bug outside this task, mark it `t.Skip("BUG: ...")` with a reproduction and report it; never weaken a test to make it pass.
>
> Every code block below was applied, in task order, to a scratch copy of `feat/phases-3-8` at `b516ca2` (Phases 3 to 8.3 and 5E merged) and passed `gofmt -l .`, `go vet ./...` and `go test -race ./...` (including `e2e/`); each "expected failure" was seen. Designs: `13-phase6-durable-replay.md`, `14-phase7-agents.md`; what was built overrides them (`16-implementation-notes.md`, `16o-phase-index.md`). Phase overview and deviations: `p67-plan-1.md`.

### Task p67.4: settlements past a reservation are logged (`llm_settle`)

A settlement within its reservation only gives back and cannot fail. One past it (the provider counted more tokens than the estimate) charges the excess to every level and fails if a level goes past its limit, which again depends on what other tasks spent first. In the test both calls reserve about 4630 tokens and both fit `tokens: 12000`; each reply reports 7000, and only one overage fits, so the call that settles second fails. The recording settles `b` second (its provider call waits until `a` is done); the replay settles `b` first and, without the fix, fails `a` instead. The fix logs such a settlement as the effect `llm_settle` (request: tokens and micro-dollars used; result `nil` or the budget error). A replay makes the settlement, so the pools end where they ended, and returns the logged outcome. A settlement within the reservation (every failed call settles to 0 or to the whole reservation) never needs a record. This task uses `newOrderedPair`, `orderedPairSrc` and `replaysAlike` from p67.3; `c` is refused in both runs because the overage left the level spent.

**Files:**
- Create (test): `arv/replay_settle_test.go`
- Modify: `arv/evaluator/llmbudget.go`
- Modify: `arv/evaluator/llmcall.go`

- [ ] **Step 1: Write the failing test**

Create `arv/replay_settle_test.go`:

```go
package arv

import (
	"context"
	"strings"
	"testing"

	"github.com/delavalom/arvlang/arv/llm"
)

// Both calls fit the budget when they reserve, but each uses more than
// it reserved (7000 tokens against about 4600), and only one overage
// fits: the call that settles second fails the budget. In the recording
// that is b, which the provider answers only after a is done. The replay
// settles b first, and must still fail b's settlement, not a's.
func TestReplayOfConcurrentModelSettlements(t *testing.T) {
	for opts, limit := range map[string]string{"tokens: 12000": "tokens (limit 12000)", "usd_cents: 12": "usd_cents (limit 12)"} {
		t.Run(opts, func(t *testing.T) {
			p := newOrderedPair(func(ctx context.Context, req llm.Request, aDone <-chan struct{}) (llm.Response, error) {
				prompt := req.Messages[0].Content[0].Text
				if prompt == "b" {
					select {
					case <-aDone:
					case <-ctx.Done():
						return llm.Response{}, context.Cause(ctx)
					}
				}
				return llm.TextReply(prompt+" answered", 6000, 1000).Response, nil
			})
			src := orderedPairSrc(opts)
			out, log, err := recordRun(t, src, Options{LLM: p.host, Caps: p.caps, Effects: p.effects})
			// c, after both, is refused: the settlement left the level spent.
			want := "a answered\n" + strings.Repeat("budget: budget exceeded: "+limit+"\n", 2)
			if err != nil || out != want || len(p.fake.Calls()) != 2 {
				t.Fatalf("recording: %v, %d calls; printed %q, want %q", err, len(p.fake.Calls()), out, want)
			}
			replaysAlike(t, src, out, log, p.effects)
			if len(p.fake.Calls()) != 2 {
				t.Fatalf("the replays called the provider")
			}
		})
	}
}
```

- [ ] **Step 2: Run it and see it fail**

Run: `cd /Users/delavalom/delavalom-labs/arvlang-wt/p67 && go test ./arv -run TestReplayOfConcurrentModelSettlements -count=1`

Expected: FAIL. Both subtests fail at the first replay with `replay diverged at task root, effect 3: the log has puts("a answered"), the program asked for puts("budget: budget exceeded: tokens (limit 12000)")` (or `usd_cents (limit 12)`).

- [ ] **Step 3: Log the settlement**

Append to `arv/evaluator/llmbudget.go`:

```go

// A settlement within the reservation only gives back and cannot fail.
// One past it (the provider counted more than the estimate) charges the
// excess to every level, and fails if a level goes past its limit: again
// a matter of what other tasks spent first. With a log and a pool that
// limits it, it is the effect "llm_settle" (request: the tokens and
// micro-dollars used). A replay makes the settlement, so the pools end
// where they ended, and returns the logged outcome, not its own.
const effectSettle = "llm_settle"

// settle is r.Settle for a call that used tokens and usdMicros, through
// the log when its outcome can depend on other tasks.
func (in *Interpreter) settle(b *runtime.Budget, r *runtime.Reservation, tokens, usdMicros int64) *value.Error {
	if in.Task.Log() == nil || !b.LimitsModels() || r.Covers(tokens, usdMicros) {
		if err := r.Settle(tokens, usdMicros); err != nil {
			return errorOf(err, runtime.KindBudget)
		}
		return nil
	}
	ran := false
	v := in.effect(effectSettle, amounts(tokens, usdMicros), func(func(int64) error) value.Object {
		ran = true
		if err := r.Settle(tokens, usdMicros); err != nil {
			return errorOf(err, runtime.KindBudget)
		}
		return NIL
	})
	if !ran {
		r.Settle(tokens, usdMicros) // replayed: the outcome is the log's
	}
	switch v := v.(type) {
	case *value.Error:
		return v
	case *value.Nil:
		return nil
	}
	return errorOf(in.Task.Log().Diverge("at task %s: the log's %s is %s, not nil or an error", in.Task.ID, effectSettle, v.Type()), runtime.KindReplay)
}
```

In `arv/evaluator/llmcall.go`, replace:

```go
	}
	if err := r.Settle(used, m.price.Cost(resp.InputTokens, resp.OutputTokens)); err != nil {
		return llm.Response{}, errorOf(err, runtime.KindBudget)
	}
```

with:

```go
	}
	if e := in.settle(b, r, used, m.price.Cost(resp.InputTokens, resp.OutputTokens)); e != nil {
		return llm.Response{}, e
	}
```

- [ ] **Step 4: Run it and see it pass**

Run: `cd /Users/delavalom/delavalom-labs/arvlang-wt/p67 && go test ./arv -run 'TestReplayOfConcurrentModel' -count=1`

Expected: PASS.

- [ ] **Step 5: Check and commit**

```bash
cd /Users/delavalom/delavalom-labs/arvlang-wt/p67 && gofmt -l . && go vet ./... && go test -race ./...
cd /Users/delavalom/delavalom-labs/arvlang-wt/p67 && git add -A && git commit -m "fix(evaluator): a settlement past its reservation is logged

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```
