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
	Tokens     int64         // model tokens, input and output, of every call under the scope
	USDCents   int64         // what those calls cost, in US cents (see llm.Price)
	MaxTurns   int           // model calls one agent(...) may make; 0 = the enclosing level's (DefaultMaxTurns at the root)
}

// Validate fails if any limit is negative. Zero means no limit (for
// LiveTasks, DefaultLiveTasks), but a negative limit is refused rather
// than read as zero: a host that computes what is left of a budget may go
// below zero, and must not get an unlimited run for it.
func (l Limits) Validate() error {
	for _, f := range []struct {
		name string
		v    int64
	}{
		{"Timeout", int64(l.Timeout)}, {"Tasks", int64(l.Tasks)}, {"Steps", l.Steps},
		{"AllocBytes", l.AllocBytes}, {"LiveTasks", int64(l.LiveTasks)},
		{"Tokens", l.Tokens}, {"USDCents", l.USDCents}, {"MaxTurns", int64(l.MaxTurns)},
	} {
		if f.v < 0 {
			return fmt.Errorf("invalid limits: %s is negative (%d)", f.name, f.v)
		}
	}
	if l.USDCents > MaxUSDCents {
		return fmt.Errorf("invalid limits: USDCents is too large (%d, max %d)", l.USDCents, MaxUSDCents)
	}
	return nil
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
	depth   *counter      // shared by the whole run: evaluator recursion of all its tasks
	steps   *pool         // nil: no steps limit at this level
	alloc   *pool         // nil: no allocation limit at this level
	slots   chan struct{} // nil: no task-slot limit at this level
	task    bool          // the first level of a task body (or of the run)
	stepsAt *Budget       // nearest level at or above this one with a steps pool
	allocAt *Budget       // nearest level at or above this one with an alloc pool
	models                // the tokens and usd pools and max_turns (budget_llm.go)
}

// pool counts down from a limit. Steps and bytes are never refunded; a
// model call's unused reservation is (budget_llm.go). Once a charge takes
// a pool below zero it stays exceeded.
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
	b.depth = &counter{limit: max(MaxRunEvalDepth, (live+1)*LiveTaskDepth)}
	if b.turns == 0 {
		b.turns = DefaultMaxTurns
	}
	return b
}

// DepthLimit is the size of the run's pool of evaluator depth.
func (b *Budget) DepthLimit() int64 { return b.depth.limit }

// Child returns a nested scope's level under b, in the same task.
func (b *Budget) Child(l Limits) *Budget {
	c := level(b, l)
	c.live, c.depth = b.live, b.depth
	return c
}

// Task returns the level for the body of a task spawned under b. It sets no
// limits of its own, and the task's own spawns do not take b's slots.
func (b *Budget) Task() *Budget {
	return &Budget{parent: b, live: b.live, depth: b.depth, task: true, stepsAt: b.stepsAt, allocAt: b.allocAt,
		models: models{tokensAt: b.tokensAt, usdAt: b.usdAt, turns: b.turns}}
}

// Depth reserves n levels of evaluator recursion from the run's pool, or
// gives them back if n is negative. It reports false, reserving nothing,
// if the run's tasks together would go past MaxRunEvalDepth.
func (b *Budget) Depth(n int64) bool {
	if b.depth.n.Add(n) > b.depth.limit && n > 0 {
		b.depth.n.Add(-n)
		return false
	}
	return true
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
	b.models = newModels(b, parent, l)
	return b
}

// Steps charges n evaluated nodes to b and every level above it. If the
// charge takes any level past its limit, it fails with an *ExceededError
// naming the level with the least left: the limit that would have tripped
// first had nodes been charged one at a time. Every level is charged
// either way, so siblings sharing an enclosing level see what was spent.
func (b *Budget) Steps(n int64) error {
	if n <= 0 {
		return nil
	}
	return tripped("steps", b.stepsAt, n, (*Budget).stepsPool)
}

// StepsSpent reports, without charging anything, the level from b up
// that an earlier charge took furthest past its limit, as Steps names it.
// A task with nothing to charge calls it before an effect, since a sibling
// may have spent the budget it shares.
func (b *Budget) StepsSpent() error {
	return tripped("steps", b.stepsAt, 0, (*Budget).stepsPool)
}

// Alloc charges n bytes the same way Steps charges nodes.
func (b *Budget) Alloc(n int64) error {
	if n <= 0 {
		return nil
	}
	return tripped("alloc", b.allocAt, n, (*Budget).allocPool)
}

// AllocSpent is StepsSpent for bytes: a task calls it before an effect,
// since a sibling may have spent the bytes it shares.
func (b *Budget) AllocSpent() error {
	return tripped("alloc", b.allocAt, 0, (*Budget).allocPool)
}

// tripped charges n to the pool of every level from l up (poolOf picks the
// kind's pool and the next level with one) and returns the error for the
// level left furthest below zero, or nil if none is below zero.
func tripped(kind string, l *Budget, n int64, poolOf func(*Budget) (*pool, *Budget)) error {
	var worst *pool
	var worstLeft int64
	for l != nil {
		p, next := poolOf(l)
		left := p.left.Add(-n)
		if left < 0 && (worst == nil || left < worstLeft) {
			worst, worstLeft = p, left
		}
		l = next
	}
	if worst == nil {
		return nil
	}
	return &ExceededError{Kind: kind, Limit: worst.limit}
}

func (b *Budget) stepsPool() (*pool, *Budget) { return b.steps, b.parent.stepsLevel() }
func (b *Budget) allocPool() (*pool, *Budget) { return b.alloc, b.parent.allocLevel() }

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
