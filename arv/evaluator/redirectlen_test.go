package evaluator

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/delavalom/arvlang/arv/runtime"
	"github.com/delavalom/arvlang/arv/value"
)

// http_get refuses a redirect to a URL longer than maxURLLen, as it
// refuses such a URL from the program, and an error never quotes a whole
// URL. A granted server sending a 60,000-byte Location of 0xff bytes made
// it follow 10 hops, each re-escaping the URL to three times its size, and
// fail with a 240 KB "too many redirects" error quoting it. A Location
// over maxURLLen is now refused before it is parsed (locationCap, see
// TestHTTPGetUnparseableLongLocationIsRefused), so this one is under the
// cap and only its escaped form (3 bytes per 0xff) is over it.
func TestHTTPGetRefusesLongRedirectURL(t *testing.T) {
	long := "/" + strings.Repeat("\xff", 3000)
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Location", long)
		w.WriteHeader(http.StatusFound)
	}))
	defer srv.Close()
	src := `http_get("` + srv.URL + `/start")`
	got, _ := runWith(t, src, grant(t, runtime.Net, "127.0.0.1"))
	e, ok := got.(*value.Error)
	if !ok || !strings.Contains(e.Message, "redirect URL is") || e.Pos != "1:1" {
		t.Fatalf("got %.300s, want a refused redirect at 1:1", got.Inspect())
	}
	if n := len(e.Inspect()); n > 2048 {
		t.Errorf("the error is %d bytes long", n)
	}
	if n := hits.Load(); n != 1 {
		t.Errorf("the server got %d requests, want 1 (the redirect not followed)", n)
	}
}

// A redirect to a URL under the cap is still followed, and the error for
// too many of them quotes the URL only up to clipLen bytes.
func TestHTTPGetRedirectErrorIsClipped(t *testing.T) {
	long := "/" + strings.Repeat("b", 2000)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", long)
		w.WriteHeader(http.StatusFound)
	}))
	defer srv.Close()
	got, _ := runWith(t, `http_get("`+srv.URL+`/start")`, grant(t, runtime.Net, "127.0.0.1"))
	e, ok := got.(*value.Error)
	if !ok || !strings.Contains(e.Message, "too many redirects") {
		t.Fatalf("got %.300s, want too many redirects", got.Inspect())
	}
	if n := len(e.Message); n > clipLen+200 {
		t.Errorf("the error is %d bytes long: %.300s", n, e.Message)
	}
}
