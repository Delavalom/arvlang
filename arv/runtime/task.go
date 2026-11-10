package runtime

import (
	"context"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
)

// MaxDepth bounds Arv call depth, so runaway recursion is reported as such
// ("call depth exceeded"). It does not by itself bound the Go stack: each
// call can nest expressions up to the parser's MaxNesting, and the two
// multiply. MaxEvalDepth is the bound that keeps the Go stack safe.
const MaxDepth = 10_000

// MaxEvalDepth bounds the evaluator's total recursion (nested Eval calls,
// whether from calls or expression nesting), so no program can overflow
// the Go stack, which is a fatal error rather than a panic.
//
// Measured on darwin/arm64, Go 1.26: at this depth the costliest shapes
// (nested arrays, hashes, scopes, call arguments, right-nested infix inside
// recursion) need at most 64 MiB of goroutine stack, 128 MiB under -race;
// Go's default limit is 1 GiB. Plain recursion to MaxDepth uses about 7
// levels per call, so it fits. TestCallsTimesNestingIsAnArvError runs these
// shapes under a 256 MiB stack cap.
const MaxEvalDepth = 100_000

// checkEvery is how many interpreter steps pass between context checks.
const checkEvery = 256

// Task is one unit of Arv execution. Each task runs on its own goroutine with
// its own interpreter; tasks share no mutable Arv state.
type Task struct {
	ID   string          // "root", "root/0", "root/0/2", ...
	Caps CapSet          // authority this task holds
	Ctx  context.Context // cancelled when the task's scope is cancelled
	Out  io.Writer       // where puts writes; safe for concurrent use

	parent   *Task // nil for the root
	steps    uint64
	children atomic.Uint64
}

// NewRoot creates the top-level task for a program run.
func NewRoot(ctx context.Context, caps CapSet, out io.Writer) *Task {
	return &Task{ID: "root", Caps: caps, Ctx: ctx, Out: &syncWriter{w: out}}
}

// Child creates a sub-task with attenuated authority and its own context.
func (t *Task) Child(ctx context.Context, caps CapSet) *Task {
	n := t.children.Add(1) - 1
	return &Task{ID: fmt.Sprintf("%s/%d", t.ID, n), Caps: caps, Ctx: ctx, Out: t.Out, parent: t}
}

// IsAncestorOf reports whether d was spawned, directly or through other
// tasks, by t. A task is not its own ancestor.
func (t *Task) IsAncestorOf(d *Task) bool {
	for p := d.parent; p != nil; p = p.parent {
		if p == t {
			return true
		}
	}
	return false
}

// Tick is called once per evaluated node. It returns the context error once
// the task has been cancelled. Budgets (Phase 3) hook in here.
func (t *Task) Tick() error {
	t.steps++
	if t.steps%checkEvery == 0 {
		return context.Cause(t.Ctx)
	}
	return nil
}

// Steps reports how many nodes this task has evaluated.
func (t *Task) Steps() uint64 { return t.steps }

// Admit is CapSet.Admit on the task's grants, with the task named in the
// error: it canonicalises a literal request and checks it, revealing
// nothing about the filesystem when it denies.
func (t *Task) Admit(r Capability) (Capability, error) {
	c, err := t.Caps.Admit(r)
	if d, ok := err.(*DeniedError); ok {
		d.Task = t.ID
	}
	return c, err
}

// Require fails with *DeniedError unless the task holds a grant covering c.
func (t *Task) Require(c Capability) error {
	if t.Caps.Allows(c) {
		return nil
	}
	return &DeniedError{Task: t.ID, Want: c, Held: t.Caps.List()}
}

type syncWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func (s *syncWriter) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.w.Write(p)
}
