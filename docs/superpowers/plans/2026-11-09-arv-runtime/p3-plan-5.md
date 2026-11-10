# Phase 3, task p3.5: `runtime.Budget`

> **For agentic workers:** this file is one task, p3.5. Do it test first, exactly as written; if something here does not match the code you find, stop and report instead of improvising. Prerequisite: tasks p3.1 to p3.4 are committed in your tree. Start every shell command with `cd <your worktree> &&`. Before committing: `gofmt -l .` prints nothing, `go vet ./...` is clean and `go test -race ./...` is green. End the commit message with a blank line and then `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`. Never use `--no-verify`; keep Go files under 15 KB. `go.mod` says 1.22.5, so do not use `os.Root`, `t.Chdir`, `testing/synctest` or `sync.WaitGroup.Go`.
>
> Every code block below was applied, in task order, to a scratch copy of the tree and passed `go vet ./...` and `go test -race ./...` (including `e2e/`). Design: `10-phase3-budgets.md`. Deviations from it, and why: `p3-plan-1.md`.

### Task p3.5: `runtime.Budget`

A tree of budgets: `Root` for the run, `Child` for each scope, `Task` for each task body. It is standard library only, and nothing uses it until p3.7.
- **Steps and alloc** are pools charged at a level and every level above it, across tasks. The smallest remaining limit wins (I6). Nothing is refunded.
- **Task slots (`tasks:`)** are a scope's direct children. They are taken up to the first task boundary only, which avoids the design's deadlock with `tasks: 2`, and they chain through the enclosing scopes of the same task (I6).
- **The run-wide live-task cap** fails instead of blocking. Its default, `DefaultLiveTasks`, is 1000: not unlimited, because spawn recursion is unbounded (16-notes), and under the race detector's limit of 8128 goroutines.

`TestBudgetsOnlyShrinkProperty` is the I6 property test that the kickoff asks for.

**Files:**
- Create: `arv/runtime/budget.go`
- Create (test): `arv/runtime/budget_tasks_test.go`
- Create (test): `arv/runtime/budget_test.go`

- [ ] **Step 1: Write the failing tests**

Create `arv/runtime/budget_tasks_test.go`:

```go
package runtime

import (
	"context"
	"errors"
	"testing"
	"time"
)

// acquireAsync calls b.Acquire on a new goroutine and reports the result.
func acquireAsync(ctx context.Context, b *Budget) <-chan error {
	ch := make(chan error, 1)
	go func() { ch <- b.Acquire(ctx) }()
	return ch
}

// blocked fails the test unless ch stays empty for a moment.
func blocked(t *testing.T, ch <-chan error) {
	t.Helper()
	select {
	case err := <-ch:
		t.Fatalf("Acquire returned %v, want it to block", err)
	case <-time.After(50 * time.Millisecond):
	}
}

// done fails the test unless ch delivers within 10 s, and returns the error.
func done(t *testing.T, ch <-chan error) error {
	t.Helper()
	select {
	case err := <-ch:
		return err
	case <-time.After(10 * time.Second):
		t.Fatal("Acquire still blocked after 10 s")
		return nil
	}
}

func TestAcquireBlocksUntilRelease(t *testing.T) {
	ctx := context.Background()
	b := Root(Limits{}).Child(Limits{Tasks: 2})
	for i := 0; i < 2; i++ {
		if err := b.Acquire(ctx); err != nil {
			t.Fatal(err)
		}
	}
	third := acquireAsync(ctx, b)
	blocked(t, third)
	b.Release()
	if err := done(t, third); err != nil {
		t.Fatal(err)
	}
}

func TestAcquireGivesUpOnCancel(t *testing.T) {
	b := Root(Limits{}).Child(Limits{Tasks: 1})
	if err := b.Acquire(context.Background()); err != nil {
		t.Fatal(err)
	}
	cause := errors.New("scope failed")
	ctx, cancel := context.WithCancelCause(context.Background())
	second := acquireAsync(ctx, b)
	blocked(t, second)
	cancel(cause)
	if err := done(t, second); err != cause {
		t.Fatalf("Acquire = %v, want the context's cause", err)
	}
	// the cancelled Acquire held nothing: one Release frees the only slot
	b.Release()
	if err := b.Acquire(context.Background()); err != nil {
		t.Fatal(err)
	}
}

// I6 within a task: a nested scope's spawns also take the enclosing
// scope's slots, so tasks: 10 inside tasks: 2 still runs two at a time.
func TestNestedScopeSlotsShrink(t *testing.T) {
	ctx := context.Background()
	outer := Root(Limits{}).Child(Limits{Tasks: 2})
	inner := outer.Child(Limits{Tasks: 10})
	for i := 0; i < 2; i++ {
		if err := inner.Acquire(ctx); err != nil {
			t.Fatal(err)
		}
	}
	third := acquireAsync(ctx, inner)
	blocked(t, third)
	inner.Release()
	if err := done(t, third); err != nil {
		t.Fatal(err)
	}
}

// "tasks" counts direct children: a task's own spawns never wait for the
// slots of the scope it was spawned into (else tasks: 2 with two children
// that each spawn a grandchild would deadlock).
func TestTaskBoundaryStopsSlots(t *testing.T) {
	ctx := context.Background()
	scope := Root(Limits{}).Child(Limits{Tasks: 2})
	for i := 0; i < 2; i++ {
		if err := scope.Acquire(ctx); err != nil {
			t.Fatal(err)
		}
	}
	child := scope.Task() // body of one of the two children
	if err := done(t, acquireAsync(ctx, child)); err != nil {
		t.Fatalf("grandchild spawn = %v", err)
	}
	if err := done(t, acquireAsync(ctx, child.Child(Limits{}))); err != nil {
		t.Fatalf("grandchild spawn from a nested scope = %v", err)
	}
}

// The live-task cap counts every task in the run, at any depth, and fails
// instead of blocking, so spawn recursion ends in an error.
func TestLiveTaskCap(t *testing.T) {
	ctx := context.Background()
	root := Root(Limits{LiveTasks: 2})
	a := root.Task()
	if err := root.Acquire(ctx); err != nil {
		t.Fatal(err)
	}
	if err := a.Acquire(ctx); err != nil {
		t.Fatal(err)
	}
	wantExceeded(t, a.Task().Acquire(ctx), "tasks", 2)
	a.Release()
	if err := a.Task().Acquire(ctx); err != nil {
		t.Fatalf("after a release: %v", err)
	}
}

// A failed live-task check gives back the slots it took on the way.
func TestLiveTaskCapReleasesSlots(t *testing.T) {
	ctx := context.Background()
	root := Root(Limits{LiveTasks: 1})
	scope := root.Child(Limits{Tasks: 5})
	if err := root.Task().Acquire(ctx); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 10; i++ { // would fill the 5 slots if failures kept them
		wantExceeded(t, scope.Acquire(ctx), "tasks", 1)
	}
	if n := len(scope.slots); n != 0 {
		t.Fatalf("%d slots still held after failed acquires", n)
	}
}
```

Create `arv/runtime/budget_test.go`:

```go
package runtime

import (
	"errors"
	"math/rand"
	"sync"
	"testing"
)

func TestExceededErrorText(t *testing.T) {
	for _, tt := range []struct {
		e    ExceededError
		want string
	}{
		{ExceededError{"steps", 1000}, "budget exceeded: steps (limit 1000)"},
		{ExceededError{"timeout", 100}, "budget exceeded: timeout (limit 100 ms)"},
		{ExceededError{"alloc", 1 << 20}, "budget exceeded: alloc (limit 1048576 bytes)"},
		{ExceededError{"tasks", 3}, "budget exceeded: tasks (limit 3 live tasks)"},
	} {
		if got := tt.e.Error(); got != tt.want {
			t.Errorf("got %q, want %q", got, tt.want)
		}
	}
}

// wantExceeded fails unless err is an *ExceededError of kind and limit.
func wantExceeded(t *testing.T, err error, kind string, limit int64) {
	t.Helper()
	var e *ExceededError
	if !errors.As(err, &e) || e.Kind != kind || e.Limit != limit {
		t.Fatalf("err = %v, want %s exceeded at limit %d", err, kind, limit)
	}
}

// I6: a charge counts at every level, so the smallest remaining limit wins,
// and what one level spends is gone for its siblings.
func TestStepsAcrossThreeLevels(t *testing.T) {
	root := Root(Limits{Steps: 1000})
	mid := root.Child(Limits{}) // no limit of its own
	inner := mid.Task().Child(Limits{Steps: 300})

	if err := inner.Steps(300); err != nil {
		t.Fatalf("exactly the limit: %v", err)
	}
	wantExceeded(t, inner.Steps(1), "steps", 300)

	sibling := mid.Child(Limits{Steps: 5000}) // more than the root has left
	if err := sibling.Steps(600); err != nil {
		t.Fatal(err)
	}
	wantExceeded(t, sibling.Steps(200), "steps", 1000) // root: 300+1+600+200 > 1000
}

func TestExceededStaysExceeded(t *testing.T) {
	root := Root(Limits{Steps: 10, AllocBytes: 100})
	wantExceeded(t, root.Steps(11), "steps", 10)
	wantExceeded(t, root.Steps(1), "steps", 10) // nothing is refunded
	if err := root.Steps(0); err != nil {
		t.Fatalf("Steps(0) = %v; charging nothing never fails", err)
	}
	wantExceeded(t, root.Alloc(101), "alloc", 100)
	wantExceeded(t, root.Task().Alloc(1), "alloc", 100)
	if err := root.Alloc(0); err != nil {
		t.Fatalf("Alloc(0) = %v", err)
	}
}

func TestAllocChargesEveryLevel(t *testing.T) {
	root := Root(Limits{AllocBytes: 100})
	inner := root.Task().Child(Limits{AllocBytes: 1000}).Task()
	if err := inner.Alloc(60); err != nil {
		t.Fatal(err)
	}
	wantExceeded(t, inner.Alloc(60), "alloc", 100) // 120 > the root's 100
}

func TestNoLimitsChargeNothing(t *testing.T) {
	b := Root(Limits{}).Child(Limits{}).Task()
	if b.Steps(1<<40) != nil || b.Alloc(1<<50) != nil || Root(Limits{}).live.limit != DefaultLiveTasks {
		t.Fatal("a budget with no limits refused a charge, or the default live-task cap is wrong")
	}
}

// Concurrent chargers never get past the limit unnoticed: once the total
// passes it, every charge fails.
func TestStepsConcurrent(t *testing.T) {
	root := Root(Limits{Steps: 100_000})
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			b := root.Task().Child(Limits{Steps: 50_000})
			for b.Steps(256) == nil {
			}
		}()
	}
	wg.Wait()
	if left := root.steps.left.Load(); left >= 0 {
		t.Fatalf("root has %d steps left after every charger failed", left)
	}
}

// I6 as a property: over random trees of nested limits and random charges,
// a charge succeeds only if no level above it has passed its limit, and a
// failure always names a level that has.
func TestBudgetsOnlyShrinkProperty(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	for round := 0; round < 200; round++ {
		type node struct {
			b      *Budget
			parent int // -1 for the root
			limit  int64
			used   int64 // charged to this level, by it or its descendants
		}
		rootLimit := rng.Int63n(5000) // 0: no limit
		nodes := []*node{{b: Root(Limits{Steps: rootLimit}), parent: -1, limit: rootLimit}}
		for i := 0; i < 1+rng.Intn(8); i++ {
			p := rng.Intn(len(nodes))
			l := Limits{}
			if rng.Intn(2) == 0 {
				l.Steps = 1 + rng.Int63n(3000)
			}
			b := nodes[p].b.Child(l)
			if rng.Intn(2) == 0 {
				b = nodes[p].b.Task().Child(l)
			}
			nodes = append(nodes, &node{b: b, parent: p, limit: l.Steps})
		}
		for op := 0; op < 300; op++ {
			i := rng.Intn(len(nodes))
			n := 1 + rng.Int63n(300)
			err := nodes[i].b.Steps(n)
			var e *ExceededError
			failed := errors.As(err, &e)
			named := false
			for j := i; j != -1; j = nodes[j].parent {
				nd := nodes[j]
				if nd.limit == 0 {
					continue
				}
				nd.used += n // the charge reaches levels up to the failing one
				if nd.used > nd.limit {
					if !failed {
						t.Fatalf("round %d: level %d at %d/%d steps, but the charge succeeded",
							round, j, nd.used, nd.limit)
					}
					named = e.Limit == nd.limit
					break
				}
			}
			if failed && !named {
				t.Fatalf("round %d: failed at limit %d, but that level has not passed it", round, e.Limit)
			}
		}
	}
}
```

- [ ] **Step 2: Run them and watch them fail**

Run: `cd <worktree> && go test -race ./arv/runtime/`

Expected: build failure, `undefined: Budget`, `undefined: Root`, `undefined: Limits`.

- [ ] **Step 3: Implement**

Create `arv/runtime/budget.go`:

```go
package runtime

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"
)

// Limits are what a scope (or the host, for the root) may cap. Zero means
// "no new limit at this level"; limits set by enclosing levels still apply,
// so a nested limit can only shrink the budget (I6).
type Limits struct {
	Timeout    time.Duration // wall clock; applied to the scope's context by NewTimedScope
	Tasks      int           // direct children running at once; spawn waits for a slot
	Steps      int64         // nodes evaluated by every task under the scope
	AllocBytes int64         // bytes of Arv values allocated under the scope
	LiveTasks  int           // Root only: tasks alive at once in the run; 0 = DefaultLiveTasks
}

// DefaultLiveTasks caps the tasks alive at once in one run when the host
// sets no LiveTasks. Every task starts at call depth 0, so without a cap,
// spawn recursion would grow until memory ran out. It stays well under
// the race detector's limit of 8128 live goroutines, so even a -race build
// reports the cap as an Arv error instead of dying.
const DefaultLiveTasks = 1000

// ExceededError is the error for a spent budget. Limit is in the kind's
// unit: ms for "timeout", live tasks for "tasks", nodes for "steps", bytes
// for "alloc".
type ExceededError struct {
	Kind  string
	Limit int64
}

func (e *ExceededError) Error() string {
	unit := map[string]string{"timeout": " ms", "tasks": " live tasks", "alloc": " bytes"}[e.Kind]
	return fmt.Sprintf("budget exceeded: %s (limit %d%s)", e.Kind, e.Limit, unit)
}

// Budget is one level of a tree of budgets: the root (the run), one level
// per scope, and one per task body. Steps and bytes are charged to a level
// and every level above it, across task boundaries; task slots are taken
// at a level and the levels above it up to the first task boundary only,
// since "tasks" counts a scope's direct children. Safe for concurrent use.
type Budget struct {
	parent  *Budget
	live    *counter      // shared by the whole run
	steps   *pool         // nil: no steps limit at this level
	alloc   *pool         // nil: no allocation limit at this level
	slots   chan struct{} // nil: no task-slot limit at this level
	task    bool          // the first level of a task body (or of the run)
	stepsAt *Budget       // nearest level at or above this one with a steps pool
	allocAt *Budget       // nearest level at or above this one with an alloc pool
}

// pool counts down from a limit. Charges are never refunded; once a charge
// takes it below zero it stays exceeded.
type pool struct {
	limit int64
	left  atomic.Int64
}

func newPool(limit int64) *pool {
	if limit <= 0 {
		return nil
	}
	p := &pool{limit: limit}
	p.left.Store(limit)
	return p
}

type counter struct {
	limit int64
	n     atomic.Int64
}

// Root returns the budget of a whole run: l's limits plus the live-task cap.
func Root(l Limits) *Budget {
	live := int64(l.LiveTasks)
	if live <= 0 {
		live = DefaultLiveTasks
	}
	b := level(nil, l)
	b.live, b.task = &counter{limit: live}, true
	return b
}

// Child returns a nested scope's level under b, in the same task.
func (b *Budget) Child(l Limits) *Budget {
	c := level(b, l)
	c.live = b.live
	return c
}

// Task returns the level for the body of a task spawned under b. It sets no
// limits of its own, and the task's own spawns do not take b's slots.
func (b *Budget) Task() *Budget {
	return &Budget{parent: b, live: b.live, task: true, stepsAt: b.stepsAt, allocAt: b.allocAt}
}

func level(parent *Budget, l Limits) *Budget {
	b := &Budget{parent: parent, steps: newPool(l.Steps), alloc: newPool(l.AllocBytes)}
	if l.Tasks > 0 {
		b.slots = make(chan struct{}, l.Tasks)
	}
	if parent != nil {
		b.stepsAt, b.allocAt = parent.stepsAt, parent.allocAt
	}
	if b.steps != nil {
		b.stepsAt = b
	}
	if b.alloc != nil {
		b.allocAt = b
	}
	return b
}

// Steps charges n evaluated nodes to b and every level above it. It fails
// with an *ExceededError naming the first level, from b up, whose limit
// the charge passes; levels above that one are not charged.
func (b *Budget) Steps(n int64) error {
	if n <= 0 {
		return nil
	}
	for l := b.stepsAt; l != nil; l = l.parent.stepsLevel() {
		if l.steps.left.Add(-n) < 0 {
			return &ExceededError{Kind: "steps", Limit: l.steps.limit}
		}
	}
	return nil
}

// Alloc charges n bytes the same way Steps charges nodes.
func (b *Budget) Alloc(n int64) error {
	if n <= 0 {
		return nil
	}
	for l := b.allocAt; l != nil; l = l.parent.allocLevel() {
		if l.alloc.left.Add(-n) < 0 {
			return &ExceededError{Kind: "alloc", Limit: l.alloc.limit}
		}
	}
	return nil
}

// stepsLevel and allocLevel are stepsAt and allocAt, nil-safe on b.
func (b *Budget) stepsLevel() *Budget {
	if b == nil {
		return nil
	}
	return b.stepsAt
}

func (b *Budget) allocLevel() *Budget {
	if b == nil {
		return nil
	}
	return b.allocAt
}

// Acquire reserves what one new task spawned under b needs: a slot at each
// level from b up to the task boundary, blocking while one is full, and one
// of the run's live tasks, failing with *ExceededError when none is left.
// It gives up with ctx's cause if ctx ends first. Call Release when the
// task finishes, on every path.
func (b *Budget) Acquire(ctx context.Context) error {
	if err := context.Cause(ctx); err != nil {
		return err
	}
	for l := b; l != nil; l = l.parent {
		if l.slots != nil {
			select {
			case l.slots <- struct{}{}:
			case <-ctx.Done():
				b.releaseSlots(l)
				return context.Cause(ctx)
			}
		}
		if l.task {
			break
		}
	}
	if b.live.n.Add(1) > b.live.limit {
		b.live.n.Add(-1)
		b.releaseSlots(nil)
		return &ExceededError{Kind: "tasks", Limit: b.live.limit}
	}
	return nil
}

// Release returns what a successful Acquire on b reserved.
func (b *Budget) Release() {
	b.live.n.Add(-1)
	b.releaseSlots(nil)
}

// releaseSlots frees the slots held from b up to (not including) stop, or
// up to the task boundary when stop is nil.
func (b *Budget) releaseSlots(stop *Budget) {
	for l := b; l != nil && l != stop; l = l.parent {
		if l.slots != nil {
			<-l.slots
		}
		if l.task {
			break
		}
	}
}
```

- [ ] **Step 4: Run the tests, then the full checks**

Run: `cd <worktree> && go test -race ./arv/runtime/`, then `cd <worktree> && gofmt -l . && go vet ./... && go test -race ./...`

Expected: `ok` for every package, and `gofmt -l .` prints nothing.

- [ ] **Step 5: Commit**

```bash
cd <worktree> && git add arv/runtime/budget.go arv/runtime/budget_tasks_test.go arv/runtime/budget_test.go && git commit -F - <<'MSG'
feat(runtime): Budget tree with steps, alloc, task slots and a live-task cap

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
MSG
```
