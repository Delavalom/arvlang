package runtime

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// A resume's deadline fires when the first live task inside the scope
// reaches it on its own reading of the recorded run's clock; a task that
// retires stops counting, and a closed scope's timer is stopped.
func TestResumeDeadlineArms(t *testing.T) {
	fired := make(chan error, 1)
	cause := &ExceededError{Kind: "timeout", Limit: 1000}
	d := &deadline{cause: cause, vdl: 1000, fire: func(err error) { fired <- err }}
	now := time.Now()
	behind := &Task{live: true, rbase: now, vbase: 0}    // reaches 1000 ms in 1000 ms
	ahead := &Task{live: true, rbase: now, vbase: 900}   // in 100 ms
	further := &Task{live: true, rbase: now, vbase: 950} // in 50 ms
	d.arm(behind)
	if got := d.when(); !got.Equal(now.Add(time.Second)) {
		t.Fatalf("one task: fires at %v, want %v", got.Sub(now), time.Second)
	}
	d.arm(ahead)
	d.arm(further)
	if got := d.when(); !got.Equal(now.Add(50 * time.Millisecond)) {
		t.Fatalf("three tasks: fires at %v, want 50ms", got.Sub(now))
	}
	further.retire()
	if got := d.when(); !got.Equal(now.Add(100*time.Millisecond)) || len(further.armed) != 0 {
		t.Fatalf("after one retired: fires at %v, want 100ms", got.Sub(now))
	}
	select {
	case err := <-fired:
		if err != cause {
			t.Fatalf("fired with %v", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("the deadline did not fire")
	}
	d.close()
	if !d.when().IsZero() || d.timer != nil {
		t.Fatal("a closed deadline still has a timer")
	}
	d.arm(behind) // a no-op once closed
	if !d.when().IsZero() {
		t.Fatal("a closed deadline was armed again")
	}
}

// arm prunes the task's list: a task that opens timed scopes in a loop
// does not keep every closed one.
func TestArmPrunesClosedDeadlines(t *testing.T) {
	task := &Task{live: true, rbase: time.Now()}
	for i := 0; i < 1000; i++ {
		d := &deadline{cause: errors.New("t"), vdl: 1 << 40, fire: func(error) {}}
		d.arm(task)
		d.arm(task) // armed again: listed once
		d.close()
	}
	if len(task.armed) > 1 {
		t.Fatalf("the task lists %d deadlines", len(task.armed))
	}
}

// A deadline that has passed while its timer is late is seen at the next
// check as the scope's timeout, which cancels the scope, and not as
// context.DeadlineExceeded: a log can then name the scope (By).
func TestLateDeadlineIsTheScopesTimeout(t *testing.T) {
	cause := &ExceededError{Kind: "timeout", Limit: 60000}
	s := NewTimedScope(context.Background(), time.Minute, cause)
	defer s.Wait()
	s.deadline = time.Now().Add(-time.Millisecond) // the timer is late
	inner := NewScope(s.Context())
	defer inner.Wait()
	if err := liveErr(inner.Context()); err != cause {
		t.Fatalf("liveErr = %v, want the timeout", err)
	}
	if s.Expired() != cause {
		t.Fatal("the scope was not cancelled with its timeout")
	}
}

// A quiet check (resume, other tasks' records left) does not see a
// failure it has not seen, but it does see a deadline, once the task is
// past the spawns the log has.
func TestQuietChecksSeeADeadline(t *testing.T) {
	var log bytes.Buffer
	l, _ := NewRecorder(&log, Header{}, &bytes.Buffer{}, nil)
	root := NewRoot(l.Start(context.Background()), NewCapSet(), &bytes.Buffer{})
	root.SetLog(l)
	root.Child(root.Ctx, NewCapSet()).Effect("now", []byte(`[]`), func() Outcome { return Outcome{Res: []byte(`1`)} })
	read, err := ReadLog(bytes.NewReader(log.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		cause error
		seen  bool
	}{{errors.New("boom"), false}, {&ExceededError{Kind: "timeout", Limit: 5}, true}} {
		l := NewResumer(read, &bytes.Buffer{}, &bytes.Buffer{}, nil)
		root := NewRoot(l.Start(context.Background()), NewCapSet(), &bytes.Buffer{})
		root.SetLog(l)
		root.Child(root.Ctx, NewCapSet()) // spawned, its record not yet used: root is not behind
		ctx, cancel := context.WithCancelCause(root.Ctx)
		cancel(tt.cause)
		root.Ctx = ctx
		if err := root.Err(); (err == tt.cause) != tt.seen {
			t.Errorf("%v: quiet Err = %v", tt.cause, err)
		}
	}
}

// A spawned task's end is a record. A replay must reach it where the
// recorded task ended, and a resume uses it, or logs it for a task that
// ends live.
func TestFinishedIsARecord(t *testing.T) {
	var log bytes.Buffer
	l, _ := NewRecorder(&log, Header{}, &bytes.Buffer{}, nil)
	root := NewRoot(l.Start(context.Background()), NewCapSet(), &bytes.Buffer{})
	root.SetLog(l)
	now := func() Outcome { return Outcome{Res: []byte(`1`)} }
	a, b := root.Child(root.Ctx, NewCapSet()), root.Child(root.Ctx, NewCapSet())
	a.Effect("now", []byte(`[]`), now)
	a.Finished()
	b.Effect("now", []byte(`[]`), now)
	// killed before b ends
	if !bytes.Contains(log.Bytes(), []byte(`"task":"root/0","seq":2,"kind":"done"`)) {
		t.Fatalf("no end record for root/0:\n%s", log.Bytes())
	}
	read, err := ReadLog(bytes.NewReader(log.Bytes()))
	if err != nil {
		t.Fatal(err)
	}

	l = NewResumer(read, &log, &bytes.Buffer{}, nil)
	root = NewRoot(l.Start(context.Background()), NewCapSet(), &bytes.Buffer{})
	root.SetLog(l)
	a, b = root.Child(root.Ctx, NewCapSet()), root.Child(root.Ctx, NewCapSet())
	a.Effect("now", []byte(`[]`), now)
	if len(l.doneAts) != 1 {
		t.Fatalf("pending ends: %v", l.doneAts)
	}
	a.Finished()
	b.Effect("now", []byte(`[]`), now)
	b.Finished()
	if err := l.waitDone(1 << 40); err != nil {
		t.Fatal(err)
	}
	if err := l.Finish([]byte(`[null]`)); err != nil {
		t.Fatalf("resume: %v", err)
	}
	if !bytes.Contains(log.Bytes(), []byte(`"task":"root/1","seq":2,"kind":"done"`)) {
		t.Fatalf("the resume did not log root/1's end:\n%s", log.Bytes())
	}

	read, err = ReadLog(bytes.NewReader(log.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	l, _ = NewReplayer(read, &bytes.Buffer{})
	root = NewRoot(l.Start(context.Background()), NewCapSet(), &bytes.Buffer{})
	root.SetLog(l)
	a = root.Child(root.Ctx, NewCapSet())
	a.Finished() // before its effect
	if err := l.failed(); err == nil || !strings.Contains(err.Error(), "at task root/0: it ended, and the log has now(") {
		t.Fatalf("a task that ended early: %v", err)
	}
}

// first is the earliest start under a task's children from a given
// number on: an attempt counts as its task, a descendant as its
// ancestor's child, and an effect starts when it began.
func TestFirstRecordUnderLaterChildren(t *testing.T) {
	l := &EffectLog{later: laterChildren(map[string][]LogRecord{
		"root/0":     {{Kind: "now", Began: 50, At: 60}},
		"root/1#2":   {{Kind: "cancelled", Check: 1, At: 40}},
		"root/3/0/1": {{Kind: "now", Began: 90, At: 95}},
		"root/3":     {{Kind: KindDone, At: 200}},
	})}
	for _, tt := range []struct {
		id    string
		n     uint64
		start int64
		ok    bool
	}{
		{"root", 0, 40, true}, {"root", 1, 40, true}, {"root", 2, 90, true}, {"root", 4, 0, false},
		{"root/3", 0, 90, true}, {"root/3", 1, 0, false}, {"root/3/0", 1, 90, true}, {"root/0", 0, 0, false},
	} {
		if start, ok := l.first(tt.id, tt.n); start != tt.start || ok != tt.ok {
			t.Errorf("first(%s, %d) = %d, %v; want %d, %v", tt.id, tt.n, start, ok, tt.start, tt.ok)
		}
	}
}

// A resumed task is behind the log, and does not see a deadline, until it
// has made every spawn the log has records under: here its second, after
// a first the log has nothing of.
func TestBehindUntilTheLastSpawnWithRecords(t *testing.T) {
	var log bytes.Buffer
	l, _ := NewRecorder(&log, Header{}, &bytes.Buffer{}, nil)
	root := NewRoot(l.Start(context.Background()), NewCapSet(), &bytes.Buffer{})
	root.SetLog(l)
	root.Child(root.Ctx, NewCapSet())
	root.Child(root.Ctx, NewCapSet()).Effect("now", []byte(`[]`), func() Outcome { return Outcome{Res: []byte(`1`)} })
	read, err := ReadLog(bytes.NewReader(log.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	r := NewResumer(read, &bytes.Buffer{}, &bytes.Buffer{}, nil)
	root = NewRoot(r.Start(context.Background()), NewCapSet(), &bytes.Buffer{})
	root.SetLog(r)
	timeout := &ExceededError{Kind: "timeout", Limit: 5}
	ctx, cancel := context.WithCancelCause(root.Ctx)
	cancel(timeout)
	root.Ctx = ctx
	for i, want := range []error{nil, nil, timeout} {
		if err := root.Err(); err != want {
			t.Errorf("after %d spawns: Err = %v, want %v", i, err, want)
		}
		root.Child(root.Ctx, NewCapSet())
	}
}
