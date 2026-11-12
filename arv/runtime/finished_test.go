package runtime

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

// Review round 6: a task's end is logged before its slot is released,
// and, replayed, counted used only after: a resumed spawn that waits for
// the logged ends (waitDone) must find their slots free when it wakes.
func TestFinishedOrdersItsEndAroundRelease(t *testing.T) {
	var log bytes.Buffer
	l, _ := NewRecorder(&log, Header{}, &bytes.Buffer{}, nil)
	root := NewRoot(l.Start(context.Background()), NewCapSet(), &bytes.Buffer{})
	root.SetLog(l)
	a := root.Child(root.Ctx, NewCapSet())
	logged := false
	a.Finished(func() { logged = strings.Contains(log.String(), `"task":"root/0","seq":1,"kind":"done"`) })
	if !logged {
		t.Fatalf("recording: the slot was released before the end was logged:\n%s", log.String())
	}
	b := root.Child(root.Ctx, NewCapSet())
	b.Effect("now", []byte(`[]`), func() Outcome { return Outcome{Res: []byte(`1`)} })
	b.Finished(nil)
	if err := l.Finish([]byte(`null`)); err != nil {
		t.Fatal(err)
	}
	read, err := ReadLog(bytes.NewReader(log.Bytes()))
	if err != nil {
		t.Fatal(err)
	}

	l, _ = NewReplayer(read, &bytes.Buffer{})
	root = NewRoot(l.Start(context.Background()), NewCapSet(), &bytes.Buffer{})
	root.SetLog(l)
	a = root.Child(root.Ctx, NewCapSet())
	pending := -1
	a.Finished(func() { pending = len(l.doneAts) })
	if pending != 2 || len(l.doneAts) != 1 {
		t.Fatalf("replay: %d ends pending as the slot was released, %d after; want 2, then 1", pending, len(l.doneAts))
	}
	b = root.Child(root.Ctx, NewCapSet())
	called := 0
	b.Finished(func() { called++ }) // before its effect: a divergence
	if called != 1 || l.failed() == nil {
		t.Fatalf("release ran %d times on a divergence (err %v), want once", called, l.failed())
	}
}
