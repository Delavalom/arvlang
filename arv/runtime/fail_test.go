package runtime

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"
)

// failRace runs a scope with two tasks that both fail, a with errA and b
// with errB. first says which reaches its failure first: the other waits
// until first has returned from its Fail (live), or 200 ms (replayed, where
// the waiting one must then wait for the log). It returns the scope's error.
func failRace(root *Task, first string, errA, errB error) error {
	s := NewScope(root.Ctx)
	a, b := root.Child(s.Context(), NewCapSet()), root.Child(s.Context(), NewCapSet())
	failed := make(chan struct{})
	run := func(t *Task, name string, err error) {
		s.SuperviseWith(t.ID, func(int) error {
			if name != first {
				select {
				case <-failed:
				case <-time.After(200 * time.Millisecond):
				}
			}
			return err
		}, nil, func(err error) {
			t.Fail(s, err)
			if name == first {
				close(failed)
			}
		}, func() { t.Finished(nil) })
	}
	run(a, "a", errA)
	run(b, "b", errB)
	return s.Wait()
}

// Review round 3 (Critical): which failure of a scope is first is part of
// the log. Recorded with b first, a replay where a gets there first still
// records b's failure: a waits for it.
func TestReplayedFirstFailure(t *testing.T) {
	errA, errB := errors.New("A"), errors.New("B")
	var log bytes.Buffer
	l, _ := NewRecorder(&log, Header{}, &bytes.Buffer{}, nil)
	root := NewRoot(l.Start(context.Background()), NewCapSet(), &bytes.Buffer{})
	root.SetLog(l)
	if err := failRace(root, "b", errA, errB); err != errB {
		t.Fatalf("recorded: %v, want B", err)
	}
	if err := l.Finish([]byte(`[null]`)); err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(log.Bytes(), []byte(`"task":"root/1","seq":1,"kind":"failed","check":1`)) {
		t.Fatalf("b's failure is not logged as the first:\n%s", log.String())
	}
	for range 3 {
		l, root := replayer(t, log.Bytes(), &bytes.Buffer{})
		if err := failRace(root, "a", errA, errB); err != errB {
			t.Fatalf("replayed: %v, want the recorded first failure, B", err)
		}
		if err := l.Finish([]byte(`[null]`)); err != nil {
			t.Fatal(err)
		}
	}
}
