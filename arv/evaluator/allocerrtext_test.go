package evaluator

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	goruntime "runtime"
	"strings"
	"testing"

	"github.com/delavalom/arvlang/arv/runtime"
	"github.com/delavalom/arvlang/arv/value"
)

// Error values are not charged, so a builtin must not copy or quote an
// argument of any size into one. With a 1 MB argument of control bytes
// (each quoted as 4 bytes), 200 tasks under alloc_mb: 1 used to allocate
// 5.3 GiB (http_get), 3.4 GiB (read_file) and 0.6 GiB (getenv).
func TestErrorTextStaysBounded(t *testing.T) {
	ctl := strings.Repeat("\x01", 1000000)
	for _, tt := range []struct{ call, msg string }{
		{"http_get(s)", "http_get: URL is 1000000 bytes long (max 8192)"},
		{"read_file(s)", "read_file: fs_read: scope is 1000000 bytes long (max 4096)"},
		{"write_file(s, \"x\")", "write_file: fs_write: scope is 1000000 bytes long (max 4096)"},
		{"getenv(s)", "getenv: env: scope is 1000000 bytes long (max 4096)"},
		// The constructors check the length before absPath copies a
		// relative path.
		{"fs_read(s)", "fs_read: scope is 1000000 bytes long (max 4096)"},
		{"fs_write(s)", "fs_write: scope is 1000000 bytes long (max 4096)"},
	} {
		t.Run(tt.call, func(t *testing.T) {
			got, _ := runWith(t, `let s = "`+ctl+`"; `+tt.call)
			if e, ok := got.(*value.Error); !ok || e.Message != tt.msg {
				t.Fatalf("got %.200q, want %q", got.Inspect(), tt.msg)
			}
			src := `let s = "` + ctl + `"; let f = fn() { sleep(300); ` + tt.call + ` }; scope (alloc_mb: 1) { for i in ` + zeros(200) + ` { spawn f() with [] } }`
			before := goruntime.NumGoroutine()
			used := heapDuring(func() { got, _ = runWith(t, src) })
			wantErrorHas(t, got, tt.msg)
			settled(t, before)
			if used > 32<<20 {
				t.Errorf("200 tasks calling %s allocated %d MiB, want under 32 MiB", tt.call, used>>20)
			}
		})
	}
}

// An invalid URL under the length cap is quoted, but only its start.
func TestInvalidURLErrorIsClipped(t *testing.T) {
	got, _ := runWith(t, `http_get("ftp://`+strings.Repeat("\x01", 8000)+`")`)
	e, ok := got.(*value.Error)
	if !ok || !strings.HasPrefix(e.Message, `http_get: invalid url "ftp://\x01`) || !strings.HasSuffix(e.Message, `... (8006 bytes)`) {
		t.Fatalf("got %.300q, want a clipped invalid url error", got.Inspect())
	}
	if len(e.Message) > 1200 {
		t.Errorf("error is %d bytes long, want at most 1200", len(e.Message))
	}
}

// A URL at the length cap still works.
func TestHTTPGetAcceptsLongURLUnderCap(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, len(r.URL.Path))
	}))
	defer srv.Close()
	u := srv.URL + "/" + strings.Repeat("a", maxURLLen-len(srv.URL)-1)
	got, _ := runWith(t, fmt.Sprintf(`http_get(%q)["body"]`, u), grant(t, runtime.Net, "127.0.0.1"))
	if want := fmt.Sprint(maxURLLen - len(srv.URL)); got.Inspect() != want {
		t.Errorf("got %.200s, want %s", got.Inspect(), want)
	}
}
