# Phase 6, task p6.5: Output in recorded order; resume; host cancel and write failure

> **For agentic workers:** this file is one task, p6.5. Do it test first, exactly as written; if something here does not match the code you find, stop and report instead of improvising. Prerequisite: tasks p6.1 to p6.4 are committed in your tree. Start every shell command with `cd <your worktree> &&`. Before committing: `gofmt -l .` prints nothing, `go vet ./...` is clean and `go test -race ./...` is green. End the commit message with a blank line and then `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`. Never use `--no-verify`; keep Go files under 15 KB and markdown under 18 KB. `go.mod` says `go 1.24.0` (toolchain 1.26.2), so do not use `testing/synctest` or `sync.WaitGroup.Go`. `arv/runtime` stays stdlib-only.
>
> Every code block below was applied, in task order, to a scratch copy of the tree and passed `go vet ./...` and `go test -race ./...` (including `e2e/`). Design: `13-phase6-durable-replay.md`. Deviations from it, and why: `p6-plan-1.md`.

### Task p6.5: Output in recorded order; resume; host cancel and write failure

- **Output** (`output.go`): a `puts` is one effect, `Task.Print(req, lines)`, whose record holds each line's index in the whole run's output (`out`). Replayed, a line waits until every line logged before it has printed, so the output is the recorded bytes whatever order the tasks get there in. `Finish` flushes what is still held.
- **Resume**: `NewResumer(log, w, out, sync)` answers each task from the log while it has records, then runs it live and appends to `w`, continuing the hash chain. A finished log is replayed instead.
- **A host cancel is a crash, not an end**: once the run's context is cancelled from outside (or the run diverged), nothing more is logged (`aborted`), and no `end` line is written, so the log can be resumed.
- **A log that cannot be written stops the run** (`fail`): no effect runs after a failed write or fsync.

**Files:**
- Modify: `arv/runtime/effectlog.go`
- Modify: `arv/runtime/gate.go`
- Create: `arv/runtime/output.go`
- Create (test): `arv/runtime/output_test.go`
- Create (test): `arv/runtime/resume_test.go`

- [ ] **Step 1: Write the failing tests**

Create `arv/runtime/output_test.go`:

```go
package runtime

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

// Replayed lines print in the recorded order, whatever order their tasks
// reach them in.
func TestReplayPrintsInRecordedOrder(t *testing.T) {
	var log, out bytes.Buffer
	l, _ := NewRecorder(&log, Header{}, &out, nil)
	root := NewRoot(l.Start(context.Background()), NewCapSet(), &out)
	root.SetLog(l)
	a, b := root.Child(root.Ctx, NewCapSet()), root.Child(root.Ctx, NewCapSet())
	a.Print([]byte(`["a1"]`), []string{"a1"})
	b.Print([]byte(`["b1","b2"]`), []string{"b1", "b2"})
	a.Print([]byte(`["a2"]`), []string{"a2"})
	l.Finish([]byte(`[null]`))
	want := "a1\nb1\nb2\na2\n"
	if out.String() != want {
		t.Fatalf("recorded %q", out.String())
	}
	out.Reset()
	l, root = replayer(t, log.Bytes(), &out)
	a, b = root.Child(root.Ctx, NewCapSet()), root.Child(root.Ctx, NewCapSet())
	b.Print([]byte(`["b1","b2"]`), []string{"b1", "b2"}) // b first this time
	if out.Len() != 0 {
		t.Fatalf("b's lines printed before a1: %q", out.String())
	}
	a.Print([]byte(`["a1"]`), []string{"a1"})
	a.Print([]byte(`["a2"]`), []string{"a2"})
	if err := l.Finish([]byte(`[null]`)); err != nil || out.String() != want {
		t.Fatalf("replayed %q, %v", out.String(), err)
	}
}

// A replayed puts that prints another number of lines diverges.
func TestReplayedPrintChecksItsLines(t *testing.T) {
	var log bytes.Buffer
	l, _ := NewRecorder(&log, Header{}, &bytes.Buffer{}, nil)
	root := NewRoot(l.Start(context.Background()), NewCapSet(), &bytes.Buffer{})
	root.SetLog(l)
	root.Print([]byte(`["a"]`), []string{"a"})
	l.Finish([]byte(`[null]`))
	_, root = replayer(t, log.Bytes(), &bytes.Buffer{})
	err := root.Print([]byte(`["a"]`), []string{"a", "b"})
	if err == nil || !strings.Contains(err.Error(), "the log printed 1 lines, the program 2") {
		t.Fatalf("Print = %v", err)
	}
}
```

Create `arv/runtime/resume_test.go`:

```go
package runtime

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
)

// A run killed after two effects resumes: the two are answered from the
// log, the rest run live and are appended, and the log stays one chain.
func TestResumeReplaysThenRecords(t *testing.T) {
	var log bytes.Buffer
	l, _ := NewRecorder(&log, Header{}, &bytes.Buffer{}, nil)
	root := NewRoot(l.Start(context.Background()), NewCapSet(), &bytes.Buffer{})
	root.SetLog(l)
	ran := 0
	live := func() Outcome { ran++; return Outcome{Res: []byte(`1`)} }
	root.Effect("now", []byte(`[]`), live)
	root.Effect("now", []byte(`[]`), live)
	// killed: no Finish

	read, err := ReadLog(bytes.NewReader(log.Bytes()))
	if err != nil || read.End != nil {
		t.Fatalf("ReadLog = %v, end %v", err, read.End)
	}
	if _, err := NewReplayer(read, &bytes.Buffer{}); err == nil {
		t.Fatal("replayed a run that did not finish")
	}
	l = NewResumer(read, &log, &bytes.Buffer{}, nil)
	root = NewRoot(l.Start(context.Background()), NewCapSet(), &bytes.Buffer{})
	root.SetLog(l)
	ran = 0
	for i := 0; i < 4; i++ {
		if o, err := root.Effect("now", []byte(`[]`), live); err != nil || o.Replayed != (i < 2) {
			t.Fatalf("effect %d: %+v, %v", i+1, o, err)
		}
	}
	if err := l.Finish([]byte(`[null]`)); err != nil || ran != 2 {
		t.Fatalf("Finish = %v; %d effects ran live, want 2", err, ran)
	}
	read, err = ReadLog(bytes.NewReader(log.Bytes()))
	if err != nil || read.End == nil || read.Records() != 4 {
		t.Fatalf("resumed log: %v, end %v, %d records", err, read.End, read.Records())
	}
}

// A run the host cancels is torn down, not finished: what it does from
// then on is not logged, and it has no End, so it can be resumed.
func TestHostCancelIsNotLogged(t *testing.T) {
	var log bytes.Buffer
	l, _ := NewRecorder(&log, Header{}, &bytes.Buffer{}, nil)
	host, cancel := context.WithCancel(context.Background())
	root := NewRoot(l.Start(host), NewCapSet(), &bytes.Buffer{})
	root.SetLog(l)
	root.Effect("now", []byte(`[]`), func() Outcome { return Outcome{Res: []byte(`1`)} })
	cancel()
	root.Effect("sleep", []byte(`[9]`), func() Outcome { return Outcome{Res: []byte(`{"error":["c","cancelled","",""]}`), Cancelled: true} })
	if root.Err() == nil {
		t.Fatal("not cancelled")
	}
	if err := l.Finish([]byte(`[null]`)); err != nil {
		t.Fatal(err)
	}
	read, err := ReadLog(bytes.NewReader(log.Bytes()))
	if err != nil || read.End != nil || read.Records() != 1 {
		t.Fatalf("ReadLog = %v, end %v, %d records", err, read.End, read.Records())
	}
}

type failingWriter struct{ after int }

func (w *failingWriter) Write(p []byte) (int, error) {
	if w.after == 0 {
		return 0, errors.New("disk full")
	}
	w.after--
	return len(p), nil
}

// A log that cannot be written stops the run: no effect runs after it.
func TestLogWriteFailureStopsTheRun(t *testing.T) {
	l, err := NewRecorder(&failingWriter{after: 1}, Header{}, &bytes.Buffer{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	root := NewRoot(l.Start(context.Background()), NewCapSet(), &bytes.Buffer{})
	root.SetLog(l)
	ran := 0
	live := func() Outcome { ran++; return Outcome{Res: []byte(`1`)} }
	if _, err := root.Effect("now", []byte(`[]`), live); err == nil || !strings.Contains(err.Error(), "disk full") {
		t.Fatalf("first effect = %v", err)
	}
	if _, err := root.Effect("now", []byte(`[]`), live); err == nil || ran != 1 {
		t.Fatalf("second effect = %v after %d ran", err, ran)
	}
	if root.Ctx.Err() == nil || l.Finish([]byte(`[null]`)) == nil {
		t.Fatal("the run went on")
	}
}
```

- [ ] **Step 2: Run them and watch them fail**

Run: `cd <worktree> && go test -race ./arv/runtime/ -run 'TestReplayPrintsInRecordedOrder|TestReplayedPrintChecksItsLines|TestResumeReplaysThenRecords|TestHostCancelIsNotLogged|TestLogWriteFailureStopsTheRun'`

Expected: the package fails to build: `b.Print undefined`, `undefined: NewResumer`. (With those alone added, `TestHostCancelIsNotLogged` and `TestLogWriteFailureStopsTheRun` would still fail: the cancelled effect and the `end` are logged, and the run goes on after a failed write.)

- [ ] **Step 3: Implement**

In `arv/runtime/effectlog.go`, replace:

```go
type EffectLog struct {
	mode  Mode
	log   *Log            // Replay, Resume
	w     *logWriter      // Record, Resume
	abort context.Context // ends when the host cancels the run or it diverges
	stop  context.CancelCauseFunc
```

with:

```go
type EffectLog struct {
	mode  Mode
	log   *Log       // Replay, Resume
	w     *logWriter // Record, Resume
	out   *output
	abort context.Context // ends when the host cancels the run or it diverges
	stop  context.CancelCauseFunc
```

In `arv/runtime/effectlog.go`, replace:

```go
// to out.
func NewRecorder(w io.Writer, h Header, out io.Writer, sync func() error) (*EffectLog, error) {
	l := &EffectLog{mode: Record, w: &logWriter{w: w, sync: sync}}
	if err := l.w.append(&LogRecord{V: LogVersion, Header: &h}); err != nil {
		return nil, err
```

with:

```go
// to out.
func NewRecorder(w io.Writer, h Header, out io.Writer, sync func() error) (*EffectLog, error) {
	l := &EffectLog{mode: Record, w: &logWriter{w: w, sync: sync}, out: newOutput(out, nil)}
	if err := l.w.append(&LogRecord{V: LogVersion, Header: &h}); err != nil {
		return nil, err
```

In `arv/runtime/effectlog.go`, replace:

```go
		return nil, ErrUnfinished
	}
	return &EffectLog{mode: Replay, log: log, consumed: map[string]uint64{}}, nil
}

```

with:

```go
		return nil, ErrUnfinished
	}
	return &EffectLog{mode: Replay, log: log, out: newOutput(out, log.outs), consumed: map[string]uint64{}}, nil
}

// NewResumer answers a run from log while each task has records, then
// runs it live and appends to w, which must be at log.Size (a partial
// last line cut off). A complete log is replayed instead.
func NewResumer(log *Log, w io.Writer, out io.Writer, sync func() error) *EffectLog {
	if log.End != nil {
		l, _ := NewReplayer(log, out)
		return l
	}
	lw := &logWriter{w: w, sync: sync, prev: log.last}
	return &EffectLog{mode: Resume, log: log, w: lw, out: newOutput(out, log.outs), consumed: map[string]uint64{}}
}

```

In `arv/runtime/effectlog.go`, replace:

```go
	return l.abort
}

// failed is the first divergence or write failure, or nil.
```

with:

```go
	return l.abort
}

// aborted reports that the run is being torn down from outside (the host
// cancelled it, or it diverged): what it does from now on is not part of
// the run and is not logged, as if it had been killed here.
func (l *EffectLog) aborted() bool { return l.abort != nil && l.abort.Err() != nil }

// failed is the first divergence or write failure, or nil.
```

In `arv/runtime/effectlog.go`, replace:

```go

// Finish ends the run, whose value was result (value.EncodeRequest of
// it): it closes the log of a recorded run with End; a replay checks that
// it used every record and ended as the recorded run did. It returns why
// the run failed, if the log made it fail.
func (l *EffectLog) Finish(result []byte) error {
	defer l.stop(context.Canceled) // release the run's context
	if err := l.failed(); err != nil {
		return err
```

with:

```go

// Finish ends the run, whose value was result (value.EncodeRequest of
// it): it prints what output is still held, then closes the log of a
// recorded or resumed run with End, unless the host cancelled it; a
// replay checks that it used every record and ended as the recorded run
// did. It returns why the run failed, if the log made it fail.
func (l *EffectLog) Finish(result []byte) error {
	defer l.stop(context.Canceled) // release the run's context
	l.out.flush()
	if err := l.failed(); err != nil {
		return err
```

In `arv/runtime/effectlog.go`, replace:

```go
		return nil
	}
	return l.w.append(&LogRecord{End: &End{Result: result}})
}

```

with:

```go
		return nil
	}
	if l.aborted() {
		return nil
	}
	if err := l.w.append(&LogRecord{End: &End{Result: result}}); err != nil {
		return l.fail(err)
	}
	return nil
}

```

In `arv/runtime/gate.go`, replace:

```go
}

// appendRecord logs r as t's next record.
func (t *Task) appendRecord(r *LogRecord) error {
	t.seq++
	r.Task, r.Seq = t.ID, t.seq
	return t.log.w.append(r)
}

```

with:

```go
}

// appendRecord logs r as t's next record, unless the run is being torn
// down: from then on nothing is logged, as if the run had been killed.
func (t *Task) appendRecord(r *LogRecord) error {
	if t.log.aborted() {
		return nil
	}
	t.seq++
	r.Task, r.Seq = t.ID, t.seq
	if err := t.log.w.append(r); err != nil {
		return t.log.fail(err)
	}
	return nil
}

```

Create `arv/runtime/output.go`:

```go
package runtime

import (
	"io"
	"sync"
)

// output prints a logged run's lines (puts) in the order the recorded run
// printed them, whatever order its tasks reach them in when replayed: a
// replayed line waits for every line logged before it. A live line (a
// recording, or a resumed task past its records) takes the next index and
// prints after every logged line.
type output struct {
	mu      sync.Mutex
	w       io.Writer
	logged  []uint64          // the logged lines' indices, sorted
	next    int               // logged[next] is the next logged line due
	pending map[uint64]string // logged lines reached before their turn
	queued  []string          // live lines that came before every logged one printed
	n       uint64            // the index the next live line takes
}

func newOutput(w io.Writer, logged []uint64) *output {
	o := &output{w: w, logged: logged, pending: map[uint64]string{}}
	if len(logged) > 0 {
		o.n = logged[len(logged)-1] + 1
	}
	return o
}

// print writes live lines, and returns the index each took.
func (o *output) print(lines []string) []uint64 {
	o.mu.Lock()
	defer o.mu.Unlock()
	ks := make([]uint64, len(lines))
	for i, s := range lines {
		ks[i] = o.n
		o.n++
		if o.next == len(o.logged) {
			WriteLine(o.w, s)
		} else {
			o.queued = append(o.queued, s)
		}
	}
	return ks
}

// emit prints replayed lines, each once every line logged before it has.
func (o *output) emit(ks []uint64, lines []string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	for i, k := range ks {
		o.pending[k] = lines[i]
	}
	o.drain()
}

func (o *output) drain() {
	for o.next < len(o.logged) {
		s, ok := o.pending[o.logged[o.next]]
		if !ok {
			return
		}
		delete(o.pending, o.logged[o.next])
		WriteLine(o.w, s)
		o.next++
	}
	for _, s := range o.queued {
		WriteLine(o.w, s)
	}
	o.queued = nil
}

// flush prints whatever is still held, in order: at the end of a run that
// diverged, some logged lines never came.
func (o *output) flush() {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.drain()
	for ; o.next < len(o.logged); o.next++ {
		if s, ok := o.pending[o.logged[o.next]]; ok {
			WriteLine(o.w, s)
		}
	}
	for _, s := range o.queued {
		WriteLine(o.w, s)
	}
	o.queued = nil
}

// Print writes lines, the text of one puts by t, as one effect named
// "puts" whose request is req (their encoding). Replayed, it prints them
// once every line the recorded run printed before them has been: the
// output is the recorded run's, byte for byte, whatever the order its
// tasks get there in.
func (t *Task) Print(req []byte, lines []string) error {
	l := t.log
	if l == nil {
		for _, s := range lines {
			WriteLine(t.Out, s)
		}
		return nil
	}
	o, err := t.Effect("puts", req, func() Outcome { return Outcome{Out: l.out.print(lines)} })
	if err != nil || !o.Replayed {
		return err
	}
	if len(o.Out) != len(lines) {
		return l.diverge("at task %s, effect %d: the log printed %d lines, the program %d", t.ID, t.seq, len(o.Out), len(lines))
	}
	l.out.emit(o.Out, lines)
	return nil
}
```

- [ ] **Step 4: Run the tests, then the full checks**

Run: `cd <worktree> && go test -race ./arv/runtime/ -run 'TestReplayPrintsInRecordedOrder|TestReplayedPrintChecksItsLines|TestResumeReplaysThenRecords|TestHostCancelIsNotLogged|TestLogWriteFailureStopsTheRun'`, then `cd <worktree> && gofmt -l . && go vet ./... && go test -race ./...`

Expected: `ok` for every package, and `gofmt -l .` prints nothing.

- [ ] **Step 5: Commit**

```bash
cd <worktree> && git add arv/runtime/effectlog.go arv/runtime/gate.go arv/runtime/output.go arv/runtime/output_test.go arv/runtime/resume_test.go && git commit -F - <<'MSG'
feat(runtime): replayed output keeps the recorded order; resume; host cancel and write failure

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
MSG
```
