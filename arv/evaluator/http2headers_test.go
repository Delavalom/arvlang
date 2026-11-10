package evaluator

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	goruntime "runtime"
	"strings"
	"testing"

	"github.com/delavalom/arvlang/arv/runtime"
)

// Over HTTP/2 (TLS), maxHeaderBytes also caps the header list, through
// the transport's MaxResponseHeaderBytes: net/http's h2 client advertises
// it and refuses a larger list. The other header tests use plain
// HTTP/1.1, so this one pins the h2 path.
func TestHTTP2HeaderListIsCapped(t *testing.T) {
	big := strings.Repeat("a", 1<<20)
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/big" {
			w.Header().Set("X-Big", big)
		}
		w.Write([]byte("x"))
	}))
	srv.EnableHTTP2 = true
	srv.StartTLS()
	defer srv.Close()
	before := goruntime.NumGoroutine()
	task := runtime.NewRoot(context.Background(), runtime.NewCapSet(grant(t, runtime.Net, "127.0.0.1")), io.Discard)
	get := func(path string) (*http.Response, error) {
		tr := transportFor(task)
		tr.TLSClientConfig = srv.Client().Transport.(*http.Transport).TLSClientConfig.Clone()
		defer tr.CloseIdleConnections()
		resp, err := (&http.Client{Transport: tr}).Get(srv.URL + path)
		if err == nil {
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
		}
		return resp, err
	}
	resp, err := get("/small")
	if err != nil || resp.ProtoMajor != 2 {
		t.Fatalf("small response: %v, %v; want one over HTTP/2", resp, err)
	}
	if _, err := get("/big"); err == nil {
		t.Errorf("a 1 MiB header list over HTTP/2 was accepted, want it refused (cap %d bytes)", maxHeaderBytes)
	}
	srv.CloseClientConnections()
	settled(t, before)
}
