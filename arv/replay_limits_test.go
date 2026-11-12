package arv

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/delavalom/arvlang/arv/runtime"
)

// A replay runs under the limits in the log's header (p6.9): a recorded
// run that hit a limit replays to the same error, though the replay is
// given none. Without the header's limits the replay would run past it,
// and fail at the end line as a divergence instead.
func TestReplayUsesTheHeaderLimits(t *testing.T) {
	for name, tt := range map[string]struct {
		src    string
		limits runtime.Limits
		want   string
	}{
		"steps":   {`let a = [1, 2, 3, 4, 5, 6, 7, 8, 9, 10]; puts("start"); for x in a { for y in a { for z in a { 1 } } }; puts("end")`, runtime.Limits{Steps: 2000}, "steps"},
		"alloc":   {`puts("start"); let dbl = fn(s, n) { if (n == 0) { s } else { dbl(s + s, n - 1) } }; puts(len(dbl("0123456789abcdef", 16)))`, runtime.Limits{AllocBytes: 1 << 20}, "alloc"},
		"timeout": {`puts("start"); sleep(60000)`, runtime.Limits{Timeout: 300 * time.Millisecond}, "timeout"},
	} {
		recorded, log, rerr := recordRun(t, tt.src, Options{Limits: tt.limits})
		if rerr == nil || !strings.Contains(rerr.Error(), tt.want) || recorded != "start\n" {
			t.Fatalf("%s: recording: %v, %q", name, rerr, recorded)
		}
		l, err := runtime.ReadLog(bytes.NewReader(log))
		if err != nil {
			t.Fatal(err)
		}
		// A replay that waited for a deadline it does not have would
		// wait for the sleep's 60 s; the host cancels it sooner.
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		var out bytes.Buffer
		_, err = Run(tt.src, Options{Ctx: ctx, Replay: l, Out: &out})
		cancel()
		if fmt.Sprint(err) != fmt.Sprint(rerr) || out.String() != recorded {
			t.Fatalf("%s: replay: %v, %q; recorded %v", name, err, out.String(), rerr)
		}
	}
}

// An effect's logged bytes are charged again on replay (p6.8), so a
// replay under alloc_mb fails where the recording did: here the 900 KB
// read leaves too little for the 900 KB concatenation, which alone fits.
func TestReplayChargesTheLoggedBytes(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "big.txt")
	if err := os.WriteFile(file, bytes.Repeat([]byte("x"), 900<<10), 0o644); err != nil {
		t.Fatal(err)
	}
	src := fmt.Sprintf(`scope (alloc_mb: 1) { let s = read_file(%q); puts("read"); len(s + "!") }`, file)
	recorded, log, rerr := recordRun(t, src, Options{Caps: []runtime.Capability{{Kind: runtime.FSRead, Scope: dir}}})
	if rerr == nil || !strings.Contains(rerr.Error(), "alloc") || recorded != "read\n" {
		t.Fatalf("recording: %v, %q", rerr, recorded)
	}
	if !bytes.Contains(log, []byte(`"charged":9`)) {
		t.Fatalf("the read charged nothing:\n%.600s", log)
	}
	replayed, err := replayRun(t, src, log)
	if fmt.Sprint(err) != fmt.Sprint(rerr) || replayed != recorded {
		t.Fatalf("replay: %v, %q; recorded %v", err, replayed, rerr)
	}
}

// slowOut is output that takes d to write each line, and ignores the
// run's context: what it prints finishes after a deadline unnoticed.
type slowOut struct {
	d   time.Duration
	buf bytes.Buffer
}

func (s *slowOut) Write(p []byte) (int, error) {
	if bytes.HasSuffix(p, []byte("\n")) {
		time.Sleep(s.d)
	}
	return s.buf.Write(p)
}

// A scope's deadline at close replays as recorded, the run's --timeout
// and a scope's timeout_ms alike (p6.10): the recorded puts outlived the
// deadline, the replayed one is instant, and the replay still times out.
func TestReplayOfAScopeThatTimedOutAtClose(t *testing.T) {
	for name, tt := range map[string]struct {
		src    string
		limits runtime.Limits
	}{
		"run":   {`puts("slow")`, runtime.Limits{Timeout: time.Second}},
		"scope": {`scope (timeout_ms: 1000) { puts("slow") }`, runtime.Limits{}},
	} {
		out := &slowOut{d: 1500 * time.Millisecond}
		var log bytes.Buffer
		_, rerr := Run(tt.src, Options{Limits: tt.limits, Out: out, Record: &log})
		if rerr == nil || !strings.Contains(rerr.Error(), "timeout (limit 1000 ms)") || !bytes.Contains(log.Bytes(), []byte(`"kind":"expired"`)) {
			t.Fatalf("%s: recording: %v\n%s", name, rerr, log.Bytes())
		}
		replayed, err := replayRun(t, tt.src, log.Bytes())
		if fmt.Sprint(err) != fmt.Sprint(rerr) || replayed != "slow\n" {
			t.Fatalf("%s: replay: %v, %q; recorded %v", name, err, replayed, rerr)
		}
	}
}
