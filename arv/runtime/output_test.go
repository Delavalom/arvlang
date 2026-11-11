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
