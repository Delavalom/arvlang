package arv

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"testing"
	"time"

	"github.com/delavalom/arvlang/arv/runtime"
)

// I7: replayed against a log that has none of them, every effect stops
// the run with a divergence and none happens: no request, no file, no
// output, no wait. Each would have been allowed by the header's grants.
func TestReplayRunsNoEffectLive(t *testing.T) {
	srv, hits := counted(t)
	dir := t.TempDir()
	in, out := filepath.Join(dir, "in.txt"), filepath.Join(dir, "out.txt")
	if err := os.WriteFile(in, []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ARV_P6_VAR", "secret")
	caps := []runtime.Capability{loopback[0], {Kind: runtime.FSRead, Scope: dir}, {Kind: runtime.FSWrite, Scope: dir}, {Kind: runtime.Env, Scope: "ARV_P6_VAR"}}
	_, log, err := recordRun(t, "1", Options{Caps: caps})
	if err != nil {
		t.Fatal(err)
	}
	for _, call := range []string{
		fmt.Sprintf("http_get(%q)", srv.URL+"/x"), fmt.Sprintf("read_file(%q)", in), fmt.Sprintf("write_file(%q, %q)", out, "x"),
		`getenv("ARV_P6_VAR")`, "sleep(60000)", "clock_ms()", "now()", "random(5)", `puts("x")`,
		fmt.Sprintf(`let f = fn() { 1 }; spawn f() with [fs_read(%q)]`, dir),
	} {
		start := time.Now()
		printed, err := replayRun(t, call+"; 1", log)
		name := call[:strings.IndexByte(call, '(')]
		if strings.HasPrefix(call, "let f") {
			name = "spawn"
		}
		if err == nil || !strings.Contains(err.Error(), "the program asked for "+name+"(") {
			t.Errorf("%s: err = %v", call, err)
		}
		if printed != "" || time.Since(start) > 30*time.Second {
			t.Errorf("%s: printed %q, took %s", call, printed, time.Since(start))
		}
	}
	if _, err := os.Stat(out); hits.Load() != 0 || !os.IsNotExist(err) {
		t.Fatalf("%d requests; %s: %v", hits.Load(), out, err)
	}
}

// Leak rule: no goroutine outlives a recorded, replayed, resumed or
// diverging run, including tasks a replay holds until they are cancelled.
func TestNoGoroutineOutlivesALoggedRun(t *testing.T) {
	srv, _ := counted(t)
	src := fmt.Sprintf(fanout, srv.URL, 1)
	failFast := `let boom = fn() { sleep(50); error("boom") };
let busy = fn() { puts("b1"); sleep(60000) };
scope { spawn boom(); spawn busy(); sleep(60000) }`
	recordRun(t, src, Options{Caps: loopback}) // warm up net/http
	before := settle(0)
	_, log, _ := recordRun(t, src, Options{Caps: loopback})
	replayRun(t, src, log)
	replayRun(t, strings.Replace(src, `get("/b")`, `get("/c")`, 1), log)
	_, ffLog, _ := recordRun(t, failFast, Options{})
	replayRun(t, failFast, ffLog)
	replayRun(t, strings.Replace(failFast, `puts("b1")`, `puts("b2")`, 1), ffLog)
	l, _ := runtime.ReadLog(bytes.NewReader(log[:bytes.LastIndexByte(log[:len(log)-1], '\n')+1]))
	Run(src, Options{Caps: loopback, Resume: l, Record: &bytes.Buffer{}, Out: &bytes.Buffer{}})
	if after := settle(before); after > before {
		buf := make([]byte, 1<<20)
		t.Fatalf("%d goroutines before, %d after:\n%s", before, after, buf[:goruntime.Stack(buf, true)])
	}
}
