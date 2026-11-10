package main

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func writeScript(t *testing.T, src string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "main.arv")
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestRunCLI(t *testing.T) {
	t.Setenv("ARV_GREETING", "hola")
	script := writeScript(t, "// greet\nputs(getenv(\"ARV_GREETING\"));")

	var out, errOut bytes.Buffer
	if code := runCLI([]string{"run", script}, &out, &errOut); code != 1 {
		t.Fatalf("without --allow: exit %d, want 1", code)
	}
	if !strings.Contains(errOut.String(), "capability denied") {
		t.Fatalf("stderr = %q", errOut.String())
	}

	out.Reset()
	errOut.Reset()
	if code := runCLI([]string{"run", "--allow", "env=ARV_GREETING", script}, &out, &errOut); code != 0 {
		t.Fatalf("with --allow: exit %d, stderr %q", code, errOut.String())
	}
	if out.String() != "hola\n" {
		t.Fatalf("stdout = %q", out.String())
	}
}

func TestRunCLIRejectsBadFlags(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := runCLI([]string{"run", "--allow", "disk=/", "x.arv"}, &out, &errOut); code != 2 {
		t.Fatalf("exit %d, want 2", code)
	}
	if code := runCLI([]string{"build"}, &out, &errOut); code != 2 {
		t.Fatalf("unknown command exit %d, want 2", code)
	}
}

func TestFanOutEndToEnd(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		time.Sleep(100 * time.Millisecond)
		fmt.Fprint(w, "ok")
	}))
	defer srv.Close()

	script := writeScript(t, fmt.Sprintf(`
		let n = 20;
		let check = fn(i) { http_get(%q)["status"] };
		let startAll = fn(i, ts) { if (i == n) { ts } else { startAll(i + 1, push(ts, spawn check(i) with [net("127.0.0.1")])) } };
		let awaitAll = fn(i, ts, out) { if (i == len(ts)) { out } else { awaitAll(i + 1, ts, push(out, await ts[i])) } };
		puts(len(scope { awaitAll(0, startAll(0, []), []) }));`, srv.URL))

	var out, errOut bytes.Buffer
	start := time.Now()
	if code := runCLI([]string{"run", "--allow", "net=127.0.0.1", script}, &out, &errOut); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	if out.String() != "20\n" || hits.Load() != 20 {
		t.Fatalf("out %q, hits %d", out.String(), hits.Load())
	}
	// 20 requests x 100ms each would take 2s sequentially.
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("fan-out took %s; requests were not concurrent", elapsed)
	}
}
