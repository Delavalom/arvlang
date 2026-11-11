package evaluator

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/delavalom/arvlang/arv/runtime"
	"github.com/delavalom/arvlang/arv/value"
)

// refusedURL is an http URL on 127.0.0.1 where nothing listens.
func refusedURL(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	l.Close()
	return "http://" + addr + "/"
}

// Every effect decides its error's kind where the error is made: a
// missing grant is capability, a failure in the world is effect, a spent
// budget is budget, and a bad argument is runtime.
func TestEffectErrorKinds(t *testing.T) {
	big := strings.Repeat("x", 2<<20)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, big)
	}))
	defer srv.Close()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "big.txt"), []byte(big), 0o644); err != nil {
		t.Fatal(err)
	}
	lo := grant(t, runtime.Net, "127.0.0.1")
	rd := grant(t, runtime.FSRead, dir)
	wr := grant(t, runtime.FSWrite, dir)
	q := func(s string) string { return fmt.Sprintf("%q", s) }

	for _, tt := range []struct {
		name, src, kind string
		caps            []runtime.Capability
	}{
		{"http_get without a grant", `http_get(` + q(srv.URL) + `)`, runtime.KindCapability, nil},
		{"http_get refused", `http_get(` + q(refusedURL(t)) + `)`, runtime.KindEffect, []runtime.Capability{lo}},
		{"http_get bad url", `http_get("ftp://x")`, runtime.KindRuntime, nil},
		{"http_get over alloc_mb", `scope (alloc_mb: 1) { http_get(` + q(srv.URL) + `) }`, runtime.KindBudget, []runtime.Capability{lo}},
		{"read_file without a grant", `read_file(` + q(filepath.Join(dir, "big.txt")) + `)`, runtime.KindCapability, nil},
		{"read_file missing", `read_file(` + q(filepath.Join(dir, "missing.txt")) + `)`, runtime.KindEffect, []runtime.Capability{rd}},
		{"read_file of a directory", `read_file(` + q(dir) + `)`, runtime.KindEffect, []runtime.Capability{rd}},
		{"read_file over alloc_mb", `scope (alloc_mb: 1) { read_file(` + q(filepath.Join(dir, "big.txt")) + `) }`, runtime.KindBudget, []runtime.Capability{rd}},
		{"read_file bad argument", `read_file(1)`, runtime.KindRuntime, nil},
		{"write_file without a grant", `write_file(` + q(filepath.Join(dir, "w.txt")) + `, "y")`, runtime.KindCapability, nil},
		{"write_file into a missing directory", `write_file(` + q(filepath.Join(dir, "no", "w.txt")) + `, "y")`, runtime.KindEffect, []runtime.Capability{wr}},
		{"write_file bad text", `write_file(` + q(filepath.Join(dir, "w.txt")) + `, 1)`, runtime.KindRuntime, []runtime.Capability{wr}},
		{"getenv without a grant", `getenv("ARV_KIND_X")`, runtime.KindCapability, nil},
		{"getenv bad argument", `getenv(1)`, runtime.KindRuntime, nil},
		{"sleep bad argument", `sleep(-1)`, runtime.KindRuntime, nil},
		{"net bad scope", `net("")`, runtime.KindRuntime, nil},
		{"fs_read bad scope", `fs_read("")`, runtime.KindRuntime, nil},
		{"puts over alloc_mb", `let d = fn(a, n) { if (n == 0) { a } else { d([a, a], n - 1) } };
			scope (alloc_mb: 1) { puts(d([1], 20)) }`, runtime.KindBudget, nil},
		{"push over alloc_mb", `let d = fn(a, n) { if (n == 0) { a } else { d(push(a, n), n - 1) } };
			scope (alloc_mb: 1) { d([], 2000) }`, runtime.KindBudget, nil},
		{"len bad argument", `len(1)`, runtime.KindRuntime, nil},
		{"child's denial, through await", `let f = fn() { getenv("ARV_KIND_X") }; let t = spawn f(); await t`, runtime.KindCapability, nil},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, _ := runWith(t, tt.src, tt.caps...)
			wantKind(t, got, tt.kind)
		})
	}
}

// An effect cut short because its task was cancelled is of kind
// cancelled, not effect: the world did not fail.
func TestCancelledEffectKinds(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	caps := runtime.NewCapSet(grant(t, runtime.Net, "127.0.0.1"))
	task := runtime.NewRoot(ctx, caps, io.Discard)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()

	wantKind(t, sleep(task, &value.Integer{Value: 5000}), runtime.KindCancelled)
	wantKind(t, httpGet(task, func(int64) error { return nil }, &value.String{Value: srv.URL}), runtime.KindCancelled)
}
