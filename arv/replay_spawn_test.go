package arv

import (
	"bytes"
	"fmt"
	"testing"
	"time"
)

// A failure that cancels its siblings replays the same: the sibling still
// prints what it printed before it was cancelled, and the run ends with
// the same error, though the replay runs at another speed.
func TestReplayOfFailFast(t *testing.T) {
	src := `let boom = fn() { sleep(200); error("boom") };
let busy = fn() { puts("b1"); sleep(100); puts("b2"); sleep(60000); puts("never") };
scope { spawn boom(); spawn busy(); puts("body"); sleep(60000) }`
	recorded, log, err := recordRun(t, src, Options{})
	if err == nil || err.Error() != "boom" {
		t.Fatalf("recording: %v", err)
	}
	for i := 0; i < 20; i++ {
		replayed, err := replayRun(t, src, log)
		if err == nil || err.Error() != "boom" || replayed != recorded {
			t.Fatalf("replay %d: %v, printed %q, recorded %q", i, err, replayed, recorded)
		}
	}
}

// Restart attempts replay in order, without their backoffs: 1 s and 2 s,
// well past the bound, so a replay that waited them fails on any machine.
func TestReplayOfRestarts(t *testing.T) {
	srv, hits := countedServer(t)
	src := fmt.Sprintf(`let f = fn() { let r = http_get("%s/flaky"); if (r["status"] == 200) { r["body"] } else { error("down") } };
scope (supervise: "restart", retries: 3, backoff_ms: 1000, retry_on: ["user"]) { puts(await spawn f() with [net("127.0.0.1")]) }`, srv.URL)
	recorded, log, err := recordRun(t, src, Options{Caps: loopback})
	if err != nil || recorded != "body of /flaky\n" || hits.Load() != 3 {
		t.Fatalf("recording: %v, %q, %d requests", err, recorded, hits.Load())
	}
	for _, id := range []string{`"task":"root/0","seq":1`, `"task":"root/0#2","seq":1`, `"task":"root/0#3","seq":1`} {
		if !bytes.Contains(log, []byte(id)) {
			t.Errorf("log has no %s", id)
		}
	}
	start := time.Now()
	replayed, err := replayRun(t, src, log)
	if took := time.Since(start); took >= 900*time.Millisecond {
		t.Errorf("replay took %s: it waited out the backoffs", took)
	}
	if err != nil || replayed != recorded || hits.Load() != 3 {
		t.Fatalf("replay: %v, %q, %d requests", err, replayed, hits.Load())
	}
}
