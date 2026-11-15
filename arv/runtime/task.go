package runtime

import (
	"context"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"time"
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

// MaxRunEvalDepth is the least evaluator recursion a run's tasks may hold
// together. MaxEvalDepth keeps one task's stack under 64 MiB (128 MiB
// under -race), but every task starts at depth 0, so without a run-wide
// bound DefaultLiveTasks deep tasks could hold tens of GB of stack. Four
// tasks' worth keeps a run's stacks to about 256 MiB (512 MiB under
// -race). What counts is the total: 900 tasks each 80 Arv calls (about
// 560 levels) deep pass it as surely as five tasks at MaxEvalDepth.
const MaxRunEvalDepth = 4 * MaxEvalDepth

// LiveTaskDepth is the evaluator depth the run's pool keeps for each task
// the live-task cap allows: the pool is the larger of MaxRunEvalDepth and
// LiveTaskDepth for every live task plus the root. Every task reserves
// depth in 64-level chunks, so a raised cap (Limits.LiveTasks) must raise
// the pool too, or a fan-out of shallow tasks would fail with a stack
// overflow long before the cap. At DefaultLiveTasks the pool is
// MaxRunEvalDepth.
const LiveTaskDepth = 256

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
	log      *EffectLog // the run's effect log, nil if it keeps none
	seq      uint64     // records of this task logged or replayed so far
	checks   uint64     // checks made so far (see observe)
	effects  uint64     // effects made so far, logged or replayed (a divergence names them)
	children atomic.Uint64
	horizon  []uint64 // spawn times from the root's child down to this task (see Horizon)
	seen     []error  // the latest cancellation causes t has seen, logged runs only (see quiet)
	timed    uint64   // timed scopes opened so far, logged runs only (see OpenTimed)
	by       string   // the deadline the record being replayed names (see awaitCancel)
	held     bool     // a record used but not yet counted: its deadline fires first (see consume)

	// A resume's clock (see deadline): t is live once past its records;
	// it is then at vbase ms on the run's clock at rbase, and counts
	// toward the deadlines armed. latest is the latest restart attempt.
	live   bool
	lastAt int64 // the At of the last record t replayed
	vbase  int64
	rbase  time.Time
	armed  []*deadline
	latest *Task
}

// NewRoot creates the top-level task for a program run.
func NewRoot(ctx context.Context, caps CapSet, out io.Writer) *Task {
	return &Task{ID: "root", Caps: caps, Ctx: ctx, Out: &syncWriter{w: out}}
}

// Child creates a sub-task with attenuated authority and its own context.
// It moves the clock: from now on, what the parent binds is invisible to
// the child and to everything the child spawns (I8, see Horizon).
func (t *Task) Child(ctx context.Context, caps CapSet) *Task {
	n := t.children.Add(1) - 1
	// Exactly one entry more than t's: spawn charges 8 bytes an entry,
	// and append onto a full slice would allocate about twice that.
	horizon := make([]uint64, len(t.horizon)+1)
	copy(horizon, t.horizon)
	horizon[len(t.horizon)] = clock.Add(1)
	c := &Task{ID: fmt.Sprintf("%s/%d", t.ID, n), Caps: caps, Ctx: ctx, Out: t.Out, parent: t, horizon: horizon, log: t.log}
	if t.log != nil && t.log.mode == Resume {
		c.vbase = t.now()
	}
	return c
}

// Attempt is the task that makes t's nth attempt under a restart
// supervisor (t itself for the first): a fresh task with t's authority,
// context, output and parent, named t.ID#n, whose children are numbered
// from 0 again. It keeps t's horizon, so it sees the environments t was
// spawned with, not what its parent has bound since (I8).
func (t *Task) Attempt(n int) *Task {
	if n <= 1 {
		return t
	}
	a := &Task{ID: t.AttemptID(n), Caps: t.Caps, Ctx: t.Ctx, Out: t.Out, parent: t.parent, horizon: t.horizon, log: t.log}
	if t.log != nil {
		// The attempt before has ended, its backoff too: with a log,
		// that is a record (Finished), so a resume replays a failed
		// attempt to its failure, as it does a finished task.
		prev := t
		if t.latest != nil {
			prev = t.latest
		}
		prev.Finished(nil)
		if t.log.mode == Resume {
			a.vbase = prev.now()
		}
		t.latest = a
	}
	return a
}

// AttemptID is the ID of the task that makes t's nth attempt (Attempt).
func (t *Task) AttemptID(n int) string {
	if n <= 1 {
		return t.ID
	}
	return fmt.Sprintf("%s#%d", t.ID, n)
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
// the task has been cancelled, looking every checkEvery nodes; each look
// is a check (see Err).
func (t *Task) Tick() error {
	t.steps++
	if t.steps%checkEvery != 0 {
		return nil
	}
	if t.log == nil {
		return context.Cause(t.Ctx)
	}
	return t.observe(func(quiet bool) (string, error) {
		if err := context.Cause(t.checkCtx(quiet)); err != nil {
			return CheckCancelled, err
		}
		return "", nil
	}, t.cancelled)
}

// LiveErr is Err as the world has it now, never logged or replayed: for
// code inside an effect, which runs only live (Effect's do).
func (t *Task) LiveErr() error { return liveErr(t.Ctx) }

// liveErr is why ctx's task must stop now, nil if it may go on. A
// deadline that has passed is the timeout of the scope whose deadline it
// is, never a bare context.DeadlineExceeded, which no timed scope names
// (a logged check would have no By, and its replay would wait for the
// deadline's firing, which waits for it: review round 5). It can pass
// after expireDue looks, or its timer can be cancelling the scopes above
// ctx but not yet ctx: liveErr then looks again, and waits for ctx to
// be cancelled once a scope above it has been. Only a deadline no scope
// has (the host's) is DeadlineExceeded.
func liveErr(ctx context.Context) error {
	if ctx.Err() != nil || expireDue(ctx) {
		return context.Cause(ctx)
	}
	if d, ok := ctx.Deadline(); !ok || time.Now().Before(d) {
		return nil
	}
	if expireDue(ctx) || scopeCancelled(ctx) {
		<-ctx.Done() // the cancellation is on its way down to ctx
		return context.Cause(ctx)
	}
	return context.DeadlineExceeded
}

// scopeCancelled reports that a scope ctx is in, or is derived from, has
// been cancelled: ctx is, or is about to be.
func scopeCancelled(ctx context.Context) bool {
	for s := scopeOf(ctx); s != nil; s = s.outer {
		if s.ctx.Err() != nil {
			return true
		}
	}
	return false
}

// Steps reports how many nodes this task has evaluated.
func (t *Task) Steps() uint64 { return t.steps }

// Admit is CapSet.AdmitCharged on the task's grants, with the task named
// in the error: it canonicalises a literal request, charging that to
// charge (unless nil), and checks it, revealing nothing about the
// filesystem when it denies.
func (t *Task) Admit(r Capability, charge func(int64) error) (Capability, error) {
	c, err := t.Caps.AdmitCharged(r, charge)
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

// WriteLine writes line and a newline as one unit: no other write comes
// between them. It does not copy line when the underlying writer is an
// io.StringWriter (*os.File, *bytes.Buffer and io.Discard are), so puts
// can print a string of any size without a copy of it, which would not be
// charged to alloc_mb.
func (s *syncWriter) WriteLine(line string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := io.WriteString(s.w, line); err != nil {
		return err
	}
	_, err := io.WriteString(s.w, "\n")
	return err
}

// WriteLine writes line and a newline to w, as one unit if w is a task's
// Out (see syncWriter.WriteLine), without copying line if w, or the writer
// a task's Out wraps, is an io.StringWriter.
func WriteLine(w io.Writer, line string) error {
	if lw, ok := w.(interface{ WriteLine(string) error }); ok {
		return lw.WriteLine(line)
	}
	if _, err := io.WriteString(w, line); err != nil {
		return err
	}
	_, err := io.WriteString(w, "\n")
	return err
}
