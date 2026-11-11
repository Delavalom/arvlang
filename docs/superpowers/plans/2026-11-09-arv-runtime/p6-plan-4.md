# Phase 6, task p6.4: EffectLog and Task.Effect: record, replay, diverge

> **For agentic workers:** this file is one task, p6.4. Do it test first, exactly as written; if something here does not match the code you find, stop and report instead of improvising. Prerequisite: tasks p6.1 to p6.3 are committed in your tree. Start every shell command with `cd <your worktree> &&`. Before committing: `gofmt -l .` prints nothing, `go vet ./...` is clean and `go test -race ./...` is green. End the commit message with a blank line and then `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`. Never use `--no-verify`; keep Go files under 15 KB and markdown under 18 KB. `go.mod` says `go 1.24.0` (toolchain 1.26.2), so do not use `testing/synctest` or `sync.WaitGroup.Go`. `arv/runtime` stays stdlib-only.
>
> Every code block below was applied, in task order, to a scratch copy of the tree and passed `go vet ./...` and `go test -race ./...` (including `e2e/`). Design: `13-phase6-durable-replay.md`. Deviations from it, and why: `p6-plan-1.md`.

### Task p6.4: EffectLog and Task.Effect: record, replay, diverge

`runtime.EffectLog` is one run's log, shared by all its tasks (each `Task` gets a `log` field, copied by `Child` and `Attempt`, and a per-task `seq`).
- `NewRecorder(w, h, out, sync)` writes the header. `NewReplayer(log, out)` answers a finished log (`ErrUnfinished` otherwise). `Start(ctx)` returns the context the root task must use. (`out` is used from p6.5 on.)
- `Task.Effect(kind, req, do)`: with no log it runs `do`; recording, it runs `do` and appends the record; replaying, it never runs `do`: the task's next record must have the same kind and request bytes, else the run fails with a `*DivergedError` (kind `replay`) that names the task, the effect number and both calls, and the whole run is cancelled. A replayed result marked `Cancelled` is returned only once this task is cancelled too (`awaitCancel`), so a sibling's failure still comes first.
- `Finish(result)` returns the first failure; a replay must have used every record and must end with the logged result; a recording appends the `end` line.

**Files:**
- Create: `arv/runtime/effectlog.go`
- Create: `arv/runtime/gate.go`
- Modify: `arv/runtime/task.go`
- Create (test): `arv/runtime/gate_test.go`

- [ ] **Step 1: Write the failing tests**

Create `arv/runtime/gate_test.go`:

```go
package runtime

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
)

// replayer reads data back and starts a replay of it printing to out.
func replayer(t *testing.T, data []byte, out *bytes.Buffer) (*EffectLog, *Task) {
	t.Helper()
	log, err := ReadLog(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	l, err := NewReplayer(log, out)
	if err != nil {
		t.Fatal(err)
	}
	root := NewRoot(l.Start(context.Background()), NewCapSet(), out)
	root.SetLog(l)
	return l, root
}

// recordRun records a small run: root makes two effects, root/0 one,
// and the run ends with result.
func recordRun(t *testing.T, result string) []byte {
	t.Helper()
	var log bytes.Buffer
	l, err := NewRecorder(&log, Header{ScriptSHA256: "abc"}, &bytes.Buffer{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	root := NewRoot(l.Start(context.Background()), NewCapSet(), &bytes.Buffer{})
	root.SetLog(l)
	for _, req := range []string{`["a"]`, `["b"]`} {
		if _, err := root.Effect("getenv", []byte(req), func() Outcome { return Outcome{Res: []byte(`"v"`), Charged: 1} }); err != nil {
			t.Fatal(err)
		}
	}
	child := root.Child(root.Ctx, NewCapSet())
	if _, err := child.Effect("sleep", []byte(`[5]`), func() Outcome { return Outcome{Res: []byte(`null`)} }); err != nil {
		t.Fatal(err)
	}
	if err := l.Finish([]byte(result)); err != nil {
		t.Fatal(err)
	}
	return log.Bytes()
}

func never(t *testing.T) func() Outcome {
	return func() Outcome { t.Error("a replayed effect ran live"); return Outcome{} }
}

// I7: a replay answers every effect from the log and runs none of them.
func TestReplayAnswersFromTheLog(t *testing.T) {
	data := recordRun(t, `[7]`)
	var out bytes.Buffer
	l, root := replayer(t, data, &out)
	for _, req := range []string{`["a"]`, `["b"]`} {
		o, err := root.Effect("getenv", []byte(req), never(t))
		if err != nil || !o.Replayed || string(o.Res) != `"v"` || o.Charged != 1 {
			t.Fatalf("Effect(%s) = %+v, %v", req, o, err)
		}
	}
	child := root.Child(root.Ctx, NewCapSet())
	if o, err := child.Effect("sleep", []byte(`[5]`), never(t)); err != nil || string(o.Res) != "null" {
		t.Fatalf("child = %+v, %v", o, err)
	}
	if err := l.Finish([]byte(`[7]`)); err != nil {
		t.Fatalf("Finish = %v", err)
	}
}

func TestReplayDiverges(t *testing.T) {
	data := recordRun(t, `[7]`)
	for _, tt := range []struct {
		name string
		run  func(l *EffectLog, root *Task) error
		want string
	}{
		{"another request", func(l *EffectLog, root *Task) error {
			_, err := root.Effect("getenv", []byte(`["z"]`), never(t))
			return err
		}, `at task root, effect 1: the log has getenv("a"), the program asked for getenv("z")`},
		{"another effect", func(l *EffectLog, root *Task) error {
			_, err := root.Effect("now", []byte(`[]`), never(t))
			return err
		}, `the log has getenv("a"), the program asked for now()`},
		{"past the log", func(l *EffectLog, root *Task) error {
			_, err := root.Child(root.Ctx, NewCapSet()).Child(root.Ctx, NewCapSet()).Effect("now", []byte(`[]`), never(t))
			return err
		}, "at task root/0/0, effect 1: the log has no more records for it; the program asked for now()"},
		{"records left", func(l *EffectLog, root *Task) error { return l.Finish([]byte(`[7]`)) },
			`at task root: the log has 2 more records for it, from getenv("a")`},
		{"another result", func(l *EffectLog, root *Task) error {
			for _, r := range []string{`["a"]`, `["b"]`} {
				root.Effect("getenv", []byte(r), never(t))
			}
			root.Child(root.Ctx, NewCapSet()).Effect("sleep", []byte(`[5]`), never(t))
			return l.Finish([]byte(`[8]`))
		}, "at the end: the recorded run ended with [7], this run with [8]"},
	} {
		l, root := replayer(t, data, &bytes.Buffer{})
		err := tt.run(l, root)
		var d *DivergedError
		if !errors.As(err, &d) || !strings.Contains(err.Error(), tt.want) || ErrorKind(err, "") != KindReplay {
			t.Errorf("%s: err = %v, want %q", tt.name, err, tt.want)
		}
		if root.Ctx.Err() == nil {
			t.Errorf("%s: a divergence did not abort the run", tt.name)
		}
		if _, err2 := root.Effect("now", nil, never(t)); err2 != err && !errors.As(err2, &d) {
			t.Errorf("%s: after diverging, Effect = %v", tt.name, err2)
		}
	}
}
```

- [ ] **Step 2: Run them and watch them fail**

Run: `cd <worktree> && go test -race ./arv/runtime/ -run 'TestReplayAnswersFromTheLog|TestReplayDiverges'`

Expected: the package fails to build: `undefined: NewRecorder`, `root.SetLog undefined`, `undefined: Outcome`, `undefined: DivergedError`, ...

- [ ] **Step 3: Implement**

Create `arv/runtime/effectlog.go`:

```go
package runtime

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"sync"
	"sync/atomic"
)

// Mode is what a run does with its effect log.
type Mode int

const (
	Record Mode = iota + 1 // run live and append every effect to the log
	Replay                 // answer every effect from the log; nothing runs live
	Resume                 // answer each task from the log while it has records, then run it live and append
)

// KindReplay is the kind of a replay that diverged from its log, or of a
// log that could not be written.
const KindReplay = "replay"

// DivergedError is why a replayed run stopped: it asked for an effect the
// log does not have, or ended other than the recorded run did.
type DivergedError struct{ Msg string }

func (e *DivergedError) Error() string   { return "replay diverged " + e.Msg }
func (*DivergedError) ErrorKind() string { return KindReplay }

// EffectLog is one run's effect log, shared by all its tasks (I7): every
// effect and every timing-dependent check a task makes goes through it
// (Task.Effect, Task.Err), to be recorded, or answered from the log.
type EffectLog struct {
	mode  Mode
	log   *Log            // Replay, Resume
	w     *logWriter      // Record, Resume
	abort context.Context // ends when the host cancels the run or it diverges
	stop  context.CancelCauseFunc

	failure  atomic.Pointer[error] // the first divergence or write failure
	mu       sync.Mutex
	consumed map[string]uint64 // per task: records answered from the log
}

// NewRecorder starts a log on w for a run described by h. Every line is
// one write to w; sync, unless nil, runs after each (fsync). Output goes
// to out.
func NewRecorder(w io.Writer, h Header, out io.Writer, sync func() error) (*EffectLog, error) {
	l := &EffectLog{mode: Record, w: &logWriter{w: w, sync: sync}}
	if err := l.w.append(&LogRecord{V: LogVersion, Header: &h}); err != nil {
		return nil, err
	}
	return l, nil
}

// ErrUnfinished is a replay of a log with no end line.
var ErrUnfinished = errors.New("effect log: the recorded run did not finish (it was killed or cancelled); resume it instead")

// NewReplayer answers a run from log, which must be complete: a run that
// did not finish can only be resumed.
func NewReplayer(log *Log, out io.Writer) (*EffectLog, error) {
	if log.End == nil {
		return nil, ErrUnfinished
	}
	return &EffectLog{mode: Replay, log: log, consumed: map[string]uint64{}}, nil
}

// Mode is what the log does.
func (l *EffectLog) Mode() Mode { return l.mode }

// Log is the log read back (Replay, Resume), else nil.
func (l *EffectLog) Log() *Log { return l.log }

// Start binds the log to a run that the host can cancel through ctx. The
// run's root task must use the context it returns, which also ends when
// the replay diverges or the log cannot be written.
func (l *EffectLog) Start(ctx context.Context) context.Context {
	l.abort, l.stop = context.WithCancelCause(ctx)
	return l.abort
}

// failed is the first divergence or write failure, or nil.
func (l *EffectLog) failed() error {
	if p := l.failure.Load(); p != nil {
		return *p
	}
	return nil
}

// fail records err as why the run stops, unless something already
// failed, and aborts the run. It returns the first failure.
func (l *EffectLog) fail(err error) error {
	if !l.failure.CompareAndSwap(nil, &err) {
		return l.failed()
	}
	if l.stop != nil {
		l.stop(err)
	}
	return err
}

// Diverge fails the run with a DivergedError: the log does not fit the
// program (for one, a record it cannot use).
func (l *EffectLog) Diverge(format string, a ...any) error { return l.diverge(format, a...) }

// diverge fails the run with a DivergedError.
func (l *EffectLog) diverge(format string, a ...any) error {
	return l.fail(&DivergedError{Msg: fmt.Sprintf(format, a...)})
}

// Finish ends the run, whose value was result (value.EncodeRequest of
// it): it closes the log of a recorded run with End; a replay checks that
// it used every record and ended as the recorded run did. It returns why
// the run failed, if the log made it fail.
func (l *EffectLog) Finish(result []byte) error {
	defer l.stop(context.Canceled) // release the run's context
	if err := l.failed(); err != nil {
		return err
	}
	if l.mode != Record {
		if err := l.unconsumed(); err != nil {
			return err
		}
	}
	if l.mode == Replay {
		if string(l.log.End.Result) != string(result) {
			return l.diverge("at the end: the recorded run ended with %s, this run with %s", clip(l.log.End.Result), clip(result))
		}
		return nil
	}
	return l.w.append(&LogRecord{End: &End{Result: result}})
}

// unconsumed fails if some task never asked for records it has.
func (l *EffectLog) unconsumed() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	ids := make([]string, 0, len(l.log.tasks))
	for id := range l.log.tasks {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	for _, id := range ids {
		recs := l.log.tasks[id]
		if c := l.consumed[id]; c < uint64(len(recs)) {
			l.mu.Unlock()
			err := l.diverge("at task %s: the log has %d more records for it, from %s, which this run never reached",
				id, uint64(len(recs))-c, describe(&recs[c]))
			l.mu.Lock()
			return err
		}
	}
	return nil
}

// clipLen is how much of a request or result a divergence error quotes.
const clipLen = 200

func clip(b []byte) string {
	if len(b) <= clipLen {
		return string(b)
	}
	return string(b[:clipLen]) + "..."
}

// call is an effect as a divergence error names it: http_get("url").
func call(kind string, req []byte) string {
	inner := req
	if len(inner) >= 2 && inner[0] == '[' && inner[len(inner)-1] == ']' {
		inner = inner[1 : len(inner)-1]
	}
	return kind + "(" + clip(inner) + ")"
}

// describe is a record as a divergence error names it.
func describe(r *LogRecord) string {
	if r.Check != 0 {
		return fmt.Sprintf("%s at check %d", r.Kind, r.Check)
	}
	return call(r.Kind, r.Req)
}
```

Create `arv/runtime/gate.go`:

```go
package runtime

import (
	"bytes"
	"context"
)

// Outcome is what one effect did, encoded for the log: its result, the
// bytes it charged to alloc_mb, whether its result is a cancellation, and,
// for puts, each line's index in the run's output. Replayed is set when
// the log answered it: the effect did not run.
type Outcome struct {
	Res       []byte
	Charged   int64
	Cancelled bool
	Out       []uint64
	Replayed  bool
}

// SetLog makes t, a root task not yet running, log its run's effects to
// l. Every task it spawns shares l.
func (t *Task) SetLog(l *EffectLog) { t.log = l }

// Log is the run's effect log, nil if the run keeps none.
func (t *Task) Log() *EffectLog { return t.log }

// next is the record that answers t's next effect or check, and whether
// the log answers t at all: in a replay always (nil once t's records are
// spent), in a resumed run while t has records left, in a recording never.
func (t *Task) next() (*LogRecord, bool) {
	l := t.log
	if l.mode == Record {
		return nil, false
	}
	if recs := l.log.tasks[t.ID]; t.seq < uint64(len(recs)) {
		return &recs[t.seq], true
	}
	return nil, l.mode == Replay
}

// consume marks t's next record used.
func (t *Task) consume() {
	t.seq++
	t.log.mu.Lock()
	t.log.consumed[t.ID] = t.seq
	t.log.mu.Unlock()
}

// appendRecord logs r as t's next record.
func (t *Task) appendRecord(r *LogRecord) error {
	t.seq++
	r.Task, r.Seq = t.ID, t.seq
	return t.log.w.append(r)
}

// awaitCancel blocks until t is cancelled and returns why: a replayed
// cancellation happens only once its cause (a sibling's failure, a
// deadline) has happened in this run too, so the first failure is the
// same as when it was recorded (I5).
func (t *Task) awaitCancel() error {
	<-t.Ctx.Done()
	return context.Cause(t.Ctx)
}

// Effect runs do, one effect of t named kind with arguments req (I7). With
// no log it just runs do. Recording, it runs do and logs what it did.
// Replaying, it runs nothing: it returns the logged outcome of t's next
// effect if that effect has the same kind and req, and otherwise fails
// the run with a DivergedError. Resuming, it replays while t has records
// left, then records. A replayed cancellation is returned only once t is
// cancelled.
func (t *Task) Effect(kind string, req []byte, do func() Outcome) (Outcome, error) {
	l := t.log
	if l == nil {
		return do(), nil
	}
	if err := l.failed(); err != nil {
		return Outcome{}, err
	}
	rec, replaying := t.next()
	if replaying {
		if rec == nil {
			return Outcome{}, l.diverge("at task %s, effect %d: the log has no more records for it; the program asked for %s",
				t.ID, t.seq+1, call(kind, req))
		}
		if rec.Check != 0 || rec.Kind != kind || !bytes.Equal(rec.Req, req) {
			return Outcome{}, l.diverge("at task %s, effect %d: the log has %s, the program asked for %s",
				t.ID, rec.Seq, describe(rec), call(kind, req))
		}
		t.consume()
		if rec.Cancelled {
			t.awaitCancel()
		}
		return Outcome{Res: rec.Res, Charged: rec.Charged, Cancelled: rec.Cancelled, Out: rec.Out, Replayed: true}, nil
	}
	o := do()
	err := t.appendRecord(&LogRecord{Kind: kind, Req: req, Res: o.Res, Charged: o.Charged, Cancelled: o.Cancelled, Out: o.Out})
	return o, err
}
```

In `arv/runtime/task.go`, replace:

```go
	parent   *Task // nil for the root
	steps    uint64
	children atomic.Uint64
	horizon  []uint64 // spawn times from the root's child down to this task (see Horizon)
```

with:

```go
	parent   *Task // nil for the root
	steps    uint64
	log      *EffectLog // the run's effect log, nil if it keeps none
	seq      uint64     // records of this task logged or replayed so far
	children atomic.Uint64
	horizon  []uint64 // spawn times from the root's child down to this task (see Horizon)
```

In `arv/runtime/task.go`, replace:

```go
	copy(horizon, t.horizon)
	horizon[len(t.horizon)] = clock.Add(1)
	return &Task{ID: fmt.Sprintf("%s/%d", t.ID, n), Caps: caps, Ctx: ctx, Out: t.Out, parent: t, horizon: horizon}
}

```

with:

```go
	copy(horizon, t.horizon)
	horizon[len(t.horizon)] = clock.Add(1)
	return &Task{ID: fmt.Sprintf("%s/%d", t.ID, n), Caps: caps, Ctx: ctx, Out: t.Out, parent: t, horizon: horizon, log: t.log}
}

```

In `arv/runtime/task.go`, replace:

```go
		return t
	}
	return &Task{ID: fmt.Sprintf("%s#%d", t.ID, n), Caps: t.Caps, Ctx: t.Ctx, Out: t.Out, parent: t.parent, horizon: t.horizon}
}

```

with:

```go
		return t
	}
	return &Task{ID: fmt.Sprintf("%s#%d", t.ID, n), Caps: t.Caps, Ctx: t.Ctx, Out: t.Out, parent: t.parent, horizon: t.horizon, log: t.log}
}

```

- [ ] **Step 4: Run the tests, then the full checks**

Run: `cd <worktree> && go test -race ./arv/runtime/ -run 'TestReplayAnswersFromTheLog|TestReplayDiverges'`, then `cd <worktree> && gofmt -l . && go vet ./... && go test -race ./...`

Expected: `ok` for every package, and `gofmt -l .` prints nothing.

- [ ] **Step 5: Commit**

```bash
cd <worktree> && git add arv/runtime/effectlog.go arv/runtime/gate.go arv/runtime/gate_test.go arv/runtime/task.go && git commit -F - <<'MSG'
feat(runtime): EffectLog records and replays effects, and fails a run that diverges

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
MSG
```
