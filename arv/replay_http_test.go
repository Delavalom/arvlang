package arv

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/delavalom/arvlang/arv/runtime"
)

// echoHTTP answers with the method, X-A and the body it got.
func echoHTTP(t *testing.T) (*httptest.Server, *atomic.Int64) {
	var n atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n.Add(1)
		b, _ := io.ReadAll(r.Body)
		fmt.Fprintf(w, "%s %s %s", r.Method, r.Header.Get("X-A"), b)
	}))
	t.Cleanup(srv.Close)
	return srv, &n
}

const httpPost = `let r = http({"url": "%s/p", "method": "POST", "headers": {"X-A": "%s", "Authorization": "Bearer tok"}, "body": "%s"});
puts(r["status"]); puts(r["body"]);
puts(parse_json(json({"k": r["body"]}))["k"]);`

// I7: http goes through the gate. The log holds the whole request,
// method, headers and body; a replay answers it from the log with no
// request and the same output.
func TestReplayOfHTTPIsOffline(t *testing.T) {
	srv, hits := echoHTTP(t)
	src := fmt.Sprintf(httpPost, srv.URL, "1", "hello")
	out, log, err := recordRun(t, src, Options{Caps: loopback})
	if err != nil || hits.Load() != 1 || out != "200\nPOST 1 hello\nPOST 1 hello\n" {
		t.Fatalf("record: %v, %d requests, %q", err, hits.Load(), out)
	}
	for _, want := range []string{`"POST"`, `"X-A"`, `"Bearer tok"`, `"hello"`, `"http"`} {
		if !strings.Contains(string(log), want) {
			t.Errorf("the log lacks %s", want)
		}
	}
	replayed, err := replayRun(t, src, log)
	if err != nil || replayed != out || hits.Load() != 1 {
		t.Fatalf("replay: %v, %d requests, %q", err, hits.Load(), replayed)
	}
}

// I7: a replay whose program sends another request diverges, naming both:
// a changed header, body or method is another request.
func TestReplayOfAChangedHTTPRequestDiverges(t *testing.T) {
	srv, hits := echoHTTP(t)
	src := fmt.Sprintf(httpPost, srv.URL, "1", "hello")
	_, log, err := recordRun(t, src, Options{Caps: loopback})
	if err != nil {
		t.Fatal(err)
	}
	for _, edit := range []struct{ from, to, has, asked string }{
		{`"X-A": "1"`, `"X-A": "2"`, `["X-A","1"]`, `["X-A","2"]`},
		{`"body": "hello"`, `"body": "bye"`, `["body","hello"]`, `["body","bye"]`},
		{`"method": "POST"`, `"method": "PUT"`, `["method","POST"]`, `["method","PUT"]`},
	} {
		changed := strings.Replace(src, edit.from, edit.to, 1)
		_, err := replayRun(t, changed, log)
		var ae *Error
		if err == nil || !errorsAs(err, &ae) || ae.Kind != runtime.KindReplay {
			t.Fatalf("%s: err = %v", edit.to, err)
		}
		msg := err.Error()
		_, has, _ := strings.Cut(msg, "the log has http(")
		has, asked, ok := strings.Cut(has, "the program asked for http(")
		if !ok || !strings.Contains(has, edit.has) || !strings.Contains(asked, edit.asked) {
			t.Fatalf("%s: %s", edit.to, msg)
		}
	}
	// http_get(url) is another effect than http({"url": url}).
	get := fmt.Sprintf(`puts(http_get("%s/p")["body"])`, srv.URL)
	_, log, err = recordRun(t, get, Options{Caps: loopback})
	if err != nil {
		t.Fatal(err)
	}
	_, err = replayRun(t, fmt.Sprintf(`puts(http({"url": "%s/p"})["body"])`, srv.URL), log)
	if err == nil || !strings.Contains(err.Error(), "the log has http_get(") {
		t.Fatalf("err = %v", err)
	}
	if hits.Load() != 2 {
		t.Fatalf("%d requests: a replay went live", hits.Load())
	}
}
