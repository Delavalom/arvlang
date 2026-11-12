package check

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/delavalom/arvlang/arv/runtime"
)

// settledGoroutines waits (generously: the machine is shared and loaded)
// for the goroutine count to come back to at most before, and returns the
// last count seen.
func settledGoroutines(before int) int {
	n := goruntime.NumGoroutine()
	for deadline := time.Now().Add(10 * time.Second); n > before && time.Now().Before(deadline); {
		time.Sleep(5 * time.Millisecond)
		n = goruntime.NumGoroutine()
	}
	return n
}

// autofsPath reports whether an absolute path lies where macOS mounts
// autofs maps that contact other hosts on lookup (/net, /Network). The
// header comparison lstats paths (16p81b: recorded limit), so the fuzzers
// skip those rather than let a fuzzed path make the automounter dial out.
func autofsPath(p string) bool {
	p = strings.ToLower(filepath.Clean(p))
	for _, root := range []string{"/net", "/network", "/system/volumes/data/net"} {
		if p == root || strings.HasPrefix(p, root+"/") {
			return true
		}
	}
	return false
}

// pathTouchesAutofs reports whether any path in the report or the header
// entries, resolved against cwd, lies under an autofs mount.
func pathTouchesAutofs(r *Report, entries []Entry, cwd string) bool {
	for _, s := range r.Sites {
		if (s.Kind == runtime.FSRead || s.Kind == runtime.FSWrite) && autofsPath(runtime.Resolve(cwd, s.Scope)) {
			return true
		}
	}
	for _, e := range entries {
		kind, scope, _ := strings.Cut(e.Text, "=")
		if (kind == string(runtime.FSRead) || kind == string(runtime.FSWrite)) && autofsPath(runtime.Resolve(cwd, scope)) {
			return true
		}
	}
	return false
}

// FuzzCheckNoEffects: arv check (Source, then the header comparison that
// arv check and run --manifest make) performs no effect on any program.
// "{{S}}" in the input stands for a local server's URL, so fuzzed http_get
// calls aim at a server that counts what it receives. After the check the
// server has seen nothing, the empty working directory is still empty,
// and no goroutine was left behind. It also checks two properties of the
// comparison: every miss is a site of the report, and the report's own
// needs, used as a header, cover every site.
func FuzzCheckNoEffects(f *testing.F) {
	var hits atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { hits.Add(1) }))
	f.Cleanup(srv.Close)
	for _, s := range []string{
		`http_get("{{S}}/x"); write_file("made.txt", "x"); puts(getenv("HOME")); read_file("in.txt");`,
		"// arv:allow fs_write=made.txt\n// arv:allow net=127.0.0.1\nwrite_file(\"made.txt\", \"x\");",
		`let u = "{{S}}" + "/y"; scope (timeout_ms: 1) { await spawn (fn() { http_get(u) })() with [net("127.0.0.1")] }`,
		`match scope (supervise: "collect") { spawn (fn() { write_file("a/../b", "x") })() with [fs_write("a")] } { ok(v) => v, err(e) => e }`,
		`scope (supervise: "restart", retries: 2, backoff_ms: 1) { await spawn (fn() { getenv("A") })() with [env("A")] }`,
		"// arv:allow fs_read=missing/../x\nread_file(\"missing/../x\");",
		"// arv:allow net=0.0.0.0:1\nhttp_get(\"http://0.0.0.0:1/\");",
		`let f = http_get; f("{{S}}");`,
		`for x in [1] { {"k": write_file("w" + ".txt", "v")} }; sleep(600000); let s = fn() { s() }; s();`,
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, src string) {
		src = strings.ReplaceAll(src, "{{S}}", srv.URL)
		dir := t.TempDir()
		t.Chdir(dir)
		before := goruntime.NumGoroutine()
		start := hits.Load()
		r, err := Source(src)
		entries, merr := Manifest(src)
		if err == nil && merr == nil && !pathTouchesAutofs(r, entries, dir) {
			if grants, gerr := Grants(entries, dir); gerr == nil {
				sites := map[string]bool{}
				for _, s := range r.Sites {
					sites[s.String()] = true
				}
				for _, m := range Outside(r, grants, dir) {
					if !sites[m.String()] {
						t.Fatalf("miss %v is not a site of %+v", m, r.Sites)
					}
				}
			}
			own := make([]Entry, len(r.Needs))
			for i, n := range r.Needs {
				own[i] = Entry{Line: i + 1, Text: n.String()}
			}
			// A need runtime.Parse refuses (missing/../x, 16p81c) is a
			// recorded limit: the run then refuses to start.
			if grants, gerr := Grants(own, dir); gerr == nil {
				if miss := Outside(r, grants, dir); len(miss) != 0 {
					t.Fatalf("the report's own needs %v do not cover %v", r.Needs, miss)
				}
			}
		}
		if n := hits.Load() - start; n != 0 {
			t.Fatalf("checking reached the server %d times", n)
		}
		if ents, err := os.ReadDir(dir); err != nil || len(ents) != 0 {
			t.Fatalf("checking changed the working directory: %v %v", ents, err)
		}
		if n := settledGoroutines(before); n > before {
			t.Fatalf("goroutines: %d before checking, %d after", before, n)
		}
	})
}
