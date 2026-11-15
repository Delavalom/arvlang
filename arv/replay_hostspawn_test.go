package arv

import (
	"bytes"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/delavalom/arvlang/arv/runtime"
)

// A spawn that hands a child a host kind's grant (Phase 8.2) is an
// effect, and a replay takes the child's grants from the log
// (loggedCaps). It must check them against the run's host kinds and give
// the child those kinds, as a live spawn does: otherwise k8s("staging")
// is "not held" and the replay diverges, and a resume that replays the
// spawn gives a child that cannot call its host effect.
func TestReplayOfASpawnWithAHostGrant(t *testing.T) {
	var n atomic.Int64
	effects := map[string]Effect{"k8s_apply": counted(&n, applied)}
	caps := []runtime.Capability{staging, {Kind: runtime.Net, Scope: "127.0.0.1"}}
	srv, hits := countedServer(t)
	src := `let deploy = fn(m) { k8s_apply("staging", m) };
let t = spawn deploy("web.yaml") with [k8s("staging")];
puts(await t);
puts(http_get("` + srv.URL + `/after")["body"]);
let u = spawn deploy("api.yaml") with [k8s("staging")];
puts(await u)`
	out, log, err := recordRun(t, src, Options{Caps: caps, Effects: effects})
	if err != nil || n.Load() != 2 || !strings.Contains(out, "applied to staging\n") {
		t.Fatalf("recording: %v, %d calls, printed %q", err, n.Load(), out)
	}
	t.Run("replay", func(t *testing.T) {
		l, err := runtime.ReadLog(bytes.NewReader(log))
		if err != nil {
			t.Fatal(err)
		}
		var got bytes.Buffer
		_, err = Run(src, Options{Replay: l, Out: &got, Effects: effects})
		if err != nil || got.String() != out || n.Load() != 2 || hits.Load() != 1 {
			t.Fatalf("replay: %v, %d calls, %d requests, printed %q, want %q", err, n.Load(), hits.Load(), got.String(), out)
		}
	})
	t.Run("resume", func(t *testing.T) {
		// Crashed during the request: the first spawn and its call are
		// replayed, the second spawn and its call run live.
		crashed := cutAfter(t, log, `"kind":"host:k8s_apply"`)
		l, err := runtime.ReadLog(bytes.NewReader(crashed))
		if err != nil || l.End != nil {
			t.Fatalf("crashed log: %v", err)
		}
		var got bytes.Buffer
		rec := bytes.NewBuffer(bytes.Clone(crashed))
		_, err = Run(src, Options{Caps: caps, Resume: l, Record: rec, Out: &got, Effects: effects})
		if err != nil || got.String() != out || n.Load() != 3 || hits.Load() != 2 {
			t.Fatalf("resume: %v, %d calls, %d requests, printed %q, want %q", err, n.Load(), hits.Load(), got.String(), out)
		}
	})
	t.Run("forged wider grant", func(t *testing.T) {
		// I2 still holds: a log that gives the child k8s("prod"), which the
		// root does not hold, diverges at the spawn.
		forged := forge(t, log, func(i int, b []byte) []byte {
			return bytes.Replace(b, []byte(`["k8s","staging"]`), []byte(`["k8s","prod"]`), 1)
		})
		l, err := runtime.ReadLog(bytes.NewReader(forged))
		if err != nil {
			t.Fatal(err)
		}
		_, err = Run(src, Options{Replay: l, Out: &bytes.Buffer{}, Effects: effects})
		if err == nil || !strings.Contains(err.Error(), `include k8s("prod"), which the task does not hold`) {
			t.Fatalf("forged replay: %v", err)
		}
	})
}
