package arv

import (
	"bytes"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/delavalom/arvlang/arv/runtime"
)

// A resumed run's deadlines count the time the crashed run had already
// spent: the run's --timeout and a scope's timeout_ms. Replayed effects
// take no time, so a fresh budget would give the live part the crashed
// run's time again, and a run that times out would succeed after a crash
// (review round 1).
func TestResumeKeepsTheDeadlines(t *testing.T) {
	for name, tt := range map[string]struct {
		src    string
		limits runtime.Limits
	}{
		"run":   {`sleep(1500); puts("first"); sleep(1500); puts("second")`, runtime.Limits{Timeout: 2 * time.Second}},
		"scope": {`scope (timeout_ms: 2000) { sleep(1500); puts("first"); sleep(1500); puts("second") }`, runtime.Limits{}},
	} {
		recorded, log, rerr := recordRun(t, tt.src, Options{Limits: tt.limits})
		if rerr == nil || !strings.Contains(rerr.Error(), "timeout (limit 2000 ms)") || recorded != "first\n" {
			t.Fatalf("%s: recording: %v, %q", name, rerr, recorded)
		}
		crashed := cutAfter(t, log, `"req":["first"]`)
		start := time.Now()
		printed, resumed, err := resumeRun(t, tt.src, crashed, nil)
		if fmt.Sprint(err) != fmt.Sprint(rerr) || printed != recorded {
			t.Fatalf("%s: resume: %v, printed %q; want %v, %q", name, err, printed, rerr, recorded)
		}
		if took := time.Since(start); took >= 1500*time.Millisecond {
			t.Errorf("%s: the resumed run took %s: its deadline started again", name, took)
		}
		if replayed, err := replayRun(t, tt.src, resumed); fmt.Sprint(err) != fmt.Sprint(rerr) || replayed != recorded {
			t.Fatalf("%s: replay of the resumed log: %v, %q", name, err, replayed)
		}
	}
}

// clock_ms() keeps counting across a resume: a live reading follows the
// crashed process's readings, so a latency that spans the resume is not
// negative (review round 1). The crashed process is simulated by a log
// whose clock_ms reading is a day later than this process's clock.
func TestClockMSIsMonotonicAcrossAResume(t *testing.T) {
	src := `let t0 = clock_ms(); puts("a"); sleep(300); puts(clock_ms() - t0)`
	_, log, err := recordRun(t, src, Options{})
	if err != nil {
		t.Fatal(err)
	}
	reading := regexp.MustCompile(`"res":(\d+)`)
	log = forge(t, log, func(_ int, body []byte) []byte {
		if !bytes.Contains(body, []byte(`"kind":"clock_ms"`)) {
			return body
		}
		return reading.ReplaceAllFunc(body, func(m []byte) []byte {
			n, _ := strconv.ParseInt(string(m[len(`"res":`):]), 10, 64)
			return []byte(fmt.Sprintf(`"res":%d`, n+86_400_000))
		})
	})
	crashed := cutAfter(t, log, `"req":["a"]`)
	printed, _, err := resumeRun(t, src, crashed, nil)
	lines := strings.Split(printed, "\n")
	if err != nil || len(lines) != 3 {
		t.Fatalf("resume: %v, %q", err, printed)
	}
	if d, err := strconv.Atoi(lines[1]); err != nil || d < 300 {
		t.Fatalf("clock_ms() - t0 across the resume = %q, want at least 300", lines[1])
	}
}

// A late cut: the crashed run had slept 300 ms before the log was cut, so
// the resumed run replays the sleep in no time and its first live
// clock_ms() must still count that time. The resumed run's start is
// back-dated by the log's largest At; started afresh, the live reading
// would land about 300 ms early, so the latency would come out near zero
// and the result would differ from the uninterrupted run's (mutation M10).
func TestClockMSCountsTheCrashedRunsTimeAfterALateCut(t *testing.T) {
	src := `let t0 = clock_ms(); puts("a"); sleep(300); puts("b"); puts(clock_ms() - t0 > 299)`
	whole, log, err := recordRun(t, src, Options{})
	if err != nil || whole != "a\nb\ntrue\n" {
		t.Fatalf("uninterrupted run: %v, %q", err, whole)
	}
	crashed := cutAfter(t, log, `"req":["b"]`)
	printed, resumed, err := resumeRun(t, src, crashed, nil)
	if err != nil || printed != whole {
		t.Fatalf("resume: %v, printed %q, want %q", err, printed, whole)
	}
	if replayed, err := replayRun(t, src, resumed); err != nil || replayed != whole {
		t.Fatalf("replay of the resumed log: %v, %q", err, replayed)
	}
}
