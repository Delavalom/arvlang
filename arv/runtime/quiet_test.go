package runtime

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"
)

// A resumed task past its records checks quietly while another task still
// has records: it does not see its cancellation until the log is used up,
// unless it has seen that cause before. A wait that began quiet ends once
// the log is used up.
func TestQuietChecks(t *testing.T) {
	var log bytes.Buffer
	l, _ := NewRecorder(&log, Header{}, &bytes.Buffer{}, nil)
	root := NewRoot(l.Start(context.Background()), NewCapSet(), &bytes.Buffer{})
	root.SetLog(l)
	now := func() Outcome { return Outcome{Res: []byte(`1`)} }
	root.Child(root.Ctx, NewCapSet()).Effect("now", []byte(`[]`), now)
	read, err := ReadLog(bytes.NewReader(log.Bytes()))
	if err != nil {
		t.Fatal(err)
	}

	l = NewResumer(read, &log, &bytes.Buffer{}, nil)
	root = NewRoot(l.Start(context.Background()), NewCapSet(), &bytes.Buffer{})
	root.SetLog(l)
	child := root.Child(root.Ctx, NewCapSet())
	ctx, cancel := context.WithCancelCause(root.Ctx)
	boom := errors.New("boom")
	cancel(boom)
	root.Ctx = ctx
	if err := root.Err(); err != nil {
		t.Fatalf("quiet Err = %v", err)
	}
	waited := make(chan error, 1)
	go func() { waited <- root.WaitFor(make(chan struct{})) }()
	select {
	case err := <-waited:
		t.Fatalf("quiet WaitFor returned %v before the log was used up", err)
	case <-time.After(50 * time.Millisecond):
	}
	if o, err := child.Effect("now", []byte(`[]`), now); err != nil || !o.Replayed {
		t.Fatalf("child: %+v, %v", o, err)
	}
	select {
	case err := <-waited:
		if err != boom {
			t.Fatalf("WaitFor = %v, want boom", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("WaitFor did not end once the log was used up")
	}
	if err := root.Err(); err != boom {
		t.Fatalf("Err after the log = %v", err)
	}
}

// A cause the task has seen is seen again even while quiet.
func TestQuietSeesASeenCause(t *testing.T) {
	var log bytes.Buffer
	l, _ := NewRecorder(&log, Header{}, &bytes.Buffer{}, nil)
	root := NewRoot(l.Start(context.Background()), NewCapSet(), &bytes.Buffer{})
	root.SetLog(l)
	root.Child(root.Ctx, NewCapSet()).Effect("now", []byte(`[]`), func() Outcome { return Outcome{Res: []byte(`1`)} })
	read, _ := ReadLog(bytes.NewReader(log.Bytes()))
	l = NewResumer(read, &log, &bytes.Buffer{}, nil)
	root = NewRoot(l.Start(context.Background()), NewCapSet(), &bytes.Buffer{})
	root.SetLog(l)
	ctx, cancel := context.WithCancelCause(root.Ctx)
	boom := errors.New("boom")
	cancel(boom)
	root.Ctx = ctx
	root.saw(boom)
	if err := root.Err(); err != boom {
		t.Fatalf("Err = %v, want the cause it saw", err)
	}
	if err := root.WaitFor(make(chan struct{})); err != boom {
		t.Fatalf("WaitFor = %v, want the cause it saw", err)
	}
	for i := 0; i < 2*maxSeen; i++ {
		root.saw(errors.New("another"))
	}
	if len(root.seen) != maxSeen {
		t.Fatalf("seen holds %d causes", len(root.seen))
	}
}
