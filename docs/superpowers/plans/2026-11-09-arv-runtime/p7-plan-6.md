# Phase 7, task p7.6: `tokens`, `usd_cents` and `max_turns` budget pools

> **For agentic workers:** this file is one task, p7.6. Do it test first, exactly as written; if something here does not match the code you find, stop and report instead of improvising. Prerequisite: task p7.5 is committed in your tree. Start every shell command with `cd <your worktree> &&` and use absolute paths: the shell's working directory does not persist. Before committing: `gofmt -l .` prints nothing, `go vet ./...` is clean and `go test -race ./...` is green. End the commit message with a blank line and then `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`. Never use `--no-verify` and never push; keep Go files under 15 KB and markdown under 18 KB. `go.mod` says `go 1.24.0` (toolchain 1.26.2): `t.Chdir`, `min` and `max` are available; do not use `testing/synctest` or `sync.WaitGroup.Go`. Before a long test run, check `df -h /System/Volumes/Data`; below 1 GB free, stop and report (only `go clean -cache` may be deleted). This machine is heavily loaded: keep wall-clock bounds generous. No test may reach the network or spend money: never set `ARV_LIVE_ANTHROPIC`, and never put a real API key anywhere.
>
> Every code block below was applied, in task order, to a scratch copy of `feat/phases-3-8` (Phases 3, 4 and 5 merged; Phase 6 is not) and passed `gofmt -l .`, `go vet ./...` and `go test -race ./...` (including `e2e/`). Design: `14-phase7-agents.md`. Deviations from it, and why: `p7-plan-1.md`.

### Task p7.6: `tokens`, `usd_cents` and `max_turns` budget pools

Two pools on the budget tree, like `steps`: `tokens`, and `usd` in micro-dollars (the limit is given in cents, times 10 000). They differ from `steps` in one way: a call does not know its use in advance, so `Reserve(tokens, usdMicros)` takes the most the call can use from every level up the tree, or nothing and an `*ExceededError` (`budget exceeded: tokens (limit 100)`, `budget exceeded: usd_cents (limit 1)`) if any level cannot cover it. `Settle` then gives back what was not used, or charges the overage, which fails like any spent budget. A settled reservation never grows a pool past its limit (I6).

`max_turns` is not a pool: it is how many calls one `agent` may make, the smallest of the enclosing levels' values, 20 at the root unless set. `LimitsUSD` tells the caller whether a usd limit applies, so an unpriced model can be refused (p7.9).

**Files:**
- Create: `arv/runtime/budget_llm.go`
- Modify: `arv/runtime/budget.go`
- Create (test): `arv/runtime/budget_llm_test.go`

- [ ] **Step 1: Write the failing tests**

Create `arv/runtime/budget_llm_test.go`:

```go
package runtime

import (
	"errors"
	"math/rand"
	"sync"
	"testing"
)

func wantModelExceeded(t *testing.T, err error, kind string, limit int64) {
	t.Helper()
	var e *ExceededError
	if !errors.As(err, &e) || e.Kind != kind || e.Limit != limit {
		t.Fatalf("err = %v, want budget exceeded: %s (limit %d)", err, kind, limit)
	}
}

func TestReserveAndSettleTokens(t *testing.T) {
	root := Root(Limits{Tokens: 100})
	r, err := root.Reserve(80, 0)
	if err != nil {
		t.Fatal(err)
	}
	// 20 left while 80 are reserved: a second call does not fit.
	_, err = root.Reserve(30, 0)
	wantModelExceeded(t, err, "tokens", 100)
	if err := r.Settle(50, 0); err != nil { // 30 come back
		t.Fatal(err)
	}
	if err := r.Settle(0, 0); err != nil { // only the first Settle counts
		t.Fatal(err)
	}
	r2, err := root.Reserve(50, 0)
	if err != nil {
		t.Fatalf("50 of the 50 left: %v", err)
	}
	_, err = root.Reserve(1, 0)
	wantModelExceeded(t, err, "tokens", 100)
	if err := r2.Settle(0, 0); err != nil { // a call that never ran
		t.Fatal(err)
	}
	if _, err := root.Reserve(50, 0); err != nil {
		t.Fatalf("after a refund: %v", err)
	}
}

func TestUSDLimitIsInCents(t *testing.T) {
	root := Root(Limits{USDCents: 1}) // 10_000 µ$
	if !root.LimitsUSD() || Root(Limits{}).LimitsUSD() {
		t.Fatal("LimitsUSD is wrong")
	}
	r, err := root.Reserve(0, 8_000)
	if err != nil {
		t.Fatal(err)
	}
	_, err = root.Reserve(0, 8_000)
	wantModelExceeded(t, err, "usd_cents", 1)
	if err := r.Settle(0, 6_000); err != nil {
		t.Fatal(err)
	}
	_, err = root.Reserve(0, 4_001)
	wantModelExceeded(t, err, "usd_cents", 1)
	if _, err := root.Reserve(0, 4_000); err != nil {
		t.Fatal(err)
	}
}

// Use beyond the reservation is charged, and a level it takes past its
// limit stays spent: every later call is refused, even an empty one.
func TestSettleOverageSpendsTheLevel(t *testing.T) {
	root := Root(Limits{Tokens: 100})
	r, _ := root.Reserve(90, 0)
	wantModelExceeded(t, r.Settle(120, 0), "tokens", 100)
	_, err := root.Reserve(0, 0)
	wantModelExceeded(t, err, "tokens", 100)
}

// A refused reservation sets nothing aside, at any level.
func TestRefusedReservationTakesNothing(t *testing.T) {
	root := Root(Limits{Tokens: 1000, USDCents: 1})
	inner := root.Child(Limits{Tokens: 100})
	_, err := inner.Reserve(50, 20_000) // tokens fit, dollars do not
	wantModelExceeded(t, err, "usd_cents", 1)
	if _, err := inner.Reserve(100, 10_000); err != nil {
		t.Fatalf("everything is still there: %v", err)
	}
}

// I6: a nested level never allows more than the levels around it have
// left, across task boundaries, and max_turns only shrinks.
func TestModelBudgetsNest(t *testing.T) {
	root := Root(Limits{Tokens: 100, MaxTurns: 5})
	if root.MaxTurns() != 5 || Root(Limits{}).MaxTurns() != DefaultMaxTurns {
		t.Fatal("root max_turns is wrong")
	}
	r, _ := root.Reserve(70, 0)
	defer r.Settle(70, 0)
	child := root.Child(Limits{}).Task().Child(Limits{Tokens: 1000, MaxTurns: 50})
	if child.MaxTurns() != 5 {
		t.Fatalf("max_turns = %d, want 5 (a nested 50 cannot widen it)", child.MaxTurns())
	}
	if root.Child(Limits{MaxTurns: 2}).Task().MaxTurns() != 2 {
		t.Fatal("a nested max_turns does not shrink")
	}
	_, err := child.Reserve(31, 0)
	wantModelExceeded(t, err, "tokens", 100)
	if _, err := child.Reserve(30, 0); err != nil {
		t.Fatal(err)
	}
}

func TestValidateModelLimits(t *testing.T) {
	for _, l := range []Limits{{Tokens: -1}, {USDCents: -1}, {MaxTurns: -1}, {USDCents: MaxUSDCents + 1}} {
		if l.Validate() == nil {
			t.Errorf("Validate(%+v) = nil", l)
		}
	}
	if err := (Limits{Tokens: 1, USDCents: MaxUSDCents, MaxTurns: 1}).Validate(); err != nil {
		t.Fatal(err)
	}
}

// I6 under concurrency: tasks reserving and settling at once never spend
// more than a level's limit, and what is left at the end is exactly the
// limit minus what was used.
func TestConcurrentReservationsProperty(t *testing.T) {
	for seed := int64(0); seed < 20; seed++ {
		rng := rand.New(rand.NewSource(seed))
		limit := 500 + rng.Int63n(1000)
		root := Root(Limits{Tokens: limit})
		levels := []*Budget{root.Child(Limits{}).Task(), root.Child(Limits{Tokens: limit / 2}), root}
		var mu sync.Mutex
		var used int64
		var wg sync.WaitGroup
		for g := 0; g < 8; g++ {
			b := levels[g%len(levels)]
			n, u := 1+rng.Int63n(100), rng.Int63n(100)
			wg.Add(1)
			go func() {
				defer wg.Done()
				for i := 0; i < 50; i++ {
					r, err := b.Reserve(n, 0)
					if err != nil {
						continue
					}
					got := min(u, n)
					if r.Settle(got, 0) == nil {
						mu.Lock()
						used += got
						mu.Unlock()
					}
				}
			}()
		}
		wg.Wait()
		if left := root.tokens.left.Load(); used > limit || left != limit-used {
			t.Fatalf("seed %d: used %d of %d, %d left", seed, used, limit, left)
		}
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test -run '^(TestReserveAndSettleTokens|TestUSDLimitIsInCents|TestSettleOverageSpendsTheLevel|TestRefusedReservationTakesNothing|TestModelBudgetsNest|TestValidateModelLimits|TestConcurrentReservationsProperty)$' ./arv/runtime/`

Expected: FAIL: the package does not compile. The output includes:

```text
# github.com/delavalom/arvlang/arv/runtime [github.com/delavalom/arvlang/arv/runtime.test]
arv/runtime/budget_llm_test.go:19:22: unknown field Tokens in struct literal of type Limits
arv/runtime/budget_llm_test.go:20:17: root.Reserve undefined (type *Budget has no field or method Reserve)
arv/runtime/budget_llm_test.go:25:16: root.Reserve undefined (type *Budget has no field or method Reserve)
```

- [ ] **Step 3: Implement**

In `arv/runtime/budget.go`, replace:

```go
	LiveTasks  int           // Root only: tasks alive at once in the run; 0 = DefaultLiveTasks
}
```

with:

```go
	LiveTasks  int           // Root only: tasks alive at once in the run; 0 = DefaultLiveTasks
	Tokens     int64         // model tokens, input and output, of every call under the scope
	USDCents   int64         // what those calls cost, in US cents (see llm.Price)
	MaxTurns   int           // model calls one agent(...) may make; 0 = the enclosing level's (DefaultMaxTurns at the root)
}
```

In `arv/runtime/budget.go`, replace:

```go
		{"AllocBytes", l.AllocBytes}, {"LiveTasks", int64(l.LiveTasks)},
	} {
```

with:

```go
		{"AllocBytes", l.AllocBytes}, {"LiveTasks", int64(l.LiveTasks)},
		{"Tokens", l.Tokens}, {"USDCents", l.USDCents}, {"MaxTurns", int64(l.MaxTurns)},
	} {
```

In `arv/runtime/budget.go`, after:

```go
			return fmt.Errorf("invalid limits: %s is negative (%d)", f.name, f.v)
		}
	}
```

add:

```go
	if l.USDCents > MaxUSDCents {
		return fmt.Errorf("invalid limits: USDCents is too large (%d, max %d)", l.USDCents, MaxUSDCents)
	}
```

In `arv/runtime/budget.go`, replace:

```go
	allocAt *Budget       // nearest level at or above this one with an alloc pool
}
```

with:

```go
	allocAt *Budget       // nearest level at or above this one with an alloc pool
	models                // the tokens and usd pools and max_turns (budget_llm.go)
}
```

In `arv/runtime/budget.go`, replace:

```go
	b.depth = &counter{limit: max(MaxRunEvalDepth, (live+1)*LiveTaskDepth)}
	return b
```

with:

```go
	b.depth = &counter{limit: max(MaxRunEvalDepth, (live+1)*LiveTaskDepth)}
	if b.turns == 0 {
		b.turns = DefaultMaxTurns
	}
	return b
```

In `arv/runtime/budget.go`, replace:

```go
func (b *Budget) Task() *Budget {
	return &Budget{parent: b, live: b.live, depth: b.depth, task: true, stepsAt: b.stepsAt, allocAt: b.allocAt}
}
```

with:

```go
func (b *Budget) Task() *Budget {
	return &Budget{parent: b, live: b.live, depth: b.depth, task: true, stepsAt: b.stepsAt, allocAt: b.allocAt,
		models: models{tokensAt: b.tokensAt, usdAt: b.usdAt, turns: b.turns}}
}
```

In `arv/runtime/budget.go`, replace:

```go
		b.allocAt = b
	}
	return b
```

with:

```go
		b.allocAt = b
	}
	b.models = newModels(b, parent, l)
	return b
```

Create `arv/runtime/budget_llm.go`:

```go
package runtime

import (
	"math"
	"sync"
)

// DefaultMaxTurns is how many model calls one agent(...) may make when no
// level sets max_turns.
const DefaultMaxTurns = 20

// MaxUSDCents is the largest usd_cents limit: the pool counts
// micro-dollars, 10_000 to the cent, in an int64.
const MaxUSDCents = math.MaxInt64 / microsPerCent

const microsPerCent = 10_000

// models is a level's share of the model budgets: its own tokens and usd
// pools (nil: no limit at this level), the nearest levels at or above it
// that have one, and the max_turns in force there.
type models struct {
	tokens, usd     *pool
	tokensAt, usdAt *Budget
	turns           int
}

// newModels is the models part of level b, opened under parent with limits l. A
// max_turns can only shrink the enclosing one (I6).
func newModels(b, parent *Budget, l Limits) models {
	m := models{tokens: newPool(l.Tokens), usd: newPool(l.USDCents * microsPerCent), turns: l.MaxTurns}
	if parent != nil {
		m.tokensAt, m.usdAt = parent.tokensAt, parent.usdAt
		if m.turns == 0 || m.turns > parent.turns {
			m.turns = parent.turns
		}
	}
	if m.tokens != nil {
		m.tokensAt = b
	}
	if m.usd != nil {
		m.usdAt = b
	}
	return m
}

// MaxTurns is the max_turns in force at b.
func (b *Budget) MaxTurns() int { return b.turns }

// LimitsUSD reports whether some level at or above b limits usd_cents, so
// a model call there needs a price.
func (b *Budget) LimitsUSD() bool { return b.usdAt != nil }

// Reservation is what Reserve set aside at every level for one model call.
type Reservation struct {
	b           *Budget
	tokens, usd int64
	once        sync.Once
}

// Reserve sets aside tokens and usd micro-dollars at every level from b
// up that limits them, before a model call. If any level cannot cover its
// share, it sets nothing aside and fails with an *ExceededError naming
// that level's limit ("tokens", or "usd_cents" in cents), so the call is
// refused before any request goes out. Settle the reservation once the
// call returns.
//
// Each level is updated atomically, but not all levels at once: two calls
// reserving together near a limit can both be refused where one alone
// would fit. That errs on the side of the limit.
func (b *Budget) Reserve(tokens, usdMicros int64) (*Reservation, error) {
	if err := take(b.tokensAt, max(tokens, 0), "tokens", (*Budget).tokensPool); err != nil {
		return nil, err
	}
	if err := take(b.usdAt, max(usdMicros, 0), "usd_cents", (*Budget).usdPool); err != nil {
		give(b.tokensAt, max(tokens, 0), (*Budget).tokensPool)
		return nil, err
	}
	return &Reservation{b: b, tokens: max(tokens, 0), usd: max(usdMicros, 0)}, nil
}

// Settle replaces the reservation with what the call really used: the
// unused part goes back to every level, and any use beyond the
// reservation is charged to them. That excess can take a level past its
// limit; Settle then fails with an *ExceededError, and the level stays
// spent. Only the first Settle of a reservation counts. Settle(0, 0)
// gives a call that never ran everything back.
func (r *Reservation) Settle(tokens, usdMicros int64) error {
	var err error
	r.once.Do(func() {
		e1 := adjust(r.b.tokensAt, r.tokens, max(tokens, 0), "tokens", (*Budget).tokensPool)
		e2 := adjust(r.b.usdAt, r.usd, max(usdMicros, 0), "usd_cents", (*Budget).usdPool)
		if err = e1; err == nil {
			err = e2
		}
	})
	return err
}

func (b *Budget) tokensPool() (*pool, *Budget) { return b.tokens, b.parent.tokensLevel() }
func (b *Budget) usdPool() (*pool, *Budget)    { return b.usd, b.parent.usdLevel() }

func (b *Budget) tokensLevel() *Budget {
	if b == nil {
		return nil
	}
	return b.tokensAt
}

func (b *Budget) usdLevel() *Budget {
	if b == nil {
		return nil
	}
	return b.usdAt
}

// take subtracts n from the pool of every level from l up, or, if any
// would go below zero, from none, failing with the error for the level
// left furthest short.
func take(l *Budget, n int64, kind string, poolOf func(*Budget) (*pool, *Budget)) error {
	err := exceeded(kind, tripped(kind, l, n, poolOf))
	if err != nil {
		give(l, n, poolOf) // a spent level refuses even n == 0
	}
	return err
}

// give adds n back to the pool of every level from l up.
func give(l *Budget, n int64, poolOf func(*Budget) (*pool, *Budget)) {
	for l != nil {
		p, next := poolOf(l)
		p.left.Add(n)
		l = next
	}
}

// adjust moves every level from l up from having reserved r to having
// spent used.
func adjust(l *Budget, r, used int64, kind string, poolOf func(*Budget) (*pool, *Budget)) error {
	if used <= r {
		give(l, r-used, poolOf)
		return nil
	}
	return exceeded(kind, tripped(kind, l, used-r, poolOf))
}

// exceeded states a usd_cents error's limit in cents, as the scope option
// set it; the pool counts micro-dollars.
func exceeded(kind string, err error) error {
	if e, ok := err.(*ExceededError); ok && kind == "usd_cents" {
		return &ExceededError{Kind: kind, Limit: e.Limit / microsPerCent}
	}
	return err
}
```

- [ ] **Step 4: Run to verify it passes**

Run: `go test -race -run '^(TestReserveAndSettleTokens|TestUSDLimitIsInCents|TestSettleOverageSpendsTheLevel|TestRefusedReservationTakesNothing|TestModelBudgetsNest|TestValidateModelLimits|TestConcurrentReservationsProperty)$' ./arv/runtime/`, then `gofmt -l .`, `go vet ./...` and `go test -race ./...`.
Expected: PASS everywhere.

- [ ] **Step 5: Commit**

```bash
git add arv/runtime/budget_llm_test.go arv/runtime/budget.go arv/runtime/budget_llm.go
git commit -m "feat(runtime): tokens, usd_cents and max_turns budgets

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```
