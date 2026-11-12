package arv

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/delavalom/arvlang/arv/runtime"
)

// counted is a test server that counts every request; /flaky fails the
// first two times it is asked.
func counted(t *testing.T) (*httptest.Server, *atomic.Int64) {
	var n, flaky atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n.Add(1)
		if r.URL.Path == "/flaky" && flaky.Add(1) <= 2 {
			w.WriteHeader(http.StatusInternalServerError)
		}
		fmt.Fprintf(w, "body of %s", r.URL.Path)
	}))
	t.Cleanup(srv.Close)
	return srv, &n
}

var loopback = []runtime.Capability{{Kind: runtime.Net, Scope: "127.0.0.1"}}

// recordRun runs src recording its log; it returns what it printed, the
// log and the run's error.
func recordRun(t *testing.T, src string, opts Options) (string, []byte, error) {
	t.Helper()
	var out, log bytes.Buffer
	opts.Out, opts.Record = &out, &log
	_, err := Run(src, opts)
	return out.String(), log.Bytes(), err
}

// replayRun replays log for src; it returns what it printed and the error.
func replayRun(t *testing.T, src string, log []byte) (string, error) {
	t.Helper()
	l, err := runtime.ReadLog(bytes.NewReader(log))
	if err != nil {
		t.Fatalf("ReadLog: %v", err)
	}
	var out bytes.Buffer
	_, err = Run(src, Options{Out: &out, Replay: l})
	return out.String(), err
}

const fanout = `let get = fn(p) { http_get("%[1]s" + p)["body"] };
let r = scope {
	let a = spawn get("/a") with [net("127.0.0.1")];
	let b = spawn get("/b") with [net("127.0.0.1")];
	sleep(%[2]d);
	puts(await a); puts(await b);
	puts(now()); puts(random(1000000)); puts(clock_ms() > -1);
	{"a": await a, "b": await b}
};
puts(r);
r`

// I7: a replay prints exactly what the recorded run printed and ends the
// same, without a single request, and without sleeping.
func TestReplayIsIdenticalAndOffline(t *testing.T) {
	srv, hits := counted(t)
	src := fmt.Sprintf(fanout, srv.URL, 2000)
	recorded, log, err := recordRun(t, src, Options{Caps: loopback})
	if err != nil || hits.Load() != 2 {
		t.Fatalf("recording: %v, %d requests", err, hits.Load())
	}
	start := time.Now()
	replayed, err := replayRun(t, src, log)
	if took := time.Since(start); took >= 2*time.Second {
		t.Errorf("replay took %s: it slept", took)
	}
	if err != nil || replayed != recorded || hits.Load() != 2 {
		t.Fatalf("replay: %v, %d requests\nprinted %q\nrecorded %q", err, hits.Load(), replayed, recorded)
	}
	if !strings.Contains(recorded, "{a: body of /a, b: body of /b}") {
		t.Fatalf("recorded %q", recorded)
	}
}

// --replay takes the grants from the header: spawn ... with works without
// any, and a replay given grants is refused.
func TestReplayUsesTheHeaderGrants(t *testing.T) {
	srv, _ := counted(t)
	src := fmt.Sprintf(fanout, srv.URL, 1)
	_, log, err := recordRun(t, src, Options{Caps: loopback})
	if err != nil {
		t.Fatal(err)
	}
	l, _ := runtime.ReadLog(bytes.NewReader(log))
	if g := l.Header.Grants; len(g) != 1 || g[0] != (runtime.LogGrant{Kind: runtime.Net, Scope: "127.0.0.1"}) {
		t.Fatalf("header grants = %+v", g)
	}
	if _, err := Run(src, Options{Replay: l, Caps: loopback}); err == nil || !strings.Contains(err.Error(), "takes its grants from the log") {
		t.Fatalf("replay with grants: %v", err)
	}
	if _, err := Run(src, Options{Replay: l, Limits: runtime.Limits{Steps: 5}}); err == nil || !strings.Contains(err.Error(), "limits from the log") {
		t.Fatalf("replay with limits: %v", err)
	}
}

// A changed script diverges, naming the task, the effect and both requests.
func TestReplayOfAnEditedScriptDiverges(t *testing.T) {
	srv, hits := counted(t)
	src := fmt.Sprintf(fanout, srv.URL, 1)
	_, log, err := recordRun(t, src, Options{Caps: loopback})
	if err != nil {
		t.Fatal(err)
	}
	_, err = replayRun(t, strings.Replace(src, `get("/a")`, `get("/zen")`, 1), log)
	var ae *Error
	want := fmt.Sprintf(`replay diverged at task root/0, effect 1: the log has http_get("%[1]s/a"), the program asked for http_get("%[1]s/zen")`, srv.URL)
	if err == nil || !strings.Contains(err.Error(), want) || !errorsAs(err, &ae) || ae.Kind != runtime.KindReplay {
		t.Fatalf("err = %v\nwant %s", err, want)
	}
	if hits.Load() != 2 {
		t.Fatalf("%d requests: the replay went live", hits.Load())
	}
}

func errorsAs(err error, ae **Error) bool {
	e, ok := err.(*Error)
	*ae = e
	return ok
}
