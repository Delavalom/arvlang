package arv

import (
	"bytes"
	"strings"
	"sync"
	"testing"
	"time"
)

// signalOut is a run's output that closes printed once a line holding
// want is printed.
type signalOut struct {
	mu      sync.Mutex
	buf     bytes.Buffer
	want    string
	printed chan struct{}
	once    sync.Once
}

func (o *signalOut) Write(p []byte) (int, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if strings.Contains(string(p), o.want) {
		o.once.Do(func() { close(o.printed) })
	}
	return o.buf.Write(p)
}

// endGate is a log that, as it is handed the end of the task named task,
// waits until printed is closed or 2 s pass, and notes which came first.
type endGate struct {
	buf     bytes.Buffer
	task    string
	printed chan struct{}
	early   bool // printed was closed while the end waited to be logged
}

func (g *endGate) Write(p []byte) (int, error) {
	if bytes.Contains(p, []byte(`"task":"`+g.task+`",`)) && bytes.Contains(p, []byte(`"kind":"done"`)) {
		select {
		case <-g.printed:
			g.early = true
		case <-time.After(2 * time.Second):
		}
	}
	return g.buf.Write(p)
}

// Review round 6 (Critical): the recorder logged a task's end after it
// freed its slot (tasks:), so the task waiting for that slot could start
// and log records before the end was logged. A crash between the two
// left an honest log in which the resumed slot holder, with no end, ran
// live, while the spawn whose child has records waited for its slot; if
// the holder failed live, its failure waited for the log to drain, which
// needed that child: the resume hung. A task's end is now logged before
// its slot is freed, so a log with records of the next task in the slot
// has the holder's end, and the resume replays the holder to it.
func TestTaskEndIsLoggedBeforeItsSlotIsFreed(t *testing.T) {
	src := `let r = scope (tasks: 1) {
	let a = spawn fn() { 1 }();
	let b = spawn fn() { puts("y"); 2 }();
	[await a, await b]
};
puts(r)`
	printed := make(chan struct{})
	out := &signalOut{want: "y", printed: printed}
	log := &endGate{task: "root/0", printed: printed}
	if _, err := Run(src, Options{Out: out, Record: log}); err != nil {
		t.Fatal(err)
	}
	if log.early {
		t.Fatal("root/1 ran in the slot while root/0's end waited to be logged")
	}
	lines := strings.Split(strings.TrimSpace(log.buf.String()), "\n")
	end, first := -1, -1
	for i, l := range lines {
		if strings.Contains(l, `"task":"root/0",`) && strings.Contains(l, `"kind":"done"`) {
			end = i
		}
		if first < 0 && strings.Contains(l, `"task":"root/1",`) {
			first = i
		}
	}
	if end < 0 || first < 0 || end > first {
		t.Fatalf("root/0's end at line %d, root/1's first record at line %d, want the end first:\n%s", end, first, log.buf.String())
	}
}
