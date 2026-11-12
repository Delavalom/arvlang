package arv

import (
	"bytes"
	"context"
	"fmt"
	goruntime "runtime"
	"strings"
	"testing"
	"time"

	"github.com/delavalom/arvlang/arv/runtime"
)

// Leak rule for Phase 6 (beyond TestNoGoroutineOutlivesALoggedRun): no
// goroutine outlives Run when it records, replays, resumes cut after
// every line of the recording, or diverges. Each program leaves tasks
// blocked (sleeping, waiting for a slot, or retrying) when its scope
// ends early.
func TestNoGoroutineOutlivesAnyResumeCut(t *testing.T) {
	srv, _ := countedServer(t)
	const nap = "let nap = fn() { sleep(600000) };\nlet get = fn(p) { http_get(\"%[1]s\" + p)[\"body\"] };\n"
	progs := map[string]string{
		"timeout with sleepers": nap + `let a = spawn get("/a") with [net("127.0.0.1")];
puts(match scope (timeout_ms: 40) { spawn nap(); spawn nap(); sleep(600000) } { ok(v) => v, err(e) => e["message"] });
puts(await a)`,
		"fail_fast with sleepers": nap + `let bad = fn() { get("/b"); error("bad") };
puts(match scope { spawn nap(); spawn nap(); await spawn bad() with [net("127.0.0.1")] } { ok(v) => v, err(e) => e["message"] })`,
		"restart with a sleeper": nap + `let f = fn() { let r = http_get("%[1]s/flaky"); if (r["status"] == 200) { r["body"] } else { error("down") } };
puts(scope (supervise: "restart", retries: 3, backoff_ms: 1, retry_on: ["user"]) { spawn fn() { sleep(30) }(); await spawn f() with [net("127.0.0.1")] })`,
		"slot wait at a deadline": nap + `puts(match scope (timeout_ms: 40, tasks: 2) { spawn get("/c") with [net("127.0.0.1")]; for i in [1, 2, 3] { spawn nap() } } { ok(v) => v, err(e) => e["message"] })`,
		"collect and await":       nap + `puts(scope (supervise: "collect") { let x = spawn get("/d") with [net("127.0.0.1")]; let y = spawn fn() { error("y") }(); [await x, random(9), now() > 0] })`,
	}
	for name, tmpl := range progs {
		t.Run(name, func(t *testing.T) {
			src := fmt.Sprintf(tmpl, srv.URL)
			before := goruntime.NumGoroutine()
			out, log, err := recordRun(t, src, Options{Caps: loopback, Ctx: hostCtx(t)})
			noLeak(t, before, "record: "+src)
			replayed, rerr := replayRun(t, src, log)
			noLeak(t, before, "replay: "+src)
			if replayed != out || fmt.Sprint(rerr) != fmt.Sprint(err) {
				t.Fatalf("replay printed %q, %v; the recording %q, %v", replayed, rerr, out, err)
			}
			lines := bytes.SplitAfter(log, []byte("\n"))
			for k := 1; k < len(lines)-1; k++ {
				crashed := bytes.Join(lines[:k], nil)
				l, err := runtime.ReadLog(bytes.NewReader(crashed))
				if err != nil {
					t.Fatal(err)
				}
				rec := bytes.NewBuffer(bytes.Clone(crashed))
				var got bytes.Buffer
				start := time.Now()
				_, err = Run(src, Options{Caps: loopback, Resume: l, Record: rec, Out: &got, Ctx: hostCtx(t)})
				noLeak(t, before, fmt.Sprintf("resume after %d lines: %s", k, src))
				if took := time.Since(start); took > time.Minute {
					t.Fatalf("resume after %d lines took %s: %v", k, took, err)
				}
				if err != nil && (strings.Contains(err.Error(), "panic") || strings.Contains(err.Error(), "diverged")) {
					t.Fatalf("resume after %d lines: %v", k, err)
				}
			}
			// An edited script diverges at its first effect.
			edited := strings.Replace(src, "puts(", "puts(\"edited\"); puts(", 1)
			if err := replayForged(t, edited, log, Options{}); err == nil || !strings.Contains(err.Error(), "diverged") {
				t.Fatalf("a replay of an edited script: %v", err)
			}
			noLeak(t, before, "divergence: "+edited)
		})
	}
}

// A host that cancels a resume while its live part is blocked: Run ends,
// and every task with it.
func TestNoGoroutineOutlivesACancelledResume(t *testing.T) {
	srv, _ := countedServer(t)
	src := fmt.Sprintf(`let nap = fn() { sleep(600000) };
puts(http_get("%s/a")["body"]);
let hs = [spawn nap(), spawn nap()];
await hs`, srv.URL)
	before := goruntime.NumGoroutine()
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	_, log, err := recordRun(t, src, Options{Caps: loopback, Ctx: ctx})
	cancel()
	if err == nil {
		t.Fatal("the recording was not cancelled")
	}
	noLeak(t, before, "record: "+src)
	l, err := runtime.ReadLog(bytes.NewReader(log))
	if err != nil || l.End != nil {
		t.Fatalf("the cancelled recording: %v, end %v", err, l.End)
	}
	ctx, cancel = context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	_, err = Run(src, Options{Caps: loopback, Resume: l, Record: bytes.NewBuffer(bytes.Clone(log[:l.Size])), Ctx: ctx})
	if err == nil {
		t.Fatal("the resume was not cancelled")
	}
	noLeak(t, before, "resume: "+src)
}

// hostCtx bounds a run so that a hang fails the test instead of the suite.
func hostCtx(t *testing.T) context.Context {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	return ctx
}
