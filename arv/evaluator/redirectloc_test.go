package evaluator

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/delavalom/arvlang/arv/runtime"
	"github.com/delavalom/arvlang/arv/value"
)

// locationServer answers every request with a 302 to loc.
func locationServer(t *testing.T, loc string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", loc)
		w.WriteHeader(http.StatusFound)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// A Location net/http cannot parse never reaches CheckRedirect: client.Do
// failed with "failed to parse Location header %q: parse %q: ...", which
// quotes the whole Location twice. With a 60,004-byte Location of 0xff
// bytes ending in "%zz", http_get returned a 480,131-byte error, and each
// call allocated that much, uncharged. A transport wrapper now refuses a
// redirect whose Location is over maxURLLen before net/http parses it, and
// the error's text is clipped.
func TestHTTPGetUnparseableLongLocationIsRefused(t *testing.T) {
	srv := locationServer(t, "/"+strings.Repeat("\xff", 60000)+"%zz")
	src := `http_get("` + srv.URL + `/start")`
	caps := grant(t, runtime.Net, "127.0.0.1")
	var got value.Object
	used := heapDuring(func() {
		for i := 0; i < 20; i++ {
			got, _ = runWith(t, src, caps)
		}
	})
	e, ok := got.(*value.Error)
	if !ok || !strings.Contains(e.Message, "Location") || e.Pos != "1:1" {
		t.Fatalf("got %.300s, want a refused redirect at 1:1", got.Inspect())
	}
	if n := len(e.Inspect()); n > 2048 {
		t.Errorf("the error is %d bytes long: %.300s", n, e.Message)
	}
	// Reading the 60 KB header block itself costs a few copies of it.
	if used > 20<<20 {
		t.Errorf("20 calls allocated %d MiB, want under 20 MiB", used>>20)
	}
}

// Any net/http error text is clipped, not only its URL: a malformed header
// line failed with "malformed MIME header line: <the whole line>", a
// 60,141-byte error.
func TestHTTPGetMalformedHeaderErrorIsClipped(t *testing.T) {
	srv := locationServer(t, "/"+strings.Repeat("a", 60000)+"\x7f")
	got, _ := runWith(t, `http_get("`+srv.URL+`/start")`, grant(t, runtime.Net, "127.0.0.1"))
	e, ok := got.(*value.Error)
	if !ok || e.Pos != "1:1" {
		t.Fatalf("got %.300s, want an error at 1:1", got.Inspect())
	}
	if n := len(e.Inspect()); n > 2048 {
		t.Errorf("the error is %d bytes long: %.300s", n, e.Message)
	}
}

// Only the statuses net/http follows (301, 302, 303, 307, 308) have their
// Location parsed. Any other 3xx comes back from client.Do as it is, so a
// long Location on it must not fail http_get (review round 9): a 300 with
// a 9001-byte Location failed with "redirect Location is 9001 bytes long".
func TestHTTPGetLongLocationOnUnfollowed3xx(t *testing.T) {
	for _, code := range []int{300, 304, 305, 399} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Location", "/"+strings.Repeat("a", 9000))
			w.WriteHeader(code)
			w.Write([]byte("hi"))
		}))
		got, _ := runWith(t, `let r = http_get("`+srv.URL+`/"); r["status"]`, grant(t, runtime.Net, "127.0.0.1"))
		srv.Close()
		if n, ok := got.(*value.Integer); !ok || n.Value != int64(code) {
			t.Errorf("status %d: got %.300s, want the response", code, got.Inspect())
		}
	}
	// A followed one is still refused.
	for _, code := range []int{301, 302, 303, 307, 308} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Location", "/"+strings.Repeat("a", 9000))
			w.WriteHeader(code)
		}))
		got, _ := runWith(t, `http_get("`+srv.URL+`/")`, grant(t, runtime.Net, "127.0.0.1"))
		srv.Close()
		if e, ok := got.(*value.Error); !ok || !strings.Contains(e.Message, "redirect Location is 9001 bytes long") {
			t.Errorf("status %d: got %.300s, want the Location refused", code, got.Inspect())
		}
	}
}
