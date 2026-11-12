package arv

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/delavalom/arvlang/arv/runtime"
)

// crashAfter is a log writer that stops the run (as a crash would) once
// it has written n records whose kind starts with kind.
type crashAfter struct {
	mu     sync.Mutex
	buf    bytes.Buffer
	kind   string
	n      int
	cancel context.CancelFunc
}

func (c *crashAfter) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.buf.Write(p)
	if bytes.Contains(p, []byte(`"kind":"`+c.kind)) && !bytes.Contains(p, []byte(`"header"`)) {
		if c.n--; c.n == 0 {
			c.cancel()
		}
	}
	return len(p), nil
}

// A run cut off after 3 of its 5 requests resumes: the 3 are answered from
// the log, the server sees only the other 2, and the output is the
// uninterrupted run's. The resumed log then replays as a whole.
func TestResumeAfterACrash(t *testing.T) {
	srv, hits := counted(t)
	src := fmt.Sprintf(`for p in ["/1", "/2", "/3", "/4", "/5"] { puts(http_get("%s" + p)["body"]) }; "done"`, srv.URL)
	whole, _, err := recordRun(t, src, Options{Caps: loopback})
	if err != nil || hits.Load() != 5 {
		t.Fatalf("uninterrupted run: %v, %d requests", err, hits.Load())
	}
	hits.Store(0)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	crash := &crashAfter{kind: "http_get", n: 3, cancel: cancel}
	if _, err := Run(src, Options{Ctx: ctx, Caps: loopback, Record: crash, Out: &bytes.Buffer{}}); err == nil {
		t.Fatal("the crashed run succeeded")
	}
	if hits.Load() != 3 {
		t.Fatalf("crashed after %d requests, want 3", hits.Load())
	}
	l, err := runtime.ReadLog(bytes.NewReader(crash.buf.Bytes()))
	if err != nil || l.End != nil {
		t.Fatalf("crashed log: %v, end %v", err, l.End)
	}
	hits.Store(0)
	var out bytes.Buffer
	log := bytes.NewBuffer(crash.buf.Bytes()[:l.Size])
	if _, err := Run(src, Options{Caps: loopback, Resume: l, Record: log, Out: &out}); err != nil {
		t.Fatalf("resume: %v", err)
	}
	if hits.Load() != 2 || out.String() != whole {
		t.Fatalf("resume made %d requests, want 2; printed %q, want %q", hits.Load(), out.String(), whole)
	}
	if _, err := Run(src, Options{Resume: l, Record: &bytes.Buffer{}}); err == nil || !strings.Contains(err.Error(), "grants the run was recorded with") {
		t.Fatalf("resume without the grants: %v", err)
	}
	hits.Store(0)
	if replayed, err := replayRun(t, src, log.Bytes()); err != nil || replayed != whole || hits.Load() != 0 {
		t.Fatalf("replay of the resumed log: %v, %q, %d requests", err, replayed, hits.Load())
	}
}
