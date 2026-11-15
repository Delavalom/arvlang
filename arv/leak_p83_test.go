package arv

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"testing"
	"time"

	"github.com/delavalom/arvlang/arv/runtime"
)

// Leak rule for the Phase 8.3 paths: each http_get builds its own transport
// with a checking dialer, and the fs effects open an os.Root. No goroutine
// (transport loops, dials, timers) may outlive arv.Run, whether the call
// succeeds, is refused by name, port or dial-time address check, follows a
// redirect, or is cancelled mid-request.
func TestP83NoGoroutineOutlivesRun(t *testing.T) {
	release := make(chan struct{})
	mux := http.NewServeMux()
	mux.HandleFunc("/ok", func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "pong") })
	mux.HandleFunc("/hang", func(w http.ResponseWriter, r *http.Request) {
		select { // hold the request until the client gives up or the test ends
		case <-r.Context().Done():
		case <-release:
		}
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	defer close(release)
	port := srv.URL[strings.LastIndex(srv.URL, ":")+1:]
	redir := httptest.NewServer(http.RedirectHandler("http://localhost:"+port+"/ok", http.StatusFound))
	defer redir.Close()
	rPort := redir.URL[strings.LastIndex(redir.URL, ":")+1:]
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "g", "a"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "g", "a", "f"), []byte("inside"), 0o644); err != nil {
		t.Fatal(err)
	}
	caps := func(t *testing.T, specs ...string) []runtime.Capability {
		var out []runtime.Capability
		for _, s := range specs {
			c, err := runtime.Parse(s, dir)
			if err != nil {
				t.Fatal(err)
			}
			out = append(out, c)
		}
		return out
	}
	ip, name := "http://127.0.0.1:"+port, "http://localhost:"+port
	get := func(u string) string { return fmt.Sprintf(`fn() { http_get(%q)["status"] }`, u) }
	runs := []struct {
		name, src string
		caps      []string
		errHas    string // "" means the run succeeds
		timeout   time.Duration
	}{
		{"parallel fetches", fmt.Sprintf(`let f = %s; let n = [net("127.0.0.1")];
			let a = spawn f() with n; let b = spawn f() with n; let c = spawn f() with n; [await a, await b, await c]`, get(ip+"/ok")), []string{"net=127.0.0.1"}, "", 0},
		{"refused by port", fmt.Sprintf(`(%s)()`, get(ip+"/ok")), []string{"net=127.0.0.1:1"}, "capability denied", 0},
		{"refused at the dial in a child", fmt.Sprintf(`let f = %s; await spawn f() with [net("localhost")]`, get(name+"/ok")),
			[]string{"net=localhost", "net=127.0.0.1"}, "reached a loopback address", 0},
		{"redirect hop refused at the dial", fmt.Sprintf(`(%s)()`, get(redir.URL)),
			[]string{"net=127.0.0.1:" + rPort, "net=localhost"}, "reached a loopback address", 0},
		{"redirect hop allowed", fmt.Sprintf(`(%s)()`, get(redir.URL)), []string{"net=127.0.0.1", "net=localhost"}, "", 0},
		{"cancelled mid-request", fmt.Sprintf(`let f = %s; let n = [net("127.0.0.1")];
			let a = spawn f() with n; let b = spawn f() with n; await a`, get(ip+"/hang")),
			[]string{"net=127.0.0.1"}, "context", 300 * time.Millisecond},
		{"fs reads through os.Root in children", `let r = fn() { read_file("` + filepath.Join(dir, "g", "a", "f") + `") };
			let a = spawn r() with [fs_read("` + filepath.Join(dir, "g", "a") + `")]; let b = spawn r() with [fs_read("` + filepath.Join(dir, "g") + `")]; [await a, await b]`,
			[]string{"fs_read=g", "fs_write=g/a"}, "", 0},
	}
	// Warm up once so lazily started runtime goroutines are in the baseline.
	if _, err := Run(fmt.Sprintf(`(%s)()`, get(ip+"/ok")), Options{Caps: caps(t, "net=127.0.0.1")}); err != nil {
		t.Fatal(err)
	}
	before := settle(0)
	for _, r := range runs {
		ctx := context.Background()
		if r.timeout > 0 {
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(ctx, r.timeout)
			defer cancel()
		}
		_, err := Run(r.src, Options{Ctx: ctx, Caps: caps(t, r.caps...)})
		switch {
		case r.errHas == "" && err != nil:
			t.Errorf("%s: %v", r.name, err)
		case r.errHas != "" && (err == nil || !strings.Contains(err.Error(), r.errHas)):
			t.Errorf("%s: err = %v, want one containing %q", r.name, err, r.errHas)
		}
	}
	if after := settle(before); after > before {
		buf := make([]byte, 1<<20)
		t.Fatalf("%d goroutines before the runs, %d after:\n%s", before, after, buf[:goruntime.Stack(buf, true)])
	}
}

// settle waits (generously: the machine may be heavily loaded) until at
// most want goroutines run, and returns the count. want 0 means "until the
// count stops falling", for taking a baseline.
func settle(want int) int {
	deadline := time.Now().Add(30 * time.Second)
	n := goruntime.NumGoroutine()
	for time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
		m := goruntime.NumGoroutine()
		if (want > 0 && m <= want) || (want == 0 && m == n) {
			return m
		}
		n = m
	}
	return n
}
