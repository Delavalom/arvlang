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

// An fsync that fails stops the run as a failed write does (p6.5): the
// effect it follows is not lost silently, and no effect runs after it.
func TestLogSyncFailureStopsTheRun(t *testing.T) {
	syncs := 0
	sync := func() error {
		if syncs++; syncs > 1 { // the header's sync succeeds
			return errors.New("I/O error")
		}
		return nil
	}
	l, err := NewRecorder(&bytes.Buffer{}, Header{}, &bytes.Buffer{}, sync)
	if err != nil {
		t.Fatal(err)
	}
	root := NewRoot(l.Start(context.Background()), NewCapSet(), &bytes.Buffer{})
	root.SetLog(l)
	ran := 0
	live := func() Outcome { ran++; return Outcome{Res: []byte(`1`)} }
	if _, err := root.Effect("now", []byte(`[]`), live); err == nil || !strings.Contains(err.Error(), "fsync: I/O error") {
		t.Fatalf("first effect = %v", err)
	}
	if _, err := root.Effect("now", []byte(`[]`), live); err == nil || ran != 1 {
		t.Fatalf("second effect = %v after %d ran", err, ran)
	}
	if root.Ctx.Err() == nil || l.Finish([]byte(`[null]`)) == nil {
		t.Fatal("the run went on")
	}
}
