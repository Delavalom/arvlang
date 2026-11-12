package arv

import (
	"bytes"
	"fmt"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/delavalom/arvlang/arv/runtime"
)

// gated is a test server where /wait answers only once /arrive has been
// asked (and /flaky fails the first time), so a recording orders two tasks without relying on timing.
func gated(t *testing.T) *httptest.Server {
	arrived := make(chan struct{})
	var once sync.Once
	var flaky atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/flaky":
			if flaky.Add(1) == 1 {
				w.WriteHeader(http.StatusInternalServerError)
			}
		case "/arrive":
			once.Do(func() { close(arrived) })
		case "/wait":
			select {
			case <-arrived:
			case <-r.Context().Done():
			}
		}
		fmt.Fprintf(w, "ok %s", r.URL.Path)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// cutAfter is log cut just after its first line that contains marker: a
// crash (SIGKILL) right after that line was written.
func cutAfter(t *testing.T, log []byte, marker string) []byte {
	t.Helper()
	i := bytes.Index(log, []byte(marker))
	if i < 0 {
		t.Fatalf("the log has no %s:\n%s", marker, log)
	}
	return log[:i+bytes.IndexByte(log[i:], '\n')+1]
}

// resumeRun resumes the crashed log for src; it returns what it printed,
// the whole resumed log and the error.
func resumeRun(t *testing.T, src string, crashed []byte, caps []runtime.Capability) (string, []byte, error) {
	t.Helper()
	l, err := runtime.ReadLog(bytes.NewReader(crashed))
	if err != nil || l.End != nil {
		t.Fatalf("crashed log: %v, end %v", err, l.End)
	}
	var out bytes.Buffer
	log := bytes.NewBuffer(append([]byte(nil), crashed[:l.Size]...))
	_, err = Run(src, Options{Caps: caps, Resume: l, Record: log, Out: &out})
	return out.String(), log.Bytes(), err
}

// A task that has used up its records goes live, but its checks must not
// see what this run's replay did early: here b's failure, which the
// recorded run had not reached when p spawned c. Seeing it, p would not
// spawn c, and c's logged puts would never be used. Until every record
// has been used, a live task's checks see nothing (Phase 6, review round 1).
func TestResumedChecksWaitForTheLog(t *testing.T) {
	srv := gated(t)
	// c needs a grant to reach the server, so p's spawn is an effect, and
	// p's last record: the Acquire after it is p's first live check.
	src := fmt.Sprintf(`let a = [1, 2, 3, 4, 5, 6, 7, 8, 9, 10];
let work = fn() { for x in a { for y in a { for z in a { for w in a { 1 } } } }; 0 };
let c = fn() { puts("child ran"); http_get("%[1]s/arrive"); "c" };
let p = fn() { work(); let h = spawn c() with [net("127.0.0.1")]; sleep(60000); await h };
let b = fn() { http_get("%[1]s/wait"); error("boom") };
scope { spawn p() with [net("127.0.0.1")]; spawn b() with [net("127.0.0.1")] }`, srv.URL)
	recorded, log, err := recordRun(t, src, Options{Caps: loopback})
	if err == nil || err.Error() != "boom" || recorded != "child ran\n" {
		t.Fatalf("recording: %v, %q", err, recorded)
	}
	crashed := cutAfter(t, log, `/wait"]`)
	for i := 0; i < 5; i++ {
		printed, resumed, err := resumeRun(t, src, crashed, loopback)
		if err == nil || err.Error() != "boom" || printed != recorded {
			t.Fatalf("resume %d: %v; printed %q, want %q", i, err, printed, recorded)
		}
		if replayed, err := replayRun(t, src, resumed); err == nil || err.Error() != "boom" || replayed != recorded {
			t.Fatalf("replay of resume %d: %v, %q", i, err, replayed)
		}
	}
}

// The same with a value: p's result reaches the parent, whose logged puts
// needs it, while b's replayed failure has already cancelled p's scope.
func TestResumedChecksKeepALoggedResult(t *testing.T) {
	srv := gated(t)
	src := fmt.Sprintf(`let a = [1, 2, 3, 4, 5, 6, 7, 8, 9, 10];
let p = fn() { for x in a { for y in a { for z in a { for w in a { 1 } } } }; "p done" };
let b = fn() { http_get("%[1]s/wait"); error("boom") };
scope { let h = spawn p(); spawn b() with [net("127.0.0.1")]; puts(await h); http_get("%[1]s/arrive"); sleep(60000) }`, srv.URL)
	recorded, log, err := recordRun(t, src, Options{Caps: loopback})
	if err == nil || err.Error() != "boom" || recorded != "p done\n" {
		t.Fatalf("recording: %v, %q", err, recorded)
	}
	crashed := cutAfter(t, log, `/wait"]`)
	for i := 0; i < 5; i++ {
		printed, _, err := resumeRun(t, src, crashed, loopback)
		if err == nil || err.Error() != "boom" || printed != recorded {
			t.Fatalf("resume %d: %v; printed %q, want %q", i, err, printed, recorded)
		}
	}
}

// A restart's backoff, quiet, does not wait when the next attempt has
// records: the recorded wait ended. A live wait here would be cut short
// by b's failure, replayed at once, and attempt 2 would never run.
func TestResumedBackoffWithALoggedNextAttempt(t *testing.T) {
	srv := gated(t)
	src := fmt.Sprintf(`let f = fn() { let r = http_get("%[1]s/flaky"); if (r["status"] == 200) { http_get("%[1]s/arrive"); r["body"] } else { error("down") } };
let b = fn() { http_get("%[1]s/wait"); 1 + true };
scope (supervise: "restart", retries: 3, backoff_ms: 300, retry_on: ["user"]) { spawn f() with [net("127.0.0.1")]; spawn b() with [net("127.0.0.1")] }`, srv.URL)
	_, log, rerr := recordRun(t, src, Options{Caps: loopback})
	if rerr == nil || !bytes.Contains(log, []byte(`"task":"root/0#2"`)) {
		t.Fatalf("recording: %v\n%s", rerr, log)
	}
	crashed := cutAfter(t, log, `/wait"]`)
	for i := 0; i < 3; i++ {
		if _, _, err := resumeRun(t, src, crashed, loopback); fmt.Sprint(err) != fmt.Sprint(rerr) {
			t.Fatalf("resume %d: %v, recorded %v", i, err, rerr)
		}
	}
}

// Differential property over programs that fail: the recorded log cut at
// a line boundary (what SIGKILL leaves after a write) resumes without
// diverging, ends with the recorded run's error, and leaves a log that
// replays identically. Seeds 7051 (cut 7) and 7137 (cut 20) diverged
// before quiet checks (review round 1; 7018 is the reviewer's seed); a
// cancelled task printed after the cancellation it had already seen
// before saw. Those three are cut at every line, the others at about 20.
func TestResumeAtEveryCutProperty(t *testing.T) {
	srv, _ := counted(t)
	seeds := []int64{7018, 7051, 7137}
	n := 20
	if testing.Short() {
		n = 3
	}
	for i := int64(0); i < int64(n); i++ {
		seeds = append(seeds, 7200+i)
	}
	for k, seed := range seeds {
		g := &progGen{r: rand.New(rand.NewSource(seed)), url: srv.URL, failure: true}
		src := g.program()
		_, full, rerr := recordRun(t, src, Options{Caps: loopback})
		lines := bytes.SplitAfter(full, []byte("\n"))
		lines = lines[:len(lines)-2] // the end line, and "" after the last newline
		step := 1
		if k >= 3 {
			step = max(1, len(lines)/20)
		}
		for cut := 1; cut <= len(lines); cut += step {
			crashed := bytes.Join(lines[:cut], nil)
			printed, resumed, err := resumeRun(t, src, crashed, loopback)
			var ae *Error
			if fmt.Sprint(err) != fmt.Sprint(rerr) || errorsAs(err, &ae) && ae.Kind == runtime.KindReplay {
				t.Fatalf("seed %d, cut %d/%d: resume %v, recorded %v\nprogram:\n%s", seed, cut, len(lines), err, rerr, src)
			}
			if replayed, err := replayRun(t, src, resumed); fmt.Sprint(err) != fmt.Sprint(rerr) || replayed != printed {
				t.Fatalf("seed %d, cut %d: replay of the resumed log: %v\n%s\nwant\n%s", seed, cut, err, replayed, printed)
			}
		}
	}
}
